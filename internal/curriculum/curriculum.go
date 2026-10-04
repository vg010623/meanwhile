// Package curriculum loads concept files and checks that they form a valid
// learning graph. It knows nothing about the UI or about user progress.
package curriculum

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// Concept is one topic in the curriculum, split into short cards.
type Concept struct {
	ID            string
	Title         string
	Summary       string
	Prerequisites []string
	Track         string
	Order         int
	File          string // file name the concept was loaded from
	Cards         []Card
}

// Card is a chunk of a concept meant to be read in roughly 20 to 40 seconds.
type Card struct {
	ID    string // stable ID from {#anchor}, or a slug of the title
	Title string
	Body  string // Markdown, without the heading and the recap line
	Recap string // one-line summary shown when resuming
	Words int
}

// Curriculum is the full set of concepts, sorted by ID.
type Curriculum struct {
	Concepts []*Concept
	byID     map[string]*Concept
}

// Concept returns the concept with the given ID.
func (c *Curriculum) Concept(id string) (*Concept, bool) {
	con, ok := c.byID[id]
	return con, ok
}

// Severity says whether an issue blocks the content (Error) or is advice (Warning).
type Severity int

const (
	Error Severity = iota
	Warning
)

func (s Severity) String() string {
	if s == Error {
		return "error"
	}
	return "warning"
}

// Issue is one problem found while loading or validating content.
type Issue struct {
	Severity Severity
	File     string
	Message  string
}

func (i Issue) String() string {
	if i.File == "" {
		return fmt.Sprintf("%s: %s", i.Severity, i.Message)
	}
	return fmt.Sprintf("%s: %s: %s", i.Severity, i.File, i.Message)
}

// HasErrors reports whether any issue is an Error.
func HasErrors(issues []Issue) bool {
	for _, i := range issues {
		if i.Severity == Error {
			return true
		}
	}
	return false
}

// Load reads every *.md file in dir, parses it and validates the whole graph.
// It returns every concept that could be parsed, plus all issues found, so an
// author sees every problem in one run instead of fixing them one at a time.
func Load(fsys fs.FS, dir string) (*Curriculum, []Issue) {
	var issues []Issue
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return &Curriculum{byID: map[string]*Concept{}}, []Issue{{Severity: Error, Message: fmt.Sprintf("cannot read content directory: %v", err)}}
	}

	var concepts []*Concept
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			issues = append(issues, Issue{Error, e.Name(), fmt.Sprintf("cannot read file: %v", err)})
			continue
		}
		c, parseIssues := parseConcept(e.Name(), data)
		issues = append(issues, parseIssues...)
		if c != nil {
			concepts = append(concepts, c)
		}
	}

	sort.Slice(concepts, func(i, j int) bool { return concepts[i].ID < concepts[j].ID })
	cur := &Curriculum{Concepts: concepts, byID: map[string]*Concept{}}
	issues = append(issues, validate(cur)...)
	return cur, issues
}
