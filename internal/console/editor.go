package console

// editor is the line being typed, its cursor and its history; it knows
// nothing of terminals, and the session draws what it changes.
type editor struct {
	line   []rune
	cursor int
	// history is the lines submitted so far, oldest first, and histPos is
	// where the arrow keys stand in it: len(history) is the line being
	// typed, whose unfinished state draft keeps while the history is walked.
	history []string
	histPos int
	draft   string
}

// editAction is what a key press asks the session to do about it.
type editAction uint8

const (
	// editDraw redraws the line, which the key changed.
	editDraw editAction = iota
	// editSubmit submits the line, which the enter key ended.
	editSubmit
	// editComplete asks for the word at the cursor to be completed (tab).
	editComplete
	// editInterrupt asks the server to stop, which Ctrl-C asks for.
	editInterrupt
	// editIgnore does nothing: the key changed nothing.
	editIgnore
)

// Key applies a key press and answers what the session should do about it.
func (e *editor) Key(k Key) editAction {
	switch k.Kind {
	case KeyRune:
		e.insert(k.Rune)
		return editDraw
	case KeyEnter:
		return editSubmit
	case KeyTab:
		return editComplete
	case KeyBackspace:
		if e.cursor == 0 {
			return editIgnore
		}
		e.delete(e.cursor-1, e.cursor)
		return editDraw
	case KeyDelete:
		if e.cursor == len(e.line) {
			return editIgnore
		}
		e.delete(e.cursor, e.cursor+1)
		return editDraw
	case KeyLeft:
		if e.cursor == 0 {
			return editIgnore
		}
		e.cursor--
		return editDraw
	case KeyRight:
		if e.cursor == len(e.line) {
			return editIgnore
		}
		e.cursor++
		return editDraw
	case KeyUp:
		if e.histPos == 0 {
			return editIgnore
		}
		if e.histPos == len(e.history) {
			e.draft = e.Text()
		}
		e.histPos--
		e.setLine(e.history[e.histPos])
		return editDraw
	case KeyDown:
		if e.histPos >= len(e.history) {
			return editIgnore
		}
		e.histPos++
		if e.histPos == len(e.history) {
			e.setLine(e.draft)
		} else {
			e.setLine(e.history[e.histPos])
		}
		return editDraw
	case KeyHome:
		if e.cursor == 0 {
			return editIgnore
		}
		e.cursor = 0
		return editDraw
	case KeyEnd:
		if e.cursor == len(e.line) {
			return editIgnore
		}
		e.cursor = len(e.line)
		return editDraw
	case KeyClear:
		if len(e.line) == 0 {
			return editIgnore
		}
		e.line = e.line[:0]
		e.cursor = 0
		return editDraw
	case KeyKill:
		if e.cursor == len(e.line) {
			return editIgnore
		}
		e.line = e.line[:e.cursor]
		return editDraw
	case KeyCtrlD:
		if e.cursor == len(e.line) {
			return editIgnore
		}
		e.delete(e.cursor, e.cursor+1)
		return editDraw
	case KeyCtrlC:
		return editInterrupt
	}
	return editIgnore
}

// Text is the line as it stands.
func (e *editor) Text() string { return string(e.line) }

// Accept submits the line, adding it to the history unless it is empty or
// repeats the last one.
func (e *editor) Accept() {
	if text := e.Text(); text != "" && (len(e.history) == 0 || e.history[len(e.history)-1] != text) {
		e.history = append(e.history, text)
	}
	e.histPos = len(e.history)
	e.draft = ""
	e.line = e.line[:0]
	e.cursor = 0
}

// ReplaceText replaces the runes between the two indexes with text, moving
// the cursor to its end; tab completion uses it.
func (e *editor) ReplaceText(from, to int, text string) {
	replacement := []rune(text)
	line := make([]rune, 0, len(e.line)-(to-from)+len(replacement))
	line = append(line, e.line[:from]...)
	line = append(line, replacement...)
	line = append(line, e.line[to:]...)
	e.line = line
	e.cursor = from + len(replacement)
}

// insert puts a character at the cursor.
func (e *editor) insert(r rune) {
	e.line = append(e.line, 0)
	copy(e.line[e.cursor+1:], e.line[e.cursor:])
	e.line[e.cursor] = r
	e.cursor++
}

// delete removes the runes between the two indexes, leaving the cursor at
// from.
func (e *editor) delete(from, to int) {
	copy(e.line[from:], e.line[to:])
	e.line = e.line[:len(e.line)-(to-from)]
	e.cursor = from
}

// setLine replaces the line wholesale, as recalling a history entry does.
func (e *editor) setLine(text string) {
	e.line = []rune(text)
	e.cursor = len(e.line)
}
