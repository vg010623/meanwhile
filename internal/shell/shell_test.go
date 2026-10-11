package shell

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vg010623/meanwhile/internal/activity"
	"github.com/vg010623/meanwhile/internal/config"
	"github.com/vg010623/meanwhile/internal/core"
	"github.com/vg010623/meanwhile/internal/events"
	"github.com/vg010623/meanwhile/internal/spool"
	"github.com/vg010623/meanwhile/internal/store"
)

// fakeActivity records what the shell asked it to do.
type fakeActivity struct {
	starts int
	view   *fakeView
}

func (a *fakeActivity) ID() string                    { return "fake" }
func (a *fakeActivity) Name() string                  { return "Fake" }
func (a *fakeActivity) Migrations() []store.Migration { return nil }
func (a *fakeActivity) Init(activity.Context) error   { return nil }
func (a *fakeActivity) Close() error                  { return nil }
func (a *fakeActivity) Start(activity.WaitInfo) activity.View {
	a.starts++
	a.view = &fakeView{}
	return a.view
}

type fakeView struct {
	calls     []string
	keys      []string
	capturing bool
	saved     int
}

func (v *fakeView) Init() tea.Cmd { return nil }
func (v *fakeView) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyMsg); ok {
		v.keys = append(v.keys, k.String())
	}
	return nil
}
func (v *fakeView) Render(w, h int) string        { return "CARD CONTENT" }
func (v *fakeView) KeyHelp() string               { return "enter next" }
func (v *fakeView) Capturing() bool               { return v.capturing }
func (v *fakeView) WaitStarted(activity.WaitInfo) { v.calls = append(v.calls, "started") }
func (v *fakeView) WaitPaused()                   { v.calls = append(v.calls, "paused") }
func (v *fakeView) WaitResumed()                  { v.calls = append(v.calls, "resumed") }
func (v *fakeView) WaitEnded(r string)            { v.calls = append(v.calls, "ended:"+r) }
func (v *fakeView) Save() error                   { v.saved++; return nil }

type harness struct {
	t     *testing.T
	m     *Model
	act   *fakeActivity
	spool string
	now   time.Time
	bells int
	in    *core.Ingestor
	db    *store.DB
}

var t0 = time.Date(2026, 10, 4, 10, 0, 0, 0, time.UTC)

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	db, _, err := store.Open(filepath.Join(dir, "state.db"), core.Migrations, t0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	h := &harness{t: t, act: &fakeActivity{}, spool: filepath.Join(dir, "spool"), now: t0, db: db}
	h.in = &core.Ingestor{DB: db, SpoolDir: h.spool, LockPath: filepath.Join(dir, "ingest.lock")}
	cfg := config.Default()
	h.m = New(Options{
		DB: db, Ingestor: h.in, Activity: h.act, Config: cfg, SpoolDir: h.spool,
		Now:  func() time.Time { return h.now },
		Bell: func() tea.Cmd { h.bells++; return nil },
	})
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.sync() // the initial sync
	return h
}

// event writes a Claude event at second sec and syncs, like the watcher would.
func (h *harness) event(sec int, kind events.Kind, opts ...func(*events.Event)) {
	h.t.Helper()
	h.now = t0.Add(time.Duration(sec) * time.Second)
	e := events.Event{V: 1, ID: events.NewID(h.now), TS: h.now, Kind: kind, SessionID: "s1", CWD: "/work/myapp", WorktreeRoot: "/work/myapp"}
	for _, o := range opts {
		o(&e)
	}
	if err := spool.Append(h.spool, e); err != nil {
		h.t.Fatal(err)
	}
	h.sync()
}

func (h *harness) sync() { h.m.Update(h.m.syncCmd(true)()) }

func (h *harness) tick(sec int) {
	h.now = t0.Add(time.Duration(sec) * time.Second)
	h.m.Update(tickMsg(h.now))
}

func (h *harness) key(k string) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	if k == "enter" {
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	}
	h.m.Update(msg)
}

func (h *harness) screen() string { return h.m.View() }

func ended(r string) func(*events.Event) { return func(e *events.Event) { e.EndReason = r } }

func TestShortWaitNeverShows(t *testing.T) {
	h := newHarness(t)
	h.event(0, events.WaitStarted)
	h.tick(2)
	h.event(3, events.WaitEnded, ended(events.EndCompleted))
	h.tick(10)
	if h.act.starts != 0 || h.m.phase != asleep {
		t.Errorf("starts=%d phase=%d", h.act.starts, h.m.phase)
	}
}

