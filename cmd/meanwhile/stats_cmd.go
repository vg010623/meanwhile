package main

import (
	"fmt"
	"time"

	"github.com/vg010623/meanwhile/internal/core"
	"github.com/vg010623/meanwhile/internal/store"
)

// statsSections lets activities add their own lines to `meanwhile stats`.
var statsSections []func(e env, db *store.DB) ([]string, error)

func cmdStats(args []string, e env) int {
	db, err := openDB(e)
	if err != nil {
		fmt.Fprintln(e.stderr, "meanwhile:", err)
		return 1
	}
	defer db.Close()
	if _, err := newIngestor(e, db).Sync(e.now()); err != nil {
		fmt.Fprintln(e.stderr, "meanwhile: reading new events:", err)
	}
	s, err := core.ComputeWaitStats(db, e.now())
	if err != nil {
		fmt.Fprintln(e.stderr, "meanwhile:", err)
		return 1
	}

	fmt.Fprintln(e.stdout, "Waiting on Claude")
	fmt.Fprintf(e.stdout, "  Total wait time   %s across %d waits\n", core.FormatDuration(s.Total), s.Count)
	fmt.Fprintf(e.stdout, "  Typical wait      %s\n", core.FormatDuration(s.Median))
	fmt.Fprintf(e.stdout, "  Last 7 days       %s across %d waits\n", core.FormatDuration(s.Last7Total), s.Last7Count)
	fmt.Fprintf(e.stdout, "  Today             %s across %d waits\n", core.FormatDuration(s.Today), s.TodayCount)
	fmt.Fprintf(e.stdout, "  meanwhile showed  %d of %d waits\n", s.Shown, s.Count)
	if s.Blocked >= time.Second {
		fmt.Fprintf(e.stdout, "  Claude waited on you for %s (not counted above)\n", core.FormatDuration(s.Blocked))
	}
	for _, section := range statsSections {
		lines, err := section(e, db)
		if err != nil {
			fmt.Fprintln(e.stderr, "meanwhile:", err)
			continue
		}
		fmt.Fprintln(e.stdout)
		for _, l := range lines {
			fmt.Fprintln(e.stdout, l)
		}
	}
	return 0
}
