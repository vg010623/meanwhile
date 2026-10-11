// Package shell is the terminal app around activities. It watches the spool,
// follows Claude's state, decides when to wake up, and draws the banners
// ("Claude finished", "Claude needs you"). The activity draws the rest.
//
// Bubble Tea uses the Elm architecture: a Model holds all state, Update turns
// messages (key presses, timer ticks, new Claude events) into a new state,
// and View renders the state as a string. Nothing else changes the screen.
package shell

import (
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fsnotify/fsnotify"

	"github.com/vg010623/meanwhile/internal/activity"
	"github.com/vg010623/meanwhile/internal/config"
	"github.com/vg010623/meanwhile/internal/core"
	"github.com/vg010623/meanwhile/internal/events"
	"github.com/vg010623/meanwhile/internal/store"
)

// Options configure the shell.
type Options struct {
	DB       *store.DB
	Ingestor *core.Ingestor
	Activity activity.Activity
	Config   config.Config
	SpoolDir string
	Now      func() time.Time
	Notice   string         // shown once on the sleep screen (config error, database recovered…)
	Bell     func() tea.Cmd // rings the terminal bell; replaceable in tests
}

type phase int

const (
	asleep  phase = iota // quiet; nothing to show
	pending              // Claude is working but the grace delay hasn't passed
	awake                // the activity is on screen
)

type status int

const (
	idle     status = iota // no wait being followed
	working                // Claude is working
	blocked                // Claude needs the user
	finished               // the followed wait ended
)

// manualDoneGuard: for this long after a wait starts, x only closes the card
// instead of telling meanwhile that Claude is done.
const manualDoneGuard = 10 * time.Second

// liveWindow: on startup, an open wait counts as live only if Claude did
// something this recently. Older ones are probably interrupted runs.
const liveWindow = 10 * time.Minute

// Model is the shell's whole state.
type Model struct {
	o             Options
	width, height int
	phase         phase
	status        status
	wait          core.Wait // the followed wait: open, or the one that just ended
	view          activity.View
	skipped       string // ID of a wait the user skipped or closed
	shownID       string // ID of the last wait recorded as shown
	othersWorking int    // other sessions with Claude working
	othersBlocked int    // other sessions where Claude is waiting for the user
	today         core.WaitStats
	started       bool
	errMsg        string
	watch         chan struct{}
}

// Messages.
type (
	syncMsg struct {
		changes []core.Change
		open    []core.Wait
		stats   *core.WaitStats
		err     error
	}
	tickMsg  time.Time
	spoolMsg struct{}
)

// New builds the model. Call Run to start the terminal program.
func New(o Options) *Model {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Bell == nil {
		o.Bell = func() tea.Cmd {
			return func() tea.Msg { fmt.Fprint(os.Stderr, "\a"); return nil }
		}
	}
	return &Model{o: o, watch: make(chan struct{}, 1)}
}

// Run starts the program in the alternate screen and blocks until quit.
func Run(o Options) error {
	m := New(o)
	stop := m.startWatcher()
	defer stop()
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if m.view != nil {
		_ = m.view.Save()
	}
	return err
}

