// Package store opens meanwhile's SQLite database, applies migrations, and
// recovers from corruption.
//
// SQLite in WAL mode lets the UI and a `meanwhile stats` run read and write
// at the same time safely, and every write is a transaction, so a crash in
// the middle of saving progress can't leave the file half-written.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver: no C compiler needed to build
)

// Migration is one schema change. Namespaces keep each component's versions
// separate: the core uses "core", activities use their own ID.
type Migration struct {
	Namespace string
	Version   int
	SQL       string
}

// DB is an open database.
type DB struct {
	*sql.DB
	Path string
}

// Recovery reports what Open had to do to get a working database.
type Recovery struct {
	CorruptPath string // where the damaged file was moved, if it was
	FromBackup  bool   // true if the daily backup was restored
}

// Open opens (or creates) the database at path, checks its integrity and
// applies migrations. A damaged file is moved aside and replaced by the
// latest backup, or by a fresh database if there is no backup.
func Open(path string, migrations []Migration, now time.Time) (*DB, Recovery, error) {
	var rec Recovery
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, rec, err
	}
	db, err := openRaw(path)
	if err == nil {
		err = quickCheck(db)
		if err != nil {
			db.Close()
		}
	}
	if err != nil {
		rec.CorruptPath = fmt.Sprintf("%s.corrupt-%s", path, now.Format("20060102-150405"))
		for _, suffix := range []string{"", "-wal", "-shm"} {
			_ = os.Rename(path+suffix, rec.CorruptPath+suffix)
		}
		if copyFile(BackupPath(path), path) == nil {
			rec.FromBackup = true
		}
		if db, err = openRaw(path); err != nil {
			return nil, rec, err
		}
		if err := quickCheck(db); err != nil { // the backup was bad too
			db.Close()
			_ = os.Remove(path)
			rec.FromBackup = false
			if db, err = openRaw(path); err != nil {
				return nil, rec, err
			}
		}
	}
	if err := migrate(db, migrations); err != nil {
		db.Close()
		return nil, rec, fmt.Errorf("migrate: %w", err)
	}
	return &DB{DB: db, Path: path}, rec, nil
}

func openRaw(path string) (*sql.DB, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection per process keeps locking simple; our queries are tiny.
	db.SetMaxOpenConns(1)
	return db, nil
}

func quickCheck(db *sql.DB) error {
	var result string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return errors.New(result)
	}
	return nil
}

// Check runs SQLite's integrity check on an open database.
func (d *DB) Check() error { return quickCheck(d.DB) }

func migrate(db *sql.DB, migrations []Migration) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		namespace TEXT NOT NULL, version INTEGER NOT NULL, applied_at INTEGER NOT NULL,
		PRIMARY KEY (namespace, version))`); err != nil {
		return err
	}
	for _, m := range migrations {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE namespace = ? AND version = ?`, m.Namespace, m.Version).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(m.SQL); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s v%d: %w", m.Namespace, m.Version, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations VALUES (?, ?, ?)`, m.Namespace, m.Version, time.Now().UnixMilli()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// BackupPath is where the daily backup of the database at path lives.
func BackupPath(path string) string { return path + ".bak" }

// BackupIfDue writes a consistent copy of the database with VACUUM INTO when
// the existing backup is older than every. It is cheap enough to call on
// every app start.
func (d *DB) BackupIfDue(now time.Time, every time.Duration) error {
	bak := BackupPath(d.Path)
	if info, err := os.Stat(bak); err == nil && now.Sub(info.ModTime()) < every {
		return nil
	}
	tmp := bak + ".tmp"
	_ = os.Remove(tmp)
	if _, err := d.Exec(`VACUUM INTO ?`, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, bak)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// GetKV and SetKV store small settings, such as "Go deeper" consent.
func (d *DB) GetKV(namespace, key string) (string, bool, error) {
	var v string
	err := d.QueryRow(`SELECT value FROM kv WHERE namespace = ? AND key = ?`, namespace, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

func (d *DB) SetKV(namespace, key, value string) error {
	_, err := d.Exec(`INSERT INTO kv (namespace, key, value) VALUES (?, ?, ?)
		ON CONFLICT (namespace, key) DO UPDATE SET value = excluded.value`, namespace, key, value)
	return err
}
