package core

import (
	"sort"
	"time"

	"github.com/vg010623/meanwhile/internal/store"
)

// WaitStats summarises finished waits. Headless runs (scripts, CI) are left
// out: nobody was sitting there waiting.
type WaitStats struct {
	Count      int
	Shown      int           // waits where the UI actually appeared
	Total      time.Duration // Claude working time, excluding time blocked on the user
	Median     time.Duration
	Blocked    time.Duration // time Claude spent waiting for the user
	Last7Count int
	Last7Total time.Duration
	Today      time.Duration
	TodayCount int
}

// ComputeWaitStats reads all finished, non-headless waits.
func ComputeWaitStats(db *store.DB, now time.Time) (WaitStats, error) {
	rows, err := db.Query(`SELECT ` + waitColumns + ` FROM waits WHERE ended_at IS NOT NULL AND headless = 0`)
	if err != nil {
		return WaitStats{}, err
	}
	defer rows.Close()

	var s WaitStats
	var durations []time.Duration
	weekAgo := now.Add(-7 * 24 * time.Hour)
	y, mo, d := now.Local().Date()
	midnight := time.Date(y, mo, d, 0, 0, 0, 0, time.Local)
	for rows.Next() {
		w, err := scanWait(rows)
		if err != nil {
			return s, err
		}
		work := w.Working(now)
		s.Count++
		s.Total += work
		s.Blocked += w.NeedsInput
		durations = append(durations, work)
		if w.Shown {
			s.Shown++
		}
		if w.StartedAt.After(weekAgo) {
			s.Last7Count++
			s.Last7Total += work
		}
		if !w.StartedAt.Before(midnight) {
			s.TodayCount++
			s.Today += work
		}
	}
	if err := rows.Err(); err != nil {
		return s, err
	}
	if n := len(durations); n > 0 {
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		if n%2 == 1 {
			s.Median = durations[n/2]
		} else {
			s.Median = (durations[n/2-1] + durations[n/2]) / 2
		}
	}
	return s, nil
}

// FormatDuration renders durations the way people say them: "42s", "3m 05s",
// "1h 12m".
func FormatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	sec := int(d.Seconds()) % 60
	switch {
	case h > 0:
		return itoa(h) + "h " + pad(m) + "m"
	case m > 0:
		return itoa(m) + "m " + pad(sec) + "s"
	default:
		return itoa(sec) + "s"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

func pad(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}