// startWatcher notifies the model when the spool folder changes, so new
// Claude events show up immediately instead of on the next tick.
func (m *Model) startWatcher() func() {
	_ = os.MkdirAll(m.o.SpoolDir, 0o700)
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return func() {}
	}
	if err := w.Add(m.o.SpoolDir); err != nil {
		w.Close()
		return func() {}
	}
	go func() {
		for {
			select {
			case _, ok := <-w.Events:
				if !ok {
					return
				}
				select {
				case m.watch <- struct{}{}:
				default: // a notification is already pending
				}
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return func() { w.Close() }
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.syncCmd(true), tick(), m.waitForSpool())
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) waitForSpool() tea.Cmd {
	return func() tea.Msg { <-m.watch; return spoolMsg{} }
}

// syncCmd ingests new events in the background and reports the result.
func (m *Model) syncCmd(withStats bool) tea.Cmd {
	return func() tea.Msg {
		now := m.o.Now()
		changes, err := m.o.Ingestor.Sync(now)
		if err != nil {
			return syncMsg{err: err}
		}
		open, err := core.OpenWaits(m.o.DB)
		msg := syncMsg{changes: changes, open: open, err: err}
		if withStats || hasClosed(changes) {
			if s, err := core.ComputeWaitStats(m.o.DB, now); err == nil {
				msg.stats = &s
			}
		}
		return msg
	}
}

func hasClosed(changes []core.Change) bool {
	for _, c := range changes {
		if c.Kind == core.Closed {
			return true
		}
	}
	return false
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		return m, tea.Batch(m.checkGrace(), m.syncCmd(false), tick())

	case spoolMsg:
		return m, tea.Batch(m.syncCmd(false), m.waitForSpool())

	case syncMsg:
		return m, m.applySync(msg)

	case activity.DoneMsg:
		m.closeView()
		return m, nil

	case tea.KeyMsg:
		return m, m.handleKey(msg)
	}

	// Anything else (e.g. streamed "Go deeper" text) belongs to the view.
	if m.view != nil {
		return m, m.view.Update(msg)
	}
	return m, nil
}

