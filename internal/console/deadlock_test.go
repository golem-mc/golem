package console

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
)

// loggingHost is a host that logs through the console from inside its calls,
// as the server's commands and shutdown do.
type loggingHost struct {
	session *Session
	stopped chan struct{}
}

func (h *loggingHost) log(line string) {
	_, _ = h.session.LogWriter().Write([]byte(line + "\n"))
}

func (h *loggingHost) RunLine(line string) []protocol.Text {
	h.log("ran " + line)
	return []protocol.Text{{Text: "done"}}
}

func (h *loggingHost) Suggest(text string) (int, []string) {
	h.log("suggested " + text)
	return 1, []string{"stop"}
}

func (h *loggingHost) Stop() {
	h.log("shutting down")
	close(h.stopped)
}

// servedWithin runs an editing console over the input with a host that
// logs from inside its calls, and fails if the console does not finish
// reading it in time: a host call made while the console held its own lock
// would wait forever on the log line it writes.
func servedWithin(t *testing.T, input string) (*loggingHost, string) {
	t.Helper()
	var out bytes.Buffer
	host := &loggingHost{stopped: make(chan struct{})}
	s := newSession(strings.NewReader(input), &out, io.Discard, host, true)
	host.session = s
	done := make(chan struct{})
	go func() {
		s.serveEditing()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("the console hung on input %q", input)
	}
	return host, out.String()
}

// TestACommandThatLogsDoesNotHangTheConsole runs a line whose command logs,
// the way /stop does, and holds that the console carries on and prints both
// the log line and the answer.
func TestACommandThatLogsDoesNotHangTheConsole(t *testing.T) {
	_, out := servedWithin(t, "stop\r")
	if !strings.Contains(out, "ran stop") || !strings.Contains(out, "done") {
		t.Errorf("the console printed %q, want the command's log line and its answer", out)
	}
}

// TestCtrlCStopsTheServerFromATerminal presses Ctrl-C and holds that the
// host is told to stop without the console hanging on the shutdown's own
// log line.
func TestCtrlCStopsTheServerFromATerminal(t *testing.T) {
	host, _ := servedWithin(t, "\x03")
	select {
	case <-host.stopped:
	default:
		t.Error("Ctrl-C did not stop the server")
	}
}

// TestTabCompletionThatLogsDoesNotHang completes while the host logs.
func TestTabCompletionThatLogsDoesNotHang(t *testing.T) {
	servedWithin(t, "st\t")
}
