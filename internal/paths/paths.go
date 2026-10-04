// Package paths decides where meanwhile keeps its files.
package paths

import (
	"os"
	"path/filepath"
)

// Paths are the directories meanwhile reads and writes. Nothing else on disk
// is touched, apart from Claude Code's settings file during setup/uninstall.
type Paths struct {
	State  string // app-owned data: spool, database, sandbox
	Config string // files a human may edit
}

// Default resolves paths from the environment:
//   - MEANWHILE_HOME puts everything under one folder (used by tests and for
//     trying meanwhile without touching real data);
//   - otherwise XDG_STATE_HOME / XDG_CONFIG_HOME, falling back to
//     ~/.local/state/meanwhile and ~/.config/meanwhile on every OS.
func Default() (Paths, error) {
	if home := os.Getenv("MEANWHILE_HOME"); home != "" {
		return Paths{State: filepath.Join(home, "state"), Config: filepath.Join(home, "config")}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		state = filepath.Join(home, ".local", "state")
	}
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	return Paths{State: filepath.Join(state, "meanwhile"), Config: filepath.Join(config, "meanwhile")}, nil
}

func (p Paths) Spool() string      { return filepath.Join(p.State, "spool") }
func (p Paths) DB() string         { return filepath.Join(p.State, "state.db") }
func (p Paths) Sandbox() string    { return filepath.Join(p.State, "sandbox") }
func (p Paths) HookLog() string    { return filepath.Join(p.State, "hook-errors.log") }
func (p Paths) UILock() string     { return filepath.Join(p.State, "ui.lock") }
func (p Paths) IngestLock() string { return filepath.Join(p.State, "ingest.lock") }
func (p Paths) ConfigFile() string { return filepath.Join(p.Config, "config.toml") }

// ClaudeSettings is the user-scope Claude Code settings file, honouring
// CLAUDE_CONFIG_DIR the same way Claude Code does.
func ClaudeSettings() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "settings.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}
