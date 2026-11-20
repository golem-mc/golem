package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// fakeClock is a clock the test moves; Advance runs the frames that fall due
// on the test's goroutine.
type fakeClock struct {
	now, next time.Time
	every     time.Duration
	f         func()
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Every(d time.Duration, f func()) func() {
	c.every, c.f, c.next = d, f, c.now.Add(d)
	return func() { c.f = nil }
}

func (c *fakeClock) Advance(d time.Duration) {
	end := c.now.Add(d)
	for c.f != nil && !c.next.After(end) {
		c.now = c.next
		c.f()
		c.next = c.next.Add(c.every)
	}
	c.now = end
}

// terminal is a fake terminal: it applies the carriage return, line feed,
// erase line and cursor up the renderer writes, drops colour codes, and
// counts the writes.
type terminal struct {
	rows     [][]rune
	row, col int
	raw      bytes.Buffer
	writes   int
}

func (t *terminal) Write(p []byte) (int, error) {
	t.writes++
	t.raw.Write(p)
	if t.rows == nil {
		t.rows = [][]rune{nil}
	}
	s := []rune(string(p))
	for i := 0; i < len(s); i++ {
		switch r := s[i]; r {
		case '\r':
			t.col = 0
		case '\n':
			t.row++
			t.col = 0
			if t.row == len(t.rows) {
				t.rows = append(t.rows, nil)
			}
		case 0x1b:
			j := i + 2 // past ESC [
			for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] == ';') {
				j++
			}
			switch s[j] {
			case 'K':
				t.rows[t.row] = t.rows[t.row][:min(t.col, len(t.rows[t.row]))]
			case 'A':
				t.row--
			case 'm':
			default:
				panic("unexpected escape " + string(s[i:j+1]))
			}
			i = j
		default:
			line := t.rows[t.row]
			for len(line) < t.col {
				line = append(line, ' ')
			}
			if t.col < len(line) {
				line[t.col] = r
			} else {
				line = append(line, r)
			}
			t.rows[t.row] = line
			t.col++
		}
	}
	return len(p), nil
}

