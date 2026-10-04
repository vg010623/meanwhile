package main

import (
	"os"
	"time"

	"github.com/vg010623/meanwhile/internal/hook"
	"github.com/vg010623/meanwhile/internal/paths"
)

// hookDeadline is how long the hook may take before giving up. Claude waits
// for synchronous hooks, so this bounds the delay we can ever add.
const hookDeadline = 500 * time.Millisecond

// runHook never writes to stdout and never reports failure to Claude: errors
// go to a small log file that `meanwhile doctor` points at.
func runHook() {
	now := time.Now()
	p, err := paths.Default()
	if err != nil {
		return
	}
	if err := hook.RunWithDeadline(os.Stdin, p.Spool(), now, hookDeadline); err != nil {
		hook.LogError(p.HookLog(), now, err)
	}
}
