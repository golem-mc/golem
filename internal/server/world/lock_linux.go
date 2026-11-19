//go:build linux

package world

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// lockSession takes an exclusive, non-blocking open-file-description lock,
// returning errSessionLocked when another holds it. Vanilla's
// FileChannel.tryLock is a POSIX record lock and Linux keeps flock and fcntl
// locks independent, so only an OFD lock sees a vanilla server holding the
// folder.
// vanilla: DirectoryLock.create (FileChannel.tryLock)
func lockSession(file *os.File) error {
	lk := unix.Flock_t{Type: unix.F_WRLCK}
	err := unix.FcntlFlock(file.Fd(), unix.F_OFD_SETLK, &lk)
	if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EACCES) {
		return errSessionLocked
	}
	return err
}

// unlockSession releases the lock; closing would too, but vanilla releases
// before closing.
func unlockSession(file *os.File) error {
	lk := unix.Flock_t{Type: unix.F_UNLCK}
	return unix.FcntlFlock(file.Fd(), unix.F_OFD_SETLK, &lk)
}
