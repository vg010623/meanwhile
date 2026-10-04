package curriculum

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type frontMatter struct {
	ID            string   `yaml:"id"`
	Title         string   `yaml:"title"`
	Summary       string   `yaml:"summary"`
	Prerequisites []string `yaml:"prerequisites"`
	Track         string   `yaml:"track"`
	Order         int      `yaml:"order"`
}

var (
	// "## Title {#card-id}" with the anchor optional.
	headingRe = regexp.MustCompile(`^##\s+(.*?)\s*(?:\{#([^}]*)\})?\s*$`)
	recapRe   = regexp.MustCompile(`^>\s*Recap:\s*(.*)$`)
	nonSlugRe = regexp.MustCompile(`[^a-z0-9]+`)
)

// parseConcept turns one concept file into a Concept. It returns nil only when
// the file is unusable (no front matter, or front matter that won't decode).
func parseConcept(file string, data []byte) (*Concept, []Issue) {
	var issues []Issue
	text := strings.ReplaceAll(string(data), "\r\n", "\n")

	if !strings.HasPrefix(text, "---\n") {
		return nil, []Issue{{Error, file, "missing front matter: the file must start with a line containing only ---"}}
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		if strings.HasSuffix(rest, "\n---") {
			end = len(rest) - len("\n---")
		} else {
			return nil, []Issue{{Error, file, "front matter is not closed: add a line containing only --- after it"}}
		}
	}
	rawFM := rest[:end]
	body := ""
	if end+len("\n---\n") <= len(rest) {
		body = rest[end+len("\n---\n"):]
	}

	var fm frontMatter
	dec := yaml.NewDecoder(bytes.NewBufferString(rawFM))
	dec.KnownFields(true) // catch typos such as "prerequisite:"
	if err := dec.Decode(&fm); err != nil {
		return nil, []Issue{{Error, file, fmt.Sprintf("invalid front matter: %v", err)}}
	}

	c := &Concept{
		ID:            fm.ID,
		Title:         strings.TrimSpace(fm.Title),
		Summary:       strings.TrimSpace(fm.Summary),
		Prerequisites: fm.Prerequisites,
		Track:         fm.Track,
		Order:         fm.Order,
		File:          file,
	}
	cards, cardIssues := parseCards(file, body)
	c.Cards = cards
	issues = append(issues, cardIssues...)
	return c, issues
}

// parseCards splits the Markdown body on "## " headings. Headings inside fenced
// code blocks are ignored so authors can show Markdown examples.
func parseCards(file, body string) ([]Card, []Issue) {
	var (
		issues   []Issue
		cards    []Card
		cur      *Card
		lines    []string
		preamble bool
		inFence  bool
	)
	flush := func() {
		if cur == nil {
			return
		}
		cur.Body = strings.TrimSpace(strings.Join(lines, "\n"))
		cur.Words = len(strings.Fields(cur.Body))
		if cur.Recap == "" {
			cur.Recap = truncate(firstSentence(cur.Body), MaxRecapChars)
		}
		cards = append(cards, *cur)
		cur, lines = nil, nil
	}

	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
		}
		if !inFence && strings.HasPrefix(line, "## ") {
			flush()
			m := headingRe.FindStringSubmatch(line)
			title, id := strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
			if id == "" {
				id = slugify(title)
			}
			cur = &Card{ID: id, Title: title}
			continue
		}
		if cur == nil {
			if strings.TrimSpace(line) != "" {
				preamble = true
			}
			continue
		}
		if m := recapRe.FindStringSubmatch(line); m != nil && !inFence {
			cur.Recap = strings.TrimSpace(m[1])
			continue
		}
		lines = append(lines, line)
	}
	flush()

	if preamble {
		issues = append(issues, Issue{Warning, file, "text before the first ## card heading is ignored"})
	}
	return cards, issues
}

func slugify(s string) string {
	return strings.Trim(nonSlugRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// firstSentence is the fallback recap when a card has no "> Recap:" line.
func firstSentence(body string) string {
	para := strings.Join(strings.Fields(strings.SplitN(body, "\n\n", 2)[0]), " ")
	if i := strings.IndexAny(para, ".!?"); i >= 0 {
		return para[:i+1]
	}
	return para
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max-1])) + "…"
}
