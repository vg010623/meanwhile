// Package events defines the small set of events the core understands, and
// how raw Claude Code hook payloads map onto them. This is the stable
// contract between the hook and everything else.
package events

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Version of the spool line format. Bump it when fields change meaning.
const Version = 1

// Kind is what happened, in Claude-agnostic terms.
type Kind string

const (
	WaitStarted    Kind = "wait_started"    // the user sent a prompt; Claude is working
	WaitEnded      Kind = "wait_ended"      // Claude stopped working (see EndReason)
	NeedsInput     Kind = "needs_input"     // Claude is blocked on the user
	InputResolved  Kind = "input_resolved"  // Claude did something again (also a heartbeat)
	SessionStarted Kind = "session_started" // a Claude session started or resumed
	SessionEnded   Kind = "session_ended"   // a Claude session exited
)

// End reasons carried by WaitEnded (and assigned by the core when it closes a
// wait for other reasons).
const (
	EndCompleted  = "completed"     // Claude finished its turn
	EndError      = "error"         // the turn failed (rate limit, API error…)
	EndIdle       = "idle"          // Claude reported it is idle; usually after an interrupt
	EndSuperseded = "superseded"    // a new prompt arrived while the old wait was open
	EndSession    = "session_ended" // the session exited
	EndStale      = "stale"         // found open long after the fact (app was not running)
	EndManual     = "manual"        // the user pressed "Claude's done" in the UI
)

// Input kinds carried by NeedsInput.
const (
	InputPermission  = "permission"
	InputQuestion    = "question"
	InputElicitation = "elicitation"
)

// Event is one line in the spool.
type Event struct {
	V            int       `json:"v"`
	ID           string    `json:"id"`
	TS           time.Time `json:"ts"`
	Kind         Kind      `json:"kind"`
	SessionID    string    `json:"session_id"`
	CWD          string    `json:"cwd,omitempty"`
	WorktreeRoot string    `json:"worktree_root,omitempty"`
	SourceHook   string    `json:"source_hook,omitempty"`
	EndReason    string    `json:"end_reason,omitempty"`
	ErrorType    string    `json:"error_type,omitempty"`
	InputKind    string    `json:"input_kind,omitempty"`
	// Headless is true when the prompt came from a non-interactive
	// `claude -p` run (a script or CI job). Such waits are logged but the UI
	// does not wake for them.
	Headless bool `json:"headless,omitempty"`
}

// HookPayload holds the fields we read from a hook's stdin JSON. Everything
// else (including the prompt text) is ignored and never stored.
type HookPayload struct {
	SessionID        string `json:"session_id"`
	CWD              string `json:"cwd"`
	HookEventName    string `json:"hook_event_name"`
	NotificationType string `json:"notification_type"`
	ToolName         string `json:"tool_name"`
	ErrorType        string `json:"error_type"`
	Error            string `json:"error"`
}

// FromHook maps a hook payload to an event. ok is false for hooks we don't
// care about, which the caller simply ignores.
func FromHook(p HookPayload, now time.Time) (e Event, ok bool) {
	e = Event{V: Version, ID: NewID(now), TS: now.UTC(), SessionID: p.SessionID, CWD: p.CWD, SourceHook: p.HookEventName}
	switch p.HookEventName {
	case "UserPromptSubmit":
		e.Kind = WaitStarted
	case "Stop":
		e.Kind, e.EndReason = WaitEnded, EndCompleted
	case "StopFailure":
		e.Kind, e.EndReason = WaitEnded, EndError
		e.ErrorType = p.ErrorType
		if e.ErrorType == "" {
			e.ErrorType = p.Error
		}
	case "PermissionRequest":
		e.Kind, e.InputKind = NeedsInput, InputPermission
	case "PreToolUse":
		if p.ToolName != "AskUserQuestion" {
			return Event{}, false
		}
		e.Kind, e.InputKind = NeedsInput, InputQuestion
	case "Elicitation":
		e.Kind, e.InputKind = NeedsInput, InputElicitation
	case "Notification":
		switch p.NotificationType {
		case "permission_prompt":
			e.Kind, e.InputKind = NeedsInput, InputPermission
		case "elicitation_dialog", "elicitation_url_dialog":
			e.Kind, e.InputKind = NeedsInput, InputElicitation
		case "idle_prompt":
			e.Kind, e.EndReason = WaitEnded, EndIdle
		default:
			return Event{}, false
		}
	case "PostToolUse", "PostToolUseFailure":
		e.Kind = InputResolved
	case "SessionStart":
		e.Kind = SessionStarted
	case "SessionEnd":
		e.Kind = SessionEnded
	default:
		return Event{}, false
	}
	if e.SessionID == "" {
		return Event{}, false
	}
	e.WorktreeRoot = WorktreeRoot(p.CWD)
	return e, true
}

// NewID returns an ID that sorts by time: 16 hex digits of nanoseconds plus 8
// random hex digits so two events in the same nanosecond still differ.
func NewID(t time.Time) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%016x%08x", uint64(t.UnixNano()), binary.BigEndian.Uint32(b[:]))
}

// WorktreeRoot walks up from dir to the nearest folder containing .git (a
// directory in a normal checkout, a file in a git worktree). It returns dir
// itself when there is no repository, and never runs git.
func WorktreeRoot(dir string) string {
	if dir == "" {
		return ""
	}
	for d := filepath.Clean(dir); ; {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return dir
		}
		d = parent
	}
}
