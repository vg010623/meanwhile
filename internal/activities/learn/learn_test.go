package learn

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vg010623/meanwhile/internal/activity"
	"github.com/vg010623/meanwhile/internal/config"
	"github.com/vg010623/meanwhile/internal/core"
	"github.com/vg010623/meanwhile/internal/deeper"
	"github.com/vg010623/meanwhile/internal/paths"
	"github.com/vg010623/meanwhile/internal/store"
)

// Test curriculum: a → b → c, and a → d. b unlocks more than d.
func testContent() fstest.MapFS {
	fs := fstest.MapFS{}
	add := func(id, title, prereqs string, order int) {
		var b strings.Builder
		fmt.Fprintf(&b, "---\nid: %s\ntitle: %s\nsummary: About %s.\nprerequisites: [%s]\ntrack: basics\norder: %d\n---\n", id, title, title, prereqs, order)
		for i := 1; i <= 3; i++ {
			fmt.Fprintf(&b, "## %s card %d {#%s-%d}\nBody of %s card %d. %s\n\n> Recap: Recap of %s card %d.\n\n", title, i, id, i, title, i, strings.Repeat("word ", 60), title, i)
		}
		fs["concepts/"+id+".md"] = &fstest.MapFile{Data: []byte(b.String())}
	}
	add("a", "Alpha", "", 10)
	add("b", "Beta", "a", 20)
	add("c", "Gamma", "b", 30)
	add("d", "Delta", "a", 5)
	return fs
}

type fakeDeeper struct {
	reqs    []deeper.Request
	respond func(req deeper.Request) []deeper.Event
}

func (f *fakeDeeper) Ask(ctx context.Context, req deeper.Request) <-chan deeper.Event {
	f.reqs = append(f.reqs, req)
	ch := make(chan deeper.Event, 32)
	evs := []deeper.Event{{Kind: deeper.Text, Text: "Deeper answer."}, {Kind: deeper.Done}}
	if f.respond != nil {
		evs = f.respond(req)
	}
	for _, e := range evs {
		ch <- e
	}
	close(ch)
	return ch
}

type fixture struct {
	t  *testing.T
	a  *Activity
	db *store.DB
	fd *fakeDeeper
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	db, _, err := store.Open(filepath.Join(dir, "state.db"), append(append([]store.Migration{}, core.Migrations...), migrations...), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	clock := time.Date(2026, 10, 4, 10, 0, 0, 0, time.UTC)
	a := New()
	fd := &fakeDeeper{}
	a.deeper = fd
	a.ctx = activity.Context{DB: db, Config: config.Default(), Paths: paths.Paths{State: dir}, Now: func() time.Time {
		clock = clock.Add(time.Second) // strictly increasing, so "most recent" is well defined
		return clock
	}}
	a.store = progressStore{db: db, now: a.ctx.Now}
	if err := a.load(testContent(), "concepts"); err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, a: a, db: db, fd: fd}
}

func (f *fixture) start() *view { return f.a.Start(activity.WaitInfo{WaitID: "w1"}).(*view) }

// press sends keys and runs any commands they return (to drain streams).
func press(v *view, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		run(v, v.Update(msg))
	}
}

// run executes a command chain until it ends, feeding messages to the view.
func run(v *view, cmd tea.Cmd) {
	for i := 0; cmd != nil && i < 100; i++ {
		msg := cmd()
		if _, isDeeper := msg.(deeperMsg); !isDeeper {
			return // e.g. cursor blink: not part of the stream
		}
		cmd = v.Update(msg)
	}
}

func screenText(v *view) string { return v.Render(80, 40) }

func TestGraphRecommend(t *testing.T) {
	f := newFixture(t)
	g := f.a.graph
	prog := map[string]Progress{}
	if got := titles(g.recommend(prog, "", 5)); got != "Alpha" {
		t.Errorf("start: %s", got)
	}
	prog["a"] = Progress{Status: Completed}
	// Beta unlocks Gamma, so it comes before Delta despite Delta's lower order.
	if got := titles(g.recommend(prog, "", 5)); got != "Beta,Delta" {
		t.Errorf("after a: %s", got)
	}
	if got := titles(g.recommend(prog, "b", 5)); got != "Delta" {
		t.Errorf("exclude: %s", got)
	}
	c, _ := f.a.cur.Concept("c")
	if got := titles(g.unmet(c, prog)); got != "Beta" {
		t.Errorf("unmet: %s", got)
	}
}

func titles(cs []*curriculumConcept) string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Title)
	}
	return strings.Join(out, ",")
}

func TestReadAConcept(t *testing.T) {
	f := newFixture(t)
	v := f.start()
	if v.screen != scrPicker || !strings.Contains(screenText(v), "Alpha") {
		t.Fatalf("expected picker:\n%s", screenText(v))
	}
	press(v, "enter")
	if v.screen != scrCard || !strings.Contains(screenText(v), "card 1 of 3") {
		t.Fatalf("expected card 1:\n%s", screenText(v))
	}
	press(v, "enter", "enter")
	if !strings.Contains(screenText(v), "Alpha card 3") {
		t.Fatalf("expected card 3:\n%s", screenText(v))
	}
	press(v, "enter")
	s := screenText(v)
	if v.screen != scrComplete || !strings.Contains(s, "Alpha complete") || !strings.Contains(s, "Beta") || !strings.Contains(s, "Delta") {
		t.Fatalf("expected completion with unlocks:\n%s", s)
	}
	var reads int
	f.db.QueryRow(`SELECT COUNT(*) FROM learn_card_reads WHERE wait_id = 'w1'`).Scan(&reads)
	prog, _ := f.a.store.all()
	if reads != 3 || prog["a"].Status != Completed {
		t.Errorf("reads=%d status=%s", reads, prog["a"].Status)
	}
}

