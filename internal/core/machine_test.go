package core

import (
	"testing"
	"time"

	"github.com/vg010623/meanwhile/internal/events"
)

var t0 = time.Date(2026, 10, 4, 10, 0, 0, 0, time.UTC)

func at(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }

func evt(sec int, kind events.Kind, session string, opts ...func(*events.Event)) events.Event {
	e := events.Event{ID: events.NewID(at(sec)), TS: at(sec), Kind: kind, SessionID: session}
	for _, o := range opts {
		o(&e)
	}
	return e
}

func reason(r string) func(*events.Event) { return func(e *events.Event) { e.EndReason = r } }

// run applies events and returns the closed waits in order.
func run(m *Machine, evs ...events.Event) (closed []Wait, all []Change) {
	for _, e := range evs {
		for _, c := range m.Apply(e) {
			all = append(all, c)
			if c.Kind == Closed {
				closed = append(closed, c.Wait)
			}
		}
	}
	return closed, all
}

func TestSimpleWait(t *testing.T) {
	m := NewMachine()
	closed, _ := run(m,
		evt(0, events.WaitStarted, "s"),
		evt(5, events.InputResolved, "s"), // a tool ran: heartbeat only
		evt(30, events.WaitEnded, "s", reason(events.EndCompleted)),
	)
	if len(closed) != 1 {
		t.Fatalf("closed %d", len(closed))
	}
	w := closed[0]
	if w.EndReason != events.EndCompleted || w.Working(at(999)) != 30*time.Second || w.LastEventAt != at(30) {
		t.Errorf("got %+v working=%s", w, w.Working(at(999)))
	}
	if len(m.Open) != 0 {
		t.Error("wait still open")
	}
}

func TestNeedsInputIsExcluded(t *testing.T) {
	m := NewMachine()
	closed, changes := run(m,
		evt(0, events.WaitStarted, "s"),
		evt(10, events.NeedsInput, "s"),
		evt(12, events.NeedsInput, "s"), // duplicate signal (Notification after PermissionRequest)
		evt(40, events.InputResolved, "s"),
		evt(50, events.WaitEnded, "s", reason(events.EndCompleted)),
	)
	kinds := []ChangeKind{}
	for _, c := range changes {
		kinds = append(kinds, c.Kind)
	}
	if len(kinds) != 4 || kinds[1] != Blocked || kinds[2] != Unblocked {
		t.Errorf("changes %v", kinds)
	}
	w := closed[0]
	if w.NeedsInput != 30*time.Second || w.Working(at(0)) != 20*time.Second {
		t.Errorf("needs_input=%s working=%s", w.NeedsInput, w.Working(at(0)))
	}
}

func TestEndWhileBlocked(t *testing.T) {
	m := NewMachine()
	closed, _ := run(m,
		evt(0, events.WaitStarted, "s"),
		evt(10, events.NeedsInput, "s"),
		evt(25, events.WaitEnded, "s", reason(events.EndCompleted)), // user denied; Claude stopped
	)
	if w := closed[0]; w.NeedsInput != 15*time.Second || w.Working(at(0)) != 10*time.Second {
		t.Errorf("got %+v", w)
	}
}

func TestOpenWaitWorkingExcludesCurrentBlock(t *testing.T) {
	m := NewMachine()
	run(m, evt(0, events.WaitStarted, "s"), evt(10, events.NeedsInput, "s"))
	if got := m.Open["s"].Working(at(100)); got != 10*time.Second {
		t.Errorf("working = %s", got)
	}
}

func TestInterruptThenNewPrompt(t *testing.T) {
	// Real case (Claude Code 2.1.296): last tool event 8 s in, Esc, next
	// prompt 4.5 minutes later. The interrupted wait must count ~8 s, not 4.5 min.
	m := NewMachine()
	closed, _ := run(m,
		evt(0, events.WaitStarted, "s"),
		evt(8, events.InputResolved, "s"),
		// user pressed Esc: no hook fires
		evt(270, events.WaitStarted, "s"),
		evt(285, events.WaitEnded, "s", reason(events.EndCompleted)),
	)
	if len(closed) != 2 || closed[0].EndReason != events.EndSuperseded {
		t.Fatalf("got %+v", closed)
	}
	if closed[0].EndedAt != at(8) || closed[0].Working(at(0)) != 8*time.Second {
		t.Errorf("superseded wait ended %s, working %s", closed[0].EndedAt.Sub(t0), closed[0].Working(at(0)))
	}
	if closed[1].Working(at(0)) != 15*time.Second {
		t.Errorf("new wait working %s", closed[1].Working(at(0)))
	}
}

