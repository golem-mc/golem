package console

import (
	"strings"
	"testing"
)

// keys turns a written shorthand into the keys it stands for: characters
// typed as themselves, special keys by the names the constants carry.
func keys(t *testing.T, shorthand string) []Key {
	t.Helper()
	var keys []Key
	for part := range strings.SplitSeq(shorthand, " ") {
		switch part {
		case "enter":
			keys = append(keys, Key{Kind: KeyEnter})
		case "tab":
			keys = append(keys, Key{Kind: KeyTab})
		case "backspace":
			keys = append(keys, Key{Kind: KeyBackspace})
		case "delete":
			keys = append(keys, Key{Kind: KeyDelete})
		case "left":
			keys = append(keys, Key{Kind: KeyLeft})
		case "right":
			keys = append(keys, Key{Kind: KeyRight})
		case "up":
			keys = append(keys, Key{Kind: KeyUp})
		case "down":
			keys = append(keys, Key{Kind: KeyDown})
		case "home":
			keys = append(keys, Key{Kind: KeyHome})
		case "end":
			keys = append(keys, Key{Kind: KeyEnd})
		case "ctrl-c":
			keys = append(keys, Key{Kind: KeyCtrlC})
		case "ctrl-d":
			keys = append(keys, Key{Kind: KeyCtrlD})
		case "ctrl-u":
			keys = append(keys, Key{Kind: KeyClear})
		case "ctrl-k":
			keys = append(keys, Key{Kind: KeyKill})
		default:
			// The characters of the word, whose underscores stand for
			// the typed spaces a shorthand's own spaces cannot be.
			for _, r := range part {
				if r == '_' {
					r = ' '
				}
				keys = append(keys, Key{Kind: KeyRune, Rune: r})
			}
		}
	}
	return keys
}

// press presses the keys of a shorthand on a fresh editor, accepting what
// an enter submits the way the session does, and answers what the last key
// left behind — after a submit, the line it carried in.
func press(t *testing.T, shorthand string) (string, int, editAction) {
	t.Helper()
	e := &editor{}
	var line string
	var cursor int
	var action editAction
	for _, k := range keys(t, shorthand) {
		action = e.Key(k)
		if action == editSubmit {
			line, cursor = e.Text(), e.cursor
			e.Accept()
			continue
		}
		line, cursor = e.Text(), e.cursor
	}
	return line, cursor, action
}

func TestEditorTypesAtTheCursor(t *testing.T) {
	tests := []struct {
		name     string
		pressed  string
		wantLine string
		wantAt   int
		want     editAction
	}{
		{name: "typed at the end", pressed: "say_hi", wantLine: "say hi", wantAt: 6, want: editDraw},
		{name: "typed in the middle", pressed: "say_hi left x", wantLine: "say hxi", wantAt: 6, want: editDraw},
		{name: "typed at the start", pressed: "hi home x", wantLine: "xhi", wantAt: 1, want: editDraw},
		{name: "backspace takes before the cursor", pressed: "say_hi backspace", wantLine: "say h", wantAt: 5, want: editDraw},
		{name: "backspace at the start does nothing", pressed: "hi home backspace", wantLine: "hi", wantAt: 0, want: editIgnore},
		{name: "delete takes after the cursor", pressed: "say_hi home delete", wantLine: "ay hi", wantAt: 0, want: editDraw},
		{name: "delete at the end does nothing", pressed: "hi delete", wantLine: "hi", wantAt: 2, want: editIgnore},
		{name: "left walks back", pressed: "hi left left", wantLine: "hi", wantAt: 0, want: editDraw},
		{name: "left at the start does nothing", pressed: "hi home left", wantLine: "hi", wantAt: 0, want: editIgnore},
		{name: "right walks forward", pressed: "hi left right", wantLine: "hi", wantAt: 2, want: editDraw},
		{name: "right at the end does nothing", pressed: "hi right", wantLine: "hi", wantAt: 2, want: editIgnore},
		{name: "home goes to the start", pressed: "hi home", wantLine: "hi", wantAt: 0, want: editDraw},
		{name: "end goes to the end", pressed: "hi home end", wantLine: "hi", wantAt: 2, want: editDraw},
		{name: "ctrl-u wipes the line", pressed: "say_hi ctrl-u", wantLine: "", wantAt: 0, want: editDraw},
		{name: "ctrl-k wipes to the end", pressed: "say_hi home ctrl-k", wantLine: "", wantAt: 0, want: editDraw},
		{name: "enter submits", pressed: "say_hi enter", wantLine: "say hi", wantAt: 6, want: editSubmit},
		{name: "a second character draws", pressed: "hi", wantLine: "hi", wantAt: 2, want: editDraw},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			line, cursor, action := press(t, test.pressed)
			if line != test.wantLine {
				t.Errorf("line = %q, want %q", line, test.wantLine)
			}
			if cursor != test.wantAt {
				t.Errorf("cursor = %d, want %d", cursor, test.wantAt)
			}
			if action != test.want {
				t.Errorf("action = %v, want %v", action, test.want)
			}
		})
	}
}