// screen answers the terminal's lines, trailing empty ones dropped.
func (t *terminal) screen() string {
	var lines []string
	for _, r := range t.rows {
		lines = append(lines, string(r))
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

func newLive(t *testing.T, color bool) (*UI, *terminal, *fakeClock) {
	t.Helper()
	term := &terminal{}
	clock := &fakeClock{now: time.Unix(1_000_000, 0)}
	u := New(&bytes.Buffer{}, term, Settings{Live: true, ErrColor: color, OutColor: color, Width: 80}, clock)
	return u, term, clock
}

func TestSpinnerFrames(t *testing.T) {
	u, term, clock := newLive(t, false)
	u.OK("first")
	s := u.Step("building golem v1.0.0")
	if got := term.screen(); got != "✓ first" {
		t.Fatalf("a step drew before its first frame:\n%s", got)
	}
	for i, want := range []string{"⠙ building golem v1.0.0 0.1s", "⠹ building golem v1.0.0 0.2s", "⠸ building golem v1.0.0 0.3s"} {
		clock.Advance(frameInterval)
		if got := term.screen(); got != "✓ first\n"+want {
			t.Errorf("frame %d:\n%s\nwant the spinner line %q", i, got, want)
		}
	}
	clock.Advance(900 * time.Millisecond)
	s.Done()
	if got := term.screen(); got != "✓ first\n✓ building golem v1.0.0 (1.2s)" {
		t.Errorf("after Done:\n%s", got)
	}
	clock.Advance(time.Second)
	if got := term.screen(); got != "✓ first\n✓ building golem v1.0.0 (1.2s)" {
		t.Errorf("a frame after the last step drew:\n%s", got)
	}
}

func TestStepFailPrintsOutput(t *testing.T) {
	u, term, clock := newLive(t, false)
	s := u.Step("checking the plugins' imports")
	clock.Advance(3 * frameInterval)
	s.Fail("main.go:3:8: use of internal package not allowed\n")
	want := "✗ checking the plugins' imports\n  main.go:3:8: use of internal package not allowed"
	if got := term.screen(); got != want {
		t.Errorf("screen:\n%s\nwant:\n%s", got, want)
	}
}

func TestDownloadBar(t *testing.T) {
	u, term, clock := newLive(t, false)
	d := u.Download("Go toolchain", 4_000_000)
	d.Add(1_000_000)
	clock.Advance(time.Second)
	want := "↓ Go toolchain ━━━━━━────────────────── 1.0 MB/4.0 MB · 1.0 MB/s · ETA 3.0s"
	if got := term.screen(); got != want {
		t.Errorf("bar at 1/4:\n%s\nwant:\n%s", got, want)
	}
	if _, err := d.Reader(bytes.NewReader(make([]byte, 3_000_000))).Read(make([]byte, 3_000_000)); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Second)
	want = "↓ Go toolchain ━━━━━━━━━━━━━━━━━━━━━━━━ 4.0 MB/4.0 MB · 2.0 MB/s · ETA 0.0s"
	if got := term.screen(); got != want {
		t.Errorf("bar at the end:\n%s\nwant:\n%s", got, want)
	}
	d.Done()
	if got := term.screen(); got != "✓ Go toolchain (4.0 MB in 2.0s)" {
		t.Errorf("after Done: %q", got)
	}
}

func TestDownloadOfUnknownSizeSpins(t *testing.T) {
	u, term, clock := newLive(t, false)
	d := u.Download("golem_v1.0.0_linux_amd64", -1)
	d.Add(2500)
	clock.Advance(frameInterval)
	if got, want := term.screen(), "⠙ golem_v1.0.0_linux_amd64 2.5 kB · 25.0 kB/s"; got != want {
		t.Errorf("screen %q, want %q", got, want)
	}
}

func TestFramesAtMostTenASecond(t *testing.T) {
	u, term, clock := newLive(t, false)
	d := u.Download("x", 1_000_000)
	for range 1000 {
		d.Add(100)
		clock.Advance(time.Millisecond)
	}
	if term.writes > 10 {
		t.Errorf("%d writes in one second of progress, want at most 10", term.writes)
	}
}

func TestTwoTasksAndALineAbove(t *testing.T) {
	u, term, clock := newLive(t, false)
	a := u.Step("a")
	u.Step("b")
	clock.Advance(frameInterval)
	if got := term.screen(); got != "⠙ a 0.1s\n⠙ b 0.1s" {
		t.Fatalf("two live lines:\n%s", got)
	}
	u.Warn("careful")
	a.Done()
	clock.Advance(frameInterval)
	if got := term.screen(); got != "! careful\n✓ a (0.1s)\n⠹ b 0.2s" {
		t.Errorf("after a line and a step ended:\n%s", got)
	}
}

func TestUpdateAboveSpinner(t *testing.T) {
	u, term, clock := newLive(t, true)
	u.Step("building golem v1.0.0")
	clock.Advance(3 * frameInterval)
	u.Update("golem %s is out (you run %s)", "v1.5.0", "v1.0.0")
	u.Hint("run `golem-launcher update` to update the server and plugins")
	want := "↑ golem v1.5.0 is out (you run v1.0.0)\n  run `golem-launcher update` to update the server and plugins"
	if got := term.screen(); got != want {
		t.Errorf("after the notice:\n%s\nwant:\n%s", got, want)
	}
	clock.Advance(frameInterval)
	if got := term.screen(); got != want+"\n⠼ building golem v1.0.0 0.4s" {
		t.Errorf("next frame:\n%s", got)
	}
	for _, code := range []string{"\x1b[33m↑ golem v1.5.0", "  \x1b[2mrun `golem-launcher update`"} {
		if !strings.Contains(term.raw.String(), code) {
			t.Errorf("output lacks %q:\n%q", code, term.raw.String())
		}
	}
}

func TestCloseClearsBeforeExec(t *testing.T) {
	u, term, clock := newLive(t, true)
	u.OK("golem v1.0.0 · no plugins · ready in 0.2s")
	u.Download("golem", 100).Add(50)
	u.Step("verifying")
	clock.Advance(frameInterval)
	if !strings.Contains(term.screen(), "verifying") {
		t.Fatalf("nothing drawn:\n%s", term.screen())
	}
	u.Close()
	if got := term.screen(); got != "✓ golem v1.0.0 · no plugins · ready in 0.2s" {
		t.Errorf("after Close:\n%q", got)
	}
	if term.col != 0 || term.row != 1 {
		t.Errorf("cursor at row %d column %d, want the start of a clean line", term.row, term.col)
	}
	n := term.writes
	clock.Advance(time.Second)
	if term.writes != n {
		t.Error("a frame drew after Close")
	}
}

func TestLongNameTruncatedToWidth(t *testing.T) {
	term := &terminal{}
	clock := &fakeClock{}
	u := New(nil, term, Settings{Live: true, Width: 30}, clock)
	u.Step(strings.Repeat("resolving ", 10))
	clock.Advance(frameInterval)
	if got := []rune(term.screen()); len(got) > 29 || !strings.Contains(string(got), "… 0.1s") {
		t.Errorf("line %q (%d columns) at width 30", string(got), len(got))
	}
}

func TestPlainModeOneLinePerStep(t *testing.T) {
	var errw bytes.Buffer
	clock := &fakeClock{now: time.Unix(0, 0)}
	u := New(nil, &errw, Settings{}, clock)
	s := u.Step("building")
	d := u.Download("golem", 2000)
	d.Add(2000)
	clock.Advance(1500 * time.Millisecond)
	d.Done()
	s.Done()
	u.Info("note")
	u.Error("go build: exit status 1", "hint")
	u.Close()
	want := "✓ golem (2.0 kB in 1.5s)\n✓ building (1.5s)\n• note\nerror: go build: exit status 1\n\nhint"
	if got := errw.String(); got != want+"\n" {
		t.Errorf("plain output:\n%s\nwant:\n%s", got, want)
	}
}

func TestColourOnAndOff(t *testing.T) {
	for _, color := range []bool{false, true} {
		var errw, out bytes.Buffer
		u := New(&out, &errw, Settings{ErrColor: color, OutColor: color}, &fakeClock{})
		u.Error("boom", "usage")
		u.Step("x").Done()
		Table(&out, u.Out(), []string{"NAME", "LATEST"}, [][]Cell{{{Text: "server"}, {Text: "v1.5.0", Paint: Green}}})
		has := strings.Contains(errw.String()+out.String(), "\x1b[")
		if has != color {
			t.Errorf("colour %v: escape codes %v in %q %q", color, has, errw.String(), out.String())
		}
		if color && !strings.Contains(errw.String(), "\x1b[1;31merror:\x1b[0m boom") {
			t.Errorf("error not red: %q", errw.String())
		}
	}
}

func TestTableAlignsPaintedCells(t *testing.T) {
	var out bytes.Buffer
	rows := [][]Cell{
		{{Text: "example.com/shops"}, {Text: "v1.0.0", Paint: Yellow}, {Text: "v1.2.0", Paint: Green}},
		{{Text: "w"}, {Text: "-", Paint: Dim}, {Text: "unknown", Paint: Dim}},
	}
	Table(&out, Style{true}, []string{"PLUGIN", "VERSION", "LATEST"}, rows)
	term := &terminal{}
	term.Write(out.Bytes())
	want := "PLUGIN             VERSION  LATEST\nexample.com/shops  v1.0.0   v1.2.0\nw                  -        unknown"
	if got := term.screen(); got != want {
		t.Errorf("table:\n%s\nwant:\n%s", got, want)
	}
	if !strings.HasPrefix(out.String(), "\x1b[1mPLUGIN\x1b[0m") {
		t.Errorf("header not bold: %q", out.String())
	}
}

func TestDetect(t *testing.T) {
	env := func(kv ...string) func(string) string {
		return func(k string) string {
			for i := 0; i < len(kv); i += 2 {
				if kv[i] == k {
					return kv[i+1]
				}
			}
			return ""
		}
	}
	for _, c := range []struct {
		name             string
		flag             string
		env              func(string) string
		outTerm, errTerm bool
		want             Settings
	}{
		{"terminal", "auto", env(), true, true, Settings{Live: true, ErrColor: true, OutColor: true}},
		{"piped stdout", "auto", env(), false, true, Settings{Live: true, ErrColor: true}},
		{"piped", "auto", env(), false, false, Settings{}},
		{"NO_COLOR", "auto", env("NO_COLOR", "1"), true, true, Settings{}},
		{"empty NO_COLOR", "auto", env("NO_COLOR", ""), true, true, Settings{Live: true, ErrColor: true, OutColor: true}},
		{"TERM=dumb", "auto", env("TERM", "dumb"), true, true, Settings{}},
		{"FORCE_COLOR piped", "auto", env("FORCE_COLOR", "1"), false, false, Settings{ErrColor: true, OutColor: true}},
		{"FORCE_COLOR=0", "auto", env("FORCE_COLOR", "0"), false, false, Settings{}},
		{"always piped", "always", env(), false, false, Settings{ErrColor: true, OutColor: true}},
		{"always dumb", "always", env("TERM", "dumb"), true, true, Settings{ErrColor: true, OutColor: true}},
		{"never", "never", env("FORCE_COLOR", "1"), true, true, Settings{}},
	} {
		got, err := Detect(c.flag, c.env, c.outTerm, c.errTerm)
		if err != nil || got != c.want {
			t.Errorf("%s: %+v, %v, want %+v", c.name, got, err, c.want)
		}
	}
	if _, err := Detect("sometimes", env(), true, true); err == nil {
		t.Error("-color=sometimes accepted")
	}
}

func TestFormats(t *testing.T) {
	for d, want := range map[time.Duration]string{0: "0.0s", 1234 * time.Millisecond: "1.2s", 65 * time.Second: "1m05s"} {
		if got := Duration(d); got != want {
			t.Errorf("Duration(%v) = %q, want %q", d, got, want)
		}
	}
	for n, want := range map[int64]string{999: "999 B", 12_300: "12.3 kB", 45_600_000: "45.6 MB"} {
		if got := Bytes(n); got != want {
			t.Errorf("Bytes(%d) = %q, want %q", n, got, want)
		}
	}
}