func TestResumeShowsContinueWithRecap(t *testing.T) {
	f := newFixture(t)
	v := f.start()
	press(v, "enter", "enter") // now on Alpha card 2
	v.Save()

	v2 := f.start()
	s := screenText(v2)
	if v2.screen != scrHome || !strings.Contains(s, "Continue: Alpha") || !strings.Contains(s, "card 2 of 3") || !strings.Contains(s, "Recap of Alpha card 1") {
		t.Fatalf("home:\n%s", s)
	}
	press(v2, "enter")
	if !strings.Contains(screenText(v2), "Alpha card 2") {
		t.Errorf("did not resume at card 2:\n%s", screenText(v2))
	}
}

func TestResumeSurvivesReorderedCards(t *testing.T) {
	f := newFixture(t)
	f.a.store.setPosition("a", "a-3", 0) // index is stale; ID wins
	v := f.start()
	if v.contIdx != 2 {
		t.Errorf("contIdx = %d", v.contIdx)
	}
	f.a.store.setPosition("a", "deleted-card", 1) // ID gone; index is the fallback
	if v := f.start(); v.contIdx != 1 {
		t.Errorf("fallback contIdx = %d", v.contIdx)
	}
}

func TestSoftPrerequisiteNote(t *testing.T) {
	f := newFixture(t)
	v := f.start()
	press(v, "b")
	if v.screen != scrBrowse {
		t.Fatal("not browsing")
	}
	// Move to Gamma (rows: header, Delta, Alpha, Beta, Gamma by order).
	press(v, "down", "down", "down")
	if v.rows[v.cursor].c.ID != "c" {
		t.Fatalf("cursor on %s", v.rows[v.cursor].c.ID)
	}
	press(v, "enter")
	if v.screen != scrNote || !strings.Contains(screenText(v), "Builds on: Beta (not done yet)") {
		t.Fatalf("note:\n%s", screenText(v))
	}
	press(v, "enter") // start anyway: never blocked
	if v.screen != scrCard || v.concept.ID != "c" {
		t.Errorf("did not start Gamma")
	}
}

func TestIKnowThis(t *testing.T) {
	f := newFixture(t)
	v := f.start()
	press(v, "i") // on Alpha in the picker
	if got := titles(v.list); got != "Beta,Delta" {
		t.Errorf("picker after known: %s", got)
	}
	press(v, "b")
	if !strings.Contains(screenText(v), "Alpha (known)") {
		t.Errorf("browse:\n%s", screenText(v))
	}
	// Undo from browse.
	for v.rows[v.cursor].c.ID != "a" {
		press(v, "down")
	}
	press(v, "i")
	if prog, _ := f.a.store.all(); prog["a"].Status != "" {
		t.Errorf("not undone: %s", prog["a"].Status)
	}

	// From a card: marks known and shows what's unlocked.
	v = f.start()
	press(v, "enter", "i")
	if v.screen != scrComplete || !strings.Contains(screenText(v), "marked as known") {
		t.Errorf("card i:\n%s", screenText(v))
	}
}

func TestEverythingDone(t *testing.T) {
	f := newFixture(t)
	for _, id := range []string{"a", "b", "c", "d"} {
		f.a.store.setStatus(id, Known)
	}
	v := f.start()
	if !strings.Contains(screenText(v), "covered everything") {
		t.Errorf("picker:\n%s", screenText(v))
	}
}

func TestStats(t *testing.T) {
	f := newFixture(t)
	v := f.start()
	press(v, "enter", "enter", "enter", "enter") // complete Alpha
	f.a.store.setStatus("d", Known)
	lines, err := f.a.Stats()
	if err != nil {
		t.Fatal(err)
	}
	out := strings.Join(lines, "\n")
	for _, want := range []string{"completed  1 of 4", "I know this\" 1", "Cards read          3"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestInvalidContentDirFallsBack(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "broken.md"), []byte("no front matter"), 0o644)
	db, _, _ := store.Open(filepath.Join(t.TempDir(), "s.db"), append(append([]store.Migration{}, core.Migrations...), migrations...), time.Now())
	defer db.Close()
	cfg := config.Default()
	cfg.ContentDir = dir
	a := New()
	if err := a.Init(activity.Context{DB: db, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	if a.notice == "" || len(a.cur.Concepts) == 0 {
		t.Errorf("notice=%q concepts=%d", a.notice, len(a.cur.Concepts))
	}
}

func TestRenderMarkdown(t *testing.T) {
	out := renderMarkdown("First **bold** and `code` here.\n\n- one\n- two that is long enough to wrap around the narrow width we give it\n\n```\nx := 1\n```", 40)
	for _, want := range []string{"bold", "code", "• one", "x := 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if w := len([]rune(stripANSI(line))); w > 40 {
			t.Errorf("line too wide (%d): %q", w, line)
		}
	}
}
