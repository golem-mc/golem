package console

import (
	"io"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// screen is a terminal small enough to check the console's drawing against:
// it prints runes at their display width, wraps at the margin as xterm and
// tmux do, scrolls, reflows wrapped rows on a resize as tmux does, and knows
// the few sequences the console sends.
type screen struct {
	t             *testing.T
	width, height int
	// cells holds each row's runes; a wide rune's second cell is -1.
	cells [][]rune
	// wrapped marks a row whose text ran on into the next by wrapping,
	// which a resize joins back up.
	wrapped []bool
	// x may equal width: the cursor then waits on the margin until the
	// next rune wraps it, as a real terminal's does.
	x, y int
	rest []byte
}

func newScreen(t *testing.T, width, height int) *screen {
	s := &screen{t: t, width: width, height: height, wrapped: make([]bool, height)}
	for range height {
		s.cells = append(s.cells, make([]rune, width))
	}
	return s
}

func (s *screen) Write(b []byte) (int, error) {
	s.rest = append(s.rest, b...)
	for len(s.rest) > 0 {
		n := s.step(s.rest)
		if n == 0 {
			break
		}
		s.rest = s.rest[n:]
	}
	return len(b), nil
}

// step interprets the first rune or sequence of b, answering the bytes it
// took, or zero when b holds only the start of one.
func (s *screen) step(b []byte) int {
	switch b[0] {
	case '\r':
		s.x = 0
		return 1
	case '\n':
		s.lineFeed()
		return 1
	case 0x1b:
		if len(b) < 2 || b[1] != '[' {
			s.t.Fatalf("screen: unexpected escape %q", b)
		}
		for i := 2; i < len(b); i++ {
			if b[i] >= 0x40 && b[i] <= 0x7e {
				s.csi(string(b[2:i]), b[i])
				return i + 1
			}
		}
		return 0
	}
	if !utf8.FullRune(b) {
		return 0
	}
	r, n := utf8.DecodeRune(b)
	s.print(r)
	return n
}

func (s *screen) csi(param string, final byte) {
	n := 1
	if param != "" {
		var err error
		if n, err = strconv.Atoi(param); err != nil {
			s.t.Fatalf("screen: bad parameter %q", param)
		}
	}
	s.x = min(s.x, s.width-1)
	switch final {
	case 'A':
		s.y = max(s.y-n, 0)
	case 'B':
		s.y = min(s.y+n, s.height-1)
	case 'C':
		s.x = min(s.x+n, s.width-1)
	case 'K':
		clear(s.cells[s.y][s.x:])
		s.wrapped[s.y] = false
	case 'J':
		clear(s.cells[s.y][s.x:])
		s.wrapped[s.y] = false
		for y := s.y + 1; y < s.height; y++ {
			clear(s.cells[y])
			s.wrapped[y] = false
		}
	default:
		s.t.Fatalf("screen: unexpected sequence %q%c", param, final)
	}
}

func (s *screen) print(r rune) {
	w := runeWidth(r)
	if w == 0 {
		return
	}
	if s.x+w > s.width {
		s.wrapped[s.y] = true
		s.x = 0
		s.lineFeed()
	}
	s.cells[s.y][s.x] = r
	if w == 2 {
		s.cells[s.y][s.x+1] = -1
	}
	s.x += w
}

func (s *screen) lineFeed() {
	if s.y < s.height-1 {
		s.y++
		return
	}
	s.cells = append(s.cells[1:], make([]rune, s.width))
	s.wrapped = append(s.wrapped[1:], false)
}

// resize changes the width, joining wrapped rows into their lines and
// wrapping those again at the new width, the cursor moving with its text.
// Narrow runes only.
func (s *screen) resize(width int) {
	type line struct {
		text   []rune
		cursor int // the cursor's offset in the line, or -1
	}
	var lines []line
	cur := line{cursor: -1}
	for y, row := range s.cells {
		if y == s.y {
			cur.cursor = len(cur.text) + min(s.x, s.width)
		}
		end := len(row)
		for end > 0 && row[end-1] == 0 {
			end--
		}
		if s.wrapped[y] {
			end = len(row)
		}
		cur.text = append(cur.text, row[:end]...)
		if !s.wrapped[y] {
			lines = append(lines, cur)
			cur = line{cursor: -1}
		}
	}

	s.width = width
	s.cells, s.wrapped = nil, nil
	for _, l := range lines {
		rows := max(1, (len(l.text)+width-1)/width)
		if l.cursor >= 0 {
			s.y, s.x = len(s.cells)+l.cursor/width, l.cursor%width
			rows = max(rows, l.cursor/width+1)
		}
		for i := range rows {
			row := make([]rune, width)
			copy(row, l.text[min(i*width, len(l.text)):])
			s.cells = append(s.cells, row)
			s.wrapped = append(s.wrapped, i < rows-1)
		}
	}
	// The rows the reflow added run off the bottom, blank ones first.
	for len(s.cells) > s.height && len(s.cells)-1 > s.y {
		last := len(s.cells) - 1
		if strings.TrimRight(string(s.cells[last]), "\x00") != "" {
			break
		}
		s.cells, s.wrapped = s.cells[:last], s.wrapped[:last]
	}
	for len(s.cells) > s.height {
		s.cells, s.wrapped = s.cells[1:], s.wrapped[1:]
		s.y--
	}
	for len(s.cells) < s.height {
		s.cells = append(s.cells, make([]rune, width))
		s.wrapped = append(s.wrapped, false)
	}
}

// rows is the screen's text, trailing blanks and blank rows trimmed.
func (s *screen) rows() []string {
	var rows []string
	for _, row := range s.cells {
		var b strings.Builder
		for _, r := range row {
			switch r {
			case 0:
				b.WriteByte(' ')
			case -1:
			default:
				b.WriteRune(r)
			}
		}
		rows = append(rows, strings.TrimRight(b.String(), " "))
	}
	for len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	return rows
}

// cursor is where the cursor shows, a cursor waiting on the margin showing
// in the last column.
func (s *screen) cursor() (x, y int) { return min(s.x, s.width-1), s.y }

// onScreen runs an editing console over the input on a screen of the size
// given, answering the session and the screen.
func onScreen(t *testing.T, input string, width, height int) (*Session, *screen, *fakeHost) {
	t.Helper()
	host := &fakeHost{}
	scr := newScreen(t, width, height)
	s := newSession(strings.NewReader(input), scr, io.Discard, host, true)
	s.columns = func() int { return width }
	s.serveEditing()
	return s, scr, host
}

func wantScreen(t *testing.T, scr *screen, rows []string, x, y int) {
	t.Helper()
	got := scr.rows()
	if strings.Join(got, "\n") != strings.Join(rows, "\n") {
		t.Errorf("screen reads\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(rows, "\n"))
	}
	if gx, gy := scr.cursor(); gx != x || gy != y {
		t.Errorf("cursor at (%d, %d), want (%d, %d)", gx, gy, x, y)
	}
}

func TestScreenWrappedLineRedrawsInPlace(t *testing.T) {
	long := strings.Repeat("abcdefghij", 12)
	input := "say " + long + strings.Repeat("\x1b[D", 4) + "Z\x7f\x7f"
	_, scr, _ := onScreen(t, input, 100, 30)

	// Left four, Z, two backspaces: the Z and the rune before it are gone,
	// and the cursor stands where the Z was typed, less one.
	line := "say " + long[:115] + long[116:]
	text := prompt + line
	wantScreen(t, scr, []string{text[:100], text[100:]}, 21, 1)
}

func TestScreenWideRunesPlaceTheCursorByColumns(t *testing.T) {
	_, scr, _ := onScreen(t, "say 日本語テスト\x1b[D\x1b[D\x1b[DX", 100, 30)
	wantScreen(t, scr, []string{"> say 日本語Xテスト"}, 13, 0)
}

func TestLoneEscapeTimesOut(t *testing.T) {
	// The escape key, then a key a moment later: the escape is given up on
	// and the later key types as itself.
	r, w := io.Pipe()
	host := &fakeHost{}
	s := newSession(r, io.Discard, io.Discard, host, true)
	done := make(chan struct{})
	go func() {
		s.serveEditing()
		close(done)
	}()
	for _, chunk := range []string{"lis", "\x1b"} {
		if _, err := io.WriteString(w, chunk); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(10 * escapeTimeout)
	if _, err := io.WriteString(w, "t\r"); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	<-done
	if len(host.lines) != 1 || host.lines[0] != "list" {
		t.Errorf("ran %q, want list", host.lines)
	}
}

// resizedScreen types the line on a screen of the first width, resizes it to
// the second when the console asks for completions (tab), and types the rest.
func resizedScreen(t *testing.T, before, after int, line, rest string) *screen {
	t.Helper()
	scr := newScreen(t, before, 30)
	_, _ = io.WriteString(scr, "a line above\r\n")
	width := before
	host := &fakeHost{onSuggest: func() {
		scr.resize(after)
		width = after
	}}
	s := newSession(strings.NewReader(line+"\t"+rest), scr, io.Discard, host, true)
	s.columns = func() int { return width }
	s.serveEditing()
	return scr
}

// wrapRows is text cut into rows of the width.
func wrapRows(text string, width int) []string {
	var rows []string
	for len(text) > width {
		rows = append(rows, text[:width])
		text = text[width:]
	}
	return append(rows, text)
}

func TestScreenResizeNarrowerLeavesNoStaleRows(t *testing.T) {
	digits := strings.Repeat("0123456789", 7)
	scr := resizedScreen(t, 40, 25, "say "+digits, "X")
	text := prompt + "say " + digits + "X"
	wantScreen(t, scr, append([]string{"a line above"}, wrapRows(text, 25)...), 2, 4)
}

func at(rows []string, i int) string {
	if i < len(rows) {
		return rows[i]
	}
	return ""
}

func TestScreenInterruptWipesAWrappedLine(t *testing.T) {
	// Ctrl-C with the cursor on the second row wipes both rows (the blank
	// row is where the line stood); the buffered line is drawn once below.
	_, scr, host := onScreen(t, "say abcdefghijklmnop\x03", 10, 10)
	wantScreen(t, scr, []string{"", "> say abcd", "efghijklmn", "op"}, 2, 3)
	if !host.stopped {
		t.Errorf("did not stop")
	}
}
