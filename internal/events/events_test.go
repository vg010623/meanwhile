package events

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFromHook(t *testing.T) {
	now := time.Date(2026, 10, 4, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		p         HookPayload
		ok        bool
		kind      Kind
		endReason string
		inputKind string
	}{
		{"prompt", HookPayload{HookEventName: "UserPromptSubmit"}, true, WaitStarted, "", ""},
		{"stop", HookPayload{HookEventName: "Stop"}, true, WaitEnded, EndCompleted, ""},
		{"stop failure", HookPayload{HookEventName: "StopFailure", ErrorType: "rate_limit"}, true, WaitEnded, EndError, ""},
		{"permission", HookPayload{HookEventName: "PermissionRequest"}, true, NeedsInput, "", InputPermission},
		{"ask question", HookPayload{HookEventName: "PreToolUse", ToolName: "AskUserQuestion"}, true, NeedsInput, "", InputQuestion},
		{"other pre tool use", HookPayload{HookEventName: "PreToolUse", ToolName: "Bash"}, false, "", "", ""},
		{"elicitation", HookPayload{HookEventName: "Elicitation"}, true, NeedsInput, "", InputElicitation},
		{"notification permission", HookPayload{HookEventName: "Notification", NotificationType: "permission_prompt"}, true, NeedsInput, "", InputPermission},
		{"notification idle", HookPayload{HookEventName: "Notification", NotificationType: "idle_prompt"}, true, WaitEnded, EndIdle, ""},
		{"notification auth", HookPayload{HookEventName: "Notification", NotificationType: "auth_success"}, false, "", "", ""},
		{"post tool", HookPayload{HookEventName: "PostToolUse"}, true, InputResolved, "", ""},
		{"post tool failure", HookPayload{HookEventName: "PostToolUseFailure"}, true, InputResolved, "", ""},
		{"session start", HookPayload{HookEventName: "SessionStart"}, true, SessionStarted, "", ""},
		{"session end", HookPayload{HookEventName: "SessionEnd"}, true, SessionEnded, "", ""},
		{"subagent stop ignored", HookPayload{HookEventName: "SubagentStop"}, false, "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.p.SessionID = "s1"
			e, ok := FromHook(tt.p, now)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if !ok {
				return
			}
			if e.Kind != tt.kind || e.EndReason != tt.endReason || e.InputKind != tt.inputKind {
				t.Errorf("got %+v", e)
			}
			if e.V != Version || e.SessionID != "s1" || !e.TS.Equal(now) || e.ID == "" || e.SourceHook != tt.p.HookEventName {
				t.Errorf("common fields wrong: %+v", e)
			}
		})
	}
}

func TestFromHookRequiresSession(t *testing.T) {
	if _, ok := FromHook(HookPayload{HookEventName: "Stop"}, time.Now()); ok {
		t.Error("event without session_id should be dropped")
	}
}

func TestStopFailureErrorField(t *testing.T) {
	e, _ := FromHook(HookPayload{SessionID: "s", HookEventName: "StopFailure", Error: "overloaded"}, time.Now())
	if e.ErrorType != "overloaded" {
		t.Errorf("got %q", e.ErrorType)
	}
}

func TestNewIDSortsByTime(t *testing.T) {
	a := NewID(time.Unix(100, 0))
	b := NewID(time.Unix(100, 1))
	if !(a < b) || len(a) != 24 || NewID(time.Unix(100, 0)) == a {
		t.Errorf("ids %s %s", a, b)
	}
}

func TestWorktreeRoot(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	sub := filepath.Join(repo, "a", "b")
	wt := filepath.Join(root, "wt")
	for _, d := range []string{filepath.Join(repo, ".git"), sub, wt} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A git worktree has a .git file instead of a directory.
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := WorktreeRoot(sub); got != repo {
		t.Errorf("nested: got %s", got)
	}
	if got := WorktreeRoot(wt); got != wt {
		t.Errorf("worktree: got %s", got)
	}
	outside := filepath.Join(root, "plain")
	_ = os.MkdirAll(outside, 0o755)
	if got := WorktreeRoot(outside); !strings.HasSuffix(got, "plain") {
		t.Errorf("no repo: got %s", got)
	}
}
