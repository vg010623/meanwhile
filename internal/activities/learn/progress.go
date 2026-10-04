package learn

import (
	"database/sql"
	"errors"
	"time"

	"github.com/vg010623/meanwhile/internal/store"
)

// Status of a concept for this user. A concept with no row is not started.
type Status string

const (
	InProgress Status = "in_progress"
	Completed  Status = "completed" // read every card
	Known      Status = "known"     // "I know this": counts as done for unlocking
)

// Done reports whether a status unlocks dependent concepts.
func (s Status) Done() bool { return s == Completed || s == Known }

var migrations = []store.Migration{
	{Namespace: "learn", Version: 1, SQL: `
CREATE TABLE learn_progress (
	concept_id TEXT PRIMARY KEY, status TEXT NOT NULL,
	card_id TEXT, card_index INTEGER NOT NULL DEFAULT 0,
	updated_at INTEGER NOT NULL, completed_at INTEGER
);
CREATE TABLE learn_card_reads (
	concept_id TEXT NOT NULL, card_id TEXT NOT NULL, read_at INTEGER NOT NULL, wait_id TEXT
);
CREATE INDEX learn_card_reads_concept ON learn_card_reads (concept_id);
CREATE TABLE learn_deep_dives (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	concept_id TEXT NOT NULL, card_id TEXT NOT NULL, parent_id INTEGER,
	question TEXT NOT NULL, answer TEXT NOT NULL, status TEXT NOT NULL,
	model TEXT, created_at INTEGER NOT NULL
);
CREATE INDEX learn_deep_dives_concept ON learn_deep_dives (concept_id);
`},
}

// Progress is one concept's saved state.
type Progress struct {
	ConceptID string
	Status    Status
	CardID    string // where the user is; IDs survive authors reordering cards
	CardIndex int    // fallback if the card ID no longer exists
	UpdatedAt time.Time
}

// progressStore reads and writes progress. Every change is written
// immediately, so quitting or crashing never loses more than the card on
// screen.
type progressStore struct {
	db  *store.DB
	now func() time.Time
}

func (s progressStore) all() (map[string]Progress, error) {
	rows, err := s.db.Query(`SELECT concept_id, status, COALESCE(card_id, ''), card_index, updated_at FROM learn_progress`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Progress{}
	for rows.Next() {
		var p Progress
		var updated int64
		if err := rows.Scan(&p.ConceptID, &p.Status, &p.CardID, &p.CardIndex, &updated); err != nil {
			return nil, err
		}
		p.UpdatedAt = time.UnixMilli(updated)
		out[p.ConceptID] = p
	}
	return out, rows.Err()
}

// setPosition records the card being read. A finished concept keeps its
// status when re-read.
func (s progressStore) setPosition(conceptID, cardID string, index int) error {
	_, err := s.db.Exec(`INSERT INTO learn_progress (concept_id, status, card_id, card_index, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (concept_id) DO UPDATE SET card_id = excluded.card_id, card_index = excluded.card_index,
			updated_at = excluded.updated_at`,
		conceptID, string(InProgress), cardID, index, s.now().UnixMilli())
	return err
}

func (s progressStore) setStatus(conceptID string, st Status) error {
	now := s.now().UnixMilli()
	_, err := s.db.Exec(`INSERT INTO learn_progress (concept_id, status, card_index, updated_at, completed_at)
		VALUES (?, ?, 0, ?, ?)
		ON CONFLICT (concept_id) DO UPDATE SET status = excluded.status, updated_at = excluded.updated_at,
			completed_at = excluded.completed_at`,
		conceptID, string(st), now, now)
	return err
}

// unmarkKnown undoes "I know this".
func (s progressStore) unmarkKnown(conceptID string) error {
	_, err := s.db.Exec(`DELETE FROM learn_progress WHERE concept_id = ? AND status = ?`, conceptID, string(Known))
	return err
}

func (s progressStore) recordRead(conceptID, cardID, waitID string) error {
	var wait any
	if waitID != "" {
		wait = waitID
	}
	_, err := s.db.Exec(`INSERT INTO learn_card_reads (concept_id, card_id, read_at, wait_id) VALUES (?, ?, ?, ?)`,
		conceptID, cardID, s.now().UnixMilli(), wait)
	return err
}

// current is the most recently touched concept that is still in progress.
func (s progressStore) current() (Progress, bool, error) {
	var p Progress
	var updated int64
	err := s.db.QueryRow(`SELECT concept_id, status, COALESCE(card_id, ''), card_index, updated_at
		FROM learn_progress WHERE status = ? ORDER BY updated_at DESC LIMIT 1`, string(InProgress)).
		Scan(&p.ConceptID, &p.Status, &p.CardID, &p.CardIndex, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return p, false, nil
	}
	if err != nil {
		return p, false, err
	}
	p.UpdatedAt = time.UnixMilli(updated)
	return p, true, nil
}
