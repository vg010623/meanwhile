package learn

import "testing"

// must(t)(f.a.Next()) fails the test on an error and returns the lesson.
func must(t *testing.T) func(Lesson, error) Lesson {
	return func(l Lesson, err error) Lesson {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
}

func TestLessonFreshStartShowsPicker(t *testing.T) {
	f := newFixture(t)
	l := must(t)(f.a.Current())
	if l.Screen != ScreenPicker || len(l.Picks) == 0 {
		t.Fatalf("got %+v", l)
	}
	// a unlocks the most, so it comes first; b and c need a.
	if l.Picks[0].ID != "a" {
		t.Errorf("first pick = %s", l.Picks[0].ID)
	}
	for _, p := range l.Picks {
		if p.ID == "b" || p.ID == "c" {
			t.Errorf("%s offered before its prerequisite", p.ID)
		}
	}
}

func TestLessonReadThroughAndResume(t *testing.T) {
	f := newFixture(t)
	l := must(t)(f.a.StartConcept("a"))
	if l.Screen != ScreenCard || l.Card.Index != 0 || l.Card.Total != 3 || l.Card.ID != "a-1" {
		t.Fatalf("start: %+v %+v", l, l.Card)
	}
	l = must(t)(f.a.Next())
	if l.Card.ID != "a-2" {
		t.Fatalf("next: %+v", l.Card)
	}

	// A later call (a new process in real use) resumes at card 2 with the
	// recap of card 1.
	l = must(t)(f.a.Current())
	if l.Screen != ScreenCard || l.Card.ID != "a-2" || l.LastTime != "Recap of Alpha card 1." {
		t.Fatalf("resume: %+v %+v", l, l.Card)
	}

	l = must(t)(f.a.Back())
	if l.Card.ID != "a-1" {
		t.Fatalf("back: %+v", l.Card)
	}
	l = must(t)(f.a.Back()) // already at the first card: stays
	if l.Card.ID != "a-1" {
		t.Fatalf("back at start: %+v", l.Card)
	}

	must(t)(f.a.Next())
	must(t)(f.a.Next())
	l = must(t)(f.a.Next()) // past the last card
	if l.Screen != ScreenComplete || l.Known || l.Concept.ID != "a" {
		t.Fatalf("complete: %+v", l)
	}
	if got := titles(infosToConcepts(f, l.Unlocked)); got != "Beta,Delta" {
		t.Errorf("unlocked = %s", got)
	}
	prog, _ := f.a.store.all()
	if prog["a"].Status != Completed {
		t.Errorf("status = %s", prog["a"].Status)
	}
	var reads int
	f.db.QueryRow(`SELECT COUNT(*) FROM learn_card_reads WHERE concept_id = 'a'`).Scan(&reads)
	if reads != 4 { // a-1, then after going back a-1 again, a-2, a-3
		t.Errorf("card reads = %d", reads)
	}

	// Nothing in progress now: the picker again.
	if l := must(t)(f.a.Current()); l.Screen != ScreenPicker {
		t.Errorf("after completing: %s", l.Screen)
	}
}

func TestLessonMarkKnown(t *testing.T) {
	f := newFixture(t)
	must(t)(f.a.StartConcept("a"))
	l := must(t)(f.a.MarkKnown())
	if l.Screen != ScreenComplete || !l.Known {
		t.Fatalf("got %+v", l)
	}
	prog, _ := f.a.store.all()
	if prog["a"].Status != Known {
		t.Errorf("status = %s", prog["a"].Status)
	}
	if l := must(t)(f.a.Current()); l.Screen != ScreenPicker {
		t.Errorf("after known: %s", l.Screen)
	}
}

func TestLessonUnmetPrerequisiteIsANoteNotABlock(t *testing.T) {
	f := newFixture(t)
	l := must(t)(f.a.StartConcept("c"))
	if l.Screen != ScreenCard || l.Card.ID != "c-1" {
		t.Fatalf("got %+v", l)
	}
	if len(l.Unmet) != 1 || l.Unmet[0].ID != "b" {
		t.Errorf("unmet = %+v", l.Unmet)
	}
	if _, err := f.a.StartConcept("nope"); err == nil {
		t.Error("unknown concept accepted")
	}
}

func TestLessonSharesProgressWithTerminalApp(t *testing.T) {
	f := newFixture(t)
	// The terminal app left Beta in progress at its last card.
	f.a.store.setPosition("b", "b-3", 2)
	l := must(t)(f.a.Current())
	if l.Screen != ScreenCard || l.Card.ID != "b-3" {
		t.Fatalf("got %+v", l.Card)
	}
	// And the terminal app sees where the panel got to.
	must(t)(f.a.StartConcept("d"))
	must(t)(f.a.Next())
	p, ok, _ := f.a.store.current()
	if !ok || p.ConceptID != "d" || p.CardID != "d-2" {
		t.Errorf("terminal app would continue at %+v", p)
	}
}

func TestLessonRereadCompletedConceptKeepsPlace(t *testing.T) {
	f := newFixture(t)
	f.a.store.setStatus("d", Completed)
	must(t)(f.a.StartConcept("d"))
	must(t)(f.a.Next())
	l := must(t)(f.a.Current())
	if l.Screen != ScreenCard || l.Card.ID != "d-2" {
		t.Fatalf("got %+v", l.Card)
	}
	prog, _ := f.a.store.all()
	if prog["d"].Status != Completed {
		t.Errorf("re-reading changed the status to %s", prog["d"].Status)
	}
}

func infosToConcepts(f *fixture, infos []ConceptInfo) []*curriculumConcept {
	var out []*curriculumConcept
	for _, i := range infos {
		c, _ := f.a.cur.Concept(i.ID)
		out = append(out, c)
	}
	return out
}

func TestLessonEverythingDoneListsAll(t *testing.T) {
	f := newFixture(t)
	for _, id := range []string{"a", "b", "c", "d"} {
		f.a.store.setStatus(id, Known)
	}
	l := must(t)(f.a.Current())
	if l.Screen != ScreenPicker || len(l.Picks) != 0 || len(l.All) != 4 || !l.All[0].Done {
		t.Fatalf("got %+v", l)
	}
	if l := must(t)(f.a.StartConcept("a")); l.Card.ID != "a-1" {
		t.Errorf("re-read starts at %s", l.Card.ID)
	}
}
