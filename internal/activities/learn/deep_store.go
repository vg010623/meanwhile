package learn

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/vg010623/meanwhile/internal/curriculum"
)

// Saved "Go deeper" explanations, for callers without a view (the Claude
// Code panel, through `meanwhile lesson deeper`). They use the same
// learn_deep_dives rows as the terminal app: a thread is a root row (the
// first explanation) plus follow-ups whose parent_id is the root.

// DeepTurn is one question and its answer.
type DeepTurn struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
	Status   string `json:"status"` // complete | cancelled
	Model    string `json:"model,omitempty"`
}

// DeepThread is the latest saved explanation of a card, with its follow-ups.
// RootID is 0 when the card has none.
type DeepThread struct {
	RootID       int64      `json:"root_id"`
	Turns        []DeepTurn `json:"turns"`
	MaxFollowUps int        `json:"max_follow_ups"`
}

// DeepThreadFor returns the most recent explanation thread for a card.
func (a *Activity) DeepThreadFor(conceptID, cardID string) (DeepThread, error) {
	t := DeepThread{Turns: []DeepTurn{}, MaxFollowUps: maxFollowUps}
	err := a.ctx.DB.QueryRow(`SELECT id FROM learn_deep_dives
		WHERE concept_id = ? AND card_id = ? AND parent_id IS NULL
		ORDER BY created_at DESC, id DESC LIMIT 1`, conceptID, cardID).Scan(&t.RootID)
	if errors.Is(err, sql.ErrNoRows) {
		return t, nil
	}
	if err != nil {
		return t, err
	}
	rows, err := a.ctx.DB.Query(`SELECT question, answer, status, COALESCE(model, '') FROM learn_deep_dives
		WHERE id = ? OR parent_id = ? ORDER BY id`, t.RootID, t.RootID)
	if err != nil {
		return t, err
	}
	defer rows.Close()
	for rows.Next() {
		var turn DeepTurn
		if err := rows.Scan(&turn.Question, &turn.Answer, &turn.Status, &turn.Model); err != nil {
			return t, err
		}
		t.Turns = append(t.Turns, turn)
	}
	return t, rows.Err()
}

// SaveDeepTurn stores one answer. rootID 0 starts a new thread; otherwise it
// is a follow-up in that thread. It returns the thread's root ID.
func (a *Activity) SaveDeepTurn(conceptID, cardID string, rootID int64, turn DeepTurn) (int64, error) {
	if c, ok := a.cur.Concept(conceptID); !ok {
		return 0, fmt.Errorf("no concept %q", conceptID)
	} else if !hasCard(c.Cards, cardID) {
		return 0, fmt.Errorf("no card %q in %q", cardID, conceptID)
	}
	if turn.Answer == "" {
		return 0, errors.New("empty answer")
	}
	if turn.Status == "" {
		turn.Status = "complete"
	}
	var parent any
	if rootID != 0 {
		var n int
		if err := a.ctx.DB.QueryRow(`SELECT COUNT(*) FROM learn_deep_dives WHERE id = ? AND parent_id IS NULL`, rootID).Scan(&n); err != nil {
			return 0, err
		}
		if n == 0 {
			return 0, fmt.Errorf("no thread %d", rootID)
		}
		parent = rootID
	}
	r, err := a.ctx.DB.Exec(`INSERT INTO learn_deep_dives (concept_id, card_id, parent_id, question, answer, status, model, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		conceptID, cardID, parent, turn.Question, turn.Answer, turn.Status, turn.Model, a.ctx.Now().UnixMilli())
	if err != nil {
		return 0, err
	}
	if rootID != 0 {
		return rootID, nil
	}
	return r.LastInsertId()
}

func hasCard(cards []curriculum.Card, id string) bool {
	for _, c := range cards {
		if c.ID == id {
			return true
		}
	}
	return false
}
