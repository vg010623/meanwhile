package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/vg010623/meanwhile/content"
	"github.com/vg010623/meanwhile/internal/curriculum"
	"github.com/vg010623/meanwhile/internal/install"
	"github.com/vg010623/meanwhile/internal/spool"
)

type checkStatus int

const (
	ok checkStatus = iota
	warn
	fail
)

func (s checkStatus) symbol() string {
	return [...]string{"✓", "!", "✗"}[s]
}

// doctorCheck is one line of `meanwhile doctor` output.
type doctorCheck struct {
	status checkStatus
	msg    string
	hint   string
}

// extraDoctorChecks lets later packages (the database) add their own checks.
var extraDoctorChecks []func(e env) []doctorCheck

func cmdDoctor(args []string, e env) int {
	var checks []doctorCheck
	add := func(s checkStatus, msg, hint string) { checks = append(checks, doctorCheck{s, msg, hint}) }

	// 1. The binary the hooks run.
	if exe, err := os.Executable(); err == nil {
		add(ok, "meanwhile binary: "+exe, "")
	}

	// 2. Hooks in the user's Claude Code settings.
	settings, err := settingsPath("")
	if err != nil {
		add(fail, "cannot locate Claude Code settings: "+err.Error(), "")
	} else {
		present, missing, err := install.Installed(settings)
		switch {
		case err != nil:
			add(fail, err.Error(), "fix the JSON in that file, then run `meanwhile setup`")
		case len(present) == 0:
			add(fail, "no meanwhile hooks in "+settings, "run `meanwhile setup`")
		case len(missing) > 0:
			add(warn, fmt.Sprintf("some hooks are missing from %s: %s", settings, strings.Join(missing, ", ")), "run `meanwhile setup` again")
		default:
			add(ok, fmt.Sprintf("all %d hooks present in %s", len(present), settings), "")
		}
		if boolSetting(settings, "disableAllHooks") {
			add(fail, "disableAllHooks is true in "+settings+", so no hooks run", "remove that setting to use meanwhile")
		}
	}

	// 3. Copies of our hook in project settings, and admin policy.
	if wd, err := os.Getwd(); err == nil {
		for _, name := range []string{"settings.json", "settings.local.json"} {
			p := filepath.Join(wd, ".claude", name)
			if install.ContainsMarker(p) {
				add(warn, "meanwhile hooks also appear in "+p+"; events may be recorded twice", "remove them from that file; user settings are enough")
			}
		}
	}
	for _, p := range managedSettingsPaths() {
		if boolSetting(p, "allowManagedHooksOnly") || boolSetting(p, "disableAllHooks") {
			add(fail, "your organisation's managed settings ("+p+") block user hooks", "meanwhile can't receive Claude events on this machine")
		}
	}

	// 4. Events actually arriving.
	if last := spool.LastModified(e.paths.Spool()); last.IsZero() {
		add(warn, "no Claude events recorded yet", "send Claude a prompt, then run doctor again")
	} else if age := e.now().Sub(last); age > 24*time.Hour {
		add(warn, fmt.Sprintf("last Claude event was %s ago", age.Round(time.Hour)), "if you've used Claude since, hooks may not be firing")
	} else {
		add(ok, fmt.Sprintf("last Claude event %s ago", age.Round(time.Second)), "")
	}
	if info, err := os.Stat(e.paths.HookLog()); err == nil && info.Size() > 0 {
		add(warn, "the hook has logged errors: "+e.paths.HookLog(), "")
	}

	// 5. Claude Code itself, needed for "Go deeper".
	if p, err := exec.LookPath("claude"); err != nil {
		add(warn, "claude not found on PATH", "\"Go deeper\" needs Claude Code installed")
	} else {
		add(ok, "claude found: "+p, "")
	}

	// 6. Curriculum.
	cur, issues := curriculum.Load(content.FS, content.Dir)
	if curriculum.HasErrors(issues) {
		add(fail, "built-in curriculum has errors", "run `meanwhile content check`")
	} else {
		add(ok, fmt.Sprintf("curriculum: %d concepts", len(cur.Concepts)), "")
	}

	for _, extra := range extraDoctorChecks {
		checks = append(checks, extra(e)...)
	}

	worst := ok
	for _, c := range checks {
		fmt.Fprintf(e.stdout, "%s %s\n", c.status.symbol(), c.msg)
		if c.hint != "" {
			fmt.Fprintf(e.stdout, "    → %s\n", c.hint)
		}
		worst = max(worst, c.status)
	}
	fmt.Fprintf(e.stdout, "\nmeanwhile keeps its data in:\n  %s\n  %s\nNothing is sent anywhere except the \"Go deeper\" calls you start, which run your own `claude`.\n", e.paths.State, e.paths.Config)
	if worst == fail {
		return 1
	}
	return 0
}

func boolSetting(path, key string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return false
	}
	v, _ := m[key].(bool)
	return v
}

func managedSettingsPaths() []string {
	if runtime.GOOS == "darwin" {
		return []string{"/Library/Application Support/ClaudeCode/managed-settings.json"}
	}
	return []string{"/etc/claude-code/managed-settings.json"}
}
