package learn

import (
	"fmt"

	"github.com/vg010623/meanwhile/internal/curriculum"
)

// This file is the lesson logic without a screen: each call reads the saved
// progress, does one thing, saves, and returns a Lesson describing what to
// show. `meanwhile lesson …` prints it as JSON for the Claude Code panel.
// Progress lives in the same tables the terminal app uses, so both share it.

// Screens a Lesson can describe.
const (
	ScreenCard     = "card"     // reading a card
	ScreenPicker   = "picker"   // nothing in progress: concepts ready to start
	ScreenComplete = "complete" // just finished (or marked known) a concept
)

// kvActive remembers the concept the panel is reading, so re-reading a
// finished concept keeps its place (store.current only finds in-progress ones).
const kvActive = "panel_concept"

// Lesson is everything the panel needs to draw one screen.
type Lesson struct {
	Screen  string       `json:"screen"`
	Concept *ConceptInfo `json:"concept,omitempty"`
	Card    *CardInfo    `json:"card,omitempty"`
	// LastTime is the recap shown when resuming: the last card read, or the
	// concept's summary on its first card. Set by Current only.
	LastTime string `json:"last_time,omitempty"`
	// Unmet lists prerequisites not done yet: a soft note, never a block.
	Unmet    []ConceptInfo `json:"unmet,omitempty"`
	Known    bool          `json:"known,omitempty"`    // complete screen: marked known, not read
	Unlocked []ConceptInfo `json:"unlocked,omitempty"` // complete screen: newly ready
	Picks    []ConceptInfo `json:"picks,omitempty"`    // what to start next
	// All is every concept, filled only when Picks is empty (everything
	// ready is done), so the user can re-read any of them.
	All    []ConceptInfo `json:"all,omitempty"`
	Notice string        `json:"notice,omitempty"` // e.g. content folder errors
}

// ConceptInfo describes a concept without its cards.
type ConceptInfo struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Summary    string `json:"summary"`
	Cards      int    `json:"cards"`
	InProgress bool   `json:"in_progress,omitempty"`
	Done       bool   `json:"done,omitempty"` // completed or marked known
}

// CardInfo is one card. Body is Markdown.
type CardInfo struct {
	Index int    `json:"index"` // 0-based
	Total int    `json:"total"`
	ID    string `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body"`
	Recap string `json:"recap,omitempty"`
	// HasDeeper is true when a "Go deeper" explanation of this card is saved.
	HasDeeper bool `json:"has_deeper,omitempty"`
}

// pickCount is how many concepts the picker offers.
const pickCount = 5

// Current returns where the user left off, or the picker.
func (a *Activity) Current() (Lesson, error) {
	c, idx, ok, err := a.active()
	if err != nil || !ok {
		return a.Pick()
	}
	l, err := a.cardLesson(c, idx)
	if err != nil {
		return l, err
	}
	l.LastTime = c.Summary
	if idx > 0 {
		l.LastTime = c.Cards[idx-1].Recap
	}
	return l, nil
}

// Next records the current card as read and moves on; after the last card
// the concept is completed.
func (a *Activity) Next() (Lesson, error) {
	c, idx, ok, err := a.active()
	if err != nil {
		return Lesson{}, err
	}
	if !ok {
		return a.Pick()
	}
	if err := a.store.recordRead(c.ID, c.Cards[idx].ID, ""); err != nil {
		return Lesson{}, err
	}
	if idx < len(c.Cards)-1 {
		if err := a.store.setPosition(c.ID, c.Cards[idx+1].ID, idx+1); err != nil {
			return Lesson{}, err
		}
		return a.cardLesson(c, idx+1)
	}
	prog, err := a.store.all()
	if err != nil {
		return Lesson{}, err
	}
	if !prog[c.ID].Status.Done() {
		if err := a.store.setStatus(c.ID, Completed); err != nil {
			return Lesson{}, err
		}
	}
	return a.complete(c, false)
}

// Back goes to the previous card.
func (a *Activity) Back() (Lesson, error) {
	c, idx, ok, err := a.active()
	if err != nil {
		return Lesson{}, err
	}
	if !ok {
		return a.Pick()
	}
	if idx > 0 {
		idx--
		if err := a.store.setPosition(c.ID, c.Cards[idx].ID, idx); err != nil {
			return Lesson{}, err
		}
	}
	return a.cardLesson(c, idx)
}

// MarkKnown marks the concept being read as known ("I know this").
func (a *Activity) MarkKnown() (Lesson, error) {
	c, _, ok, err := a.active()
	if err != nil {
		return Lesson{}, err
	}
	if !ok {
		return a.Pick()
	}
	if err := a.store.setStatus(c.ID, Known); err != nil {
		return Lesson{}, err
	}
	return a.complete(c, true)
}

