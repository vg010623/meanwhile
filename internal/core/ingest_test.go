package core

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/vg010623/meanwhile/internal/events"
	"github.com/vg010623/meanwhile/internal/spool"
	"github.com/vg010623/meanwhile/internal/store"
)

type fixture struct {
	db  *store.DB
	in  *Ingestor
	dir string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	dir := t.TempDir()
	db, _, err := store.Open(filepath.Join(dir, "state.db"), Migrations, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return fixture{db: db, dir: dir, in: &Ingestor{DB: db, SpoolDir: filepath.Join(dir, "spool"), LockPath: filepath.Join(dir, "ingest.lock")}}
}

func (f fixture) write(t *testing.T, evs ...events.Event) {
	t.Helper()
	for _, e := range evs {
		if err := spool.Append(f.in.SpoolDir, e); err != nil {
			t.Fatal(err)
		}
	}
}

func TestIngestAcrossSyncs(t *testing.T) {
	f := newFixture(t)
	f.write(t, evt(0, events.WaitStarted, "s"))
	changes, err := f.in.Sync(at(1))
	if err != nil || len(changes) != 1 || changes[0].Kind != Opened {
		t.Fatalf("sync 1: %v %+v", err, changes)
	}
	open, _ := OpenWaits(f.db)
	if len(open) != 1 {
		t.Fatalf("open waits %d", len(open))
	}

	// The machine state survives between syncs via the database.
	f.write(t, evt(10, events.NeedsInput, "s"), evt(20, events.InputResolved, "s"), evt(40, events.WaitEnded, "s", reason(events.EndCompleted)))
	changes, err = f.in.Sync(at(41))
	if err != nil || len(changes) != 3 {
		t.Fatalf("sync 2: %v %+v", err, changes)
	}
	closed := changes[2].Wait
	if closed.NeedsInput != 10*time.Second || closed.Working(at(0)) != 30*time.Second {
		t.Errorf("closed %+v", closed)
	}

	// Nothing new: nothing changes.
	if changes, _ := f.in.Sync(at(50)); len(changes) != 0 {
		t.Errorf("sync 3: %+v", changes)
	}

	s, err := ComputeWaitStats(f.db, at(60))
	if err != nil || s.Count != 1 || s.Total != 30*time.Second || s.Blocked != 10*time.Second {
		t.Errorf("stats %+v %v", s, err)
	}
}

func TestReingestIsIdempotent(t *testing.T) {
	f := newFixture(t)
	f.write(t, evt(0, events.WaitStarted, "s"), evt(5, events.WaitEnded, "s", reason(events.EndCompleted)))
	if _, err := f.in.Sync(at(6)); err != nil {
		t.Fatal(err)
	}
	// Simulate lost offsets (e.g. a crash before they were saved).
	f.db.Exec(`DELETE FROM ingest_offsets`)
	changes, err := f.in.Sync(at(7))
	if err != nil || len(changes) != 0 {
		t.Errorf("re-ingest produced %+v %v", changes, err)
	}
	var n int
	f.db.QueryRow(`SELECT COUNT(*) FROM waits`).Scan(&n)
	if n != 1 {
		t.Errorf("waits = %d", n)
	}
}

func TestConcurrentSyncs(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 20; i++ {
		f.write(t, evt(i*10, events.WaitStarted, "s"), evt(i*10+5, events.WaitEnded, "s", reason(events.EndCompleted)))
	}
	// Two ingestors (like the UI and `meanwhile stats`) on separate connections.
	db2, _, err := store.Open(f.db.Path, Migrations, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	in2 := &Ingestor{DB: db2, SpoolDir: f.in.SpoolDir, LockPath: f.in.LockPath}

	var wg sync.WaitGroup
	for _, in := range []*Ingestor{f.in, in2} {
		wg.Add(1)
		go func(in *Ingestor) {
			defer wg.Done()
			if _, err := in.Sync(at(1000)); err != nil {
				t.Error(err)
			}
		}(in)
	}
	wg.Wait()
	s, _ := ComputeWaitStats(f.db, at(1000))
	if s.Count != 20 {
		t.Errorf("waits = %d, want 20", s.Count)
	}
}

func TestStaleWaitClosedOnSync(t *testing.T) {
	f := newFixture(t)
	f.write(t, evt(0, events.WaitStarted, "s"))
	changes, _ := f.in.Sync(at(0).Add(StaleAfter + time.Minute))
	if len(changes) != 2 || changes[1].Wait.EndReason != events.EndStale {
		t.Fatalf("got %+v", changes)
	}
}

func TestHeartbeatPersistsLastEvent(t *testing.T) {
	f := newFixture(t)
	f.write(t, evt(0, events.WaitStarted, "s"))
	f.in.Sync(at(1))
	f.write(t, evt(600, events.InputResolved, "s"))
	f.in.Sync(at(601))
	open, _ := OpenWaits(f.db)
	if len(open) != 1 || open[0].LastEventAt != at(600) {
		t.Fatalf("got %+v", open)
	}
	// Not stale 29 minutes after the heartbeat, even though the wait is older.
	if changes, _ := f.in.Sync(at(600).Add(29 * time.Minute)); len(changes) != 0 {
		t.Errorf("closed too early: %+v", changes)
	}
}

func TestManualCloseAndShown(t *testing.T) {
	f := newFixture(t)
	f.write(t, evt(0, events.WaitStarted, "s"))
	changes, _ := f.in.Sync(at(1))
	if err := MarkShown(f.db, changes[0].Wait.ID, "learn"); err != nil {
		t.Fatal(err)
	}
	f.write(t, evt(20, events.InputResolved, "s")) // last sign of Claude
	f.in.Sync(at(21))
	closed, err := f.in.CloseManual("s", at(150))
	if err != nil || len(closed) != 1 || closed[0].Wait.EndReason != events.EndManual {
		t.Fatalf("got %+v %v", closed, err)
	}
	s, _ := ComputeWaitStats(f.db, at(60))
	// Ends at the last event (20 s), not at the key press (150 s).
	if s.Shown != 1 || s.Total != 20*time.Second {
		t.Errorf("stats %+v", s)
	}
}

func TestHeadlessExcludedFromStats(t *testing.T) {
	f := newFixture(t)
	f.write(t,
		evt(0, events.WaitStarted, "ci", func(e *events.Event) { e.Headless = true }),
		evt(9, events.WaitEnded, "ci", reason(events.EndCompleted)),
	)
	f.in.Sync(at(10))
	if s, _ := ComputeWaitStats(f.db, at(10)); s.Count != 0 {
		t.Errorf("headless counted: %+v", s)
	}
}

func TestFormatDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		42 * time.Second:               "42s",
		3*time.Minute + 5*time.Second:  "3m 05s",
		72*time.Minute + 9*time.Second: "1h 12m",
		0:                              "0s",
	} {
		if got := FormatDuration(d); got != want {
			t.Errorf("%s: got %q want %q", d, got, want)
		}
	}
}

