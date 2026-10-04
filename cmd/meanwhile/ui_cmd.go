package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/vg010623/meanwhile/internal/config"
	"github.com/vg010623/meanwhile/internal/lock"
	"github.com/vg010623/meanwhile/internal/shell"
)

// cmdUI starts the terminal app. Only one may run at a time, because it is
// the one that wakes up and shows lessons.
func cmdUI(args []string, e env) int {
	l, err := lock.TryAcquire(e.paths.UILock())
	if errors.Is(err, lock.ErrHeld) {
		fmt.Fprintln(e.stderr, "meanwhile is already running in another pane or tab.")
		return 1
	}
	if err != nil {
		fmt.Fprintln(e.stderr, "meanwhile:", err)
		return 1
	}
	defer l.Release()

	var notices []string
	cfg, err := config.Load(e.paths.ConfigFile())
	if err != nil {
		notices = append(notices, fmt.Sprintf("Ignoring %s: %v", e.paths.ConfigFile(), err))
	}
	db, err := openDB(e)
	if err != nil {
		fmt.Fprintln(e.stderr, "meanwhile: can't open the database:", err)
		return 1
	}
	defer db.Close()
	if err := db.BackupIfDue(e.now(), 24*time.Hour); err != nil {
		notices = append(notices, "Couldn't back up the database: "+err.Error())
	}

	act := activities()[0]
	if err := act.Init(activityContext(e, db, cfg)); err != nil {
		fmt.Fprintln(e.stderr, "meanwhile:", err)
		return 1
	}
	defer act.Close()

	notice := ""
	if len(notices) > 0 {
		notice = notices[0]
	}
	err = shell.Run(shell.Options{
		DB:       db,
		Ingestor: newIngestor(e, db),
		Activity: act,
		Config:   cfg,
		SpoolDir: e.paths.Spool(),
		Now:      e.now,
		Notice:   notice,
	})
	if err != nil {
		fmt.Fprintln(e.stderr, "meanwhile:", err)
		return 1
	}
	return 0
}
