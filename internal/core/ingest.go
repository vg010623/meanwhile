package core

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/vg010623/meanwhile/internal/events"
	"github.com/vg010623/meanwhile/internal/lock"
	"github.com/vg010623/meanwhile/internal/spool"
	"github.com/vg010623/meanwhile/internal/store"
)

// Migrations are the core's tables. Times are stored as Unix milliseconds.
var Migrations = []store.Migration{
	{Namespace: "core", Version: 1, SQL: `
CREATE TABLE events (
	id TEXT PRIMARY KEY, ts INTEGER NOT NULL, kind TEXT NOT NULL,
	session_id TEXT NOT NULL, cwd TEXT, worktree_root TEXT, source_hook TEXT,
	end_reason TEXT, error_type TEXT, input_kind TEXT, headless INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX events_session_ts ON events (session_id, ts);
CREATE TABLE sessions (
	session_id TEXT PRIMARY KEY, cwd TEXT, worktree_root TEXT,
	first_seen INTEGER NOT NULL, last_seen INTEGER NOT NULL, ended_at INTEGER
);
CREATE TABLE waits (
	id TEXT PRIMARY KEY, session_id TEXT NOT NULL, cwd TEXT, worktree_root TEXT,
	started_at INTEGER NOT NULL, ended_at INTEGER, end_reason TEXT, error_type TEXT,
	state TEXT NOT NULL, input_since INTEGER, input_kind TEXT,
	needs_input_ms INTEGER NOT NULL DEFAULT 0, last_event_at INTEGER NOT NULL,
	headless INTEGER NOT NULL DEFAULT 0, shown INTEGER NOT NULL DEFAULT 0, activity_id TEXT
);
CREATE INDEX waits_open ON waits (ended_at);
CREATE INDEX waits_started ON waits (started_at);
CREATE TABLE ingest_offsets (file TEXT PRIMARY KEY, byte_offset INTEGER NOT NULL);
CREATE TABLE kv (namespace TEXT NOT NULL, key TEXT NOT NULL, value TEXT NOT NULL, PRIMARY KEY (namespace, key));
`},
	{Namespace: "core", Version: 2, SQL: `CREATE INDEX events_ts ON events (ts);`},
	{Namespace: "core", Version: 3, SQL: `ALTER TABLE waits ADD COLUMN closed_at INTEGER;`},
}

// eventRetention is how long raw events are kept. Waits (the useful summary)
// are kept forever. This must stay longer than spoolRetention, because the
// events table is what makes re-reading a spool file harmless.
const eventRetention = 30 * 24 * time.Hour

// StaleAfter is how long an open wait may go without any event before it is
// closed automatically. Tool calls arrive as events, so a real run this
// quiet is very unlikely; an interrupted one is the usual cause.
const StaleAfter = 30 * time.Minute

// spoolRetention is how long fully read spool files are kept.
const spoolRetention = 7 * 24 * time.Hour

// Ingestor moves new spool lines into the database and keeps waits current.
// Several processes may ingest (the UI, `meanwhile stats`); a file lock makes
// them take turns, and inserting events by ID makes repeats harmless.
type Ingestor struct {
	DB       *store.DB
	SpoolDir string
	LockPath string
}

