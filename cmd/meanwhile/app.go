package main

import (
	"fmt"

	"github.com/vg010623/meanwhile/internal/core"
	"github.com/vg010623/meanwhile/internal/store"
)

// allMigrations is the core schema plus every activity's tables.
func allMigrations() []store.Migration {
	return append([]store.Migration{}, core.Migrations...)
}

// openDB opens the database, telling the user if it had to recover from a
// damaged file.
func openDB(e env) (*store.DB, error) {
	db, rec, err := store.Open(e.paths.DB(), allMigrations(), e.now())
	if err != nil {
		return nil, err
	}
	if rec.CorruptPath != "" {
		if rec.FromBackup {
			fmt.Fprintf(e.stderr, "meanwhile: the database was damaged; restored yesterday's backup (damaged copy kept at %s)\n", rec.CorruptPath)
		} else {
			fmt.Fprintf(e.stderr, "meanwhile: the database was damaged and there was no backup; started fresh (damaged copy kept at %s)\n", rec.CorruptPath)
		}
	}
	return db, nil
}

func newIngestor(e env, db *store.DB) *core.Ingestor {
	return &core.Ingestor{DB: db, SpoolDir: e.paths.Spool(), LockPath: e.paths.IngestLock()}
}

func init() {
	extraDoctorChecks = append(extraDoctorChecks, func(e env) []doctorCheck {
		db, err := openDB(e)
		if err != nil {
			return []doctorCheck{{fail, "database: " + err.Error(), "delete " + e.paths.DB() + " to start fresh"}}
		}
		defer db.Close()
		if err := db.Check(); err != nil {
			return []doctorCheck{{fail, "database integrity check failed: " + err.Error(), ""}}
		}
		return []doctorCheck{{ok, "database: " + e.paths.DB(), ""}}
	})
}
