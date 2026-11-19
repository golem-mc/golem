package console

import "testing"

// decode reads the keys one chunk of a terminal's bytes carries.
func decode(t *testing.T, b []byte) []Key {
	t.Helper()
	var kr keyReader
	return kr.Keys(b)
}

// decodeInChunks reads the keys bytes cut into pieces would arrive as,
// which is how a read can cut an escape sequence or a rune in half.
func decodeInChunks(t *testing.T, chunks ...string) []Key {
	t.Helper()
	var kr keyReader
	var keys []Key
	for _, chunk := range chunks {
		keys = append(keys, kr.Keys([]byte(chunk))...)
	}
	return keys
}

func TestDecodeKeys(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantKind KeyKind
		wantRune rune
	}{
		{name: "a character", input: "a", wantKind: KeyRune, wantRune: 'a'},
		{name: "a space", input: " ", wantKind: KeyRune, wantRune: ' '},
		{name: "a multi-byte rune", input: "é", wantKind: KeyRune, wantRune: 'é'},
		{name: "an emoji", input: "🐢", wantKind: KeyRune, wantRune: '🐢'},
		{name: "enter", input: "\r", wantKind: KeyEnter},
		{name: "a newline", input: "\n", wantKind: KeyEnter},
		{name: "backspace", input: "\x7f", wantKind: KeyBackspace},
		{name: "tab", input: "\t", wantKind: KeyTab},
		{name: "ctrl-c", input: "\x03", wantKind: KeyCtrlC},
		{name: "ctrl-d", input: "\x04", wantKind: KeyCtrlD},
		{name: "ctrl-u", input: "\x15", wantKind: KeyClear},
		{name: "ctrl-a", input: "\x01", wantKind: KeyHome},
		{name: "ctrl-e", input: "\x05", wantKind: KeyEnd},
		{name: "an arrow up", input: "\x1b[A", wantKind: KeyUp},
		{name: "an arrow down", input: "\x1b[B", wantKind: KeyDown},
		{name: "an arrow right", input: "\x1b[C", wantKind: KeyRight},
		{name: "an arrow left", input: "\x1b[D", wantKind: KeyLeft},
		{name: "an arrow up in application mode", input: "\x1bOA", wantKind: KeyUp},
		{name: "a home key", input: "\x1b[H", wantKind: KeyHome},
		{name: "a home key numbered", input: "\x1b[1~", wantKind: KeyHome},
		{name: "an end key", input: "\x1b[F", wantKind: KeyEnd},
		{name: "an end key numbered", input: "\x1b[4~", wantKind: KeyEnd},
		{name: "a delete key", input: "\x1b[3~", wantKind: KeyDelete},
		{name: "a sequence the console does not know", input: "\x1b[5~", wantKind: KeyUnknown},
		{name: "an alt-keypress", input: "\x1ba", wantKind: KeyUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			keys := decode(t, []byte(test.input))
			if len(keys) != 1 {
				t.Fatalf("decoded %d keys, want 1", len(keys))
			}
			if keys[0].Kind != test.wantKind {
				t.Errorf("kind = %v, want %v", keys[0].Kind, test.wantKind)
			}
			if keys[0].Rune != test.wantRune {
				t.Errorf("rune = %q, want %q", keys[0].Rune, test.wantRune)
			}
			// The whole input was consumed by the one key.
			var kr keyReader
			kr.Keys([]byte(test.input))
			if len(kr.pending) != 0 {
				t.Errorf("%d bytes left pending, want none", len(kr.pending))
			}
		})
	}
}

func TestDecodeEscapeKey(t *testing.T) {
	// The escape key, given up on, is one ignored key; the key typed after
	// it is itself rather than half of an alt-keypress.
	var kr keyReader
	if keys := kr.Keys([]byte("lis\x1b")); len(keys) != 3 || !kr.waiting() {
		t.Fatalf("decoded %v, waiting %v; want three runes and a held escape", keys, kr.waiting())
	}
	if keys := kr.Flush(); len(keys) != 1 || keys[0].Kind != KeyUnknown || kr.waiting() {
		t.Fatalf("flushed %v, want one unknown", keys)
	}
	if keys := kr.Keys([]byte("t")); len(keys) != 1 || keys[0].Rune != 't' {
		t.Fatalf("decoded %v, want t", keys)
	}

	// The escape key just before an arrow drops only the escape.
	keys := decode(t, []byte("\x1b\x1b[D"))
	if len(keys) != 2 || keys[0].Kind != KeyUnknown || keys[1].Kind != KeyLeft {
		t.Fatalf("decoded %v, want unknown then left", keys)
	}
}
