package learn

import "testing"

func TestDeepThreadSaveAndLoad(t *testing.T) {
	f := newFixture(t)
	if th, err := f.a.DeepThreadFor("a", "a-1"); err != nil || th.RootID != 0 || len(th.Turns) != 0 || th.MaxFollowUps != maxFollowUps {
		t.Fatalf("empty: %+v %v", th, err)
	}
	root, err := f.a.SaveDeepTurn("a", "a-1", 0, DeepTurn{Question: "Explain this card more deeply.", Answer: "first", Model: "haiku"})
	if err != nil || root == 0 {
		t.Fatal(root, err)
	}
	if r, err := f.a.SaveDeepTurn("a", "a-1", root, DeepTurn{Question: "why?", Answer: "because"}); err != nil || r != root {
		t.Fatal(r, err)
	}
	th, err := f.a.DeepThreadFor("a", "a-1")
	if err != nil || th.RootID != root || len(th.Turns) != 2 || th.Turns[1].Question != "why?" || th.Turns[0].Model != "haiku" || th.Turns[1].Status != "complete" {
		t.Fatalf("thread: %+v %v", th, err)
	}

	// "Ask again" starts a new thread; the latest one is what comes back.
	newer, _ := f.a.SaveDeepTurn("a", "a-1", 0, DeepTurn{Question: "Explain this card more deeply.", Answer: "second"})
	if th, _ := f.a.DeepThreadFor("a", "a-1"); th.RootID != newer || len(th.Turns) != 1 || th.Turns[0].Answer != "second" {
		t.Errorf("latest thread: %+v", th)
	}
	// The card now says it has one; its neighbour doesn't.
	must(t)(f.a.StartConcept("a"))
	if l := must(t)(f.a.Current()); !l.Card.HasDeeper {
		t.Error("card a-1 should report a saved explanation")
	}
	if l := must(t)(f.a.Next()); l.Card.HasDeeper {
		t.Error("card a-2 has none")
	}
	// Other cards are separate.
	if th, _ := f.a.DeepThreadFor("a", "a-2"); th.RootID != 0 {
		t.Errorf("a-2: %+v", th)
	}
	// The terminal app's saved-answers list sees panel answers.
	var n int
	f.db.QueryRow(`SELECT COUNT(*) FROM learn_deep_dives WHERE concept_id = 'a' AND parent_id IS NULL`).Scan(&n)
	if n != 2 {
		t.Errorf("roots = %d", n)
	}
}

func TestSaveDeepTurnRejectsBadInput(t *testing.T) {
	f := newFixture(t)
	for _, c := range []struct {
		concept, card string
		root          int64
		answer        string
	}{
		{"nope", "a-1", 0, "x"},
		{"a", "nope", 0, "x"},
		{"a", "a-1", 0, ""},
		{"a", "a-1", 999, "x"},
	} {
		if _, err := f.a.SaveDeepTurn(c.concept, c.card, c.root, DeepTurn{Answer: c.answer}); err == nil {
			t.Errorf("%+v accepted", c)
		}
	}
}