func TestOldEventsPruned(t *testing.T) {
	f := newFixture(t)
	f.write(t, evt(0, events.WaitStarted, "s"), evt(5, events.WaitEnded, "s", reason(events.EndCompleted)))
	f.in.Sync(at(10))
	f.in.Sync(at(0).Add(eventRetention + time.Hour))
	var evs, waits int
	f.db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&evs)
	f.db.QueryRow(`SELECT COUNT(*) FROM waits`).Scan(&waits)
	if evs != 0 || waits != 1 {
		t.Errorf("events=%d waits=%d", evs, waits)
	}
}

func TestStopAfterManualCloseCorrectsIt(t *testing.T) {
	// Real case (2.1.296): x pressed while Claude was still running; its
	// Stop arrived 10 s later. The wait must end at the Stop, as completed.
	f := newFixture(t)
	f.write(t, evt(0, events.WaitStarted, "s"), evt(3, events.InputResolved, "s"))
	f.in.Sync(at(4))
	f.in.CloseManual("s", at(5))
	f.write(t, evt(9, events.InputResolved, "s"), evt(15, events.WaitEnded, "s", reason(events.EndCompleted)))
	changes, err := f.in.Sync(at(16))
	if err != nil || len(changes) != 1 || changes[0].Kind != Corrected {
		t.Fatalf("changes %+v %v", changes, err)
	}
	s, _ := ComputeWaitStats(f.db, at(20))
	var reason string
	f.db.QueryRow(`SELECT end_reason FROM waits`).Scan(&reason)
	if reason != events.EndCompleted || s.Total != 15*time.Second {
		t.Errorf("reason=%q total=%s", reason, s.Total)
	}
}
