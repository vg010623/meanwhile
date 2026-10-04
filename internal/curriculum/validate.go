package curriculum

import (
	"fmt"
	"regexp"
	"strings"
)

// Limits for a card that fits inside a wait. At about 200 words per minute,
// 60 to 140 words takes roughly 20 to 40 seconds to read.
const (
	MinCards      = 3
	MaxCards      = 5
	MinCardWords  = 60
	MaxCardWords  = 140
	MaxRecapChars = 120
)

var idRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// validate checks each concept on its own, then the graph as a whole, and
// fills cur.byID. Concepts with duplicate IDs keep the first one seen.
func validate(cur *Curriculum) []Issue {
	var issues []Issue
	add := func(sev Severity, file, format string, args ...any) {
		issues = append(issues, Issue{sev, file, fmt.Sprintf(format, args...)})
	}

	for _, c := range cur.Concepts {
		switch {
		case c.ID == "":
			add(Error, c.File, "front matter is missing id")
		case !idRe.MatchString(c.ID):
			add(Error, c.File, "id %q must be lowercase letters, digits and single hyphens", c.ID)
		case c.ID+".md" != c.File:
			add(Error, c.File, "id %q does not match the file name; rename the file to %s.md", c.ID, c.ID)
		}
		if other, dup := cur.byID[c.ID]; dup && c.ID != "" {
			add(Error, c.File, "duplicate concept id %q (also in %s)", c.ID, other.File)
		} else if c.ID != "" {
			cur.byID[c.ID] = c
		}
		if c.Title == "" {
			add(Error, c.File, "front matter is missing title")
		}
		if c.Summary == "" {
			add(Error, c.File, "front matter is missing summary")
		}

		if n := len(c.Cards); n < MinCards || n > MaxCards {
			add(Error, c.File, "has %d cards; a concept needs %d to %d", n, MinCards, MaxCards)
		}
		seenCards := map[string]bool{}
		for i, card := range c.Cards {
			label := fmt.Sprintf("card %d (%q)", i+1, card.Title)
			if card.ID == "" {
				add(Error, c.File, "%s has no usable id; add {#some-id} to the heading", label)
			} else if !idRe.MatchString(card.ID) {
				add(Error, c.File, "%s id %q must be lowercase letters, digits and single hyphens", label, card.ID)
			} else if seenCards[card.ID] {
				add(Error, c.File, "%s duplicates card id %q", label, card.ID)
			}
			seenCards[card.ID] = true
			if card.Words == 0 {
				add(Error, c.File, "%s is empty", label)
				continue
			}
			if card.Words < MinCardWords || card.Words > MaxCardWords {
				add(Warning, c.File, "%s has %d words; aim for %d to %d (about 20 to 40 seconds)", label, card.Words, MinCardWords, MaxCardWords)
			}
			if len([]rune(card.Recap)) > MaxRecapChars {
				add(Warning, c.File, "%s recap is %d characters; keep it under %d", label, len([]rune(card.Recap)), MaxRecapChars)
			}
		}

		seenPre := map[string]bool{}
		for _, p := range c.Prerequisites {
			if p == c.ID {
				add(Error, c.File, "lists itself as a prerequisite")
			}
			if seenPre[p] {
				add(Warning, c.File, "lists prerequisite %q twice", p)
			}
			seenPre[p] = true
		}
	}

	// Graph-level checks need the full ID index, so they run after the loop.
	dependents := map[string]int{}
	for _, c := range cur.Concepts {
		for _, p := range c.Prerequisites {
			if _, ok := cur.byID[p]; !ok {
				add(Error, c.File, "prerequisite %q does not exist", p)
				continue
			}
			dependents[p]++
		}
	}
	for _, cycle := range findCycles(cur) {
		add(Error, "", "prerequisite cycle: %s", strings.Join(cycle, " → "))
	}
	if len(cur.byID) > 1 {
		for _, c := range cur.Concepts {
			if len(c.Prerequisites) == 0 && dependents[c.ID] == 0 {
				add(Warning, c.File, "is isolated: it has no prerequisites and nothing builds on it")
			}
		}
	}
	return issues
}

// findCycles runs a depth-first search over prerequisite edges. A "gray" node
// is on the current path; reaching a gray node again means we walked in a
// circle, and the path from that node to here is the cycle. Each cycle is
// reported once, rotated to start at its smallest ID.
func findCycles(cur *Curriculum) [][]string {
	const (
		white = iota // not visited
		gray         // on the current path
		black        // fully explored
	)
	color := map[string]int{}
	var (
		stack  []string
		cycles [][]string
		seen   = map[string]bool{}
	)

	var visit func(id string)
	visit = func(id string) {
		color[id] = gray
		stack = append(stack, id)
		for _, p := range cur.byID[id].Prerequisites {
			if _, ok := cur.byID[p]; !ok || p == id {
				continue // reported elsewhere
			}
			switch color[p] {
			case white:
				visit(p)
			case gray:
				start := len(stack) - 1
				for stack[start] != p {
					start--
				}
				cycle := canonicalCycle(stack[start:])
				if key := strings.Join(cycle, ","); !seen[key] {
					seen[key] = true
					cycles = append(cycles, cycle)
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[id] = black
	}

	for _, c := range cur.Concepts {
		if cur.byID[c.ID] == c && color[c.ID] == white {
			visit(c.ID)
		}
	}
	return cycles
}

// canonicalCycle rotates a cycle to start at its smallest ID and closes it,
// so a→b→a and b→a→b are reported the same way.
func canonicalCycle(path []string) []string {
	min := 0
	for i, id := range path {
		if id < path[min] {
			min = i
		}
	}
	out := append(append([]string{}, path[min:]...), path[:min]...)
	return append(out, out[0])
}
