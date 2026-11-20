package main

import (
	"encoding/json"
	"fmt"

	"github.com/vg010623/meanwhile/internal/activities/learn"
	"github.com/vg010623/meanwhile/internal/config"
)

const lessonUsage = `Usage: meanwhile lesson current|next|back|known|pick|start ID

Prints the lesson screen as JSON, for the Claude Code panel. next, back,
known and start save progress, shared with the terminal app.
`

// cmdLesson runs one lesson action and prints the result as JSON. Errors are
// JSON too ({"error": "..."}, exit 1), so the panel can always parse stdout.
func cmdLesson(args []string, e env) int {
	if len(args) == 0 {
		fmt.Fprint(e.stderr, lessonUsage)
		return 2
	}
	fail := func(err error) int {
		out, _ := json.Marshal(map[string]string{"error": err.Error()})
		fmt.Fprintln(e.stdout, string(out))
		return 1
	}

	cfg, _ := config.Load(e.paths.ConfigFile()) // a bad config file falls back to defaults
	db, err := openDB(e)
	if err != nil {
		return fail(err)
	}
	defer db.Close()
	l := learn.New()
	if err := l.Init(activityContext(e, db, cfg)); err != nil {
		return fail(err)
	}

	var res learn.Lesson
	switch args[0] {
	case "current":
		res, err = l.Current()
	case "next":
		res, err = l.Next()
	case "back":
		res, err = l.Back()
	case "known":
		res, err = l.MarkKnown()
	case "pick":
		res, err = l.Pick()
	case "start":
		if len(args) < 2 {
			fmt.Fprint(e.stderr, lessonUsage)
			return 2
		}
		res, err = l.StartConcept(args[1])
	default:
		fmt.Fprint(e.stderr, lessonUsage)
		return 2
	}
	if err != nil {
		return fail(err)
	}
	out, err := json.Marshal(res)
	if err != nil {
		return fail(err)
	}
	fmt.Fprintln(e.stdout, string(out))
	return 0
}
