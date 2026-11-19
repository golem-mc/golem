package console

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
)

// fakeHost is a ConsoleHost the session tests drive: it keeps the lines it
// was given, answers each with what the test queued for it, and completes
// with what the test handed it.
type fakeHost struct {
	lines   []string
	replies [][]protocol.Text
	start   int
	matches []string
	stopped bool
	// onSuggest, when set, runs as the console asks for completions.
	onSuggest func()
}

func (h *fakeHost) RunLine(line string) []protocol.Text {
	h.lines = append(h.lines, line)
	if len(h.replies) == 0 {
		return nil
	}
	reply := h.replies[0]
	h.replies = h.replies[1:]
	return reply
}

func (h *fakeHost) Suggest(text string) (int, []string) {
	if h.onSuggest != nil {
		h.onSuggest()
	}
	return h.start, h.matches
}

func (h *fakeHost) Stop() { h.stopped = true }

// testNow is the time the sessions under test read, and replied the prefix
// their replies carry for it.
var testNow = time.Date(2026, 10, 16, 21, 4, 5, 0, time.Local)

const replied = "[21:04:05] [Server thread/INFO]: "

// served runs a console over the input given and answers what it printed.
func served(t *testing.T, host *fakeHost, input string) string {
	t.Helper()
	var out bytes.Buffer
	s := newSession(strings.NewReader(input), &out, io.Discard, host, false)
	s.now = func() time.Time { return testNow }
	s.Serve()
	return out.String()
}

// pressed runs a console that edits, over the key presses the shorthand
// carries, and answers what it printed.
func pressed(t *testing.T, host *fakeHost, shorthand string) string {
	t.Helper()
	var out bytes.Buffer
	s := newSession(strings.NewReader(decodeForTest(t, shorthand)), &out, io.Discard, host, true)
	s.serveEditing()
	return out.String()
}

// decodeForTest is the bytes the keys' shorthand stands for, which the
// terminal would have sent: a readable line of the editor tests' keys.
func decodeForTest(t *testing.T, shorthand string) string {
	t.Helper()
	var b strings.Builder
	for _, k := range keys(t, shorthand) {
		switch k.Kind {
		case KeyRune:
			b.WriteRune(k.Rune)
		case KeyEnter:
			b.WriteByte('\r')
		case KeyTab:
			b.WriteByte('\t')
		case KeyBackspace:
			b.WriteByte('\x7f')
		case KeyDelete:
			b.WriteString("\x1b[3~")
		case KeyLeft:
			b.WriteString("\x1b[D")
		case KeyRight:
			b.WriteString("\x1b[C")
		case KeyUp:
			b.WriteString("\x1b[A")
		case KeyDown:
			b.WriteString("\x1b[B")
		case KeyHome:
			b.WriteString("\x1b[H")
		case KeyEnd:
			b.WriteString("\x1b[F")
		case KeyCtrlC:
			b.WriteByte('\x03')
		case KeyCtrlD:
			b.WriteByte('\x04')
		case KeyClear:
			b.WriteByte('\x15')
		case KeyKill:
			b.WriteByte('\x0b')
		}
	}
	return b.String()
}

func TestSessionPlainRunsLines(t *testing.T) {
	host := &fakeHost{replies: [][]protocol.Text{{
		{Translate: "multiplayer.player.joined",
			With: []protocol.Text{{Text: "Steve"}}},
	}}}
	out := served(t, host, "say hi\n/seed\n")

	if len(host.lines) != 2 || host.lines[0] != "say hi" || host.lines[1] != "/seed" {
		t.Fatalf("ran %q, want the two lines as typed", host.lines)
	}
	if !strings.Contains(out, "Steve joined the game\n") {
		t.Errorf("printed %q, want the rendered feedback", out)
	}
}

func TestSessionEditingTypesAndSubmits(t *testing.T) {
	host := &fakeHost{replies: [][]protocol.Text{{{Text: "answered"}}}}
	out := pressed(t, host, "say_hi enter")

	if len(host.lines) != 1 || host.lines[0] != "say hi" {
		t.Fatalf("ran %q, want say hi", host.lines)
	}
	// The submitted line is left on its row, the prompt and all, and the
	// row is then ended for the answer's lines to follow.
	if !strings.Contains(out, "\r\x1b[J"+prompt+"say hi\r") || !strings.Contains(out, "\x1b[8C\r\n") {
		t.Errorf("output %q does not carry the submitted row", out)
	}
	if !strings.Contains(out, "answered\r\n") {
		t.Errorf("output %q does not carry the rendered answer", out)
	}
}

func TestSessionEditingRedrawsAroundKeys(t *testing.T) {
	host := &fakeHost{}
	out := pressed(t, host, "say_hi left x")

	// The redraws repaint the prompt and the line as it stands, wiping
	// whatever stood on the row before.
	if !strings.Contains(out, "\r\x1b[J"+prompt+"say hi") {
		t.Errorf("output %q does not carry the first redraw", out)
	}
	if !strings.Contains(out, "\r\x1b[J"+prompt+"say hxi") {
		t.Errorf("output %q does not carry the redraw after the insert", out)
	}
}