func TestEditorHistoryWalks(t *testing.T) {
	e := &editor{}
	press := func(shorthand string) (string, editAction) {
		var line string
		var action editAction
		for _, k := range keys(t, shorthand) {
			action = e.Key(k)
			if action == editSubmit {
				line, action = e.Text(), editSubmit
				e.Accept()
				continue
			}
			line = e.Text()
		}
		return line, action
	}

	// Two lines submitted, then the history walked up and back down, the
	// line being typed kept for the walk's return.
	if line, action := press("say_one enter"); line != "say one" || action != editSubmit {
		t.Fatalf("first submit = %q, %v", line, action)
	}
	if line, action := press("say_two enter"); line != "say two" || action != editSubmit {
		t.Fatalf("second submit = %q, %v", line, action)
	}
	if line, action := press("say_draft"); line != "say draft" || action != editDraw {
		t.Fatalf("draft = %q, %v", line, action)
	}
	if line, _ := press("up"); line != "say two" {
		t.Errorf("one up = %q, want say two", line)
	}
	if line, _ := press("up"); line != "say one" {
		t.Errorf("two up = %q, want say one", line)
	}
	if _, action := press("up"); action != editIgnore {
		t.Errorf("up past the first line = %v, want editIgnore", action)
	}
	if line, _ := press("down"); line != "say two" {
		t.Errorf("one down = %q, want say two", line)
	}
	if line, _ := press("down"); line != "say draft" {
		t.Errorf("two down = %q, want say draft", line)
	}
	if _, action := press("down"); action != editIgnore {
		t.Errorf("down past the draft = %v, want editIgnore", action)
	}

	// A recalled line edited and submitted again joins the history as the
	// line it became.
	if line, _ := press("up"); line != "say two" {
		t.Fatalf("recall = %q, want say two", line)
	}
	if line, action := press("backspace x enter"); line != "say twx" || action != editSubmit {
		t.Fatalf("edited submit = %q, %v", line, action)
	}
	if line, _ := press("up"); line != "say twx" {
		t.Errorf("the edited line recalled = %q, want say twx", line)
	}
}

func TestEditorAcceptRecordsHistory(t *testing.T) {
	e := &editor{}
	e.Key(Key{Kind: KeyRune, Rune: 'h'})
	e.Key(Key{Kind: KeyRune, Rune: 'i'})
	e.Accept()
	if len(e.history) != 1 || e.history[0] != "hi" {
		t.Fatalf("history = %v, want [hi]", e.history)
	}
	if len(e.line) != 0 || e.cursor != 0 || e.histPos != 1 {
		t.Fatalf("editor not reset: %q, %d, %d", e.Text(), e.cursor, e.histPos)
	}

	// An empty line joins no history, and the same line twice keeps one
	// entry.
	e.Accept()
	if len(e.history) != 1 {
		t.Errorf("empty accept grew the history to %v", e.history)
	}
	e.Key(Key{Kind: KeyRune, Rune: 'h'})
	e.Key(Key{Kind: KeyRune, Rune: 'i'})
	e.Accept()
	if len(e.history) != 1 {
		t.Errorf("repeat accept grew the history to %v", e.history)
	}
}
