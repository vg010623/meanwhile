package spool

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/vg010623/meanwhile/internal/events"
)

func ev(t time.Time, kind events.Kind) events.Event {
	return events.Event{V: 1, ID: events.NewID(t), TS: t, Kind: kind, SessionID: "s1"}
}

func TestAppendAndReadNew(t *testing.T) {
	dir := t.TempDir()
	day1 := time.Date(2026, 10, 3, 23, 59, 0, 0, time.UTC)
	day2 := day1.Add(2 * time.Minute)
	for _, e := range []events.Event{ev(day1, events.WaitStarted), ev(day2, events.WaitEnded)} {
		if err := Append(dir, e); err != nil {
			t.Fatal(err)
		}
	}
	res, err := ReadNew(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 2 || res.Events[0].Kind != events.WaitStarted || len(res.Offsets) != 2 {
		t.Fatalf("got %+v", res)
	}
	// Nothing new on the second read.
	res2, _ := ReadNew(dir, res.Offsets)
	if len(res2.Events) != 0 {
		t.Errorf("re-read %d events", len(res2.Events))
	}
	// One more event shows up alone.
	_ = Append(dir, ev(day2.Add(time.Second), events.WaitStarted))
	res3, _ := ReadNew(dir, res2.Offsets)
	if len(res3.Events) != 1 {
		t.Errorf("got %d new events", len(res3.Events))
	}
}

func TestPartialAndMalformedLines(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 4, 10, 0, 0, 0, time.UTC)
	_ = Append(dir, ev(now, events.WaitStarted))
	f, _ := os.OpenFile(filepath.Join(dir, FileFor(now)), os.O_APPEND|os.O_WRONLY, 0)
	fmt.Fprint(f, "not json\n{\"v\":1,\"id\":\"partial")
	f.Close()

	res, err := ReadNew(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 1 || res.Skipped != 1 {
		t.Fatalf("events=%d skipped=%d", len(res.Events), res.Skipped)
	}
	// Finish the partial line; it is read next time.
	f, _ = os.OpenFile(filepath.Join(dir, FileFor(now)), os.O_APPEND|os.O_WRONLY, 0)
	fmt.Fprint(f, "\",\"kind\":\"wait_ended\",\"session_id\":\"s1\"}\n")
	f.Close()
	res2, _ := ReadNew(dir, res.Offsets)
	if len(res2.Events) != 1 || res2.Events[0].ID != "partial" {
		t.Fatalf("got %+v", res2.Events)
	}
}

func TestConcurrentAppendsStayWhole(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = Append(dir, ev(now, events.InputResolved))
		}()
	}
	wg.Wait()
	res, _ := ReadNew(dir, nil)
	if len(res.Events) != 50 || res.Skipped != 0 {
		t.Errorf("events=%d skipped=%d", len(res.Events), res.Skipped)
	}
}

func TestPrune(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -10)
	unread := now.AddDate(0, 0, -9)
	recent := now.AddDate(0, 0, -1)
	for _, ts := range []time.Time{old, unread, recent} {
		_ = Append(dir, ev(ts, events.WaitStarted))
	}
	res, _ := ReadNew(dir, nil)
	offsets := res.Offsets
	offsets[FileFor(unread)] = 0 // pretend this one was never read

	if err := Prune(dir, offsets, 7*24*time.Hour, now); err != nil {
		t.Fatal(err)
	}
	exists := func(ts time.Time) bool {
		_, err := os.Stat(filepath.Join(dir, FileFor(ts)))
		return err == nil
	}
	if exists(old) || !exists(unread) || !exists(recent) {
		t.Errorf("old=%v unread=%v recent=%v", exists(old), exists(unread), exists(recent))
	}
	if _, ok := offsets[FileFor(old)]; ok {
		t.Error("offset for pruned file kept")
	}
}

func TestReadMissingDir(t *testing.T) {
	res, err := ReadNew(filepath.Join(t.TempDir(), "nope"), nil)
	if err != nil || len(res.Events) != 0 {
		t.Errorf("got %v %v", res, err)
	}
}
