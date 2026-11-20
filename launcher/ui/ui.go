// Package ui is the launcher's console output: steps with spinners,
// downloads with progress bars and coloured messages on standard error, and
// styled tables and help on standard output. In plain mode (not a terminal,
// NO_COLOR, TERM=dumb) nothing is redrawn and each step prints one line when
// it ends.
package ui

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
)

// frameInterval is the time between redraws of the live lines: 10 frames a
// second at most.
const frameInterval = 100 * time.Millisecond

// Settings is how the output is drawn.
type Settings struct {
	// Live redraws spinners and progress bars in place on standard error.
	Live bool
	// ErrColor and OutColor colour standard error and standard output.
	ErrColor, OutColor bool
	// Width is standard error's width in columns; 0 means 80.
	Width int
}

// Detect answers the settings for the --color flag (auto, always or never),
// the environment and whether standard output and error are terminals.
func Detect(color string, getenv func(string) string, outTerm, errTerm bool) (Settings, error) {
	var s Settings
	switch color {
	case "always":
		s.ErrColor, s.OutColor = true, true
	case "never":
	case "auto", "":
		// no-color.org and force-color.org: set means non-empty.
		force := getenv("FORCE_COLOR")
		forced := force != "" && force != "0" && force != "false"
		off := getenv("NO_COLOR") != "" || getenv("TERM") == "dumb"
		s.ErrColor = forced || errTerm && !off
		s.OutColor = forced || outTerm && !off
	default:
		return s, fmt.Errorf("-color must be auto, always or never, not %q", color)
	}
	// Redrawing needs a terminal that understands escape codes; colour
	// forced into a pipe or log stays one line per step.
	s.Live = errTerm && s.ErrColor && getenv("TERM") != "dumb"
	return s, nil
}

// Terminal reports whether f is a terminal that takes ANSI escape codes,
// enabling them first where the console needs it (Windows).
func Terminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd())) && enableVT(f)
}

// Width answers f's width in columns, or 0 when it has none.
func Width(f *os.File) int {
	w, _, err := term.GetSize(int(f.Fd()))
	if err != nil {
		return 0
	}
	return w
}

// Clock is the time the renderer reads and the ticker that drives its frames.
type Clock interface {
	Now() time.Time
	// Every calls f every d until the returned stop is called; stop waits
	// for a running f to return.
	Every(d time.Duration, f func()) (stop func())
}

// RealClock is the wall clock.
type RealClock struct{}

// Now answers time.Now.
func (RealClock) Now() time.Time { return time.Now() }

// Every runs f on a time.Ticker.
func (RealClock) Every(d time.Duration, f func()) func() {
	t := time.NewTicker(d)
	done, exited := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(exited)
		for {
			select {
			case <-t.C:
				f()
			case <-done:
				return
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			t.Stop()
			close(done)
			<-exited
		})
	}
}

// UI writes the launcher's output. It is safe for concurrent use.
type UI struct {
	mu       sync.Mutex
	out, err io.Writer
	s        Settings
	clock    Clock
	stopTick func()
	tasks    []*Task
	// drawn is how many live lines are on screen, the cursor at the end of
	// the last one.
	drawn int
}

// New answers a UI writing results to out and progress to err.
func New(out, err io.Writer, s Settings, clock Clock) *UI {
	if s.Width <= 0 {
		s.Width = 80
	}
	u := &UI{out: out, err: err, s: s, clock: clock}
	if s.Live {
		u.stopTick = clock.Every(frameInterval, u.frame)
	}
	return u
}

// Close clears the live lines and stops redrawing, so whatever runs next
// (the server's console, a shell prompt) starts on a clean line.
func (u *UI) Close() {
	if u.stopTick != nil {
		u.stopTick() // outside mu: a running frame holds it
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.tasks = nil
	u.erase()
}

// Out is the style of standard output.
func (u *UI) Out() Style { return Style{u.s.OutColor} }

// Err is the style of standard error.
func (u *UI) Err() Style { return Style{u.s.ErrColor} }

// Live reports whether progress is redrawn in place.
func (u *UI) Live() bool { return u.s.Live }

// Since answers the time since t on the UI's clock.
func (u *UI) Since(t time.Time) time.Duration { return u.clock.Now().Sub(t) }

// Now answers the UI clock's time.
func (u *UI) Now() time.Time { return u.clock.Now() }

// erase removes the live lines; mu is held.
func (u *UI) erase() {
	_, _ = io.WriteString(u.err, u.eraseCodes())
	u.drawn = 0
}

// eraseCodes answers the escape codes that erase the live lines, leaving
// the cursor at the start of the first.
func (u *UI) eraseCodes() string {
	if u.drawn == 0 {
		return ""
	}
	return "\r\x1b[K" + strings.Repeat("\x1b[1A\x1b[K", u.drawn-1)
}

// print writes finished lines to standard error above the live ones, which
// the next frame draws again.
func (u *UI) print(s string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	_, _ = io.WriteString(u.err, u.eraseCodes()+s)
	u.drawn = 0
}

// frame redraws the live lines; the clock calls it every frameInterval.
func (u *UI) frame() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.tasks) == 0 && u.drawn == 0 {
		return
	}
	now := u.clock.Now()
	// One write a frame, so the terminal never shows it half drawn.
	var b strings.Builder
	b.WriteString(u.eraseCodes())
	for i, t := range u.tasks {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(t.line(now, u.s.Width-1, u.Err()))
	}
	_, _ = io.WriteString(u.err, b.String())
	u.drawn = len(u.tasks)
}

