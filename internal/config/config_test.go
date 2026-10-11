package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "none.toml"))
	if err != nil || c.MinWaitSeconds != 4 || c.Bell != BellNeedsInput || c.StillWorkingMinutes != 2 {
		t.Errorf("got %+v %v", c, err)
	}
}

func TestLoadOverrides(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte(`
min_wait_seconds = 0
bell = "never"
ignore_dirs = ["/work/ci"]
[deeper]
model = "haiku"
`), 0o600)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.MinWaitSeconds != 0 || c.Bell != BellNever || c.Deeper.Model != "haiku" {
		t.Errorf("got %+v", c)
	}
	if !c.Ignored("/work/ci/job") || !c.Ignored("/work/ci") || c.Ignored("/work/cig") {
		t.Error("Ignored wrong")
	}
}

func TestLoadInvalid(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte(`bell = "loud"`), 0o600)
	c, err := Load(p)
	if err == nil || c.Bell != BellNeedsInput {
		t.Errorf("got %+v %v", c, err)
	}
	os.WriteFile(p, []byte(`min_wait_seconds = `), 0o600)
	if _, err := Load(p); err == nil {
		t.Error("syntax error not reported")
	}
}
