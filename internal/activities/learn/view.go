package learn

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vg010623/meanwhile/internal/activity"
	"github.com/vg010623/meanwhile/internal/curriculum"
)

type screen int

const (
	scrHome     screen = iota // "Continue: Tokens (card 3 of 4)"
	scrPicker                 // up to five concepts you're ready for
	scrBrowse                 // every concept, grouped by track
	scrNote                   // "Builds on X (not done yet)": a soft warning, never a block
	scrCard                   // reading a card
	scrComplete               // finished a concept
	scrConsent                // one-time "Go deeper" notice and model choice
	scrDeeper                 // a streamed deeper explanation
	scrHistory                // saved deeper explanations for a concept
)

// pickerSize is how many recommendations the picker shows.
const pickerSize = 5

type browseRow struct {
	header string // track name, for header rows
	c      *curriculum.Concept
}

// view is one open lesson session. It implements activity.View.
type view struct {
	a    *Activity
	wait activity.WaitInfo

	screen screen
	back   screen // where Esc goes from scrNote
	prog   map[string]Progress
	errMsg string

	cont    *curriculum.Concept // the in-progress concept offered on the home screen
	contIdx int

	list   []*curriculum.Concept // picker rows
	rows   []browseRow           // browse rows
	cursor int

	concept *curriculum.Concept // concept being read
	card    int
	scroll  int

	noteFor  *curriculum.Concept
	unmet    []*curriculum.Concept
	unlocked []*curriculum.Concept
	known    bool // the completion screen is for "I know this", not for reading

	deep deepState // "Go deeper" (deeper_view.go)
}

func (v *view) Init() tea.Cmd   { return nil }
func (v *view) Capturing() bool { return v.screen == scrDeeper && v.deep.typing }
func (v *view) Save() error {
	if v.screen == scrDeeper && v.deep.streaming {
		v.closeDeeper() // keeps the partial answer
	}
	v.deep.cancel()
	return nil // progress is written as it happens
}
func (v *view) WaitStarted(w activity.WaitInfo) { v.wait = w }
func (v *view) WaitPaused()                     {}
func (v *view) WaitResumed()                    {}
func (v *view) WaitEnded(string)                {}

func (v *view) refresh() {
	prog, err := v.a.store.all()
	if err != nil {
		v.errMsg = "Couldn't load your progress: " + err.Error()
		prog = map[string]Progress{}
	}
	v.prog = prog
}

func (v *view) fail(err error) {
	if err != nil {
		v.errMsg = "Couldn't save your progress: " + err.Error()
	}
}

// home shows "Continue" when something is in progress, else the picker.
func (v *view) home() {
	v.refresh()
	v.cont = nil
	if p, ok, err := v.a.store.current(); err == nil && ok {
		if c, ok := v.a.cur.Concept(p.ConceptID); ok && len(c.Cards) > 0 {
			v.cont, v.contIdx = c, cardIndex(c, p)
		}
	}
	if v.cont == nil {
		v.picker()
		return
	}
	v.screen = scrHome
}

func (v *view) picker() {
	v.refresh()
	exclude := ""
	if v.cont != nil {
		exclude = v.cont.ID
	}
	v.list = v.a.graph.recommend(v.prog, exclude, pickerSize)
	v.cursor = min(v.cursor, max(len(v.list)-1, 0))
	v.screen = scrPicker
}

func (v *view) browse() {
	v.refresh()
	tracks, byTrack := v.a.graph.byTrack()
	v.rows = nil
	for _, t := range tracks {
		v.rows = append(v.rows, browseRow{header: t})
		for _, c := range byTrack[t] {
			v.rows = append(v.rows, browseRow{c: c})
		}
	}
	if v.screen != scrBrowse {
		v.cursor = v.nextRow(-1, 1)
	}
	v.screen = scrBrowse
}

// nextRow moves from i in direction dir to the next concept row.
func (v *view) nextRow(i, dir int) int {
	for j := i + dir; j >= 0 && j < len(v.rows); j += dir {
		if v.rows[j].c != nil {
			return j
		}
	}
	if i < 0 {
		return 0
	}
	return i
}

// open starts a concept, first showing a one-line note if its prerequisites
// aren't done. The note can always be dismissed with Enter.
func (v *view) open(c *curriculum.Concept, from screen) {
	if unmet := v.a.graph.unmet(c, v.prog); len(unmet) > 0 {
		v.noteFor, v.unmet, v.back = c, unmet, from
		v.screen = scrNote
		return
	}
	v.start(c)
}

func (v *view) start(c *curriculum.Concept) {
	v.concept = c
	v.card = 0
	if p, ok := v.prog[c.ID]; ok && p.Status == InProgress {
		v.card = cardIndex(c, p)
	}
	v.showCard()
}

