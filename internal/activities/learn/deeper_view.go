package learn

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/vg010623/meanwhile/internal/activity"
	"github.com/vg010623/meanwhile/internal/curriculum"
	"github.com/vg010623/meanwhile/internal/deeper"
)

// Deeper asks Claude for a deeper explanation; replaceable in tests.
type Deeper interface {
	Ask(ctx context.Context, req deeper.Request) <-chan deeper.Event
}

func newClaudeDeeper(ctx activity.Context) Deeper {
	return deeper.Runner{Dir: ctx.Paths.Sandbox()}
}

// maxFollowUps is how many follow-up questions one thread allows.
const maxFollowUps = 2

const (
	kvNamespace = "learn"
	kvConsent   = "deeper_consent"
	kvModel     = "deeper_model"
)

// models offered on the consent screen. "" means the user's Claude Code default.
var models = []struct{ id, label, note string }{
	{"", "Your Claude Code default", "whatever `claude` normally uses"},
	{"sonnet", "Sonnet", "strong and fast"},
	{"haiku", "Haiku", "fastest, uses the least of your limits"},
}

const systemPrompt = `You are a patient tutor inside "meanwhile", a terminal app that teaches AI concepts to software developers while they wait for their coding agent.

Explain from first principles, in plain language:
- Build on what the learner already knows; they are a developer, not an ML researcher.
- Explain why the idea exists and what problem it solves, not just what it is.
- Use one concrete analogy or example.
- No math notation and no code unless the learner asks for it.
- Keep it under about 250 words, in short paragraphs. Light Markdown only (bold, bullet lists).
You have no tools and cannot see any files. Never offer to run, open or look at anything.`

type turn struct {
	question string
	answer   strings.Builder
	status   string // complete | cancelled | error
}

// deepState is the "Go deeper" part of a view.
type deepState struct {
	gen       int // increases with each call so stale stream messages are ignored
	stop      context.CancelFunc
	stream    <-chan deeper.Event
	streaming bool
	retry     string
	err       *deeper.Error
	turns     []*turn
	rootID    int64 // database ID of the first answer in this thread
	model     string
	cardID    string
	typing    bool
	input     textinput.Model
	scroll    int
	readOnly  bool // viewing a saved thread

	history    []savedThread
	histCursor int
	consentSel int
}

type savedThread struct {
	id       int64
	cardID   string
	question string
	created  time.Time
}

// deeperMsg carries one streamed event to the view.
type deeperMsg struct {
	gen int
	ev  deeper.Event
	ok  bool // false when the stream has closed
}

func (d *deepState) cancel() {
	if d.stop != nil {
		d.stop()
		d.stop = nil
	}
}

func waitDeeper(gen int, ch <-chan deeper.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		return deeperMsg{gen: gen, ev: ev, ok: ok}
	}
}

// openDeeper starts "Go deeper" for the current card, asking for consent
// the first time.
func (v *view) openDeeper() tea.Cmd {
	if _, ok, _ := v.a.ctx.DB.GetKV(kvNamespace, kvConsent); !ok {
		v.deep.consentSel = 0
		v.screen = scrConsent
		return nil
	}
	v.deep = deepState{cardID: v.concept.Cards[v.card].ID, model: v.model()}
	return v.ask("Explain this card more deeply.")
}

func (v *view) model() string {
	if m := v.a.ctx.Config.Deeper.Model; m != "" {
		return m
	}
	m, _, _ := v.a.ctx.DB.GetKV(kvNamespace, kvModel)
	return m
}

// ask sends a question with the whole thread so far. Each call is a fresh,
// stateless `claude -p` run: re-sending the earlier turns costs a few
// hundred tokens but leaves nothing behind in the user's Claude sessions.
func (v *view) ask(question string) tea.Cmd {
	v.deep.cancel()
	ctx, stop := context.WithCancel(context.Background())
	v.deep.gen++
	v.deep.stop = stop
	v.deep.streaming = true
	v.deep.err = nil
	v.deep.retry = ""
	v.deep.scroll = 0
	req := deeper.Request{System: systemPrompt, Prompt: v.buildPrompt(question), Model: v.deep.model}
	v.deep.turns = append(v.deep.turns, &turn{question: question})
	v.deep.stream = v.a.deeper.Ask(ctx, req)
	v.screen = scrDeeper
	return waitDeeper(v.deep.gen, v.deep.stream)
}

