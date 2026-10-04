package curriculum

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
)

// words returns n filler words so test cards land inside the word limits.
func words(n int) string {
	return strings.TrimSpace(strings.Repeat("word ", n))
}

// conceptMD builds a valid concept file with the given number of cards.
func conceptMD(id string, prereqs []string, cards int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\nid: %s\ntitle: Title %s\nsummary: Summary of %s.\n", id, id, id)
	if len(prereqs) > 0 {
		fmt.Fprintf(&b, "prerequisites: [%s]\n", strings.Join(prereqs, ", "))
	}
	b.WriteString("---\n\n")
	for i := 1; i <= cards; i++ {
		fmt.Fprintf(&b, "## Card %d {#card-%d}\n%s.\n\n", i, i, words(80))
	}
	return b.String()
}

func load(t *testing.T, files map[string]string) (*Curriculum, []Issue) {
	t.Helper()
	fsys := fstest.MapFS{}
	for name, body := range files {
		fsys["concepts/"+name] = &fstest.MapFile{Data: []byte(body)}
	}
	return Load(fsys, "concepts")
}

func TestParseConcept(t *testing.T) {
	md := `---
id: tokens
title: Tokens
summary: How text becomes pieces.
prerequisites: [what-is-a-model]
track: foundations
order: 20
---

## Text becomes numbers {#text-becomes-numbers}
Models never see letters. They see numbers.

> Recap: Models only see token IDs.

## Why not whole words?
First sentence here. Second one.

` + "```markdown\n## Not a card\n```\n"

	c, issues := parseConcept("tokens.md", []byte(md))
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	if c.ID != "tokens" || c.Track != "foundations" || c.Order != 20 || c.Prerequisites[0] != "what-is-a-model" {
		t.Errorf("front matter parsed wrong: %+v", c)
	}
	if len(c.Cards) != 2 {
		t.Fatalf("got %d cards, want 2 (heading inside a code fence must not start a card)", len(c.Cards))
	}
	first, second := c.Cards[0], c.Cards[1]
	if first.ID != "text-becomes-numbers" || first.Recap != "Models only see token IDs." {
		t.Errorf("first card: %+v", first)
	}
	if strings.Contains(first.Body, "Recap") {
		t.Errorf("recap line should be removed from the body: %q", first.Body)
	}
	if second.ID != "why-not-whole-words" {
		t.Errorf("slug fallback: got %q", second.ID)
	}
	if second.Recap != "First sentence here." {
		t.Errorf("first-sentence fallback: got %q", second.Recap)
	}
}

func TestValidContentHasNoIssues(t *testing.T) {
	cur, issues := load(t, map[string]string{
		"a.md": conceptMD("a", nil, 3),
		"b.md": conceptMD("b", []string{"a"}, 4),
		"c.md": conceptMD("c", []string{"a", "b"}, 5),
	})
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	if len(cur.Concepts) != 3 {
		t.Fatalf("got %d concepts", len(cur.Concepts))
	}
	if _, ok := cur.Concept("b"); !ok {
		t.Error("lookup by id failed")
	}
}

func TestValidationIssues(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string // substring expected in one issue
		sev   Severity
	}{
		{
			name:  "missing prerequisite",
			files: map[string]string{"a.md": conceptMD("a", []string{"ghost"}, 3)},
			want:  `prerequisite "ghost" does not exist`,
		},
		{
			name: "two-node cycle",
			files: map[string]string{
				"a.md": conceptMD("a", []string{"b"}, 3),
				"b.md": conceptMD("b", []string{"a"}, 3),
			},
			want: "prerequisite cycle: a → b → a",
		},
		{
			name: "three-node cycle reached from outside",
			files: map[string]string{
				"root.md": conceptMD("root", []string{"x"}, 3),
				"x.md":    conceptMD("x", []string{"y"}, 3),
				"y.md":    conceptMD("y", []string{"z"}, 3),
				"z.md":    conceptMD("z", []string{"x"}, 3),
			},
			want: "prerequisite cycle: x → y → z → x",
		},
		{
			name:  "self prerequisite",
			files: map[string]string{"a.md": conceptMD("a", []string{"a"}, 3)},
			want:  "lists itself as a prerequisite",
		},
		{
			name:  "too few cards",
			files: map[string]string{"a.md": conceptMD("a", nil, 2)},
			want:  "has 2 cards",
		},
		{
			name:  "too many cards",
			files: map[string]string{"a.md": conceptMD("a", nil, 6)},
			want:  "has 6 cards",
		},
		{
			name:  "id does not match file name",
			files: map[string]string{"wrong.md": conceptMD("a", nil, 3)},
			want:  "does not match the file name",
		},
		{
			name:  "missing front matter",
			files: map[string]string{"a.md": "## Card\nhello\n"},
			want:  "missing front matter",
		},
		{
			name:  "unknown front matter field",
			files: map[string]string{"a.md": strings.Replace(conceptMD("a", nil, 3), "title:", "prerequisite: [x]\ntitle:", 1)},
			want:  "invalid front matter",
		},
		{
			name:  "empty card",
			files: map[string]string{"a.md": conceptMD("a", nil, 3) + "## Empty {#empty}\n\n"},
			want:  "is empty",
		},
		{
			name:  "duplicate card id",
			files: map[string]string{"a.md": strings.Replace(conceptMD("a", nil, 3), "{#card-2}", "{#card-1}", 1)},
			want:  `duplicates card id "card-1"`,
		},
		{
			name:  "short card is a warning",
			files: map[string]string{"a.md": strings.Replace(conceptMD("a", nil, 3), words(80), "too short", 1)},
			want:  "has 2 words",
			sev:   Warning,
		},
		{
			name: "isolated concept is a warning",
			files: map[string]string{
				"a.md":    conceptMD("a", nil, 3),
				"b.md":    conceptMD("b", []string{"a"}, 3),
				"lone.md": conceptMD("lone", nil, 3),
			},
			want: "is isolated",
			sev:  Warning,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, issues := load(t, tt.files)
			for _, i := range issues {
				if strings.Contains(i.Message, tt.want) {
					if i.Severity != tt.sev {
						t.Errorf("issue %q has severity %s, want %s", i.Message, i.Severity, tt.sev)
					}
					return
				}
			}
			t.Errorf("no issue containing %q; got %v", tt.want, issues)
		})
	}
}

func TestCycleReportedOnce(t *testing.T) {
	_, issues := load(t, map[string]string{
		"a.md": conceptMD("a", []string{"b"}, 3),
		"b.md": conceptMD("b", []string{"a"}, 3),
	})
	n := 0
	for _, i := range issues {
		if strings.Contains(i.Message, "cycle") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("cycle reported %d times, want 1: %v", n, issues)
	}
}
