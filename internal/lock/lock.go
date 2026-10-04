// Package lock provides advisory file locks between meanwhile processes. The
// OS releases a lock automatically when its process exits, even after a
// crash, so a stale lock can never block the app.
package lock

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// ErrHeld means another process holds the lock.
var ErrHeld = errors.New("lock is held by another process")

// Lock is a held lock. Call Release when done.
type Lock struct{ f *os.File }

// Acquire waits until the lock is free.
func Acquire(path string) (*Lock, error) { return acquire(path, syscall.LOCK_EX) }

// TryAcquire fails immediately with ErrHeld if the lock is taken.
func TryAcquire(path string) (*Lock, error) {
	l, err := acquire(path, syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return nil, ErrHeld
	}
	return l, err
}

func acquire(path string, how int) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		f.Close()
		return nil, err
	}
	return &Lock{f: f}, nil
}

// Release frees the lock.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	err := l.f.Close()
	l.f = nil
	return err
}
