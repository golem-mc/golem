package world

import (
	"os"
	"path/filepath"
	"strings"
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