func TestFullWaitLifecycle(t *testing.T) {
	h := newHarness(t)
	h.event(0, events.WaitStarted)
	if h.m.phase != pending || !strings.Contains(h.screen(), "Claude is working") {
		t.Fatalf("not pending:\n%s", h.screen())
	}
	h.tick(5) // past the 4 s grace delay
	if h.act.starts != 1 || h.m.phase != awake {
		t.Fatalf("did not wake: starts=%d phase=%d", h.act.starts, h.m.phase)
	}
	if s := h.screen(); !strings.Contains(s, "CARD CONTENT") || !strings.Contains(s, "myapp") {
		t.Errorf("awake screen:\n%s", s)
	}

	// Claude asks for permission: lesson hidden, bell rings.
	h.event(10, events.NeedsInput, func(e *events.Event) { e.InputKind = events.InputPermission })
	s := h.screen()
	if !strings.Contains(s, "Claude needs your input") || strings.Contains(s, "CARD CONTENT") || h.bells != 1 {
		t.Errorf("blocked screen (bells=%d):\n%s", h.bells, s)
	}
	h.key("n") // keys don't reach the hidden card
	if len(h.act.view.keys) != 0 {
		t.Error("key reached the view while blocked")
	}

	h.event(20, events.InputResolved)
	if !strings.Contains(h.screen(), "CARD CONTENT") {
		t.Error("card not back after input resolved")
	}

	h.event(30, events.WaitEnded, ended(events.EndCompleted))
	s = h.screen()
	if !strings.Contains(s, "Claude finished") || !strings.Contains(s, "CARD CONTENT") || !strings.Contains(s, "20s") {
		t.Errorf("finished screen (want 20s working: 30s minus 10s blocked):\n%s", s)
	}
	want := []string{"paused", "resumed", "ended:completed"}
	if strings.Join(h.act.view.calls, ",") != strings.Join(want, ",") {
		t.Errorf("view calls %v", h.act.view.calls)
	}

	view := h.act.view
	h.key("x")
	if h.m.phase != asleep || h.m.view != nil || view.saved == 0 {
		t.Errorf("x did not close: phase=%d saved=%d", h.m.phase, view.saved)
	}

	var shown int
	h.db.QueryRow(`SELECT shown FROM waits`).Scan(&shown)
	if shown != 1 {
		t.Error("wait not marked shown")
	}
}

func TestSkipStaysAsleepForThisWait(t *testing.T) {
	h := newHarness(t)
	h.event(0, events.WaitStarted)
	h.tick(5)
	h.key("s")
	h.tick(8)
	if h.m.phase != asleep || h.act.starts != 1 {
		t.Fatalf("skip failed: phase=%d starts=%d", h.m.phase, h.act.starts)
	}
	h.event(20, events.WaitEnded, ended(events.EndCompleted))
	h.event(25, events.WaitStarted)
	h.tick(30)
	if h.act.starts != 2 || h.m.phase != awake {
		t.Errorf("next wait did not wake: starts=%d", h.act.starts)
	}
}

func TestQuickSuccessionKeepsView(t *testing.T) {
	h := newHarness(t)
	h.event(0, events.WaitStarted)
	h.tick(5)
	h.event(10, events.WaitEnded, ended(events.EndCompleted))
	h.event(12, events.WaitStarted) // user still reading; Claude off again
	if h.act.starts != 1 || h.m.phase != awake || h.m.status != working {
		t.Errorf("starts=%d phase=%d status=%d", h.act.starts, h.m.phase, h.m.status)
	}
	if got := strings.Join(h.act.view.calls, ","); got != "ended:completed,started" {
		t.Errorf("calls %s", got)
	}
}

func TestManualDone(t *testing.T) {
	h := newHarness(t)
	h.event(0, events.WaitStarted)
	h.tick(5)
	h.tick(60) // Esc was pressed in Claude: no event arrives
	h.key("x")
	if h.m.phase != asleep {
		t.Error("not asleep")
	}
	var reason string
	h.db.QueryRow(`SELECT end_reason FROM waits`).Scan(&reason)
	if reason != events.EndManual {
		t.Errorf("end_reason %q", reason)
	}
}

func TestStillWorkingPrompt(t *testing.T) {
	// Esc in Claude fires no hook (verified on 2.1.296), so after the default
	// 2 minutes of silence the header asks.
	h := newHarness(t)
	h.event(0, events.WaitStarted)
	h.tick(5)
	h.tick(110)
	if strings.Contains(h.screen(), "Still working?") {
		t.Errorf("prompt too early:\n%s", h.screen())
	}
	h.tick(130)
	if !strings.Contains(h.screen(), "Still working?") {
		t.Errorf("no prompt after 2 min:\n%s", h.screen())
	}
}

func TestBlockedScreenQuietHint(t *testing.T) {
	// Esc on a permission dialog fires no hook (verified on 2.1.296): the
	// blocked screen must offer a way out.
	h := newHarness(t)
	h.event(0, events.WaitStarted)
	h.tick(5)
	h.event(10, events.NeedsInput, func(e *events.Event) { e.InputKind = events.InputPermission })
	h.tick(60)
	if strings.Contains(h.screen(), "No sign of Claude") {
		t.Errorf("hint too early:\n%s", h.screen())
	}
	h.tick(10 + 121)
	if !strings.Contains(h.screen(), "Dealt with it? Press x.") {
		t.Fatalf("no hint:\n%s", h.screen())
	}
	h.key("x")
	var reason string
	h.db.QueryRow(`SELECT end_reason FROM waits`).Scan(&reason)
	if h.m.phase != asleep || reason != events.EndManual {
		t.Errorf("x did not close: phase=%d reason=%q", h.m.phase, reason)
	}
}