func (m *Model) applySync(msg syncMsg) tea.Cmd {
	if msg.err != nil {
		m.errMsg = "Couldn't read Claude events: " + msg.err.Error()
		return nil
	}
	m.errMsg = ""
	if msg.stats != nil {
		m.today = *msg.stats
	}
	var cmds []tea.Cmd
	if !m.started {
		// First sync after launch: pick up a run already in progress rather
		// than replaying old history as if it were happening now.
		m.started = true
		now := m.o.Now()
		for _, w := range msg.open {
			if m.relevant(w) && now.Sub(w.LastEventAt) < liveWindow {
				m.follow(w)
				break
			}
		}
	} else {
		for _, c := range msg.changes {
			if cmd := m.applyChange(c); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	m.othersWorking, m.othersBlocked = 0, 0
	for _, w := range msg.open {
		switch {
		case w.ID == m.wait.ID:
			// Heartbeats (tool calls) move LastEventAt without producing a
			// Change, so refresh the followed wait from the database. The
			// "still working?" nudge measures from LastEventAt.
			m.wait = w
		case !m.relevant(w):
		case w.State == core.NeedsInput:
			m.othersBlocked++
		default:
			m.othersWorking++
		}
	}
	cmds = append(cmds, m.checkGrace())
	return tea.Batch(cmds...)
}

// follow starts tracking a newly opened (or just discovered) wait.
func (m *Model) follow(w core.Wait) {
	m.wait = w
	m.status = working
	if w.State == core.NeedsInput {
		m.status = blocked
	}
	if m.view != nil {
		// The user is still reading from the last wait: keep their place.
		m.phase = awake
		m.view.WaitStarted(info(w))
		// Not marked shown yet: checkGrace does that once the wait passes
		// the grace delay, so quick answers don't count as shown.
		if m.status == blocked {
			m.view.WaitPaused()
		}
	} else {
		m.phase = pending
	}
}

func (m *Model) applyChange(c core.Change) tea.Cmd {
	w := c.Wait
	if !m.relevant(w) {
		return nil
	}
	if c.Kind == core.Opened {
		m.follow(w)
		return nil
	}
	if w.ID != m.wait.ID {
		return nil // another session; only counted in othersWorking/othersBlocked
	}
	m.wait = w
	switch c.Kind {
	case core.Blocked:
		m.status = blocked
		if m.phase == awake {
			m.view.WaitPaused()
			if m.o.Config.Bell != config.BellNever {
				return m.o.Bell()
			}
		}
	case core.Unblocked:
		m.status = working
		if m.phase == awake {
			m.view.WaitResumed()
		}
	case core.Closed:
		if w.EndReason == events.EndSuperseded {
			return nil // a new wait in this session follows right away
		}
		m.status = finished
		switch m.phase {
		case pending:
			m.phase = asleep // a quick answer: never shown
		case awake:
			m.view.WaitEnded(w.EndReason)
			if m.o.Config.Bell == config.BellAlways {
				return m.o.Bell()
			}
		}
	}
	return nil
}

// checkGrace wakes the UI once Claude has been working long enough, and
// records the wait as shown at that moment.
func (m *Model) checkGrace() tea.Cmd {
	if m.status != working || m.wait.ID == "" || m.wait.ID == m.skipped {
		return nil
	}
	if m.wait.Working(m.o.Now()) < time.Duration(m.o.Config.MinWaitSeconds)*time.Second {
		return nil
	}
	switch m.phase {
	case pending:
		return m.wake()
	case awake: // a card was already open from the previous wait
		m.markShown()
	}
	return nil
}

func (m *Model) wake() tea.Cmd {
	m.phase = awake
	m.markShown()
	if m.view != nil {
		return nil
	}
	m.view = m.o.Activity.Start(info(m.wait))
	return m.view.Init()
}

func (m *Model) markShown() {
	if m.wait.ID != "" && m.wait.ID != m.shownID {
		m.shownID = m.wait.ID
		_ = core.MarkShown(m.o.DB, m.wait.ID, m.o.Activity.ID())
	}
}

// closeView saves and closes the activity, and stays asleep for the rest of
// this wait.
func (m *Model) closeView() {
	if m.view != nil {
		_ = m.view.Save()
		m.view = nil
	}
	m.phase = asleep
	m.skipped = m.wait.ID
}

func (m *Model) handleKey(k tea.KeyMsg) tea.Cmd {
	if k.Type == tea.KeyCtrlC {
		return m.quit()
	}
	if m.view != nil && m.phase == awake && m.status != blocked && m.view.Capturing() {
		return m.view.Update(k)
	}
	switch k.String() {
	case "q":
		return m.quit()
	case "s":
		if m.phase != asleep {
			m.closeView() // skip this wait: no penalty, no record of "missed"
			return nil
		}
	case "x":
		switch m.status {
		case finished:
			m.closeView()
			return nil
		case working, blocked:
			if m.o.Now().Sub(m.wait.StartedAt) < manualDoneGuard {
				// A new run just started under an open card: the user was
				// most likely dismissing the last "Claude finished" banner.
				// Close the card, but don't end a run that just began.
				m.closeView()
				return nil
			}
			// An interrupt fires no hook, so the user can tell us directly.
			if m.wait.ID != "" && m.wait.EndedAt.IsZero() {
				changes, err := m.o.Ingestor.CloseManual(m.wait.SessionID, m.o.Now())
				if err != nil {
					m.errMsg = err.Error()
				}
				for _, c := range changes {
					m.applyChange(c)
				}
			}
			m.closeView()
			return nil
		}
	case "enter":
		if m.phase == asleep && m.view == nil {
			// Start a lesson on demand, outside any wait.
			m.view = m.o.Activity.Start(activity.WaitInfo{})
			m.phase = awake
			return m.view.Init()
		}
	}
	if m.view != nil && m.phase == awake && m.status != blocked {
		return m.view.Update(k)
	}
	return nil
}

func (m *Model) quit() tea.Cmd {
	if m.view != nil {
		_ = m.view.Save()
		m.view = nil
	}
	return tea.Quit
}

func (m *Model) relevant(w core.Wait) bool {
	return !w.Headless && !m.o.Config.Ignored(w.CWD)
}

func info(w core.Wait) activity.WaitInfo {
	return activity.WaitInfo{WaitID: w.ID, SessionID: w.SessionID, CWD: w.CWD, WorktreeRoot: w.WorktreeRoot, StartedAt: w.StartedAt}
}
