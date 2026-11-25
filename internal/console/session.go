// Package console is the server console: it runs each line of standard
// input through the players' command tree as the server itself and prints
// the answers. On a terminal it also edits the line being typed and keeps
// the prompt clear of the log lines arriving around it.
package console

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/golem-mc/golem/internal/protocol"
	"golang.org/x/term"
)

// prompt is what the console holds the line being typed in front of.
const prompt = "> "

// escapeTimeout is how long a lone escape waits for the rest of a sequence
// before it counts as the escape key; terminals send a sequence in one
// write, so shells wait only tens of milliseconds.
const escapeTimeout = 30 * time.Millisecond

// defaultColumns is the width assumed when the terminal does not say.
const defaultColumns = 80

// Session is one console. Serve reads it until the input ends; the server
// outlives it.
type Session struct {
	in   io.Reader
	out  io.Writer
	logs io.Writer
	// file, when set, also takes every command reply as the uncoloured line
	// latest.log keeps.
	file io.Writer
	host ConsoleHost

	// interactive turns on line editing and the prompt: input and output
	// are both a terminal.
	interactive bool

	// mu guards out and the editor state against log lines from other
	// goroutines, so a log line cannot land in the middle of a redraw.
	mu sync.Mutex
	ed editor
	kr keyReader

	// columns answers the terminal's width, read on every draw. row is the
	// cursor's row within a draw, counted from the prompt's. drawn is what
	// is on screen (nil when nothing), drawnAt the cursor's index in it and
	// drawnBreak whether it ended with a newline on the margin; wipeLine
	// needs them to recount rows after a resize reflowed them.
	columns    func() int
	row        int
	drawn      []rune
	drawnAt    int
	drawnBreak bool

	// rawState is nil when the terminal is not raw.
	rawFd    int
	rawState *term.State
	// closed makes log lines go out plainly, with no prompt under them.
	closed bool

	// now is the replies' clock.
	now    func() time.Time
	colour bool
}

// Close restores the terminal and wipes the prompt. The process calls it on
// its way out, since the reading goroutine is still blocked on input and
// its deferred restore would never run. Safe to call more than once, and
// before Serve, which then leaves the terminal alone.
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.rawState == nil {
		return
	}
	s.wipeLine()
	_ = term.Restore(s.rawFd, s.rawState)
	s.rawState = nil
}

// put drops the write error: the console cannot act on a writer that has
// gone away, and failing for it would strand the reader.
func put(w io.Writer, s string) { _, _ = io.WriteString(w, s) }

// New builds the console. It edits lines only when input and output are
// both a terminal: a terminal whose output is piped to a log would fill it
// with redraws.
func New(in io.Reader, out, logs io.Writer, host ConsoleHost) *Session {
	s := newSession(in, out, logs, host, isTerminal(in) && isTerminal(out))
	if f, ok := out.(*os.File); ok {
		s.columns = func() int {
			width, _, err := term.GetSize(int(f.Fd()))
			if err != nil || width <= 0 {
				return defaultColumns
			}
			return width
		}
	}
	s.colour = Colourful(out)
	return s
}

