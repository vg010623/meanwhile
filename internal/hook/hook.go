// Package hook is what Claude Code runs at each hook event. It must be fast,
// must never print to stdout (for some events Claude adds stdout to the
// conversation), and must never fail in a way Claude can see. The caller
// always exits 0.
package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vg010623/meanwhile/internal/events"
	"github.com/vg010623/meanwhile/internal/spool"
)

// InternalEnv is set on our own headless Claude calls ("Go deeper") so their
// hooks, if any fire, are ignored instead of looking like a user wait.
const InternalEnv = "MEANWHILE_INTERNAL"

// maxPayload caps how much stdin we read; hook payloads are small.
const maxPayload = 1 << 20

// Run reads one hook payload from stdin and appends the matching event to the
// spool. It returns errors for logging only.
func Run(stdin io.Reader, spoolDir string, now time.Time) error {
	if os.Getenv(InternalEnv) == "1" {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(stdin, maxPayload))
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	var p events.HookPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	e, ok := events.FromHook(p, now)
	if !ok {
		return nil
	}
	if e.Kind == events.WaitStarted {
		e.Headless = IsHeadless()
	}
	return spool.Append(spoolDir, e)
}

// RunWithDeadline runs Run but gives up after d, so a stuck disk can never
// hold Claude up for longer than that.
func RunWithDeadline(stdin io.Reader, spoolDir string, now time.Time, d time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- Run(stdin, spoolDir, now) }()
	select {
	case err := <-done:
		return err
	case <-time.After(d):
		return fmt.Errorf("gave up after %s", d)
	}
}

// LogError appends a line to the hook error log, keeping the file small.
// Failures here are ignored: there is nowhere left to report them.
func LogError(path string, now time.Time, err error) {
	if err == nil {
		return
	}
	if info, statErr := os.Stat(path); statErr == nil && info.Size() > 256<<10 {
		_ = os.Remove(path)
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	f, ferr := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if ferr != nil {
		return
	}
	defer f.Close()
	msg := strings.ReplaceAll(err.Error(), "\n", " ")
	fmt.Fprintf(f, "%s %s\n", now.UTC().Format(time.RFC3339), msg)
}
