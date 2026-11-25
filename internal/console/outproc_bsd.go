//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package console

import "golang.org/x/sys/unix"

// keepOutputProcessing turns OPOST|ONLCR back on after term.MakeRaw cleared
// them, so a bare "\n" from any writer (the runtime's crash trace included)
// still returns the carriage. Input stays raw.
func keepOutputProcessing(fd int) {
	t, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return
	}
	t.Oflag |= unix.OPOST | unix.ONLCR
	_ = unix.IoctlSetTermios(fd, unix.TIOCSETA, t)
}