func (u *UI) remove(t *Task) {
	u.mu.Lock()
	defer u.mu.Unlock()
	for i, x := range u.tasks {
		if x == t {
			u.tasks = append(u.tasks[:i], u.tasks[i+1:]...)
			return
		}
	}
}

func (u *UI) add(t *Task) *Task {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.tasks = append(u.tasks, t)
	return t
}

// OK prints a finished step: ✓ and the message.
func (u *UI) OK(format string, args ...any) {
	u.print(u.Err().Paint(Green, "✓") + " " + fmt.Sprintf(format, args...) + "\n")
}

// Info prints a note: • and the message.
func (u *UI) Info(format string, args ...any) {
	u.print(u.Err().Paint(Cyan, "•") + " " + fmt.Sprintf(format, args...) + "\n")
}

// Warn prints a warning: ! and the message, in yellow.
func (u *UI) Warn(format string, args ...any) {
	st := u.Err()
	u.print(st.Paint(Yellow, "! "+fmt.Sprintf(format, args...)) + "\n")
}

// Error prints a red "error:" and msg, then hint, dimmed, when there is one.
func (u *UI) Error(msg, hint string) {
	st := u.Err()
	s := st.Paint(BoldRed, "error:") + " " + msg + "\n"
	if hint != "" {
		s += "\n" + st.PaintLines(Dim, strings.TrimRight(hint, "\n")) + "\n"
	}
	u.print(s)
}

// Task is a step or a download in progress.
type Task struct {
	u     *UI
	name  string
	start time.Time
	// download is a byte count, total < 0 when the size is unknown.
	download    bool
	mu          sync.Mutex
	done, total int64
}

// Step starts a step called name: a spinner and its elapsed time while it
// runs.
func (u *UI) Step(name string) *Task {
	return u.add(&Task{u: u, name: name, start: u.clock.Now()})
}

// Download starts a download of total bytes (-1 when unknown) called name: a
// progress bar, or a spinner when the size is unknown.
func (u *UI) Download(name string, total int64) *Task {
	return u.add(&Task{u: u, name: name, start: u.clock.Now(), download: true, total: total})
}

// Add counts n more bytes downloaded.
func (t *Task) Add(n int) {
	t.mu.Lock()
	t.done += int64(n)
	t.mu.Unlock()
}

// Reader answers r counting what is read from it into t.
func (t *Task) Reader(r io.Reader) io.Reader { return &countReader{r, t} }

type countReader struct {
	r io.Reader
	t *Task
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.t.Add(n)
	return n, err
}

// Done ends t: ✓, its name and how long it took.
func (t *Task) Done() { t.DoneNote("") }

// DoneNote ends t like Done, with note before the time.
func (t *Task) DoneNote(note string) {
	t.u.remove(t)
	took := t.u.clock.Now().Sub(t.start)
	detail := Duration(took)
	if t.download {
		t.mu.Lock()
		detail = Bytes(t.done) + " in " + detail
		t.mu.Unlock()
	}
	if note != "" {
		detail = note + ", " + detail
	}
	st := t.u.Err()
	t.u.print(st.Paint(Green, "✓") + " " + t.name + " " + st.Paint(Dim, "("+detail+")") + "\n")
}

// Fail ends t: ✗ and its name, then output, the step's own messages, when
// there are any.
func (t *Task) Fail(output string) {
	t.u.remove(t)
	st := t.u.Err()
	var s strings.Builder
	s.WriteString(st.Paint(Red, "✗ "+t.name) + "\n")
	if output = strings.TrimRight(output, "\n"); output != "" {
		for line := range strings.Lines(output) {
			s.WriteString("  " + strings.TrimRight(line, "\n") + "\n")
		}
	}
	t.u.print(s.String())
}

