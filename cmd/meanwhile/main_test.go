package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vg010623/meanwhile/internal/install"
	"github.com/vg010623/meanwhile/internal/paths"
	"github.com/vg010623/meanwhile/internal/spool"
)

type testEnv struct {
	env
	out, errOut *bytes.Buffer
}

func newTestEnv(t *testing.T, stdin string) testEnv {
	t.Helper()
	home := t.TempDir()
	var out, errOut bytes.Buffer
	return testEnv{
		env: env{
			stdin:  strings.NewReader(stdin),
			stdout: &out,
			stderr: &errOut,
			now:    func() time.Time { return time.Date(2026, 10, 4, 10, 0, 0, 0, time.UTC) },
			paths:  paths.Paths{State: filepath.Join(home, "state"), Config: filepath.Join(home, "config")},
		},
		out:    &out,
		errOut: &errOut,
	}
}

func TestContentCheckEmbedded(t *testing.T) {
	te := newTestEnv(t, "")
	if code := run([]string{"content", "check"}, te.env); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, te.out, te.errOut)
	}
	if !strings.Contains(te.out.String(), "0 errors") {
		t.Errorf("unexpected output: %s", te.out)
	}
}

func TestContentCheckFailsOnCycle(t *testing.T) {
	dir := t.TempDir()
	write := func(id, prereq string) {
		body := "---\nid: " + id + "\ntitle: T\nsummary: S\nprerequisites: [" + prereq + "]\n---\n"
		for _, c := range []string{"one", "two", "three"} {
			body += "## Card {#" + c + "}\n" + strings.Repeat("word ", 80) + "\n\n"
		}
		if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a", "b")
	write("b", "a")

	te := newTestEnv(t, "")
	if code := run([]string{"content", "check", "--dir", dir}, te.env); code != 1 {
		t.Fatalf("exit %d, want 1\n%s", code, te.out)
	}
	if !strings.Contains(te.out.String(), "prerequisite cycle: a → b → a") {
		t.Errorf("cycle not reported:\n%s", te.out)
	}
}

func fakeBinary(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "meanwhile")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSetupAndUninstall(t *testing.T) {
	settings := filepath.Join(t.TempDir(), "settings.json")
	original := "{\n  \"model\": \"opus\"\n}\n"
	_ = os.WriteFile(settings, []byte(original), 0o644)
	bin := fakeBinary(t)

	// Declining changes nothing.
	te := newTestEnv(t, "n\n")
	if code := run([]string{"setup", "--settings", settings, "--bin", bin}, te.env); code != 1 {
		t.Fatalf("decline: exit %d\n%s", code, te.out)
	}
	if b, _ := os.ReadFile(settings); string(b) != original {
		t.Fatal("declined setup changed the file")
	}

	// Dry run shows a diff but writes nothing.
	te = newTestEnv(t, "")
	run([]string{"setup", "--settings", settings, "--bin", bin, "--dry-run"}, te.env)
	if !strings.Contains(te.out.String(), "+ ") || !strings.Contains(te.out.String(), "Dry run") {
		t.Errorf("dry run output:\n%s", te.out)
	}

	te = newTestEnv(t, "y\n")
	if code := run([]string{"setup", "--settings", settings, "--bin", bin}, te.env); code != 0 {
		t.Fatalf("setup: exit %d\n%s%s", code, te.out, te.errOut)
	}
	if _, missing, _ := install.Installed(settings); len(missing) != 0 {
		t.Fatalf("missing hooks: %v", missing)
	}

	te = newTestEnv(t, "")
	if code := run([]string{"uninstall", "--settings", settings, "--yes"}, te.env); code != 0 {
		t.Fatalf("uninstall: exit %d\n%s%s", code, te.out, te.errOut)
	}
	if b, _ := os.ReadFile(settings); string(b) != original {
		t.Errorf("uninstall did not restore the file:\n%s", b)
	}
}

func TestSetupRefusesInvalidJSON(t *testing.T) {
	settings := filepath.Join(t.TempDir(), "settings.json")
	_ = os.WriteFile(settings, []byte(`{"a": 1,}`), 0o644)
	te := newTestEnv(t, "y\n")
	if code := run([]string{"setup", "--settings", settings, "--bin", fakeBinary(t)}, te.env); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(te.errOut.String(), "won't edit a file") {
		t.Errorf("stderr: %s", te.errOut)
	}
}

func TestSimulate(t *testing.T) {
	te := newTestEnv(t, "")
	if code := run([]string{"simulate", "start", "--session", "s9"}, te.env); code != 0 {
		t.Fatalf("exit %d: %s", code, te.errOut)
	}
	res, _ := spool.ReadNew(te.paths.Spool(), nil)
	if len(res.Events) != 1 || res.Events[0].SessionID != "s9" || res.Events[0].Kind != "wait_started" {
		t.Errorf("got %+v", res.Events)
	}
	if code := run([]string{"simulate", "bogus"}, te.env); code != 2 {
		t.Errorf("bogus event: exit %d", code)
	}
}
