//go:build linux

package world

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// TestSessionLockConflictsWithAPOSIXLock checks that our lock sees a POSIX
// record lock held by another process, which is what a vanilla server's
// FileChannel.tryLock is on Linux. flock and fcntl locks are independent, so
// this is the case that has to hold to stop a vanilla server and ours writing
// one world folder together.
func TestSessionLockConflictsWithAPOSIXLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, sessionLockFile)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	helper := exec.Command(os.Args[0], "-test.run=TestSessionLockPOSIXLockHelper")
	helper.Env = append(os.Environ(), "SESSION_LOCK_HELPER=1", "SESSION_LOCK_FILE="+path)
	helper.Stderr = os.Stderr
	stdout, err := helper.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := helper.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = helper.Wait()
	})

	ready := bufio.NewReader(stdout)
	line, err := ready.ReadString('\n')
	if err != nil {
		t.Fatalf("the helper did not take the lock: %v", err)
	}
	if strings.TrimSpace(line) != "locked" {
		t.Fatalf("the helper said %q, want locked", line)
	}
	go func() { _, _ = io.Copy(io.Discard, stdout) }()

	lock, err := Lock(dir)
	if err == nil {
		_ = lock.Release()
		t.Fatal("Lock took a folder a POSIX lock already held")
	}
	if !strings.Contains(err.Error(), "already locked") {
		t.Errorf("Lock error = %v, want the vanilla already-locked text", err)
	}
}

// TestSessionLockPOSIXLockHelper is the other process: with SESSION_LOCK_HELPER
// set it takes a POSIX record lock on SESSION_LOCK_FILE, prints locked, and
// holds it until its standard input closes.
func TestSessionLockPOSIXLockHelper(t *testing.T) {
	if os.Getenv("SESSION_LOCK_HELPER") != "1" {
		t.Skip("not the helper process")
	}
	file, err := os.OpenFile(os.Getenv("SESSION_LOCK_FILE"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	lk := unix.Flock_t{Type: unix.F_WRLCK}
	if err := unix.FcntlFlock(file.Fd(), unix.F_SETLK, &lk); err != nil {
		t.Fatalf("helper POSIX lock: %v", err)
	}
	if _, err := os.Stdout.WriteString("locked\n"); err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
}