// StartConcept opens a concept: at its saved card if it is in progress, else
// at the first card. Unmet prerequisites are reported, not enforced.
func (a *Activity) StartConcept(id string) (Lesson, error) {
	c, ok := a.cur.Concept(id)
	if !ok || len(c.Cards) == 0 {
		return Lesson{}, fmt.Errorf("no concept %q", id)
	}
	prog, err := a.store.all()
	if err != nil {
		return Lesson{}, err
	}
	idx := 0
	if p, ok := prog[c.ID]; ok && p.Status == InProgress {
		idx = cardIndex(c, p)
	}
	if err := a.store.setPosition(c.ID, c.Cards[idx].ID, idx); err != nil {
		return Lesson{}, err
	}
	if err := a.ctx.DB.SetKV(kvNamespace, kvActive, c.ID); err != nil {
		return Lesson{}, err
	}
	l, err := a.cardLesson(c, idx)
	for _, u := range a.graph.unmet(c, prog) {
		l.Unmet = append(l.Unmet, conceptInfo(u, prog))
	}
	return l, err
}

// Pick lists the concepts the user is ready for, most useful first.
func (a *Activity) Pick() (Lesson, error) {
	prog, err := a.store.all()
	if err != nil {
		return Lesson{}, err
	}
	l := Lesson{Screen: ScreenPicker, Notice: a.notice}
	for _, c := range a.graph.recommend(prog, "", pickCount) {
		l.Picks = append(l.Picks, conceptInfo(c, prog))
	}
	l.All = a.allIfNoPicks(l.Picks, prog)
	return l, nil
}

func (a *Activity) allIfNoPicks(picks []ConceptInfo, prog map[string]Progress) []ConceptInfo {
	if len(picks) > 0 {
		return nil
	}
	var all []ConceptInfo
	tracks, byTrack := a.graph.byTrack()
	for _, t := range tracks {
		for _, c := range byTrack[t] {
			all = append(all, conceptInfo(c, prog))
		}
	}
	return all
}

// active finds the concept being read and its card: the one the panel last
// opened if it was touched more recently than any in-progress concept,
// otherwise the most recent in-progress concept (as the terminal app does).
func (a *Activity) active() (*curriculum.Concept, int, bool, error) {
	prog, err := a.store.all()
	if err != nil {
		return nil, 0, false, err
	}
	cur, hasCur, err := a.store.current()
	if err != nil {
		return nil, 0, false, err
	}
	pick := cur
	if id, ok, _ := a.ctx.DB.GetKV(kvNamespace, kvActive); ok {
		if p, ok := prog[id]; ok && (!hasCur || !p.UpdatedAt.Before(cur.UpdatedAt)) {
			pick, hasCur = p, true
		}
	}
	if !hasCur {
		return nil, 0, false, nil
	}
	c, ok := a.cur.Concept(pick.ConceptID)
	if !ok || len(c.Cards) == 0 {
		return nil, 0, false, nil // removed from the curriculum
	}
	return c, cardIndex(c, pick), true, nil
}

func (a *Activity) cardLesson(c *curriculum.Concept, idx int) (Lesson, error) {
	prog, err := a.store.all()
	if err != nil {
		return Lesson{}, err
	}
	card := c.Cards[idx]
	info := conceptInfo(c, prog)
	var saved int
	if err := a.ctx.DB.QueryRow(`SELECT COUNT(*) FROM learn_deep_dives WHERE concept_id = ? AND card_id = ? AND parent_id IS NULL`, c.ID, card.ID).Scan(&saved); err != nil {
		return Lesson{}, err
	}
	return Lesson{
		Screen:  ScreenCard,
		Concept: &info,
		Card: &CardInfo{Index: idx, Total: len(c.Cards), ID: card.ID, Title: card.Title, Body: card.Body, Recap: card.Recap,
			HasDeeper: saved > 0},
		Notice: a.notice,
	}, nil
}

func (a *Activity) complete(c *curriculum.Concept, known bool) (Lesson, error) {
	_ = a.ctx.DB.SetKV(kvNamespace, kvActive, "") // done with it
	prog, err := a.store.all()
	if err != nil {
		return Lesson{}, err
	}
	info := conceptInfo(c, prog)
	l := Lesson{Screen: ScreenComplete, Concept: &info, Known: known, Notice: a.notice}
	for _, u := range a.graph.newlyUnlocked(c.ID, prog) {
		l.Unlocked = append(l.Unlocked, conceptInfo(u, prog))
	}
	for _, p := range a.graph.recommend(prog, "", pickCount) {
		l.Picks = append(l.Picks, conceptInfo(p, prog))
	}
	l.All = a.allIfNoPicks(l.Picks, prog)
	return l, nil
}

func conceptInfo(c *curriculum.Concept, prog map[string]Progress) ConceptInfo {
	return ConceptInfo{ID: c.ID, Title: c.Title, Summary: c.Summary, Cards: len(c.Cards),
		InProgress: prog[c.ID].Status == InProgress, Done: prog[c.ID].Status.Done()}
}
