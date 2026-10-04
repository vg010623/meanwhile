// Package learn is the curriculum activity: short first-principles cards
// about AI, organised as a graph of concepts with prerequisites.
package learn

import (
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/vg010623/meanwhile/content"
	"github.com/vg010623/meanwhile/internal/activity"
	"github.com/vg010623/meanwhile/internal/curriculum"
	"github.com/vg010623/meanwhile/internal/store"
)

// Activity implements activity.Activity.
type Activity struct {
	ctx    activity.Context
	cur    *curriculum.Curriculum
	graph  *graph
	store  progressStore
	notice string // e.g. "your content folder has errors; using built-in lessons"

	// deeper runs "Go deeper"; replaceable in tests. Set in Init.
	deeper Deeper
}

// New creates the activity. Content is loaded in Init.
func New() *Activity { return &Activity{} }

func (a *Activity) ID() string                    { return "learn" }
func (a *Activity) Name() string                  { return "Learn AI" }
func (a *Activity) Migrations() []store.Migration { return migrations }
func (a *Activity) Close() error                  { return nil }

// Init loads the curriculum: from config's content_dir if set and valid,
// otherwise the lessons built into the binary.
func (a *Activity) Init(ctx activity.Context) error {
	a.ctx = ctx
	if a.ctx.Now == nil {
		a.ctx.Now = time.Now
	}
	a.store = progressStore{db: ctx.DB, now: a.ctx.Now}
	if a.deeper == nil {
		a.deeper = newClaudeDeeper(ctx)
	}

	if dir := ctx.Config.ContentDir; dir != "" {
		cur, issues := curriculum.Load(os.DirFS(dir), ".")
		if !curriculum.HasErrors(issues) && len(cur.Concepts) > 0 {
			a.setCurriculum(cur)
			return nil
		}
		a.notice = fmt.Sprintf("Your content folder (%s) has errors; using the built-in lessons. Run `meanwhile content check --dir %s`.", dir, dir)
	}
	return a.load(content.FS, content.Dir)
}

func (a *Activity) load(fsys fs.FS, dir string) error {
	cur, issues := curriculum.Load(fsys, dir)
	if curriculum.HasErrors(issues) {
		return fmt.Errorf("built-in curriculum is invalid: %v", issues[0])
	}
	a.setCurriculum(cur)
	return nil
}

func (a *Activity) setCurriculum(cur *curriculum.Curriculum) {
	a.cur = cur
	a.graph = newGraph(cur)
}

// Start opens a view for a wait (or for an on-demand lesson when w is empty).
func (a *Activity) Start(w activity.WaitInfo) activity.View {
	v := &view{a: a, wait: w}
	v.home()
	return v
}

// Stats returns lines for `meanwhile stats`.
func (a *Activity) Stats() ([]string, error) {
	prog, err := a.store.all()
	if err != nil {
		return nil, err
	}
	completed, known := 0, 0
	for _, p := range prog {
		if _, ok := a.cur.Concept(p.ConceptID); !ok {
			continue // a concept removed from the curriculum
		}
		switch p.Status {
		case Completed:
			completed++
		case Known:
			known++
		}
	}
	var cards, dives int
	if err := a.ctx.DB.QueryRow(`SELECT COUNT(*) FROM learn_card_reads`).Scan(&cards); err != nil {
		return nil, err
	}
	if err := a.ctx.DB.QueryRow(`SELECT COUNT(*) FROM learn_deep_dives WHERE status = 'complete'`).Scan(&dives); err != nil {
		return nil, err
	}
	return []string{
		"Learning",
		fmt.Sprintf("  Concepts completed  %d of %d", completed, len(a.cur.Concepts)),
		fmt.Sprintf("  Marked \"I know this\" %d", known),
		fmt.Sprintf("  Cards read          %d", cards),
		fmt.Sprintf("  Deeper explanations %d", dives),
	}, nil
}
