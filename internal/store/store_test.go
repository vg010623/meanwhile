package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

var testMigrations = []Migration{
	{"core", 1, `CREATE TABLE kv (namespace TEXT, key TEXT, value TEXT, PRIMARY KEY (namespace, key))`},
	{"test", 1, `CREATE TABLE things (id INTEGER PRIMARY KEY, name TEXT)`},
}

func TestOpenMigratesOnce(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	db, rec, err := Open(p, testMigrations, time.Now())
	if err != nil || rec.CorruptPath != "" {
		t.Fatal(err, rec)
	}
	if _, err := db.Exec(`INSERT INTO things (name) VALUES ('a')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	// Reopening must not re-run migrations (which would fail: table exists).
	db, _, err = Open(p, testMigrations, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM things`).Scan(&n)
	if n != 1 {
		t.Errorf("rows = %d", n)
	}
}

func TestKV(t *testing.T) {
	db, _, err := Open(filepath.Join(t.TempDir(), "s.db"), testMigrations, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, ok, _ := db.GetKV("ns", "k"); ok {
		t.Error("unexpected value")
	}
	_ = db.SetKV("ns", "k", "v1")
	_ = db.SetKV("ns", "k", "v2")
	if v, ok, err := db.GetKV("ns", "k"); !ok || v != "v2" || err != nil {
		t.Errorf("got %q %v %v", v, ok, err)
	}
}

func TestCorruptFileIsReplacedFromBackup(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.db")
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

	db, _, err := Open(p, testMigrations, now)
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`INSERT INTO things (name) VALUES ('saved')`)
	if err := db.BackupIfDue(now, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	db.Close()
	os.Remove(p + "-wal")
	os.Remove(p + "-shm")
	if err := os.WriteFile(p, []byte("this is not a database at all, just junk bytes......"), 0o600); err != nil {
		t.Fatal(err)
	}

	db, rec, err := Open(p, testMigrations, now)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if !rec.FromBackup || rec.CorruptPath == "" {
		t.Fatalf("recovery = %+v", rec)
	}
	if _, err := os.Stat(rec.CorruptPath); err != nil {
		t.Error("corrupt file not kept for inspection")
	}
	var name string
	db.QueryRow(`SELECT name FROM things`).Scan(&name)
	if name != "saved" {
		t.Errorf("backup not restored, got %q", name)
	}
}

func TestCorruptFileWithoutBackupStartsFresh(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	_ = os.WriteFile(p, []byte("junk junk junk junk junk junk junk junk junk junk junk"), 0o600)
	db, rec, err := Open(p, testMigrations, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if rec.CorruptPath == "" || rec.FromBackup {
		t.Errorf("recovery = %+v", rec)
	}
	if err := db.Check(); err != nil {
		t.Error(err)
	}
}

func TestBackupIfDueSkipsRecentBackup(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	now := time.Now()
	db, _, _ := Open(p, testMigrations, now)
	defer db.Close()
	_ = db.BackupIfDue(now, time.Hour)
	info1, _ := os.Stat(BackupPath(p))
	db.Exec(`INSERT INTO things (name) VALUES ('x')`)
	_ = db.BackupIfDue(now, time.Hour) // backup is fresh: skipped
	info2, _ := os.Stat(BackupPath(p))
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Error("backup rewritten too soon")
	}
}
