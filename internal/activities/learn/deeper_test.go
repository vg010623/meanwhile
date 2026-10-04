package learn

import (
	"regexp"
	"strings"
	"testing"

	"github.com/vg010623/meanwhile/internal/curriculum"
	"github.com/vg010623/meanwhile/internal/deeper"
)

type curriculumConcept = curriculum.Concept

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

func TestDeeperConsentThenAnswer(t *testing.T) {
	f := newFixture(t)
	f.a.store.setStatus("d", Completed)
	v := f.start()
	press(v, "enter") // Alpha card 1
	press(v, "d")
	if v.screen != scrConsent || !strings.Contains(screenText(v), "your own Claude subscription") {
		t.Fatalf("consent:\n%s", screenText(v))
	}
	press(v, "down", "down", "enter") // choose Haiku
	if v.screen != scrDeeper || !strings.Contains(screenText(v), "Deeper answer.") {
		t.Fatalf("answer:\n%s", screenText(v))
	}
	req := f.fd.reqs[0]
	if req.Model != "haiku" || !strings.Contains(req.Prompt, "Alpha card 1") || !strings.Contains(req.Prompt, "already covered: Delta") {
		t.Errorf("request: %+v", req)
	}
	if !strings.Contains(req.System, "first principles") {
		t.Error("system prompt")
	}
	var n int
	f.db.QueryRow(`SELECT COUNT(*) FROM learn_deep_dives WHERE status = 'complete' AND parent_id IS NULL`).Scan(&n)
	if n != 1 {
		t.Errorf("saved %d", n)
	}

	// Consent is remembered.
	press(v, "esc", "d")
	if v.screen != scrDeeper || len(f.fd.reqs) != 2 {
		t.Errorf("asked consent twice")
	}
}

func TestDeeperFollowUps(t *testing.T) {
	f := newFixture(t)
	f.a.ctx.DB.SetKV(kvNamespace, kvConsent, "yes")
	v := f.start()
	press(v, "enter", "d")
	for i := 0; i < maxFollowUps; i++ {
		press(v, "f")
		if !v.Capturing() {
			t.Fatal("not capturing input")
		}
		press(v, "w", "h", "y", "?", "enter")
	}
	if len(f.fd.reqs) != 1+maxFollowUps {
		t.Fatalf("requests %d", len(f.fd.reqs))
	}
	last := f.fd.reqs[len(f.fd.reqs)-1].Prompt
	if !strings.Contains(last, "Earlier in this conversation") || !strings.Contains(last, "Learner: why?") {
		t.Errorf("follow-up prompt:\n%s", last)
	}
	press(v, "f") // no more follow-ups allowed
	if v.Capturing() {
		t.Error("allowed too many follow-ups")
	}
	var children int
	f.db.QueryRow(`SELECT COUNT(*) FROM learn_deep_dives WHERE parent_id IS NOT NULL`).Scan(&children)
	if children != maxFollowUps {
		t.Errorf("follow-ups saved: %d", children)
	}

	// History lists the thread and opens it read-only.
	press(v, "esc", "D")
	if v.screen != scrHistory || len(v.deep.history) != 1 {
		t.Fatalf("history:\n%s", screenText(v))
	}
	press(v, "enter")
	if !v.deep.readOnly || len(v.deep.turns) != 1+maxFollowUps || !strings.Contains(screenText(v), "You: why?") {
		t.Errorf("thread:\n%s", screenText(v))
	}
}

func TestDeeperErrorFallsBackToCards(t *testing.T) {
	f := newFixture(t)
	f.a.ctx.DB.SetKV(kvNamespace, kvConsent, "yes")
	f.fd.respond = func(deeper.Request) []deeper.Event {
		return []deeper.Event{{Kind: deeper.Failed, Err: &deeper.Error{Kind: deeper.ErrNotLoggedIn}}}
	}
	v := f.start()
	press(v, "enter", "d")
	if !strings.Contains(screenText(v), "isn't logged in") {
		t.Fatalf("error:\n%s", screenText(v))
	}
	press(v, "esc")
	if v.screen != scrCard {
		t.Error("not back on the card")
	}
}

func TestDeeperCancelKeepsPartial(t *testing.T) {
	f := newFixture(t)
	f.a.ctx.DB.SetKV(kvNamespace, kvConsent, "yes")
	f.fd.respond = func(deeper.Request) []deeper.Event {
		return []deeper.Event{{Kind: deeper.Text, Text: "Half an ans"}} // no Done: still streaming
	}
	v := f.start()
	press(v, "enter", "d")
	v.deep.streaming = true // the fake channel closed; pretend it is still open
	press(v, "esc")
	var status, answer string
	f.db.QueryRow(`SELECT status, answer FROM learn_deep_dives`).Scan(&status, &answer)
	if status != "cancelled" || answer != "Half an ans" || v.screen != scrCard {
		t.Errorf("status=%q answer=%q screen=%d", status, answer, v.screen)
	}
}

func TestConfigModelOverrides(t *testing.T) {
	f := newFixture(t)
	f.a.ctx.DB.SetKV(kvNamespace, kvConsent, "yes")
	f.a.ctx.DB.SetKV(kvNamespace, kvModel, "haiku")
	f.a.ctx.Config.Deeper.Model = "sonnet"
	v := f.start()
	press(v, "enter", "d")
	if f.fd.reqs[0].Model != "sonnet" {
		t.Errorf("model %q", f.fd.reqs[0].Model)
	}
}
