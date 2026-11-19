//go:build unix && !linux

package world

import (
	"errors"
	"os"
	"syscall"
)

// lockSession takes an exclusive, non-blocking lock on file, returning
// errSessionLocked when another open file description already holds it.
func lockSession(file *os.File) error {
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return errSessionLocked
	}
	return err
}

// unlockSession releases the lock; closing would too, but vanilla releases
// before closing.
func unlockSession(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
