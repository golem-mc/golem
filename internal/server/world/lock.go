package world

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// sessionLockFile is held locked while a world is open, so a second process
// cannot keep its own region offset tables and free-sector maps for the same
// files.
// vanilla: DirectoryLock.LOCK_FILE
const sessionLockFile = "session.lock"

// errSessionLocked is the reason vanilla's DirectoryLock reports when the
// session lock is held elsewhere.
// vanilla: DirectoryLock.LockException
var errSessionLocked = errors.New("already locked (possibly by other Minecraft instance?)")

// SessionLock is a session.lock held on a world folder.
type SessionLock struct {
	mu   sync.Mutex // guards file, which Release clears
	file *os.File
}

// Lock takes the session lock on dir, creating the file if needed. A second
// Lock on the folder fails while the first is held, so two servers never write
// one world's regions from two sets of tables.
// vanilla: DirectoryLock.create
func Lock(dir string) (*SessionLock, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, sessionLockFile)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	// The snowman is vanilla's marker in the held file.
	// vanilla: DirectoryLock.create
	if _, err := file.WriteString("\u2603"); err != nil {
		file.Close()
		return nil, err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return nil, err
	}
	if err := lockSession(file); err != nil {
		file.Close()
		if errors.Is(err, errSessionLocked) {
			// vanilla names the lock's absolute path in the error.
			if abs, aerr := filepath.Abs(path); aerr == nil {
				path = abs
			}
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return &SessionLock{file: file}, nil
}

// Release gives the session lock back and closes the file. Calling it more
// than once is safe.
func (l *SessionLock) Release() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	file := l.file
	l.file = nil
	l.mu.Unlock()
	if file == nil {
		return nil
	}
	if err := unlockSession(file); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