func TestSessionEditingHistory(t *testing.T) {
	host := &fakeHost{}
	out := pressed(t, host, "say_one enter say_two enter up down say_d up enter")

	want := []string{"say one", "say two", "say two"}
	if len(host.lines) != len(want) {
		t.Fatalf("ran %q, want %q", host.lines, want)
	}
	for i, line := range want {
		if host.lines[i] != line {
			t.Errorf("line %d = %q, want %q", i, host.lines[i], line)
		}
	}
	if !strings.Contains(out, prompt+"say d\r") || !strings.Contains(out, prompt+"say two\r") {
		t.Errorf("output %q does not carry the draft and the recall", out)
	}
}

func TestSessionEditingTabCompletesFromTheHost(t *testing.T) {
	host := &fakeHost{start: 1, matches: []string{"seed"}}
	out := pressed(t, host, "tab enter")

	if len(host.lines) != 1 || host.lines[0] != "seed " {
		t.Fatalf("ran %q, want the completed line", host.lines)
	}
	if !strings.Contains(out, prompt+"seed \r") {
		t.Errorf("output %q does not carry the completed redraw", out)
	}
}

func TestSessionEditingTabListsTheChoices(t *testing.T) {
	host := &fakeHost{start: 1, matches: []string{"say", "seed", "summon"}}
	out := pressed(t, host, "tab enter")

	// Several matches are completed as far as they agree and listed, and
	// the line submitted is what the common prefix left it at.
	if len(host.lines) != 1 || host.lines[0] != "s" {
		t.Fatalf("ran %q, want the common prefix", host.lines)
	}
	for _, match := range host.matches {
		if !strings.Contains(out, match+"\r\n") {
			t.Errorf("output %q does not list %q", out, match)
		}
	}
}

func TestSessionEditingCtrlCStopsTheServer(t *testing.T) {
	host := &fakeHost{}
	pressed(t, host, "ctrl-c")
	if !host.stopped {
		t.Error("ctrl-c did not stop the host")
	}
}

func TestSessionEditingCtrlDEndsAnEmptyConsole(t *testing.T) {
	host := &fakeHost{}
	pressed(t, host, "ctrl-d say hi enter")
	if len(host.lines) != 0 {
		t.Fatalf("ran %q, want nothing: the console ended before the typing", host.lines)
	}
}

func TestSessionLogWriterOnATerminal(t *testing.T) {
	host := &fakeHost{}
	var out bytes.Buffer
	s := newSession(strings.NewReader(decodeForTest(t, "say_hi enter")), &out, io.Discard, host, true)
	w := s.LogWriter()
	if _, err := io.WriteString(w, "joined the game\n"); err != nil {
		t.Fatalf("write log: %v", err)
	}
	if !strings.Contains(out.String(), "joined the game\r\n") {
		t.Errorf("output %q does not carry the log line", out.String())
	}
}

func TestSessionLogWriterOnAPlainConsole(t *testing.T) {
	host := &fakeHost{}
	var out, logs bytes.Buffer
	s := newSession(strings.NewReader(""), &out, &logs, host, false)
	if _, err := io.WriteString(s.LogWriter(), "a log line\n"); err != nil {
		t.Fatalf("write log: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("the plain console printed %q, want nothing", out.String())
	}
	if logs.String() != "a log line\n" {
		t.Errorf("logs = %q, want the log line", logs.String())
	}
}

func TestSessionRepliesInColour(t *testing.T) {
	host := &fakeHost{replies: [][]protocol.Text{{{Text: "Unknown command", Color: protocol.ColorRed}}}}
	var out bytes.Buffer
	s := newSession(strings.NewReader("bogus\n"), &out, io.Discard, host, false)
	s.now = func() time.Time { return testNow }
	s.colour = true
	s.Serve()
	want := "\x1b[2m[21:04:05] [Server thread/\x1b[0mINFO\x1b[2m]:\x1b[0m \x1b[0;91mUnknown command\x1b[0m\n"
	if out.String() != want {
		t.Errorf("printed %q, want %q", out.String(), want)
	}
}

// TestSessionRepliesGoToTheLogFile has a console whose file is attached run a
// line: the stream keeps the reply in colour while the file takes the plain
// line.
func TestSessionRepliesGoToTheLogFile(t *testing.T) {
	host := &fakeHost{replies: [][]protocol.Text{{{Text: "Saved the game", Color: protocol.ColorRed}}}}
	var out, file bytes.Buffer
	s := newSession(strings.NewReader("save-all\n"), &out, io.Discard, host, false)
	s.now = func() time.Time { return testNow }
	s.colour = true
	s.SetLogFile(&file)
	s.Serve()

	if !strings.Contains(out.String(), "\x1b[0;91mSaved the game\x1b[0m") {
		t.Errorf("the stream lost the reply's colour: %q", out.String())
	}
	if want := replied + "Saved the game\n"; file.String() != want {
		t.Errorf("file = %q, want the plain reply %q", file.String(), want)
	}
}
