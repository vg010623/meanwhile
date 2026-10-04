package events

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRecordedPayloads runs the mapping over real payloads captured from
// Claude Code 2.1.286 (see docs/claude-code-behaviour.md). If Claude changes
// its payload shape, this is the test that should catch it.
func TestRecordedPayloads(t *testing.T) {
	want := map[string]Kind{
		"SessionStart":     SessionStarted,
		"UserPromptSubmit": WaitStarted,
		"PostToolUse":      InputResolved,
		"Stop":             WaitEnded,
		"SessionEnd":       SessionEnded,
	}
	files, _ := filepath.Glob("../../testdata/hook-payloads/*.json")
	if len(files) == 0 {
		t.Fatal("no recorded payloads found")
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var p HookPayload
		if err := json.Unmarshal(data, &p); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		e, ok := FromHook(p, time.Now())
		wantKind, interesting := want[name]
		if !interesting {
			if ok {
				t.Errorf("%s: should be ignored, got %s", name, e.Kind)
			}
			continue
		}
		if !ok || e.Kind != wantKind {
			t.Errorf("%s: got %q ok=%v, want %q", name, e.Kind, ok, wantKind)
		}
		if e.SessionID == "" || e.CWD != "/Users/dev/project" {
			t.Errorf("%s: session/cwd not read: %+v", name, e)
		}
	}
}
