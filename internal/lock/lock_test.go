package lock

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestTryAcquire(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.lock")
	a, err := TryAcquire(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := TryAcquire(p); !errors.Is(err, ErrHeld) {
		t.Errorf("second acquire: %v", err)
	}
	a.Release()
	b, err := TryAcquire(p)
	if err != nil {
		t.Errorf("after release: %v", err)
	}
	b.Release()
}
