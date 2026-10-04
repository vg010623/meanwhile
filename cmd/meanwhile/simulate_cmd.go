package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/vg010623/meanwhile/internal/events"
	"github.com/vg010623/meanwhile/internal/spool"
)

// simulated maps friendly names to the hook payload Claude Code would send.
var simulated = map[string]events.HookPayload{
	"session-start": {HookEventName: "SessionStart"},
	"start":         {HookEventName: "UserPromptSubmit"},
	"input":         {HookEventName: "PermissionRequest"},
	"question":      {HookEventName: "PreToolUse", ToolName: "AskUserQuestion"},
	"resume":        {HookEventName: "PostToolUse"},
	"end":           {HookEventName: "Stop"},
	"error":         {HookEventName: "StopFailure", ErrorType: "rate_limit"},
	"idle":          {HookEventName: "Notification", NotificationType: "idle_prompt"},
	"session-end":   {HookEventName: "SessionEnd"},
}

// cmdSimulate writes a fake event to the spool so the UI can be driven by
// hand without spending Claude usage.
func cmdSimulate(args []string, e env) int {
	names := make([]string, 0, len(simulated))
	for k := range simulated {
		names = append(names, k)
	}
	sort.Strings(names)

	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintf(e.stderr, "usage: meanwhile simulate EVENT [--session ID] [--cwd DIR]\nevents: %s\n", strings.Join(names, ", "))
		return 2
	}
	name := args[0]
	fl := flag.NewFlagSet("simulate", flag.ContinueOnError)
	fl.SetOutput(e.stderr)
	session := fl.String("session", "simulated", "session ID")
	cwd := fl.String("cwd", "", "working directory (default: current)")
	if err := fl.Parse(args[1:]); err != nil {
		return 2
	}
	p, ok := simulated[name]
	if !ok {
		fmt.Fprintf(e.stderr, "unknown event %q; choose one of: %s\n", name, strings.Join(names, ", "))
		return 2
	}
	p.SessionID = *session
	p.CWD = *cwd
	if p.CWD == "" {
		p.CWD, _ = os.Getwd()
	}
	ev, _ := events.FromHook(p, e.now())
	if err := spool.Append(e.paths.Spool(), ev); err != nil {
		fmt.Fprintln(e.stderr, "meanwhile:", err)
		return 1
	}
	fmt.Fprintf(e.stdout, "wrote %s (%s) for session %s\n", ev.Kind, p.HookEventName, ev.SessionID)
	return 0
}
