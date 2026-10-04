// Package activity defines what a meanwhile activity must provide. The
// learning curriculum is one activity; a future "predict which files Claude
// will change" game would be another. Activities never see Claude hooks:
// the shell tells them about waits through this interface.
package activity

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vg010623/meanwhile/internal/config"
	"github.com/vg010623/meanwhile/internal/paths"
	"github.com/vg010623/meanwhile/internal/store"
)

// WaitInfo describes the wait an activity is running in.
type WaitInfo struct {
	WaitID       string
	SessionID    string
	CWD          string
	WorktreeRoot string // the git checkout Claude is working in, for activities that look at diffs
	StartedAt    time.Time
}

// Context is what an activity gets at startup.
type Context struct {
	DB     *store.DB // shared database; an activity uses only its own tables
	Config config.Config
	Paths  paths.Paths
	Now    func() time.Time
}

// Activity is registered once and creates a View each time the UI wakes.
type Activity interface {
	ID() string   // short, stable, used as the table prefix (e.g. "learn")
	Name() string // shown to the user
	// Migrations create the activity's own tables. The core schema never
	// changes when an activity is added.
	Migrations() []store.Migration
	Init(ctx Context) error
	// Start is called when the UI wakes for a wait and no view is open.
	Start(w WaitInfo) View
	Close() error
}

// View is the activity's part of the screen while the UI is awake. The
// shell owns the banners ("Claude finished", "Claude needs you") and the
// global keys (skip, quit); the view owns everything in between.
type View interface {
	Init() tea.Cmd
	Update(msg tea.Msg) tea.Cmd
	Render(width, height int) string
	// KeyHelp is a short line of the view's own keys, shown in the footer.
	KeyHelp() string
	// Capturing is true while the view needs every key (typing a question),
	// so the shell must not treat "s" or "q" as skip or quit.
	Capturing() bool

	// Lifecycle, called by the shell:
	WaitStarted(w WaitInfo)     // a new wait began while this view was still open
	WaitPaused()                // Claude needs the user: not teaching time
	WaitResumed()               // Claude is working again
	WaitEnded(endReason string) // Claude stopped; the user may finish what they're reading
	Save() error                // persist progress; called on close and on quit
}

// DoneMsg is sent by a view when the user has finished with it. The shell
// closes the view and goes back to sleep.
type DoneMsg struct{}

// Done is a tea.Cmd that sends DoneMsg.
func Done() tea.Msg { return DoneMsg{} }
