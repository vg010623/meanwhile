package hook

import "testing"

func TestIsClaudePrint(t *testing.T) {
	tests := map[string]bool{
		"claude -p hello": true,
		"/Users/me/.local/bin/claude --print --model haiku": true,
		"node /usr/lib/node_modules/claude -p x":            true,
		"claude":                                            false,
		"claude --resume abc":                               false,
		"/bin/sh -c meanwhile hook":                         false,
		"grep -p claude":                                    false,
	}
	for args, want := range tests {
		if got := isClaudePrint(args); got != want {
			t.Errorf("%q: got %v, want %v", args, got, want)
		}
	}
}

func TestHeadlessAncestor(t *testing.T) {
	procs := map[int]struct {
		ppid int
		args string
	}{
		100: {90, "/bin/sh -c '/bin/meanwhile' hook"},
		90:  {80, "claude -p do things"},
		80:  {1, "zsh"},
		50:  {40, "/bin/sh -c x"},
		40:  {1, "claude"},
	}
	lookup := func(pid int) (int, string, bool) {
		p, ok := procs[pid]
		return p.ppid, p.args, ok
	}
	if !headlessAncestor(100, lookup, 4) {
		t.Error("grandparent claude -p not detected")
	}
	if headlessAncestor(50, lookup, 4) {
		t.Error("interactive claude reported as headless")
	}
	if headlessAncestor(100, lookup, 1) {
		t.Error("depth limit ignored")
	}
}
