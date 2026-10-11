// Package core turns the stream of events into waits: the stretches of time
// when Claude was working and the user was waiting. It knows nothing about
// learning or the UI.
package core

import (
	"time"

	"github.com/vg010623/meanwhile/internal/events"
)

// State of an open wait.
type State string

const (
	Working    State = "working"     // Claude is working: teaching time
	NeedsInput State = "needs_input" // Claude is blocked on the user: not teaching time
	Ended      State = "ended"
)

// Wait is one prompt-to-finish stretch for one session.
type Wait struct {
	ID           string // the ID of the event that started it
	SessionID    string
	CWD          string
	WorktreeRoot string
	StartedAt    time.Time
	EndedAt      time.Time // zero while open
	EndReason    string
	ErrorType    string
	State        State
	InputSince   time.Time // when the current needs-input stretch began
	InputKind    string
	NeedsInput   time.Duration // total time blocked on the user
	LastEventAt  time.Time
	Headless     bool
	Shown        bool
	ClosedAt     time.Time // when the user pressed "Claude's done"; zero otherwise
}

// maxWait caps a single wait so clock jumps or missed end events can't
// produce absurd totals.
const maxWait = 24 * time.Hour

// Working is the time Claude spent working, excluding time blocked on the
// user. For an open wait it is measured up to now.
func (w Wait) Working(now time.Time) time.Duration {
	end := w.EndedAt
	if end.IsZero() {
		end = now
	}
	blocked := w.NeedsInput
	if w.State == NeedsInput && !w.InputSince.IsZero() {
		blocked += end.Sub(w.InputSince)
	}
	d := end.Sub(w.StartedAt) - blocked
	return min(max(d, 0), maxWait)
}

// ChangeKind says what happened to a wait.
type ChangeKind int

const (
	Opened    ChangeKind = iota // a wait started
	Blocked                     // Claude needs the user
	Unblocked                   // Claude is working again
	Closed                      // the wait ended (see Wait.EndReason)
	Corrected                   // an already-closed wait's end was fixed up (see Machine.RecentManual)
)

// Change is reported to whoever is watching (the UI).
type Change struct {
	Kind ChangeKind
	Wait Wait
}

// idleDelay is how long after going idle Claude sends idle_prompt. When that
// notification closes a wait, we date the end back by this much.
const idleDelay = 60 * time.Second

// Machine holds the open wait of each session and applies events to them.
// It is pure: no I/O, no clock. The ingestor loads open waits from the
// database, applies new events, and saves the result.
type Machine struct {
	Open map[string]*Wait // by session ID
	// RecentManual holds, per session, a wait the user closed with "Claude's
	// done" in the last few minutes. If Claude's Stop arrives after all, the
	// user was wrong (Claude was still running) and the wait is corrected.
	RecentManual map[string]*Wait
}

// manualCorrectionWindow is how long after a manual close a Stop still
// corrects it.
const manualCorrectionWindow = 5 * time.Minute

func NewMachine() *Machine {
	return &Machine{Open: map[string]*Wait{}, RecentManual: map[string]*Wait{}}
}