// isTerminal says whether v is a file open on a terminal.
func isTerminal(v any) bool {
	f, ok := v.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// newSession is New with the interactivity given, for the editing tests.
func newSession(in io.Reader, out, logs io.Writer, host ConsoleHost, interactive bool) *Session {
	return &Session{in: in, out: out, logs: logs, host: host, interactive: interactive,
		columns: func() int { return defaultColumns }, now: time.Now}
}

// reply is a command's answer as the console prints it: an info log line.
func (s *Session) reply(component protocol.Text) string {
	if s.colour {
		return logPrefix(s.now(), slog.LevelInfo, true) + component.ANSI()
	}
	return s.replyLine(component)
}

// replyLine is a reply as the uncoloured line the log file takes.
func (s *Session) replyLine(component protocol.Text) string {
	return logPrefix(s.now(), slog.LevelInfo, false) + protocol.EscapeControls(component.Plain())
}

// SetLogFile attaches the file every command reply is also appended to.
// vanilla: log4j2's rolling file appender
func (s *Session) SetLogFile(w io.Writer) { s.file = w }

// logReply appends one reply to the log file, if any. The file carries its
// own lock; the session's is not held.
func (s *Session) logReply(component protocol.Text) {
	if s.file == nil {
		return
	}
	put(s.file, s.replyLine(component)+"\n")
}

// LogColour reports whether log lines should be coloured: on a terminal
// they share the console's output.
func (s *Session) LogColour() bool {
	if s.interactive {
		return s.colour
	}
	return Colourful(s.logs)
}

// Serve runs the console until its input ends.
func (s *Session) Serve() {
	if !s.interactive {
		s.servePlain()
		return
	}
	s.serveEditing()
}

// servePlain reads whole lines and runs each, echoing nothing.
func (s *Session) servePlain() {
	buf := make([]byte, 4096)
	var pending []byte
	for {
		n, err := s.in.Read(buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)
			for {
				i := bytes.IndexByte(pending, '\n')
				if i < 0 {
					break
				}
				line := strings.TrimSuffix(string(pending[:i]), "\r")
				pending = pending[i+1:]
				for _, component := range s.host.RunLine(line) {
					put(s.out, s.reply(component)+"\n")
					s.logReply(component)
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// serveEditing runs the console in raw mode, the prompt redrawn around
// every key press and log lines written above it.
func (s *Session) serveEditing() {
	defer s.Close()
	// Raw mode is taken under the lock so a Close racing the start (from a
	// server that failed to listen) cannot leave the terminal raw.
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if f, ok := s.in.(*os.File); ok {
		if state, err := term.MakeRaw(int(f.Fd())); err == nil {
			s.rawFd, s.rawState = int(f.Fd()), state
			keepOutputProcessing(s.rawFd)
		}
	}
	s.draw()
	s.mu.Unlock()

	// Reads run on their own goroutine so a lone escape can time out.
	reads := make(chan inputRead)
	done := make(chan struct{})
	defer close(done)
	go s.readInput(reads, done)

	var timeout <-chan time.Time
	for {
		var keys []Key
		var err error
		select {
		case r := <-reads:
			keys, err = s.kr.Keys(r.b), r.err
			if err != nil {
				keys = append(keys, s.kr.Flush()...)
			}
		case <-timeout:
			keys = s.kr.Flush()
		}
		if slices.ContainsFunc(keys, s.handleKey) {
			return
		}
		if err != nil {
			return
		}
		timeout = nil
		if s.kr.waiting() {
			timeout = time.After(escapeTimeout)
		}
	}
}

// inputRead is one read of the input.
type inputRead struct {
	b   []byte
	err error
}

// readInput reads the input into reads until it ends or done closes.
func (s *Session) readInput(reads chan<- inputRead, done <-chan struct{}) {
	for {
		buf := make([]byte, 256)
		n, err := s.in.Read(buf)
		select {
		case reads <- inputRead{b: buf[:n], err: err}:
		case <-done:
			return
		}
		if err != nil {
			return
		}
	}
}

// handleKey acts on one key press, answering whether the console should end
// (Ctrl-D on an empty line).
//
// The lock is held for editing and drawing only, never across a host call:
// commands, stopping and completion all log through this console, and a log
// line takes the lock to draw above the prompt, so it would deadlock.
func (s *Session) handleKey(key Key) bool {
	s.mu.Lock()
	if key.Kind == KeyCtrlD && len(s.ed.line) == 0 {
		// Deliberately unlike vanilla, which stops reading silently: say
		// that lines typed from here on are ignored.
		s.wipeLine()
		s.writeLine("Console input closed")
		s.mu.Unlock()
		return true
	}

	switch s.ed.Key(key) {
	case editDraw:
		s.draw()
	case editSubmit:
		// The rows already show the prompt and the line, so moving past
		// their end is the whole echo.
		text := s.ed.Text()
		s.leaveLine()
		s.ed.Accept()
		s.mu.Unlock()
		s.run(text)
		s.mu.Lock()
		s.draw()
	case editComplete:
		s.mu.Unlock()
		s.complete()
		return false
	case editInterrupt:
		s.wipeLine()
		put(s.out, "\r\n")
		s.draw()
		s.mu.Unlock()
		s.host.Stop()
		return false
	case editIgnore:
	}
	s.mu.Unlock()
	return false
}

// run runs one line through the host and prints each component answered as
// a plain line. Called without the lock, which it takes per line.
func (s *Session) run(line string) {
	for _, component := range s.host.RunLine(line) {
		s.mu.Lock()
		s.wipeLine()
		s.writeLine(s.reply(component))
		s.mu.Unlock()
		s.logReply(component)
	}
}

// complete completes the word at the cursor. One match replaces it, then a
// space unless the match ends in a game rule's '='; several are completed
// as far as they agree and listed above the prompt.
func (s *Session) complete() {
	s.mu.Lock()
	text := "/" + string(s.ed.line[:s.ed.cursor])
	s.mu.Unlock()
	start, matches := s.host.Suggest(text)
	s.mu.Lock()
	defer s.mu.Unlock()
	// Keys are handled one at a time on the reading goroutine, so the line
	// is unchanged, though a log line may have redrawn it.
	if len(matches) == 0 {
		return
	}
	// start indexes the text sent, which has a leading slash the line does
	// not.
	from := utf8.RuneCountInString(text[:start]) - 1
	if len(matches) == 1 {
		match := matches[0]
		if s.ed.cursor == len(s.ed.line) && !strings.HasSuffix(match, "=") {
			match += " "
		}
		s.ed.ReplaceText(from, s.ed.cursor, match)
		s.draw()
		return
	}
	s.ed.ReplaceText(from, s.ed.cursor, commonPrefix(matches))
	s.wipeLine()
	for _, match := range matches {
		s.writeLine(match)
	}
	s.draw()
}

// commonPrefix is the longest text every one of the matches begins with.
func commonPrefix(matches []string) string {
	prefix := matches[0]
	for _, match := range matches[1:] {
		for !strings.HasPrefix(match, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

// draw repaints the prompt and the line and puts the cursor back. A wrapped
// line spans several rows, so it climbs to the prompt's row, clears to the
// end of the screen and places the cursor by row and column.
func (s *Session) draw() {
	s.wipeLine()
	text := []rune(prompt + s.ed.Text())
	put(s.out, string(text))
	width := s.columns()
	endRow, endCol := place(text, len(text), width)
	if endCol == 0 {
		// The terminal holds the cursor on the margin until the next
		// character; this moves it to the next row for real.
		put(s.out, "\r\n")
	}
	s.row = endRow
	at := len([]rune(prompt)) + s.ed.cursor
	s.moveTo(place(text, at, width))
	s.drawn, s.drawnAt, s.drawnBreak = text, at, endCol == 0
}

// leaveLine moves the cursor to a fresh row, leaving the prompt and line
// above it.
func (s *Session) leaveLine() {
	text := []rune(prompt + s.ed.Text())
	width := s.columns()
	// A resize reflows the line, so the climb is counted at the current width.
	s.row = s.cursorRow(width)
	row, col := place(text, len(text), width)
	s.moveTo(row, col)
	if col > 0 {
		put(s.out, "\r\n")
	}
	s.row, s.drawn = 0, nil
}

// wipeLine removes the prompt and the line, leaving the cursor at the start
// of the prompt's row.
func (s *Session) wipeLine() {
	if row := s.cursorRow(s.columns()); row > 0 {
		put(s.out, fmt.Sprintf("\x1b[%dA", row))
	}
	put(s.out, "\r\x1b[J")
	s.row, s.drawn = 0, nil
}

// cursorRow is the row the last draw left the cursor on, counted at the
// current width: a resized terminal reflows wrapped rows (tmux, VTE, kitty,
// iTerm2 do).
func (s *Session) cursorRow(width int) int {
	if s.drawn == nil {
		return 0
	}
	if s.drawnAt < len(s.drawn) {
		row, _ := place(s.drawn, s.drawnAt, width)
		return row
	}
	row, col := place(s.drawn, len(s.drawn), width)
	if col > 0 && s.drawnBreak {
		// The newline drawn on the old margin is a hard break the reflow
		// keeps.
		row++
	}
	return row
}

// moveTo moves the cursor to row and col, counted from the start of the
// prompt's row.
func (s *Session) moveTo(row, col int) {
	switch {
	case row < s.row:
		put(s.out, fmt.Sprintf("\x1b[%dA", s.row-row))
	case row > s.row:
		put(s.out, fmt.Sprintf("\x1b[%dB", row-s.row))
	}
	put(s.out, "\r")
	if col > 0 {
		put(s.out, fmt.Sprintf("\x1b[%dC", col))
	}
	s.row = row
}

// place is where rune i of text stands on a terminal width columns wide (i
// may be len(text)). A rune that does not fit before the margin wraps, and
// an end on the margin is the start of the next row.
func place(text []rune, i, width int) (row, col int) {
	for j, r := range text {
		w := runeWidth(r)
		if col+w > width || col == width {
			row, col = row+1, 0
		}
		if j == i {
			return row, col
		}
		col += w
	}
	if col >= width {
		row, col = row+1, 0
	}
	return row, col
}

// writeLine writes one line under the prompt; raw mode does not return the
// carriage.
func (s *Session) writeLine(text string) {
	put(s.out, text+"\r\n")
}

// LogWriter is the writer for the server's log lines: above the prompt on a
// terminal, else to the log writer New was given.
func (s *Session) LogWriter() io.Writer {
	return &logWriter{session: s}
}

// logWriter holds a write's partial last line until the rest arrives.
type logWriter struct {
	session *Session

	mu      sync.Mutex
	pending []byte
}

// Write hands every line b completes to the session.
func (w *logWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	w.pending = append(w.pending, b...)
	var lines [][]byte
	for {
		i := bytes.IndexByte(w.pending, '\n')
		if i < 0 {
			break
		}
		lines = append(lines, w.pending[:i])
		w.pending = w.pending[i+1:]
	}
	w.mu.Unlock()

	for _, line := range lines {
		w.session.writeLog(string(line))
	}
	return len(b), nil
}

// writeLog writes one log line through the console.
func (s *Session) writeLog(line string) {
	if !s.interactive {
		put(s.logs, line+"\n")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		put(s.out, line+"\n")
		return
	}
	s.wipeLine()
	s.writeLine(line)
	s.draw()
}
