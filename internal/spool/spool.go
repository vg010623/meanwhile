// Package spool is the append-only log between the hook and the app.
//
// There is one JSONL file per UTC day. The hook appends a single line with
// O_APPEND, which the OS applies atomically for small writes, so two Claude
// sessions writing at once never interleave. Daily files (instead of renaming
// a file to rotate it) mean a hook never writes into a file being rotated.
package spool

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vg010623/meanwhile/internal/events"
)

const suffix = ".jsonl"

// FileFor is the spool file name that an event at t belongs in.
func FileFor(t time.Time) string { return t.UTC().Format("2006-01-02") + suffix }

// Append writes e as one line to the day's file, creating the folder if needed.
func Append(dir string, e events.Event) error {
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, FileFor(e.TS)), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(line) // one write call, so the line lands whole
	cerr := f.Close()
	return errors.Join(werr, cerr)
}

// Offsets records how many bytes of each file have been read.
type Offsets map[string]int64

// ReadResult is what ReadNew found.
type ReadResult struct {
	Events  []events.Event
	Offsets Offsets // updated copy; persist it after the events are stored
	Skipped int     // malformed lines that were skipped
}

// ReadNew returns every complete line written since offsets, oldest file
// first. A trailing line without a newline is still being written, so it is
// left for the next call.
func ReadNew(dir string, offsets Offsets) (ReadResult, error) {
	res := ReadResult{Offsets: Offsets{}}
	for k, v := range offsets {
		res.Offsets[k] = v
	}
	files, err := list(dir)
	if err != nil {
		return res, err
	}
	for _, name := range files {
		start := res.Offsets[name]
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			return res, err
		}
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			f.Close()
			return res, err
		}
		r := bufio.NewReader(f)
		pos := start
		for {
			line, err := r.ReadBytes('\n')
			if err != nil { // io.EOF: partial or no line left
				break
			}
			pos += int64(len(line))
			var e events.Event
			if jerr := json.Unmarshal(bytes.TrimSpace(line), &e); jerr != nil || e.ID == "" {
				res.Skipped++
				continue
			}
			res.Events = append(res.Events, e)
		}
		f.Close()
		res.Offsets[name] = pos
	}
	// Order by time across files; equal timestamps keep their file order
	// (the order they were written in), which the random ID suffix would not.
	sort.SliceStable(res.Events, func(i, j int) bool { return res.Events[i].TS.Before(res.Events[j].TS) })
	return res, nil
}

// Prune deletes fully read files older than keep, and drops their offsets.
func Prune(dir string, offsets Offsets, keep time.Duration, now time.Time) error {
	files, err := list(dir)
	if err != nil {
		return err
	}
	cutoff := now.UTC().Add(-keep).Format("2006-01-02") + suffix
	for _, name := range files {
		if name >= cutoff {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || offsets[name] < info.Size() {
			continue // not fully read yet; keep it
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return err
		}
		delete(offsets, name)
	}
	return nil
}

// LastModified returns when the newest spool file was last written, or the
// zero time if there are none. Used by doctor to say "no events seen".
func LastModified(dir string) time.Time {
	files, _ := list(dir)
	if len(files) == 0 {
		return time.Time{}
	}
	info, err := os.Stat(filepath.Join(dir, files[len(files)-1]))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

func list(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), suffix) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // YYYY-MM-DD sorts chronologically
	return names, nil
}
