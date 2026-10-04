package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContentCheckEmbedded(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"content", "check"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "0 errors") {
		t.Errorf("unexpected output: %s", out.String())
	}
}

func TestContentCheckFailsOnCycle(t *testing.T) {
	dir := t.TempDir()
	card := "## Card {#card-%d}\n" + strings.Repeat("word ", 80) + "\n\n"
	write := func(id, prereq string) {
		body := "---\nid: " + id + "\ntitle: T\nsummary: S\nprerequisites: [" + prereq + "]\n---\n"
		for i := 1; i <= 3; i++ {
			body += strings.Replace(card, "%d", string(rune('0'+i)), 1)
		}
		if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a", "b")
	write("b", "a")

	var out, errOut bytes.Buffer
	if code := run([]string{"content", "check", "--dir", dir}, &out, &errOut); code != 1 {
		t.Fatalf("exit %d, want 1\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "prerequisite cycle: a → b → a") {
		t.Errorf("cycle not reported:\n%s", out.String())
	}
}
