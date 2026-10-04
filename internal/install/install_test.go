package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const bin = "/usr/local/bin/meanwhile"

// A settings file shaped like a real one: an existing user hook on the same
// event we also use (PreToolUse), unrelated keys, two-space indent.
const userSettings = `{
  "model": "opus",
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "rtk hook claude"
          }
        ]
      }
    ]
  },
  "theme": "dark",
  "permissions": {
    "allow": ["Bash(git status)", "Read(<docs>&more)"]
  },
  "cleanupPeriodDays": 30
}
`

func writeSettings(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "settings.json")
	if content != "" {
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func setup(t *testing.T, p string) {
	t.Helper()
	r, err := Plan(p, bin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(p, r, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func uninstall(t *testing.T, p string) Result {
	t.Helper()
	r, err := PlanUninstall(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(p, r, time.Now()); err != nil {
		t.Fatal(err)
	}
	return r
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSetupThenUninstallIsByteIdentical(t *testing.T) {
	p := writeSettings(t, userSettings)
	setup(t, p)

	after := read(t, p)
	if !strings.Contains(after, "rtk hook claude") {
		t.Fatal("user hook was lost")
	}
	if strings.Count(after, Marker) != len(Specs) {
		t.Fatalf("want %d meanwhile hooks, got %d", len(Specs), strings.Count(after, Marker))
	}
	// Key order preserved: model before hooks before theme.
	if !(strings.Index(after, `"model"`) < strings.Index(after, `"hooks"`) && strings.Index(after, `"hooks"`) < strings.Index(after, `"theme"`)) {
		t.Error("top-level key order changed")
	}

	uninstall(t, p)
	// The permissions array was written inline in the original, so it comes
	// back expanded; compare after normalising that one difference.
	want := strings.Replace(userSettings, `["Bash(git status)", "Read(<docs>&more)"]`, "[\n      \"Bash(git status)\",\n      \"Read(<docs>&more)\"\n    ]", 1)
	if got := read(t, p); got != want {
		t.Errorf("round trip changed the file:\n%s", Diff([]byte(want), []byte(got), 2))
	}
}

func TestRoundTripOfCanonicalFileIsExact(t *testing.T) {
	canonical := "{\n  \"model\": \"opus\",\n  \"n\": 1.50,\n  \"empty\": {},\n  \"list\": []\n}"
	p := writeSettings(t, canonical)
	setup(t, p)
	uninstall(t, p)
	if got := read(t, p); got != canonical {
		t.Errorf("got %q, want %q", got, canonical)
	}
}

func TestSetupIsIdempotent(t *testing.T) {
	p := writeSettings(t, userSettings)
	setup(t, p)
	first := read(t, p)
	r, err := Plan(p, bin)
	if err != nil {
		t.Fatal(err)
	}
	if r.Changed {
		t.Errorf("second setup would change the file:\n%s", Diff(r.Before, r.After, 2))
	}
	if read(t, p) != first {
		t.Error("file changed")
	}
}

func TestSetupReplacesOldBinaryPath(t *testing.T) {
	p := writeSettings(t, userSettings)
	setup(t, p)
	r, err := Plan(p, "/opt/new/meanwhile")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(r.After), bin) || strings.Count(string(r.After), Marker) != len(Specs) {
		t.Errorf("old entries not replaced:\n%s", r.After)
	}
}

func TestSetupOnMissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nested", "settings.json")
	setup(t, p)
	present, missing, err := Installed(p)
	if err != nil || len(missing) != 0 || len(present) != len(Specs) {
		t.Fatalf("present=%v missing=%v err=%v", present, missing, err)
	}
	r := uninstall(t, p)
	if r.Removed != len(Specs) {
		t.Errorf("removed %d", r.Removed)
	}
	if got := read(t, p); got != "{}\n" {
		t.Errorf("got %q", got)
	}
}

func TestInvalidJSONIsRefused(t *testing.T) {
	p := writeSettings(t, `{"model": "opus",}`)
	if _, err := Plan(p, bin); !errors.Is(err, ErrInvalidSettings) {
		t.Errorf("Plan: got %v", err)
	}
	if _, err := PlanUninstall(p); !errors.Is(err, ErrInvalidSettings) {
		t.Errorf("PlanUninstall: got %v", err)
	}
}

func TestUninstallAfterUserEdits(t *testing.T) {
	p := writeSettings(t, userSettings)
	setup(t, p)
	// The user moved one of our entries into their own group and changed its
	// timeout, and added their own hook next to it.
	edited := strings.Replace(read(t, p), `"command": "rtk hook claude"`,
		`"command": "rtk hook claude"
          },
          {
            "type": "command",
            "command": "`+strings.ReplaceAll(Command(bin), `"`, `\"`)+`",
            "timeout": 30`, 1)
	if err := os.WriteFile(p, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	r := uninstall(t, p)
	if r.Removed != len(Specs)+1 {
		t.Errorf("removed %d entries, want %d", r.Removed, len(Specs)+1)
	}
	got := read(t, p)
	if strings.Contains(got, Marker) {
		t.Errorf("marker still present:\n%s", got)
	}
	if !strings.Contains(got, "rtk hook claude") {
		t.Error("user hook removed")
	}
}

func TestUninstallWithNothingInstalled(t *testing.T) {
	p := writeSettings(t, userSettings)
	r := uninstall(t, p)
	if r.Changed || read(t, p) != userSettings {
		t.Error("uninstall changed a file with none of our hooks")
	}
	missing := filepath.Join(t.TempDir(), "none.json")
	if r, err := PlanUninstall(missing); err != nil || r.Changed {
		t.Errorf("missing file: %v %v", r, err)
	}
}

func TestApplyWritesBackup(t *testing.T) {
	p := writeSettings(t, userSettings)
	r, _ := Plan(p, bin)
	backup, err := Apply(p, r, time.Date(2026, 10, 4, 9, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(backup, ".meanwhile-backup-20261004-093000") || read(t, backup) != userSettings {
		t.Errorf("backup %q wrong", backup)
	}
}

func TestCommandQuotesPath(t *testing.T) {
	got := Command("/Users/o'neil/bin/meanwhile")
	want := `[ -x '/Users/o'\''neil/bin/meanwhile' ] && '/Users/o'\''neil/bin/meanwhile' hook 2>/dev/null; exit 0 # meanwhile hook`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if !strings.Contains(got, Marker) {
		t.Error("command must contain the marker")
	}
}

func TestDiff(t *testing.T) {
	d := Diff([]byte("a\nb\nc\n"), []byte("a\nx\nc\n"), 1)
	if d != "  a\n- b\n+ x\n  c\n" {
		t.Errorf("got %q", d)
	}
}
