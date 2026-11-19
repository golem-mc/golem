package console

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/golem-mc/golem/internal/protocol"
)

// LogHandler is a slog.Handler writing vanilla's console layout,
// "[HH:MM:SS] [Server thread/INFO]: message". The message is unquoted but
// escaped (protocol.EscapeControls), since players' text reaches the log;
// attributes follow as key=value, quoted only when not one word. The
// colours are this console's: vanilla's log4j2.xml has no highlighting.
type LogHandler struct {
	w      io.Writer
	level  slog.Leveler
	colour bool
	// file, when set, also takes every record as the uncoloured line.
	file io.Writer
	mu   *sync.Mutex
	// attrs is the " key=value" text WithAttrs added, and group the dotted
	// prefix WithGroup added for the keys after it.
	attrs string
	group string
}

// NewLogHandler builds a LogHandler writing the records at level or above
// to w, in colour when colour is set.
func NewLogHandler(w io.Writer, level slog.Leveler, colour bool) *LogHandler {
	return &LogHandler{w: w, level: level, colour: colour, mu: &sync.Mutex{}}
}

// Enabled reports whether a record at level is written.
func (h *LogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

// Handle writes one record as one line.
func (h *LogHandler) Handle(_ context.Context, r slog.Record) error {
	message := protocol.EscapeControls(r.Message)
	// plain is the message before it is recoloured; the log file takes it.
	plain := message
	var attrs strings.Builder
	attrs.WriteString(h.attrs)
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == protocol.LogComponentKey {
			if t, ok := a.Value.Any().(protocol.Text); ok {
				if h.colour {
					message = t.ANSI()
				}
				return true
			}
		}
		appendAttr(&attrs, h.group, a)
		return true
	})

	var file string
	if h.file != nil {
		file = logPrefix(r.Time, r.Level, false) + plain + attrs.String() + "\n"
	}

	var b strings.Builder
	b.WriteString(logPrefix(r.Time, r.Level, h.colour))
	switch {
	case !h.colour:
		b.WriteString(message + attrs.String())
	case r.Level >= slog.LevelWarn:
		b.WriteString(levelColour(r.Level) + message + attrs.String() + ansiReset)
	default:
		b.WriteString(message)
		if attrs.Len() > 0 {
			b.WriteString(ansiDim + attrs.String() + ansiReset)
		}
	}
	b.WriteByte('\n')
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, b.String())
	if h.file != nil {
		if _, fileErr := io.WriteString(h.file, file); err == nil {
			err = fileErr
		}
	}
	return err
}

// WithFile returns a handler that also writes every record, uncoloured, to
// file.
func (h *LogHandler) WithFile(file io.Writer) *LogHandler {
	clone := *h
	clone.file = file
	return &clone
}

// WithAttrs returns a handler that writes attrs after every message.
func (h *LogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	var b strings.Builder
	for _, a := range attrs {
		appendAttr(&b, h.group, a)
	}
	clone := *h
	clone.attrs += b.String()
	return &clone
}

// WithGroup returns a handler that writes the keys after it under name.
func (h *LogHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := *h
	clone.group += name + "."
	return &clone
}

// The escape codes the console draws its own parts in.
const (
	ansiReset  = "\x1b[0m"
	ansiDim    = "\x1b[2m"
	ansiYellow = "\x1b[33m"
	ansiRed    = "\x1b[31m"
)

// levelColour is the colour a level is drawn in, empty for none.
func levelColour(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return ansiRed
	case level >= slog.LevelWarn:
		return ansiYellow
	}
	return ""
}

// logPrefix is the time and level a console line starts with. Every line
// names the server thread, since this server logs from goroutines.
// vanilla: log4j2.xml in com.mojang:logging
func logPrefix(t time.Time, level slog.Level, colour bool) string {
	if t.IsZero() {
		t = time.Now()
	}
	stamp := "[" + t.Format(time.TimeOnly) + "] [Server thread/"
	if !colour {
		return stamp + level.String() + "]: "
	}
	name := level.String()
	if c := levelColour(level); c != "" {
		name = c + name + ansiReset
	}
	return ansiDim + stamp + ansiReset + name + ansiDim + "]:" + ansiReset + " "
}

// Colourful says whether output to w should be in colour: w is a terminal
// and NO_COLOR (no-color.org) is unset or empty.
func Colourful(w io.Writer) bool {
	return isTerminal(w) && os.Getenv("NO_COLOR") == ""
}

// appendAttr writes one attribute as " key=value", a group's members under
// its name.
func appendAttr(b *strings.Builder, group string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return
	}
	if a.Value.Kind() == slog.KindGroup {
		prefix := group
		if a.Key != "" {
			prefix += a.Key + "."
		}
		for _, member := range a.Value.Group() {
			appendAttr(b, prefix, member)
		}
		return
	}
	b.WriteByte(' ')
	b.WriteString(group + a.Key)
	b.WriteByte('=')
	value := a.Value.String()
	if needsQuoting(value) {
		value = strconv.Quote(value)
	}
	b.WriteString(value)
}

// needsQuoting says whether a value would not read back as one word. A byte
// that is not UTF-8 decodes as RuneError and is quoted, which escapes it.
func needsQuoting(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		if r == '=' || r == '"' || r == utf8.RuneError || unicode.IsSpace(r) || !unicode.IsPrint(r) {
			return true
		}
	}
	return false
}