// Apply updates the open waits for one event and returns what changed.
func (m *Machine) Apply(e events.Event) []Change {
	w := m.Open[e.SessionID]
	var prevEvent time.Time // last thing we knew Claude did, before this event
	if w != nil {
		prevEvent = w.LastEventAt
		if e.TS.After(w.LastEventAt) {
			w.LastEventAt = e.TS
		}
	}

	switch e.Kind {
	case events.WaitStarted:
		delete(m.RecentManual, e.SessionID) // a newer wait: too late to correct the old one
		var changes []Change
		if w != nil {
			// The user sent a new prompt while the old wait was still open:
			// they must have interrupted Claude, which fires no hook. End it
			// at the last thing Claude did; the time since then was the user
			// reading and typing, not Claude working.
			changes = append(changes, m.close(w, prevEvent, events.EndSuperseded, ""))
		}
		nw := &Wait{
			ID: e.ID, SessionID: e.SessionID, CWD: e.CWD, WorktreeRoot: e.WorktreeRoot,
			StartedAt: e.TS, LastEventAt: e.TS, State: Working, Headless: e.Headless,
		}
		m.Open[e.SessionID] = nw
		return append(changes, Change{Opened, *nw})

	case events.NeedsInput:
		if w == nil || w.State == NeedsInput {
			return nil // nothing to block, or a duplicate signal
		}
		w.State, w.InputSince, w.InputKind = NeedsInput, e.TS, e.InputKind
		return []Change{{Blocked, *w}}

	case events.InputResolved:
		if w == nil || w.State != NeedsInput {
			return nil // just a heartbeat
		}
		w.NeedsInput += nonNegative(e.TS.Sub(w.InputSince))
		w.State, w.InputSince, w.InputKind = Working, time.Time{}, ""
		return []Change{{Unblocked, *w}}

	case events.WaitEnded:
		if w == nil {
			return m.correctManual(e)
		}
		end := e.TS
		if e.EndReason == events.EndIdle {
			// idle_prompt arrives about a minute after Claude went quiet.
			end = maxTime(prevEvent, e.TS.Add(-idleDelay))
		}
		return []Change{m.close(w, end, e.EndReason, e.ErrorType)}

	case events.SessionEnded:
		if w == nil {
			return nil
		}
		return []Change{m.close(w, e.TS, events.EndSession, "")}
	}
	return nil
}

// CloseStale closes open waits that have seen no event for longer than
// maxQuiet, dating their end to the last event. This covers an interrupt
// followed by the user walking away, or events lost while nothing ran.
func (m *Machine) CloseStale(now time.Time, maxQuiet time.Duration) []Change {
	var changes []Change
	for _, w := range m.Open {
		if now.Sub(w.LastEventAt) > maxQuiet {
			changes = append(changes, m.close(w, w.LastEventAt, events.EndStale, ""))
		}
	}
	return changes
}

// CloseManual closes a session's open wait because the user said Claude is
// done (the `x` key), for when an interrupt left it open.
//
// The end is dated to the last thing Claude did, not to the key press: after
// an Esc interrupt nothing happens until the user notices, and that idle
// time isn't Claude working. If Claude was in fact still running, its Stop
// will arrive soon and correctManual fixes the record.
func (m *Machine) CloseManual(sessionID string, now time.Time) []Change {
	w := m.Open[sessionID]
	if w == nil {
		return nil
	}
	w.ClosedAt = now
	c := m.close(w, w.LastEventAt, events.EndManual, "")
	closed := c.Wait
	m.RecentManual[sessionID] = &closed
	return []Change{c}
}

// correctManual handles a Stop (or StopFailure) for a session with no open
// wait: if the user closed its wait by hand moments ago, Claude was actually
// still working, so the wait gets Claude's real end time and reason.
func (m *Machine) correctManual(e events.Event) []Change {
	r := m.RecentManual[e.SessionID]
	if r == nil || (e.EndReason != events.EndCompleted && e.EndReason != events.EndError) {
		return nil
	}
	delete(m.RecentManual, e.SessionID)
	if e.TS.Sub(r.ClosedAt) > manualCorrectionWindow || e.TS.Before(r.EndedAt) {
		return nil
	}
	r.EndedAt, r.EndReason, r.ErrorType = e.TS, e.EndReason, e.ErrorType
	r.LastEventAt = e.TS
	return []Change{{Corrected, *r}}
}

func (m *Machine) close(w *Wait, end time.Time, reason, errType string) Change {
	if end.Before(w.StartedAt) {
		end = w.StartedAt
	}
	if w.State == NeedsInput && !w.InputSince.IsZero() {
		w.NeedsInput += nonNegative(end.Sub(w.InputSince))
	}
	w.EndedAt, w.EndReason, w.ErrorType = end, reason, errType
	w.State, w.InputSince, w.InputKind = Ended, time.Time{}, ""
	delete(m.Open, w.SessionID)
	return Change{Closed, *w}
}

func nonNegative(d time.Duration) time.Duration { return max(d, 0) }

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
