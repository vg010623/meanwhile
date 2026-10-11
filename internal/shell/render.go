package shell

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/vg010623/meanwhile/internal/core"
	"github.com/vg010623/meanwhile/internal/events"
)

// Colours adapt to light and dark terminals. Lip Gloss drops them entirely
// when NO_COLOR is set or the terminal has no colour support, so every
// banner also carries a symbol and words: colour is never the only signal.
var (
	dim      = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#6b7680", Dark: "#7d8b96"})
	bold     = lipgloss.NewStyle().Bold(true)
	okBanner = lipgloss.NewStyle().Bold(true).Padding(0, 1).
			Foreground(lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#0b1014"}).
			Background(lipgloss.AdaptiveColor{Light: "#2f7d4f", Dark: "#7fcf9a"})
	stopBanner = lipgloss.NewStyle().Bold(true).Padding(0, 1).
			Foreground(lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#0b1014"}).
			Background(lipgloss.AdaptiveColor{Light: "#5b6872", Dark: "#93a1ac"})
	errBanner = lipgloss.NewStyle().Bold(true).Padding(0, 1).
			Foreground(lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#0b1014"}).
			Background(lipgloss.AdaptiveColor{Light: "#a83a32", Dark: "#ef8a80"})
	warnText = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#9a6a00", Dark: "#f2c14e"})
	accent   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#b4580f", Dark: "#f0a35e"})
)

// Below this size the shell switches to a compact layout.
const (
	minWidth  = 40
	minHeight = 10
)

func (m *Model) View() string {
	if m.width == 0 {
		return "" // no size yet
	}
	if m.width < minWidth || m.height < minHeight {
		return m.compactView()
	}
	switch {
	case m.phase == awake && m.status == blocked:
		return m.blockedView()
	case m.phase == awake && m.view != nil:
		return m.awakeView()
	default:
		return m.sleepView()
	}
}

func (m *Model) sleepView() string {
	var top []string
	top = append(top, bold.Render("meanwhile")+dim.Render(" · waiting for Claude"))
	if m.phase == pending && m.status == working {
		top = append(top, dim.Render("● Claude is working…"))
	} else if m.status == finished && m.wait.ID != "" && m.wait.ID != m.skipped {
		top = append(top, dim.Render("Last run: "+m.endSummary()))
	}
	if m.o.Notice != "" {
		top = append(top, "", warnText.Render(m.o.Notice))
	}
	if m.errMsg != "" {
		top = append(top, "", warnText.Render(m.errMsg))
	}
	if m.today.Count == 0 && m.today.Shown == 0 && !m.started {
		top = append(top, "", dim.Render("Reading Claude events…"))
	}

	var bottom []string
	if m.today.TodayCount > 0 {
		bottom = append(bottom, dim.Render(fmt.Sprintf("today: %d waits · %s", m.today.TodayCount, core.FormatDuration(m.today.Today))))
	} else if m.started {
		bottom = append(bottom, dim.Render("No Claude runs yet today. Send Claude a prompt to begin."))
	}
	bottom = append(bottom, dim.Render("enter start a lesson now · q quit"))
	return m.stack(top, bottom)
}

func (m *Model) blockedView() string {
	kind := map[string]string{
		events.InputPermission:  "Claude is asking for permission",
		events.InputQuestion:    "Claude is asking you a question",
		events.InputElicitation: "A tool is asking for your input",
	}[m.wait.InputKind]
	if kind == "" {
		kind = "Claude is waiting for you"
	}
	lines := []string{
		warnText.Render("⚠  Claude needs your input"),
		"",
		kind + dim.Render(" · "+m.dirName()),
		"",
		"Switch to Claude. Your card will be here when you get back.",
	}
	// Rejecting a permission dialog with Esc fires no hook, so this screen
	// could otherwise stay up forever.
	if quiet := m.o.Now().Sub(m.wait.LastEventAt); !m.wait.LastEventAt.IsZero() && quiet > m.stillWorkingAfter() {
		lines = append(lines, "", warnText.Render(fmt.Sprintf("No sign of Claude for %s. Dealt with it? Press x.", core.FormatDuration(quiet))))
	}
	body := lipgloss.Place(m.width, m.height-1, lipgloss.Center, lipgloss.Center,
		lipgloss.NewStyle().Width(min(m.width-4, 60)).Align(lipgloss.Center).Render(strings.Join(lines, "\n")))
	return body + "\n" + dim.Render(m.footerKeys(""))
}

func (m *Model) awakeView() string {
	header := m.header()
	footer := m.footer()
	bodyHeight := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	body := m.view.Render(m.width, max(bodyHeight, 1))
	body = lipgloss.NewStyle().Height(max(bodyHeight, 1)).MaxHeight(max(bodyHeight, 1)).Render(body)
	return header + "\n" + body + "\n" + footer
}

func (m *Model) header() string {
	now := m.o.Now()
	var line string
	switch m.status {
	case finished:
		line = m.banner()
	case working:
		line = accent.Render("●") + dim.Render(fmt.Sprintf(" Claude is working · %s · %s", core.FormatDuration(m.wait.Working(now)), m.dirName()))
	default:
		line = dim.Render("Lesson time · no Claude run in progress")
	}
	if m.others > 0 {
		line += dim.Render(fmt.Sprintf(" · +%d other session working", m.others))
	}
	lines := []string{line}
	if m.status == working && !m.wait.LastEventAt.IsZero() {
		quiet := now.Sub(m.wait.LastEventAt)
		if quiet > m.stillWorkingAfter() {
			lines = append(lines, warnText.Render(fmt.Sprintf("No sign of Claude for %s. Still working? Press x if it's done.", core.FormatDuration(quiet))))
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

// banner is the full-width "Claude finished" line.
func (m *Model) banner() string {
	style := okBanner
	text := "✔ Claude finished · " + m.dirName() + " · " + core.FormatDuration(m.wait.Working(m.o.Now()))
	switch m.wait.EndReason {
	case events.EndError:
		style = errBanner
		text = "✖ Claude stopped with an error"
		if m.wait.ErrorType != "" {
			text += " (" + strings.ReplaceAll(m.wait.ErrorType, "_", " ") + ")"
		}
		text += " · " + m.dirName()
	case events.EndIdle, events.EndStale, events.EndManual, events.EndSession:
		style = stopBanner
		text = "■ Claude stopped · " + m.dirName() + " · " + core.FormatDuration(m.wait.Working(m.o.Now()))
	}
	return style.Width(m.width).Render(text)
}

func (m *Model) endSummary() string {
	return fmt.Sprintf("%s in %s", core.FormatDuration(m.wait.Working(m.o.Now())), m.dirName())
}

func (m *Model) footer() string {
	viewKeys := ""
	if m.view != nil {
		viewKeys = m.view.KeyHelp()
	}
	return dim.Render(m.footerKeys(viewKeys))
}

func (m *Model) footerKeys(viewKeys string) string {
	var parts []string
	if viewKeys != "" {
		parts = append(parts, viewKeys)
	}
	switch m.status {
	case finished:
		parts = append(parts, "x close")
	case working, blocked:
		parts = append(parts, "s skip", "x Claude's done")
	default:
		parts = append(parts, "s close")
	}
	parts = append(parts, "q quit")
	return truncate(strings.Join(parts, " · "), m.width)
}

func (m *Model) compactView() string {
	var line string
	switch {
	case m.status == blocked && m.phase != asleep:
		line = warnText.Render("⚠ Claude needs you")
	case m.status == finished && m.phase == awake:
		line = "✔ Claude finished"
	case m.phase == awake:
		line = "● Claude working · lesson open"
	case m.phase == pending:
		line = "● Claude working"
	default:
		line = "meanwhile · waiting"
	}
	out := truncate(line, m.width)
	if m.height > 1 {
		out += "\n" + dim.Render(truncate("enlarge this pane to read", m.width))
	}
	return out
}

// stack places top lines at the top and bottom lines at the bottom.
func (m *Model) stack(top, bottom []string) string {
	gap := m.height - len(top) - len(bottom)
	if gap < 1 {
		gap = 1
	}
	return strings.Join(top, "\n") + strings.Repeat("\n", gap) + strings.Join(bottom, "\n")
}

func (m *Model) dirName() string {
	dir := m.wait.WorktreeRoot
	if dir == "" {
		dir = m.wait.CWD
	}
	if dir == "" {
		return "Claude"
	}
	return filepath.Base(dir)
}

func truncate(s string, width int) string {
	if width <= 0 || lipgloss.Width(s) <= width {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r)) > width-1 {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

func (m *Model) stillWorkingAfter() time.Duration {
	return time.Duration(m.o.Config.StillWorkingMinutes) * time.Minute
}
