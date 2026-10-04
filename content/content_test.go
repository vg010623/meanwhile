package content_test

import (
	"testing"

	"github.com/vg010623/meanwhile/content"
	"github.com/vg010623/meanwhile/internal/curriculum"
)

// TestShippedContentIsValid fails CI when a pull request breaks the curriculum:
// a missing prerequisite, a cycle, a bad card count, and so on.
func TestShippedContentIsValid(t *testing.T) {
	cur, issues := curriculum.Load(content.FS, content.Dir)
	for _, i := range issues {
		if i.Severity == curriculum.Error {
			t.Error(i)
		} else {
			t.Log(i)
		}
	}
	if len(cur.Concepts) == 0 {
		t.Error("no concepts found")
	}
}
