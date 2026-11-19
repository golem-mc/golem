package world

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestSessionLockFailsWhileHeld checks that a second Lock on one world folder
// fails the way a second server opening the same world does, and that giving
// the first back lets the next one in.
func TestSessionLockFailsWhileHeld(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	first, err := Lock(dir)
	if err != nil {
		t.Fatalf("first Lock: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, sessionLockFile)); err != nil {
		t.Errorf("Lock did not create %s: %v", sessionLockFile, err)
	}

	if _, err := Lock(dir); err == nil {
		t.Fatal("a second Lock on the held folder succeeded")
	} else if !strings.Contains(err.Error(), "already locked") {
		t.Errorf("second Lock error = %v, want the vanilla already-locked text", err)
	}

	if err := first.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	again, err := Lock(dir)
	if err != nil {
		t.Fatalf("Lock after Release: %v", err)
	}
	if err := again.Release(); err != nil {
		t.Fatalf("Release the second lock: %v", err)
	}
}

// TestSessionLockWritesTheSnowman checks that Lock leaves vanilla's marker in
// the file it holds.
func TestSessionLockWritesTheSnowman(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lock, err := Lock(dir)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	defer func() { _ = lock.Release() }()
	data, err := os.ReadFile(filepath.Join(dir, sessionLockFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "\u2603" {
		t.Errorf("session.lock holds %q, want the snowman", data)
	}
}

// TestSessionLockErrorNamesThePathOnce checks that a failed Lock does not
// print the world path twice: the os.MkdirAll and os.OpenFile errors already
// carry it.
func TestSessionLockErrorNamesThePathOnce(t *testing.T) {
	t.Parallel()
	t.Run("create", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "world")
		if err := os.WriteFile(dir, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := Lock(dir)
		if err == nil {
			t.Fatal("Lock on a folder that is a file succeeded")
		}
		if n := strings.Count(err.Error(), dir); n != 1 {
			t.Errorf("error names %s %d times: %v", dir, n, err)
		}
	})
	t.Run("open", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, sessionLockFile)
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := Lock(dir)
		if err == nil {
			t.Fatal("Lock with a directory named session.lock succeeded")
		}
		if n := strings.Count(err.Error(), path); n != 1 {
			t.Errorf("error names %s %d times: %v", path, n, err)
		}
	})
}

// TestSessionLockReleaseConcurrently checks that two goroutines giving the lock
// back at once do not race on it.
func TestSessionLockReleaseConcurrently(t *testing.T) {
	t.Parallel()
	l, err := Lock(t.TempDir())
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if err := l.Release(); err != nil {
				t.Errorf("Release: %v", err)
			}
		})
	}
	wg.Wait()
}
