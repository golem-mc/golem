package console

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func openPty(t *testing.T) (master, slave *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	n, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = master.Close(); _ = slave.Close() })
	return master, slave
}

func TestEditingSessionKeepsNewlineReturningTheCarriage(t *testing.T) {
	master, slave := openPty(t)
	s := newSession(slave, slave, io.Discard, &fakeHost{}, true)
	go s.Serve()
	t.Cleanup(s.Close)

	// Reads of a pty master take no deadline, so they run on a goroutine.
	chunks := make(chan string)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				chunks <- string(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	// The first output is the prompt: raw mode is on once it is drawn.
	select {
	case <-chunks:
	case <-time.After(5 * time.Second):
		t.Fatal("no prompt")
	}

	old := os.Stderr
	os.Stderr = slave
	fmt.Fprint(os.Stderr, "a\nb\n")
	os.Stderr = old

	var got strings.Builder
	timeout := time.After(5 * time.Second)
	for !strings.Contains(got.String(), "a\r\nb\r\n") {
		select {
		case c := <-chunks:
			got.WriteString(c)
		case <-timeout:
			t.Fatalf("read %q, want a\\r\\nb\\r\\n", got.String())
		}
	}
}
