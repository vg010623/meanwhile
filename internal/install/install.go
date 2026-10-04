// Package install adds and removes meanwhile's hooks in Claude Code's
// settings file without disturbing anything else in it.
package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Marker identifies our hook commands. Every command we write ends with it
// as a shell comment, so it is present whatever the binary path looks like.
// Uninstall removes any hook whose command contains it, wherever the entry
// has ended up.
const Marker = "# meanwhile hook"

// Spec is one hook registration.
type Spec struct {
	Event   string
	Matcher string // empty means "match everything"
}

// Specs lists every hook meanwhile listens to. See internal/events for what
// each one means.
var Specs = []Spec{
	{"SessionStart", ""},
	{"UserPromptSubmit", ""},
	{"PermissionRequest", ""},
	{"PreToolUse", "AskUserQuestion"},
	{"PostToolUse", ""},
	{"PostToolUseFailure", ""},
	{"Elicitation", ""},
	{"Notification", "permission_prompt|elicitation_dialog|elicitation_url_dialog|idle_prompt"},
	{"Stop", ""},
	{"StopFailure", ""},
	{"SessionEnd", ""},
}

// hookTimeoutSeconds is Claude's limit for our hook. The binary gives up on
// its own after 500 ms, so this is a backstop only.
const hookTimeoutSeconds = 5

// Command builds the shell command for a given binary path. If the binary is
// later deleted without running uninstall, the command does nothing and
// still exits 0, so Claude never sees an error.
func Command(bin string) string {
	q := "'" + strings.ReplaceAll(bin, "'", `'\''`) + "'"
	return fmt.Sprintf("[ -x %s ] && %s hook 2>/dev/null; exit 0 %s", q, q, Marker)
}

// Result describes a change to the settings file.
type Result struct {
	Before, After []byte
	Changed       bool
	Removed       int // hook entries removed (uninstall, or replaced on setup)
}

// ErrInvalidSettings means the file is not valid JSON; we refuse to touch it.
var ErrInvalidSettings = errors.New("settings file is not valid JSON")

// Plan computes what setup would write, without writing it.
func Plan(settingsPath, bin string) (Result, error) {
	before, root, err := load(settingsPath)
	if err != nil {
		return Result{}, err
	}
	removed := removeOurs(root)
	hooks := ensureObject(root, "hooks")
	for _, s := range Specs {
		group := newObject()
		if s.Matcher != "" {
			group.set("matcher", s.Matcher)
		}
		h := newObject()
		h.set("type", "command")
		h.set("command", Command(bin))
		h.set("timeout", json.Number(fmt.Sprint(hookTimeoutSeconds)))
		group.set("hooks", []any{h})

		arr, _ := hooks.get(s.Event)
		list, _ := arr.([]any)
		hooks.set(s.Event, append(list, group))
	}
	after := render(root, before)
	return Result{Before: before, After: after, Changed: string(before) != string(after), Removed: removed}, nil
}

// PlanUninstall computes the settings file with all our hooks removed.
func PlanUninstall(settingsPath string) (Result, error) {
	before, root, err := load(settingsPath)
	if err != nil {
		return Result{}, err
	}
	if before == nil {
		return Result{}, nil // no settings file: nothing to do
	}
	removed := removeOurs(root)
	if removed == 0 {
		return Result{Before: before, After: before, Removed: 0}, nil
	}
	after := render(root, before)
	return Result{Before: before, After: after, Changed: true, Removed: removed}, nil
}

// Installed reports which of our Specs are present in the settings file.
func Installed(settingsPath string) (present []string, missing []string, err error) {
	_, root, err := load(settingsPath)
	if err != nil {
		return nil, nil, err
	}
	have := map[string]bool{}
	if hooks, ok := root.get("hooks"); ok {
		if h, ok := hooks.(*object); ok {
			for _, event := range h.keys {
				if countOurs(h.vals[event]) > 0 {
					have[event] = true
				}
			}
		}
	}
	for _, s := range Specs {
		if have[s.Event] {
			present = append(present, s.Event)
		} else {
			missing = append(missing, s.Event)
		}
	}
	return present, missing, nil
}