// spinner is the frames of a spinner, one every frameInterval.
var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const barWidth = 24

// line answers t's live line at now, at most width columns.
func (t *Task) line(now time.Time, width int, st Style) string {
	elapsed := now.Sub(t.start)
	icon := spinner[int(elapsed/frameInterval)%len(spinner)]
	var detail, bar string
	if !t.download {
		detail = Duration(elapsed)
	} else {
		t.mu.Lock()
		done, total := t.done, t.total
		t.mu.Unlock()
		speed := 0.0
		if s := elapsed.Seconds(); s > 0 {
			speed = float64(done) / s
		}
		rate := Bytes(int64(speed)) + "/s"
		if total > 0 {
			icon = "↓"
			filled := int(min(done, total) * barWidth / total)
			bar = st.Paint(Cyan, strings.Repeat("━", filled)) + st.Paint(Dim, strings.Repeat("─", barWidth-filled))
			eta := "--"
			if speed > 0 {
				eta = Duration(time.Duration(float64(total-done) / speed * float64(time.Second)))
			}
			detail = Bytes(done) + "/" + Bytes(total) + " · " + rate + " · ETA " + eta
		} else {
			detail = Bytes(done) + " · " + rate
		}
	}
	// Truncate the name, not the numbers, so the line never wraps: a
	// wrapped line would break erasing it.
	fixed := 2 + 1 + utf8.RuneCountInString(detail)
	if bar != "" {
		fixed += barWidth + 1
	}
	name := t.name
	if room := width - fixed; utf8.RuneCountInString(name) > room {
		r := []rune(name)
		name = string(r[:max(room-1, 0)]) + "…"
	}
	s := st.Paint(Cyan, icon) + " " + name + " "
	if bar != "" {
		s += bar + " "
	}
	return s + st.Paint(Dim, detail)
}

// Duration formats d for a person: 0.4s, 12.3s, 1m05s.
func Duration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	d = d.Round(time.Second)
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

// Bytes formats n bytes in decimal units: 512 B, 12.3 kB, 45.6 MB.
func Bytes(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d B", n)
	case n < 1000*1000:
		return fmt.Sprintf("%.1f kB", float64(n)/1e3)
	case n < 1000*1000*1000:
		return fmt.Sprintf("%.1f MB", float64(n)/1e6)
	}
	return fmt.Sprintf("%.2f GB", float64(n)/1e9)
}

// Paint is a text style.
type Paint string

// The styles, as SGR parameters.
const (
	Plain   Paint = ""
	Bold    Paint = "1"
	Dim     Paint = "2"
	Red     Paint = "31"
	BoldRed Paint = "1;31"
	Green   Paint = "32"
	Yellow  Paint = "33"
	Cyan    Paint = "36"
)

// Style paints text when colour is on and leaves it as it is when off.
type Style struct{ on bool }

// On reports whether the style colours.
func (s Style) On() bool { return s.on }

// Paint answers text in p.
func (s Style) Paint(p Paint, text string) string {
	if !s.on || p == Plain || text == "" {
		return text
	}
	return "\x1b[" + string(p) + "m" + text + "\x1b[0m"
}

// PaintLines paints each line of text on its own, so a terminal that a
// line break resets keeps the style.
func (s Style) PaintLines(p Paint, text string) string {
	var b strings.Builder
	for line := range strings.Lines(text) {
		body, nl := strings.CutSuffix(line, "\n")
		b.WriteString(s.Paint(p, body))
		if nl {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// Cell is one table cell.
type Cell struct {
	Text  string
	Paint Paint
}

// Table writes rows under a bold header, each column as wide as its
// widest cell plus two spaces.
func Table(w io.Writer, st Style, header []string, rows [][]Cell) error {
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = utf8.RuneCountInString(h)
	}
	for _, r := range rows {
		for i, c := range r {
			widths[i] = max(widths[i], utf8.RuneCountInString(c.Text))
		}
	}
	var b bytes.Buffer
	line := func(cells []Cell) {
		var l strings.Builder
		for i, c := range cells {
			text := c.Text
			if i < len(cells)-1 {
				text += strings.Repeat(" ", widths[i]-utf8.RuneCountInString(c.Text)+2)
			}
			// Paint the text, not the padding.
			l.WriteString(st.Paint(c.Paint, c.Text) + text[len(c.Text):])
		}
		b.WriteString(strings.TrimRight(l.String(), " ") + "\n")
	}
	hc := make([]Cell, len(header))
	for i, h := range header {
		hc[i] = Cell{h, Bold}
	}
	line(hc)
	for _, r := range rows {
		line(r)
	}
	_, err := w.Write(b.Bytes())
	return err
}