func (v *view) showCard() {
	v.screen = scrCard
	v.scroll = 0
	card := v.concept.Cards[v.card]
	v.fail(v.a.store.setPosition(v.concept.ID, card.ID, v.card))
}

func (v *view) nextCard() {
	card := v.concept.Cards[v.card]
	v.fail(v.a.store.recordRead(v.concept.ID, card.ID, v.wait.WaitID))
	if v.card < len(v.concept.Cards)-1 {
		v.card++
		v.showCard()
		return
	}
	v.refresh()
	if !v.prog[v.concept.ID].Status.Done() {
		v.fail(v.a.store.setStatus(v.concept.ID, Completed))
	}
	v.finish(v.concept, false)
}

func (v *view) finish(c *curriculum.Concept, known bool) {
	v.refresh()
	v.concept = c
	v.known = known
	v.unlocked = v.a.graph.newlyUnlocked(c.ID, v.prog)
	v.screen = scrComplete
}

func (v *view) markKnown(c *curriculum.Concept) {
	v.fail(v.a.store.setStatus(c.ID, Known))
	v.refresh()
}

func (v *view) Update(msg tea.Msg) tea.Cmd {
	if cmd, handled := v.updateDeeper(msg); handled {
		return cmd
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	key := k.String()
	switch v.screen {
	case scrHome:
		switch key {
		case "enter":
			v.start(v.cont)
		case "p":
			v.picker()
		case "b":
			v.browse()
		case "i":
			v.markKnown(v.cont)
			v.home()
		}

	case scrPicker:
		switch key {
		case "up", "k":
			v.cursor = max(v.cursor-1, 0)
		case "down", "j":
			v.cursor = min(v.cursor+1, max(len(v.list)-1, 0))
		case "enter":
			if len(v.list) > 0 {
				v.open(v.list[v.cursor], scrPicker)
			}
		case "i":
			if len(v.list) > 0 {
				v.markKnown(v.list[v.cursor])
				v.picker()
			}
		case "b":
			v.browse()
		case "esc":
			if v.cont != nil {
				v.screen = scrHome
			}
		}

	case scrBrowse:
		switch key {
		case "up", "k":
			v.cursor = v.nextRow(v.cursor, -1)
		case "down", "j":
			v.cursor = v.nextRow(v.cursor, 1)
		case "enter":
			if c := v.rows[v.cursor].c; c != nil {
				v.open(c, scrBrowse)
			}
		case "i":
			if c := v.rows[v.cursor].c; c != nil {
				if v.prog[c.ID].Status == Known {
					v.fail(v.a.store.unmarkKnown(c.ID))
				} else {
					v.markKnown(c)
				}
				v.browse()
			}
		case "esc":
			v.picker()
		}

	case scrNote:
		switch key {
		case "enter":
			v.start(v.noteFor)
		case "esc":
			v.screen = v.back
		}

	case scrCard:
		switch key {
		case "enter", "right", "l", " ":
			v.nextCard()
		case "left", "h":
			if v.card > 0 {
				v.card--
				v.showCard()
			}
		case "down", "j":
			v.scroll++
		case "up", "k":
			v.scroll = max(v.scroll-1, 0)
		case "i":
			v.markKnown(v.concept)
			v.finish(v.concept, true)
		case "d":
			return v.openDeeper()
		case "D":
			v.openHistory()
		case "esc":
			v.home()
		}

	case scrComplete:
		switch key {
		case "enter":
			v.cont = nil
			v.picker()
		case "b":
			v.browse()
		}
	}
	return nil
}

func (v *view) KeyHelp() string {
	switch v.screen {
	case scrHome:
		return "enter continue · p pick another · b browse all"
	case scrPicker:
		return "↑↓ choose · enter start · i I know this · b browse all"
	case scrBrowse:
		return "↑↓ choose · enter open · i I know this (toggle) · esc back"
	case scrNote:
		return "enter start anyway · esc back"
	case scrCard:
		return "enter next · ← back · i I know this · d go deeper"
	case scrComplete:
		return "enter pick next · b browse all"
	default:
		return v.deeperKeyHelp()
	}
}

func (v *view) Render(width, height int) string {
	var top []string
	if v.a.notice != "" && (v.screen == scrHome || v.screen == scrPicker) {
		top = append(top, warn.Render(wrap(v.a.notice, textWidth(width), "")), "")
	}
	if v.errMsg != "" {
		top = append(top, warn.Render(wrap(v.errMsg, textWidth(width), "")), "")
	}
	var body string
	switch v.screen {
	case scrHome:
		body = v.renderHome(width)
	case scrPicker:
		body = v.renderPicker(width)
	case scrBrowse:
		body = v.renderBrowse(width, height-len(top))
	case scrNote:
		body = v.renderNote(width)
	case scrCard:
		body = v.renderCard(width, height-len(top))
	case scrComplete:
		body = v.renderComplete(width)
	default:
		body = v.renderDeeper(width, height-len(top))
	}
	return "\n" + strings.Join(append(top, body), "\n")
}

func (v *view) renderHome(width int) string {
	w := textWidth(width)
	c := v.cont
	recap := c.Summary
	if v.contIdx > 0 {
		recap = c.Cards[v.contIdx-1].Recap
	}
	lines := []string{
		selSty.Render("› Continue: "+c.Title) + dim.Render(fmt.Sprintf("  (card %d of %d)", v.contIdx+1, len(c.Cards))),
		dim.Render(wrap("Last time: "+recap, w, "  ")),
		"",
		dim.Render("  p  pick something else"),
	}
	return strings.Join(lines, "\n")
}

func (v *view) renderPicker(width int) string {
	w := textWidth(width)
	if len(v.list) == 0 {
		return wrap("You've covered everything that's ready right now. Press b to browse all concepts.", w, "")
	}
	lines := []string{bold.Render("Ready for you"), ""}
	for i, c := range v.list {
		marker, title := "  ", c.Title
		if i == v.cursor {
			marker, title = selSty.Render("› "), selSty.Render(c.Title)
		}
		if v.prog[c.ID].Status == InProgress {
			title += dim.Render("  (in progress)")
		}
		lines = append(lines, marker+title)
		lines = append(lines, dim.Render(wrap(c.Summary, w-2, "  ")))
	}
	return strings.Join(lines, "\n")
}

func (v *view) renderBrowse(width, height int) string {
	w := textWidth(width)
	var lines []string
	cursorLine := 0
	for i, r := range v.rows {
		if r.c == nil {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, bold.Render(strings.ToUpper(r.header)))
			continue
		}
		mark, tag := "·", ""
		switch {
		case v.prog[r.c.ID].Status == Completed:
			mark = "✓"
		case v.prog[r.c.ID].Status == Known:
			mark, tag = "✓", " (known)"
		case v.prog[r.c.ID].Status == InProgress:
			mark = "◐"
		case v.a.graph.ready(r.c, v.prog):
			mark = "○"
		}
		line := mark + " " + r.c.Title
		if i == v.cursor {
			line = selSty.Render("› " + line)
			cursorLine = len(lines)
		} else {
			line = "  " + line
		}
		lines = append(lines, truncateLine(line+dim.Render(tag), w))
	}
	lines = append(lines, "", dim.Render("✓ done  ◐ in progress  ○ ready  · builds on something not done yet"))
	return window(lines, cursorLine, height)
}