// ContainsMarker reports whether a settings file mentions our hook at all.
// Used by doctor to spot copies in project-level settings.
func ContainsMarker(path string) bool {
	data, err := os.ReadFile(path)
	return err == nil && strings.Contains(string(data), Marker)
}

// Apply writes r.After to path: first a timestamped backup of the old file,
// then a temporary file that is renamed over the original, so a crash can
// never leave a half-written settings file. It returns the backup path.
func Apply(path string, r Result, now time.Time) (backup string, err error) {
	if !r.Changed {
		return "", nil
	}
	mode := fs.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if r.Before != nil {
		backup = fmt.Sprintf("%s.meanwhile-backup-%s", path, now.Format("20060102-150405"))
		if err := os.WriteFile(backup, r.Before, mode); err != nil {
			return "", fmt.Errorf("write backup: %w", err)
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*.json")
	if err != nil {
		return backup, err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(r.After); err != nil {
		tmp.Close()
		return backup, err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return backup, err
	}
	if err := tmp.Close(); err != nil {
		return backup, err
	}
	return backup, os.Rename(tmp.Name(), path)
}

// load reads and parses the settings file. A missing file is an empty object.
func load(path string) ([]byte, *object, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, newObject(), nil
	}
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return data, newObject(), nil
	}
	v, err := parseJSON(data)
	if err != nil {
		return nil, nil, fmt.Errorf("%w (%s): %v", ErrInvalidSettings, path, err)
	}
	root, ok := v.(*object)
	if !ok {
		return nil, nil, fmt.Errorf("%w (%s): top level must be an object", ErrInvalidSettings, path)
	}
	return data, root, nil
}

// render formats root, keeping the original file's trailing-newline style.
func render(root *object, before []byte) []byte {
	out := formatJSON(root)
	if before == nil || strings.HasSuffix(string(before), "\n") {
		out = append(out, '\n')
	}
	return out
}

func ensureObject(parent *object, key string) *object {
	if v, ok := parent.get(key); ok {
		if o, ok := v.(*object); ok {
			return o
		}
	}
	o := newObject()
	parent.set(key, o)
	return o
}

// removeOurs deletes every hook entry carrying our marker, then removes any
// group, event list or "hooks" object that we left empty. Entries belonging
// to the user are never touched, even inside a group we share.
func removeOurs(root *object) int {
	hv, ok := root.get("hooks")
	if !ok {
		return 0
	}
	hooks, ok := hv.(*object)
	if !ok {
		return 0
	}
	removed := 0
	for _, event := range append([]string{}, hooks.keys...) {
		groups, ok := hooks.vals[event].([]any)
		if !ok {
			continue
		}
		var keptGroups []any
		for _, g := range groups {
			group, ok := g.(*object)
			if !ok {
				keptGroups = append(keptGroups, g)
				continue
			}
			entries, ok := group.vals["hooks"].([]any)
			if !ok {
				keptGroups = append(keptGroups, g)
				continue
			}
			var kept []any
			for _, e := range entries {
				if isOurs(e) {
					removed++
					continue
				}
				kept = append(kept, e)
			}
			if len(kept) == len(entries) {
				keptGroups = append(keptGroups, g)
			} else if len(kept) > 0 {
				group.set("hooks", kept)
				keptGroups = append(keptGroups, g)
			}
		}
		if len(keptGroups) == 0 {
			hooks.del(event)
		} else {
			hooks.set(event, keptGroups)
		}
	}
	if len(hooks.keys) == 0 && removed > 0 {
		root.del("hooks")
	}
	return removed
}

func countOurs(v any) int {
	n := 0
	groups, _ := v.([]any)
	for _, g := range groups {
		if group, ok := g.(*object); ok {
			entries, _ := group.vals["hooks"].([]any)
			for _, e := range entries {
				if isOurs(e) {
					n++
				}
			}
		}
	}
	return n
}

func isOurs(e any) bool {
	o, ok := e.(*object)
	if !ok {
		return false
	}
	cmd, _ := o.vals["command"].(string)
	return strings.Contains(cmd, Marker)
}