func (v *view) buildPrompt(question string) string {
	c := v.concept
	card := c.Cards[v.card]
	var done, todo []string
	for _, other := range v.a.cur.Concepts {
		if other.ID == c.ID {
			continue
		}
		if v.prog[other.ID].Status.Done() {
			done = append(done, other.Title)
		} else if len(todo) < 15 {
			todo = append(todo, other.Title)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Concept: %s. %s\n\nThe card the learner is reading, \"%s\":\n%s\n\n", c.Title, c.Summary, card.Title, card.Body)
	if len(done) > 0 {
		fmt.Fprintf(&b, "Concepts the learner has already covered: %s.\n", strings.Join(done, ", "))
	} else {
		b.WriteString("The learner is just starting; assume no prior AI concepts.\n")
	}
	if len(todo) > 0 {
		fmt.Fprintf(&b, "Not covered yet (avoid relying on these): %s.\n", strings.Join(todo, ", "))
	}
	if len(v.deep.turns) > 0 {
		b.WriteString("\nEarlier in this conversation:\n")
		for _, t := range v.deep.turns {
			fmt.Fprintf(&b, "Learner: %s\nYou: %s\n", t.question, t.answer.String())
		}
	}
	fmt.Fprintf(&b, "\nLearner: %s", question)
	return b.String()
}

// updateDeeper handles messages for the deeper screens. handled is false
// when the message belongs to another screen.
func (v *view) updateDeeper(msg tea.Msg) (tea.Cmd, bool) {
	if m, ok := msg.(deeperMsg); ok {
		if m.gen != v.deep.gen {
			return nil, true // from a cancelled call
		}
		return v.onDeeperEvent(m), true
	}
	k, isKey := msg.(tea.KeyMsg)
	switch v.screen {
	case scrConsent:
		if !isKey {
			return nil, true
		}
		switch k.String() {
		case "up", "k":
			v.deep.consentSel = max(v.deep.consentSel-1, 0)
		case "down", "j":
			v.deep.consentSel = min(v.deep.consentSel+1, len(models)-1)
		case "1", "2", "3":
			v.deep.consentSel = int(k.String()[0] - '1')
			fallthrough
		case "enter", "y":
			_ = v.a.ctx.DB.SetKV(kvNamespace, kvModel, models[v.deep.consentSel].id)
			if err := v.a.ctx.DB.SetKV(kvNamespace, kvConsent, time.Now().UTC().Format(time.RFC3339)); err != nil {
				v.errMsg = err.Error()
			}
			return v.openDeeper(), true
		case "n", "esc":
			v.screen = scrCard
		}
		return nil, true

	case scrDeeper:
		if v.deep.typing {
			if isKey {
				switch k.Type {
				case tea.KeyEnter:
					q := strings.TrimSpace(v.deep.input.Value())
					v.deep.typing = false
					if q != "" {
						return v.ask(q), true
					}
					return nil, true
				case tea.KeyEsc:
					v.deep.typing = false
					return nil, true
				}
			}
			var cmd tea.Cmd
			v.deep.input, cmd = v.deep.input.Update(msg)
			return cmd, true
		}
		if !isKey {
			return nil, false
		}
		switch k.String() {
		case "esc":
			v.closeDeeper()
		case "f":
			if v.canFollowUp() {
				ti := textinput.New()
				ti.Placeholder = "Ask a follow-up question"
				ti.CharLimit = 300
				ti.Focus()
				v.deep.input = ti
				v.deep.typing = true
				return textinput.Blink, true
			}
		case "down", "j":
			v.deep.scroll++
		case "up", "k":
			v.deep.scroll = max(v.deep.scroll-1, 0)
		}
		return nil, true

	case scrHistory:
		if !isKey {
			return nil, true
		}
		switch k.String() {
		case "up", "k":
			v.deep.histCursor = max(v.deep.histCursor-1, 0)
		case "down", "j":
			v.deep.histCursor = min(v.deep.histCursor+1, max(len(v.deep.history)-1, 0))
		case "enter":
			if len(v.deep.history) > 0 {
				v.loadThread(v.deep.history[v.deep.histCursor].id)
			}
		case "esc":
			v.screen = scrCard
		}
		return nil, true
	}
	return nil, false
}

func (v *view) canFollowUp() bool {
	return !v.deep.readOnly && !v.deep.streaming && v.deep.err == nil && len(v.deep.turns) > 0 && len(v.deep.turns) <= maxFollowUps
}

func (v *view) onDeeperEvent(m deeperMsg) tea.Cmd {
	cur := v.deep.turns[len(v.deep.turns)-1]
	if !m.ok {
		v.deep.streaming = false
		return nil
	}
	switch m.ev.Kind {
	case deeper.Text:
		cur.answer.WriteString(m.ev.Text)
		v.deep.retry = ""
	case deeper.Retry:
		v.deep.retry = fmt.Sprintf("Claude is busy, retrying (%d/%d)…", m.ev.Attempt, m.ev.MaxAttempts)
	case deeper.Done:
		v.deep.streaming = false
		cur.status = "complete"
		v.saveTurn(cur)
	case deeper.Failed:
		v.deep.streaming = false
		v.deep.err = m.ev.Err
		if m.ev.Err != nil && m.ev.Err.Kind == deeper.ErrCancelled {
			cur.status = "cancelled"
			v.saveTurn(cur)
		}
	}
	return waitDeeper(m.gen, v.deep.stream)
}

// closeDeeper cancels a running call (keeping what arrived) and returns to the card.
func (v *view) closeDeeper() {
	if v.deep.streaming {
		v.deep.cancel()
		v.deep.streaming = false
		if cur := v.deep.turns[len(v.deep.turns)-1]; cur.answer.Len() > 0 {
			cur.status = "cancelled"
			v.saveTurn(cur)
		}
		v.deep.gen++ // ignore anything still in flight
	}
	if v.deep.readOnly {
		v.screen = scrHistory
		return
	}
	v.screen = scrCard
}

func (v *view) saveTurn(t *turn) {
	if t.answer.Len() == 0 || v.deep.readOnly {
		return
	}
	var parent any
	if v.deep.rootID != 0 {
		parent = v.deep.rootID
	}
	r, err := v.a.ctx.DB.Exec(`INSERT INTO learn_deep_dives (concept_id, card_id, parent_id, question, answer, status, model, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		v.concept.ID, v.deep.cardID, parent, t.question, t.answer.String(), t.status, v.deep.model, v.a.ctx.Now().UnixMilli())
	if err != nil {
		v.errMsg = "Couldn't save the explanation: " + err.Error()
		return
	}
	if v.deep.rootID == 0 {
		v.deep.rootID, _ = r.LastInsertId()
	}
}

// openHistory lists saved explanations for the current concept.
func (v *view) openHistory() {
	v.deep.history = nil
	v.deep.histCursor = 0
	rows, err := v.a.ctx.DB.Query(`SELECT id, card_id, question, created_at FROM learn_deep_dives
		WHERE concept_id = ? AND parent_id IS NULL ORDER BY created_at DESC`, v.concept.ID)
	if err != nil {
		v.errMsg = err.Error()
		return
	}
	defer rows.Close()
	for rows.Next() {
		var t savedThread
		var created int64
		if rows.Scan(&t.id, &t.cardID, &t.question, &created) == nil {
			t.created = time.UnixMilli(created)
			v.deep.history = append(v.deep.history, t)
		}
	}
	v.screen = scrHistory
}

func (v *view) loadThread(rootID int64) {
	rows, err := v.a.ctx.DB.Query(`SELECT question, answer, status FROM learn_deep_dives
		WHERE id = ? OR parent_id = ? ORDER BY id`, rootID, rootID)
	if err != nil {
		v.errMsg = err.Error()
		return
	}
	defer rows.Close()
	history := v.deep.history
	v.deep = deepState{readOnly: true, rootID: rootID, history: history, histCursor: v.deep.histCursor}
	for rows.Next() {
		var q, a string
		var st sql.NullString
		if rows.Scan(&q, &a, &st) == nil {
			t := &turn{question: q, status: st.String}
			t.answer.WriteString(a)
			v.deep.turns = append(v.deep.turns, t)
		}
	}
	v.screen = scrDeeper
}

func (v *view) deeperKeyHelp() string {
	switch v.screen {
	case scrConsent:
		return "↑↓ choose · enter enable · n not now"
	case scrHistory:
		return "↑↓ choose · enter read · esc back"
	case scrDeeper:
		switch {
		case v.deep.typing:
			return "enter ask · esc cancel"
		case v.deep.streaming:
			return "esc stop"
		case v.canFollowUp():
			return fmt.Sprintf("f follow-up (%d left) · ↑↓ scroll · esc back to card", maxFollowUps+1-len(v.deep.turns))
		default:
			return "↑↓ scroll · esc back"
		}
	}
	return ""
}

func (v *view) renderDeeper(width, height int) string {
	w := textWidth(width)
	switch v.screen {
	case scrConsent:
		lines := []string{
			bold.Render("Go deeper with Claude"),
			"",
			wrap("This asks your installed Claude Code (`claude -p`) to explain the card in more depth. It uses your own Claude subscription or API usage.", w, ""),
			"",
			wrap("It runs in an empty folder with every tool switched off, so it can't read or change your files. Answers are saved on this computer only.", w, ""),
			"",
			"Which model should it use?",
		}
		for i, m := range models {
			label := fmt.Sprintf("%d. %s", i+1, m.label)
			if i == v.deep.consentSel {
				lines = append(lines, selSty.Render("› "+label)+dim.Render("  "+m.note))
			} else {
				lines = append(lines, "  "+label+dim.Render("  "+m.note))
			}
		}
		lines = append(lines, "", dim.Render("You can change this later in ~/.config/meanwhile/config.toml ([deeper] model = \"haiku\")."))
		return strings.Join(lines, "\n")

	case scrHistory:
		if len(v.deep.history) == 0 {
			return wrap("No saved explanations for "+v.concept.Title+" yet. Press d on a card to ask for one.", w, "")
		}
		lines := []string{bold.Render("Saved explanations · " + v.concept.Title), ""}
		for i, t := range v.deep.history {
			label := t.created.Local().Format("Jan 2") + "  " + cardTitle(v.concept.Cards, t.cardID)
			if i == v.deep.histCursor {
				lines = append(lines, selSty.Render("› "+label))
			} else {
				lines = append(lines, "  "+label)
			}
		}
		return strings.Join(lines, "\n")
	}

	// scrDeeper
	var lines []string
	head := accent.Render("Going deeper") + dim.Render(" · "+v.concept.Title)
	if v.deep.model != "" {
		head += dim.Render(" · " + v.deep.model)
	}
	lines = append(lines, head, "")
	for i, t := range v.deep.turns {
		if i > 0 {
			lines = append(lines, "", bold.Render(wrap("You: "+t.question, w, "")), "")
		}
		if t.answer.Len() > 0 {
			lines = append(lines, strings.Split(renderMarkdown(t.answer.String(), width), "\n")...)
		}
	}
	switch {
	case v.deep.err != nil && v.deep.err.Kind != deeper.ErrCancelled:
		lines = append(lines, "", warn.Render(wrap(v.deep.err.Message(), w, "")), dim.Render("Your cards are still here: press esc."))
	case v.deep.retry != "":
		lines = append(lines, "", dim.Render(v.deep.retry))
	case v.deep.streaming && v.deep.turns[len(v.deep.turns)-1].answer.Len() == 0:
		lines = append(lines, dim.Render("Asking Claude…"))
	}

	footer := []string{}
	if v.deep.typing {
		footer = append(footer, "", v.deep.input.View())
	}
	room := max(height-len(footer), 3)
	if len(lines) > room {
		// Follow the stream unless the user scrolled up.
		maxScroll := len(lines) - room
		if v.deep.streaming {
			v.deep.scroll = maxScroll
		}
		v.deep.scroll = min(v.deep.scroll, maxScroll)
		lines = lines[v.deep.scroll : v.deep.scroll+room]
	}
	return strings.Join(append(lines, footer...), "\n")
}

func cardTitle(cards []curriculum.Card, id string) string {
	for _, c := range cards {
		if c.ID == id {
			return c.Title
		}
	}
	return id
}
