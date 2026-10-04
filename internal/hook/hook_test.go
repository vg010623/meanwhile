package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vg010623/meanwhile/internal/spool"
)

func TestMain(m *testing.M) {
	IsHeadless = func() bool { return false }
	m.Run()
}

func TestRunWritesEvent(t *testing.T) {
	dir := t.TempDir()
	in := `{"session_id":"abc","cwd":"/tmp","hook_event_name":"UserPromptSubmit","prompt":"secret prompt text"}`
	if err := Run(strings.NewReader(in), dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	res, _ := spool.ReadNew(dir, nil)
	if len(res.Events) != 1 || res.Events[0].SessionID != "abc" {
		t.Fatalf("got %+v", res.Events)
	}
	data, _ := os.ReadFile(filepath.Join(dir, spool.FileFor(res.Events[0].TS)))
	if strings.Contains(string(data), "secret") {
		t.Error("prompt text must never be stored")
	}
}

func TestRunIgnoresUninterestingHooks(t *testing.T) {
	dir := t.TempDir()
	in := `{"session_id":"abc","hook_event_name":"SubagentStop"}`
	if err := Run(strings.NewReader(in), dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir + "/x"); err == nil {
		t.Fatal("unexpected")
	}
	res, _ := spool.ReadNew(dir, nil)
	if len(res.Events) != 0 {
		t.Error("ignored hook was written")
	}
}

func TestRunSkipsInternalCalls(t *testing.T) {
	t.Setenv(InternalEnv, "1")
	dir := t.TempDir()
	in := `{"session_id":"abc","hook_event_name":"UserPromptSubmit"}`
	_ = Run(strings.NewReader(in), dir, time.Now())
	res, _ := spool.ReadNew(dir, nil)
	if len(res.Events) != 0 {
		t.Error("internal call recorded")
	}
}

func TestRunBadPayload(t *testing.T) {
	if err := Run(strings.NewReader("garbage"), t.TempDir(), time.Now()); err == nil {
		t.Error("want error for logging")
	}
}

func TestLogError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "hook-errors.log")
	LogError(p, time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC), os.ErrPermission)
	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), "2026-10-04T00:00:00Z permission denied") {
		t.Errorf("got %q", data)
	}
}