func TestInterruptWithNoEventsEndsAtStart(t *testing.T) {
	m := NewMachine()
	closed, _ := run(m, evt(0, events.WaitStarted, "s"), evt(100, events.WaitStarted, "s"))
	if closed[0].EndedAt != at(0) {
		t.Errorf("ended %s", closed[0].EndedAt.Sub(t0))
	}
}

func TestInterruptThenIdleNotification(t *testing.T) {
	m := NewMachine()
	closed, _ := run(m,
		evt(0, events.WaitStarted, "s"),
		evt(20, events.InputResolved, "s"), // last tool before the interrupt
		evt(95, events.WaitEnded, "s", reason(events.EndIdle)),
	)
	w := closed[0]
	// idle_prompt comes ~60 s after Claude went quiet: end ≈ 35 s, not 95 s.
	if w.EndReason != events.EndIdle || w.EndedAt != at(35) {
		t.Errorf("ended %s (%s)", w.EndedAt.Sub(t0), w.EndReason)
	}
}

func TestIdleNotificationNeverBeforeLastEvent(t *testing.T) {
	m := NewMachine()
	closed, _ := run(m,
		evt(0, events.WaitStarted, "s"),
		evt(50, events.InputResolved, "s"),
		evt(70, events.WaitEnded, "s", reason(events.EndIdle)),
	)
	if closed[0].EndedAt != at(50) {
		t.Errorf("ended %s", closed[0].EndedAt.Sub(t0))
	}
}

func TestStrayEventsAreIgnored(t *testing.T) {
	m := NewMachine()
	_, changes := run(m,
		evt(0, events.WaitEnded, "s", reason(events.EndCompleted)),
		evt(1, events.NeedsInput, "s"),
		evt(2, events.InputResolved, "s"),
		evt(3, events.WaitEnded, "s", reason(events.EndIdle)),
		evt(4, events.SessionEnded, "s"),
	)
	if len(changes) != 0 || len(m.Open) != 0 {
		t.Errorf("changes %v", changes)
	}
}

func TestSessionEndClosesWait(t *testing.T) {
	m := NewMachine()
	closed, _ := run(m, evt(0, events.WaitStarted, "s"), evt(9, events.SessionEnded, "s"))
	if closed[0].EndReason != events.EndSession {
		t.Errorf("got %+v", closed[0])
	}
}

func TestTwoSessionsAreIndependent(t *testing.T) {
	m := NewMachine()
	closed, _ := run(m,
		evt(0, events.WaitStarted, "a"),
		evt(5, events.WaitStarted, "b"),
		evt(6, events.NeedsInput, "b"),
		evt(10, events.WaitEnded, "a", reason(events.EndCompleted)),
		evt(20, events.InputResolved, "b"),
		evt(30, events.WaitEnded, "b", reason(events.EndCompleted)),
	)
	if len(closed) != 2 || closed[0].SessionID != "a" || closed[0].Working(at(0)) != 10*time.Second ||
		closed[1].Working(at(0)) != 11*time.Second {
		t.Fatalf("got %+v", closed)
	}
}

func TestQuickSuccession(t *testing.T) {
	m := NewMachine()
	closed, _ := run(m,
		evt(0, events.WaitStarted, "s"),
		evt(2, events.WaitEnded, "s", reason(events.EndCompleted)),
		evt(3, events.WaitStarted, "s"),
		evt(4, events.WaitEnded, "s", reason(events.EndCompleted)),
		evt(5, events.WaitStarted, "s"),
	)
	if len(closed) != 2 || len(m.Open) != 1 {
		t.Errorf("closed=%d open=%d", len(closed), len(m.Open))
	}
}

func TestCloseStale(t *testing.T) {
	m := NewMachine()
	run(m, evt(0, events.WaitStarted, "old"), evt(100, events.WaitStarted, "new"))
	changes := m.CloseStale(at(0).Add(31*time.Minute), 30*time.Minute)
	if len(changes) != 1 || changes[0].Wait.SessionID != "old" || changes[0].Wait.EndedAt != at(0) {
		t.Fatalf("got %+v", changes)
	}
	if _, ok := m.Open["new"]; !ok {
		t.Error("recent wait closed")
	}
}

func TestClampsAbsurdDurations(t *testing.T) {
	w := Wait{StartedAt: at(0), EndedAt: at(0).Add(48 * time.Hour)}
	if w.Working(at(0)) != 24*time.Hour {
		t.Error("not clamped to 24h")
	}
	w = Wait{StartedAt: at(10), EndedAt: at(0)}
	if w.Working(at(0)) != 0 {
		t.Error("negative duration")
	}
}

func TestHeadlessFlagCarried(t *testing.T) {
	m := NewMachine()
	_, changes := run(m, evt(0, events.WaitStarted, "s", func(e *events.Event) { e.Headless = true }))
	if !changes[0].Wait.Headless {
		t.Error("headless lost")
	}
}