// Sync ingests everything new and returns the wait changes, oldest first.
func (in *Ingestor) Sync(now time.Time) ([]Change, error) {
	l, err := lock.Acquire(in.LockPath)
	if err != nil {
		return nil, err
	}
	defer l.Release()

	offsets, err := loadOffsets(in.DB)
	if err != nil {
		return nil, err
	}
	res, err := spool.ReadNew(in.SpoolDir, offsets)
	if err != nil {
		return nil, err
	}

	tx, err := in.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() // no-op after Commit

	m, err := loadOpen(tx, now)
	if err != nil {
		return nil, err
	}
	var changes []Change
	for _, e := range res.Events {
		fresh, err := insertEvent(tx, e)
		if err != nil {
			return nil, err
		}
		if !fresh {
			continue // already applied by an earlier sync
		}
		if err := upsertSession(tx, e); err != nil {
			return nil, err
		}
		changes = append(changes, m.Apply(e)...)
	}
	changes = append(changes, m.CloseStale(now, StaleAfter)...)
	for _, c := range changes {
		if err := saveWait(tx, c.Wait); err != nil {
			return nil, err
		}
	}
	// Open waits whose LastEventAt moved (heartbeats) need saving too.
	for _, w := range m.Open {
		if err := saveWait(tx, *w); err != nil {
			return nil, err
		}
	}
	if err := saveOffsets(tx, res.Offsets); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM events WHERE ts < ?`, ms(now.Add(-eventRetention))); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	if err := spool.Prune(in.SpoolDir, res.Offsets, spoolRetention, now); err == nil {
		_ = saveOffsetsDB(in.DB, res.Offsets)
	}
	return changes, nil
}

// CloseManual ends a session's open wait because the user said Claude is done.
func (in *Ingestor) CloseManual(sessionID string, now time.Time) ([]Change, error) {
	l, err := lock.Acquire(in.LockPath)
	if err != nil {
		return nil, err
	}
	defer l.Release()
	tx, err := in.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	m, err := loadOpen(tx, now)
	if err != nil {
		return nil, err
	}
	changes := m.CloseManual(sessionID, now)
	for _, c := range changes {
		if err := saveWait(tx, c.Wait); err != nil {
			return nil, err
		}
	}
	return changes, tx.Commit()
}

// OpenWaits returns the currently open waits, newest first.
func OpenWaits(db *store.DB) ([]Wait, error) {
	rows, err := db.Query(`SELECT ` + waitColumns + ` FROM waits WHERE ended_at IS NULL ORDER BY started_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Wait
	for rows.Next() {
		w, err := scanWait(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// MarkShown records that the UI appeared for a wait, and which activity ran.
func MarkShown(db *store.DB, waitID, activityID string) error {
	_, err := db.Exec(`UPDATE waits SET shown = 1, activity_id = ? WHERE id = ?`, activityID, waitID)
	return err
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

type querier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

func insertEvent(tx execer, e events.Event) (bool, error) {
	r, err := tx.Exec(`INSERT OR IGNORE INTO events
		(id, ts, kind, session_id, cwd, worktree_root, source_hook, end_reason, error_type, input_kind, headless)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, ms(e.TS), string(e.Kind), e.SessionID, e.CWD, e.WorktreeRoot, e.SourceHook,
		e.EndReason, e.ErrorType, e.InputKind, boolInt(e.Headless))
	if err != nil {
		return false, fmt.Errorf("insert event: %w", err)
	}
	n, _ := r.RowsAffected()
	return n == 1, nil
}

func upsertSession(tx execer, e events.Event) error {
	var ended any
	if e.Kind == events.SessionEnded {
		ended = ms(e.TS)
	}
	_, err := tx.Exec(`INSERT INTO sessions (session_id, cwd, worktree_root, first_seen, last_seen, ended_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (session_id) DO UPDATE SET
			cwd = COALESCE(NULLIF(excluded.cwd, ''), sessions.cwd),
			worktree_root = COALESCE(NULLIF(excluded.worktree_root, ''), sessions.worktree_root),
			last_seen = MAX(sessions.last_seen, excluded.last_seen),
			ended_at = COALESCE(excluded.ended_at, sessions.ended_at)`,
		e.SessionID, e.CWD, e.WorktreeRoot, ms(e.TS), ms(e.TS), ended)
	return err
}

const waitColumns = `id, session_id, cwd, worktree_root, started_at, ended_at, end_reason, error_type,
	state, input_since, input_kind, needs_input_ms, last_event_at, headless, shown, closed_at`

func saveWait(tx execer, w Wait) error {
	_, err := tx.Exec(`INSERT INTO waits (`+waitColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			ended_at = excluded.ended_at, end_reason = excluded.end_reason, error_type = excluded.error_type,
			state = excluded.state, input_since = excluded.input_since, input_kind = excluded.input_kind,
			needs_input_ms = excluded.needs_input_ms, last_event_at = excluded.last_event_at,
			closed_at = excluded.closed_at`,
		w.ID, w.SessionID, w.CWD, w.WorktreeRoot, ms(w.StartedAt), nullMS(w.EndedAt), w.EndReason, w.ErrorType,
		string(w.State), nullMS(w.InputSince), w.InputKind, w.NeedsInput.Milliseconds(), ms(w.LastEventAt),
		boolInt(w.Headless), boolInt(w.Shown), nullMS(w.ClosedAt))
	return err
}

type scanner interface{ Scan(dest ...any) error }

func scanWait(r scanner) (Wait, error) {
	var (
		w                            Wait
		started, lastEvent, needMS   int64
		ended, inputSince, closedAt  sql.NullInt64
		endReason, errType, inputKnd sql.NullString
		cwd, root                    sql.NullString
		state                        string
		headless, shown              int
	)
	err := r.Scan(&w.ID, &w.SessionID, &cwd, &root, &started, &ended, &endReason, &errType,
		&state, &inputSince, &inputKnd, &needMS, &lastEvent, &headless, &shown, &closedAt)
	if err != nil {
		return w, err
	}
	w.CWD, w.WorktreeRoot = cwd.String, root.String
	w.StartedAt, w.LastEventAt = fromMS(started), fromMS(lastEvent)
	if ended.Valid {
		w.EndedAt = fromMS(ended.Int64)
	}
	if inputSince.Valid {
		w.InputSince = fromMS(inputSince.Int64)
	}
	if closedAt.Valid {
		w.ClosedAt = fromMS(closedAt.Int64)
	}
	w.EndReason, w.ErrorType, w.InputKind = endReason.String, errType.String, inputKnd.String
	w.State = State(state)
	w.NeedsInput = time.Duration(needMS) * time.Millisecond
	w.Headless, w.Shown = headless == 1, shown == 1
	return w, nil
}

// loadOpen rebuilds the machine from the database: every open wait, plus
// waits closed by hand recently enough that a late Stop may still correct
// them (and with no newer wait in their session).
func loadOpen(q querier, now time.Time) (*Machine, error) {
	rows, err := q.Query(`SELECT ` + waitColumns + ` FROM waits WHERE ended_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := NewMachine()
	for rows.Next() {
		w, err := scanWait(rows)
		if err != nil {
			return nil, err
		}
		if prev, ok := m.Open[w.SessionID]; ok && prev.StartedAt.After(w.StartedAt) {
			continue // keep the newest if the data is ever inconsistent
		}
		m.Open[w.SessionID] = &w
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	recent, err := q.Query(`SELECT `+waitColumns+` FROM waits w
		WHERE end_reason = ? AND closed_at >= ?
		AND NOT EXISTS (SELECT 1 FROM waits n WHERE n.session_id = w.session_id AND n.started_at > w.started_at)`,
		events.EndManual, ms(now.Add(-manualCorrectionWindow)))
	if err != nil {
		return nil, err
	}
	defer recent.Close()
	for recent.Next() {
		w, err := scanWait(recent)
		if err != nil {
			return nil, err
		}
		if _, open := m.Open[w.SessionID]; !open {
			m.RecentManual[w.SessionID] = &w
		}
	}
	return m, recent.Err()
}

func loadOffsets(db *store.DB) (spool.Offsets, error) {
	rows, err := db.Query(`SELECT file, byte_offset FROM ingest_offsets`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	o := spool.Offsets{}
	for rows.Next() {
		var f string
		var n int64
		if err := rows.Scan(&f, &n); err != nil {
			return nil, err
		}
		o[f] = n
	}
	return o, rows.Err()
}

func saveOffsets(tx execer, o spool.Offsets) error {
	for f, n := range o {
		if _, err := tx.Exec(`INSERT INTO ingest_offsets (file, byte_offset) VALUES (?, ?)
			ON CONFLICT (file) DO UPDATE SET byte_offset = excluded.byte_offset`, f, n); err != nil {
			return err
		}
	}
	return nil
}

// saveOffsetsDB rewrites the offsets table after pruning removed files.
func saveOffsetsDB(db *store.DB, o spool.Offsets) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM ingest_offsets`); err != nil {
		return err
	}
	if err := saveOffsets(tx, o); err != nil {
		return err
	}
	return tx.Commit()
}

func ms(t time.Time) int64 { return t.UnixMilli() }

func nullMS(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UnixMilli()
}

func fromMS(n int64) time.Time { return time.UnixMilli(n).UTC() }

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