func TestQuickWaitWithOpenCardIsNotCountedShown(t *testing.T) {
	h := newHarness(t)
	h.event(0, events.WaitStarted)
	h.tick(5)
	h.event(10, events.WaitEnded, ended(events.EndCompleted))
	// The user leaves the card open; a 3-second wait comes and goes.
	h.event(20, events.WaitStarted)
	quickID := h.m.wait.ID
	h.tick(22)
	h.event(23, events.WaitEnded, ended(events.EndCompleted))
	// Then a long one passes the grace delay.
	h.event(30, events.WaitStarted)
	longID := h.m.wait.ID
	h.tick(32)
	shown := func(id string) (s int) {
		h.db.QueryRow(`SELECT shown FROM waits WHERE id = ?`, id).Scan(&s)
		return
	}
	if shown(longID) != 0 {
		t.Error("long wait marked shown before the grace delay")
	}
	h.tick(35)
	if shown(quickID) != 0 || shown(longID) != 1 {
		t.Errorf("quick shown=%d long shown=%d", shown(quickID), shown(longID))
	}
	if h.act.starts != 1 {
		t.Errorf("view restarted: %d", h.act.starts)
	}
}

func TestHeadlessAndIgnoredDoNotWake(t *testing.T) {
	h := newHarness(t)
	h.event(0, events.WaitStarted, func(e *events.Event) { e.Headless = true })
	h.tick(10)
	if h.act.starts != 0 {
		t.Error("headless woke the UI")
	}
	h.m.o.Config.IgnoreDirs = []string{"/work"}
	h.event(20, events.WaitStarted, func(e *events.Event) { e.SessionID = "s2" })
	h.tick(30)
	if h.act.starts != 0 {
		t.Error("ignored dir woke the UI")
	}
}

func TestOtherSessionsCounted(t *testing.T) {
	h := newHarness(t)
	h.event(0, events.WaitStarted, func(e *events.Event) { e.SessionID = "a" })
	h.event(1, events.WaitStarted, func(e *events.Event) { e.SessionID = "b" })
	h.tick(6)
	if h.m.wait.SessionID != "b" || !strings.Contains(h.screen(), "+1 other session") {
		t.Errorf("following %s:\n%s", h.m.wait.SessionID, h.screen())
	}
	// Session a finishing doesn't end b's lesson.
	h.event(8, events.WaitEnded, ended(events.EndCompleted), func(e *events.Event) { e.SessionID = "a" })
	if h.m.status != working {
		t.Error("other session's end affected the followed one")
	}
}

func TestStartupPicksUpRunningWait(t *testing.T) {
	h := newHarness(t)
	h.event(0, events.WaitStarted)
	// A second UI starting now (e.g. after a crash) should follow it.
	m2 := New(h.m.o)
	m2.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.now = t0.Add(6 * time.Second)
	m2.Update(m2.syncCmd(true)())
	if m2.phase != awake || m2.wait.SessionID != "s1" {
		t.Errorf("phase=%d wait=%+v", m2.phase, m2.wait)
	}
}

func TestCapturingViewGetsKeys(t *testing.T) {
	h := newHarness(t)
	h.event(0, events.WaitStarted)
	h.tick(5)
	h.act.view.capturing = true
	h.key("q")
	h.key("s")
	if h.m.view == nil || len(h.act.view.keys) != 2 {
		t.Errorf("keys %v", h.act.view.keys)
	}
}

func TestLessonOnDemand(t *testing.T) {
	h := newHarness(t)
	h.key("enter")
	if h.act.starts != 1 || !strings.Contains(h.screen(), "no Claude run in progress") {
		t.Errorf("screen:\n%s", h.screen())
	}
	h.m.Update(activity.DoneMsg{})
	if h.m.view != nil || h.m.phase != asleep {
		t.Error("DoneMsg did not close the view")
	}
}

func TestCompactAndErrorBanner(t *testing.T) {
	h := newHarness(t)
	h.event(0, events.WaitStarted)
	h.tick(5)
	h.event(9, events.WaitEnded, ended(events.EndError), func(e *events.Event) { e.ErrorType = "rate_limit" })
	if !strings.Contains(h.screen(), "Claude stopped with an error (rate limit)") {
		t.Errorf("error banner:\n%s", h.screen())
	}
	h.m.Update(tea.WindowSizeMsg{Width: 30, Height: 5})
	if s := h.screen(); !strings.Contains(s, "Claude finished") || !strings.Contains(s, "enlarge") {
		t.Errorf("compact:\n%s", s)
	}
}
