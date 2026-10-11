// Package config reads the optional ~/.config/meanwhile/config.toml. Every
// setting has a default, so the file never needs to exist.
package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Bell settings.
const (
	BellNeedsInput = "needs_input" // ring when Claude is blocked on you (default)
	BellAlways     = "always"      // also ring when Claude finishes
	BellNever      = "never"
)

// Config holds the user's preferences.
type Config struct {
	// MinWaitSeconds: the UI appears only if Claude is still working after
	// this long, so quick answers never flash a card.
	MinWaitSeconds int `toml:"min_wait_seconds"`
	// StillWorkingMinutes: with no sign of life for this long, ask "Still
	// working?". Esc in Claude fires no hook at all, so this nudge is how an
	// interrupted run gets noticed. A long silent tool call (a slow test run)
	// can trigger it too; pressing nothing just leaves it showing.
	StillWorkingMinutes int    `toml:"still_working_minutes"`
	Bell                string `toml:"bell"`
	// ContentDir loads concept files from disk instead of the built-in ones.
	ContentDir string `toml:"content_dir"`
	// IgnoreDirs: waits started under these folders never wake the UI.
	IgnoreDirs []string `toml:"ignore_dirs"`
	Deeper     Deeper   `toml:"deeper"`
}

// Deeper configures "Go deeper".
type Deeper struct {
	// Model overrides the model chosen on the consent screen.
	Model string `toml:"model"`
}

// Default returns the built-in settings.
func Default() Config {
	return Config{MinWaitSeconds: 4, StillWorkingMinutes: 2, Bell: BellNeedsInput}
}

// Load reads path over the defaults. A missing file is not an error. An
// invalid file returns the defaults plus the error, so the app still runs
// and can show what's wrong.
func Load(path string) (Config, error) {
	c := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	var file Config
	md, err := toml.Decode(string(data), &file)
	if err != nil {
		return c, err
	}
	if md.IsDefined("min_wait_seconds") && file.MinWaitSeconds >= 0 {
		c.MinWaitSeconds = file.MinWaitSeconds
	}
	if md.IsDefined("still_working_minutes") && file.StillWorkingMinutes > 0 {
		c.StillWorkingMinutes = file.StillWorkingMinutes
	}
	switch file.Bell {
	case BellNeedsInput, BellAlways, BellNever:
		c.Bell = file.Bell
	case "":
	default:
		return c, errors.New(`bell must be "needs_input", "always" or "never"`)
	}
	c.ContentDir = expandHome(file.ContentDir)
	for _, d := range file.IgnoreDirs {
		c.IgnoreDirs = append(c.IgnoreDirs, expandHome(d))
	}
	c.Deeper = file.Deeper
	return c, nil
}

// Ignored reports whether dir is inside one of IgnoreDirs.
func (c Config) Ignored(dir string) bool {
	for _, ig := range c.IgnoreDirs {
		ig = filepath.Clean(ig)
		if dir == ig || strings.HasPrefix(dir, ig+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}
