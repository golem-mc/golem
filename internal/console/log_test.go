package console

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// handle has the handler write one record made at testNow.
func handle(t *testing.T, h slog.Handler, level slog.Level, msg string, args ...any) {
	t.Helper()
	r := slog.NewRecord(testNow, level, msg, 0)
	r.Add(args...)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}
}

func TestLogHandlerWritesTheMessageRaw(t *testing.T) {
	var out bytes.Buffer
	h := NewLogHandler(&out, slog.LevelInfo, false)
	handle(t, h, slog.LevelInfo, `[Not Secure] <Tester> he said "hi" 日本 \ ok`)
	want := `[21:04:05] [Server thread/INFO]: [Not Secure] <Tester> he said "hi" 日本 \ ok` + "\n"
	if out.String() != want {
		t.Errorf("wrote %q, want %q", out.String(), want)
	}
}

func TestLogHandlerLevels(t *testing.T) {
	var out bytes.Buffer
	h := NewLogHandler(&out, slog.LevelInfo, false)
	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("debug is enabled at info")
	}
	handle(t, h, slog.LevelWarn, "careful")
	handle(t, h, slog.LevelError, "broken")
	want := "[21:04:05] [Server thread/WARN]: careful\n[21:04:05] [Server thread/ERROR]: broken\n"
	if out.String() != want {
		t.Errorf("wrote %q, want %q", out.String(), want)
	}
}

func TestLogHandlerAttributes(t *testing.T) {
	var out bytes.Buffer
	h := NewLogHandler(&out, slog.LevelInfo, false).WithAttrs([]slog.Attr{slog.String("player", "Tester")})
	handle(t, h, slog.LevelInfo, "player joined",
		"remote", "127.0.0.1:5", "players", 1, "reason", "timed out", "empty", "",
		slog.Group("pos", "x", 1.5))
	handle(t, h.WithGroup("chunk"), slog.LevelInfo, "saved", "x", 3)
	want := `[21:04:05] [Server thread/INFO]: player joined player=Tester remote=127.0.0.1:5 players=1 reason="timed out" empty="" pos.x=1.5` + "\n" +
		"[21:04:05] [Server thread/INFO]: saved player=Tester chunk.x=3\n"
	if out.String() != want {
		t.Errorf("wrote\n%s\nwant\n%s", out.String(), want)
	}
}

func TestLogHandlerEscapesControlCharacters(t *testing.T) {
	// A player's /me that would retitle the terminal and overwrite its own
	// line arrives as text, the newline of a multi-line message kept.
	var out bytes.Buffer
	h := NewLogHandler(&out, slog.LevelInfo, false)
	handle(t, h, slog.LevelInfo, "[Not Secure] * Nobody \x1b]0;pwned\x07\x1b[2K\rFAKE LINE\nsecond")
	want := `[21:04:05] [Server thread/INFO]: [Not Secure] * Nobody \x1b]0;pwned\a\x1b[2K\rFAKE LINE` + "\nsecond\n"
	if out.String() != want {
		t.Errorf("wrote %q, want %q", out.String(), want)
	}
}

func TestLogHandlerColour(t *testing.T) {
	var out bytes.Buffer
	h := NewLogHandler(&out, slog.LevelInfo, true)
	handle(t, h, slog.LevelInfo, "server started", "port", 25565)
	handle(t, h, slog.LevelWarn, "careful")
	handle(t, h, slog.LevelError, "broken")
	want := "\x1b[2m[21:04:05] [Server thread/\x1b[0mINFO\x1b[2m]:\x1b[0m server started\x1b[2m port=25565\x1b[0m\n" +
		"\x1b[2m[21:04:05] [Server thread/\x1b[0m\x1b[33mWARN\x1b[0m\x1b[2m]:\x1b[0m \x1b[33mcareful\x1b[0m\n" +
		"\x1b[2m[21:04:05] [Server thread/\x1b[0m\x1b[31mERROR\x1b[0m\x1b[2m]:\x1b[0m \x1b[31mbroken\x1b[0m\n"
	if out.String() != want {
		t.Errorf("wrote\n%q\nwant\n%q", out.String(), want)
	}
}

func TestLogHandlerDrawsTheComponent(t *testing.T) {
	line := protocol.Text{Text: "[Not Secure] ", Extra: []protocol.Text{{Text: "hi", Color: protocol.ColorRed}}}
	var plain, coloured bytes.Buffer
	handle(t, NewLogHandler(&plain, slog.LevelInfo, false), slog.LevelInfo, line.Plain(), protocol.LogComponentKey, line)
	handle(t, NewLogHandler(&coloured, slog.LevelInfo, true), slog.LevelInfo, line.Plain(), protocol.LogComponentKey, line)
	if want := "[21:04:05] [Server thread/INFO]: [Not Secure] hi\n"; plain.String() != want {
		t.Errorf("plain wrote %q, want %q", plain.String(), want)
	}
	if want := "]:\x1b[0m [Not Secure] \x1b[0;91mhi\x1b[0m\n"; !strings.HasSuffix(coloured.String(), want) {
		t.Errorf("colour wrote %q, want it to end %q", coloured.String(), want)
	}
}

// panicked is an error carrying a stack, as a plugin's recovered panic does.
type panicked struct{}

func (panicked) Error() string { return "panic: boom" }
func (panicked) Stack() string {
	return "goroutine 1 [running]:\nmain.f()\n\t/x.go:3 +0x1\n"
}

func TestLogHandlerPrintsStacksUnderTheLine(t *testing.T) {
	var out bytes.Buffer
	h := NewLogHandler(&out, slog.LevelInfo, false)
	handle(t, h, slog.LevelError, "command panicked", "input", "boom",
		StackKey, "goroutine 7 [running]:\nmain.g()\n\t/y.go:9 +0x2\n")
	handle(t, h, slog.LevelError, "Error in command", "error", panicked{})
	want := "[21:04:05] [Server thread/ERROR]: command panicked input=boom\n" +
		"\tgoroutine 7 [running]:\n\tmain.g()\n\t\t/y.go:9 +0x2\n" +
		`[21:04:05] [Server thread/ERROR]: Error in command error="panic: boom"` + "\n" +
		"\tgoroutine 1 [running]:\n\tmain.f()\n\t\t/x.go:3 +0x1\n"
	if out.String() != want {
		t.Errorf("wrote\n%s\nwant\n%s", out.String(), want)
	}
}