func (v *view) renderNote(width int) string {
	w := textWidth(width)
	names := make([]string, len(v.unmet))
	for i, c := range v.unmet {
		names[i] = c.Title
	}
	return strings.Join([]string{
		bold.Render(v.noteFor.Title),
		"",
		wrap("Builds on: "+strings.Join(names, ", ")+" (not done yet).", w, ""),
		dim.Render("Enter to start anyway, Esc to go back."),
	}, "\n")
}

func (v *view) renderCard(width, height int) string {
	w := textWidth(width)
	c := v.concept
	card := c.Cards[v.card]
	head := []string{
		accent.Render(c.Title) + dim.Render(fmt.Sprintf(" · card %d of %d", v.card+1, len(c.Cards))),
		"",
		bold.Render(wrap(card.Title, w, "")),
		"",
	}
	body := strings.Split(renderMarkdown(card.Body, width), "\n")
	room := max(height-len(head)-1, 3)
	if len(body) > room {
		v.scroll = min(v.scroll, len(body)-room)
		visible := body[v.scroll : v.scroll+room]
		more := ""
		if v.scroll+room < len(body) {
			more = dim.Render("↓ more (j/k to scroll)")
		}
		return strings.Join(append(append(head, visible...), more), "\n")
	}
	return strings.Join(append(head, body...), "\n")
}

func (v *view) renderComplete(width int) string {
	w := textWidth(width)
	title := "✓ " + v.concept.Title + " complete"
	if v.known {
		title = "✓ " + v.concept.Title + " marked as known"
	}
	lines := []string{selSty.Render(title), ""}
	if len(v.unlocked) > 0 {
		lines = append(lines, "Now ready:")
		for _, c := range v.unlocked {
			lines = append(lines, "  • "+c.Title)
		}
	} else {
		lines = append(lines, wrap("Nice. Pick what's next, or close and get back to Claude.", w, ""))
	}
	return strings.Join(lines, "\n")
}

// window shows the part of lines around focus that fits in height.
func window(lines []string, focus, height int) string {
	if height <= 0 || len(lines) <= height {
		return strings.Join(lines, "\n")
	}
	start := min(max(focus-height/2, 0), len(lines)-height)
	return strings.Join(lines[start:start+height], "\n")
}

func truncateLine(s string, width int) string {
	r := []rune(s)
	if len(r) <= width+20 { // styled text has escape codes; be generous
		return s
	}
	return string(r[:width]) + "…"
}
