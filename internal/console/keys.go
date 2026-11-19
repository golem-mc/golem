package console

import "unicode/utf8"

// KeyKind is a key press as the line editor reads it; anything else a
// terminal sends is KeyUnknown.
type KeyKind uint8

const (
	// KeyRune is a printable character, whose rune is on the Key.
	KeyRune KeyKind = iota
	// KeyEnter is the enter key, which submits the line.
	KeyEnter
	// KeyTab is the tab key, which completes the word at the cursor.
	KeyTab
	// KeyBackspace takes the character before the cursor away.
	KeyBackspace
	// KeyDelete takes the character after the cursor away.
	KeyDelete
	// KeyLeft moves the cursor one character back.
	KeyLeft
	// KeyRight moves the cursor one character forward.
	KeyRight
	// KeyUp recalls the previous line of the history.
	KeyUp
	// KeyDown walks the history back towards the line being typed.
	KeyDown
	// KeyHome moves the cursor to the start of the line.
	KeyHome
	// KeyEnd moves the cursor to the end of the line.
	KeyEnd
	// KeyClear is Ctrl-U, which wipes the whole line.
	KeyClear
	// KeyKill is Ctrl-K, which wipes the line from the cursor to its end.
	KeyKill
	// KeyCtrlC is Ctrl-C, which the session asks the server to stop with.
	KeyCtrlC
	// KeyCtrlD is Ctrl-D, which ends the console on an empty line.
	KeyCtrlD
	// KeyUnknown is consumed so a sequence the reader does not know cannot
	// wedge the decoding of the keys after it.
	KeyUnknown
)

// Key is one press of a key.
type Key struct {
	Kind KeyKind
	// Rune is the character pressed, set for a KeyRune and none of the rest.
	Rune rune
}

// keyReader decodes the bytes a terminal sends into keys. A read can cut
// an escape sequence or a rune's UTF-8 bytes in half, so the reader keeps
// what it cannot yet read past and finishes it when the next bytes arrive.
type keyReader struct {
	pending []byte
}

// Keys decodes every complete key the chunk of bytes carries and keeps the
// tail that is not one yet.
func (k *keyReader) Keys(b []byte) []Key {
	k.pending = append(k.pending, b...)
	var keys []Key
	for {
		key, n, ok := decodeKey(k.pending)
		if !ok {
			break
		}
		k.pending = k.pending[n:]
		keys = append(keys, key)
	}
	return keys
}

// waiting reports whether an escape is held whose sequence has not finished.
func (k *keyReader) waiting() bool {
	return len(k.pending) > 0 && k.pending[0] == 0x1b
}

// Flush gives up on the held escape, which was the escape key alone (and
// ignored), and decodes what came after it afresh.
func (k *keyReader) Flush() []Key {
	if !k.waiting() {
		return nil
	}
	k.pending = k.pending[1:]
	return append([]Key{{Kind: KeyUnknown}}, k.Keys(nil)...)
}

// decodeKey reads the first key of b, answering the bytes it took and
// whether a complete key was there.
func decodeKey(b []byte) (Key, int, bool) {
	if len(b) == 0 {
		return Key{}, 0, false
	}
	c := b[0]
	switch {
	case c == 0x1b:
		return decodeEscape(b)
	case c == 0x0d || c == 0x0a:
		return Key{Kind: KeyEnter}, 1, true
	case c == 0x7f || c == 0x08:
		return Key{Kind: KeyBackspace}, 1, true
	case c == 0x09:
		return Key{Kind: KeyTab}, 1, true
	case c == 0x03:
		return Key{Kind: KeyCtrlC}, 1, true
	case c == 0x04:
		return Key{Kind: KeyCtrlD}, 1, true
	case c == 0x15:
		return Key{Kind: KeyClear}, 1, true
	case c == 0x0b:
		return Key{Kind: KeyKill}, 1, true
	case c == 0x01:
		return Key{Kind: KeyHome}, 1, true
	case c == 0x05:
		return Key{Kind: KeyEnd}, 1, true
	case c < 0x20:
		return Key{Kind: KeyUnknown}, 1, true
	case c < 0x80:
		return Key{Kind: KeyRune, Rune: rune(c)}, 1, true
	}
	// A rune's UTF-8 bytes are read as one, waiting out a sequence the
	// read has cut in half; a byte that starts nothing valid is skipped.
	need := utf8SeqLen(c)
	if need == 0 {
		return Key{Kind: KeyUnknown}, 1, true
	}
	if len(b) < need {
		return Key{}, 0, false
	}
	r, size := utf8.DecodeRune(b[:need])
	if r == utf8.RuneError {
		return Key{Kind: KeyUnknown}, 1, true
	}
	return Key{Kind: KeyRune, Rune: r}, size, true
}

// utf8SeqLen is the encoded length of the rune whose first byte is c, or
// zero when c starts none.
func utf8SeqLen(c byte) int {
	switch {
	case c&0xE0 == 0xC0:
		return 2
	case c&0xF0 == 0xE0:
		return 3
	case c&0xF8 == 0xF0:
		return 4
	}
	return 0
}

// decodeEscape reads the arrow, home, end and delete keys in both encodings
// terminals send; any other sequence is skipped whole so the keys after it
// still arrive.
func decodeEscape(b []byte) (Key, int, bool) {
	if len(b) == 1 {
		// A lone escape may be the head of a sequence still to arrive.
		return Key{}, 0, false
	}
	switch b[1] {
	case 0x1b:
		// The escape key pressed before another sequence: only the first
		// escape is dropped, so the key after it still decodes.
		return Key{Kind: KeyUnknown}, 1, true
	case '[':
		// A CSI sequence runs to its final byte, which the parameters
		// before it never share a range with.
		for i := 2; i < len(b); i++ {
			if b[i] < 0x40 || b[i] > 0x7e {
				continue
			}
			switch string(b[:i+1]) {
			case "\x1b[A":
				return Key{Kind: KeyUp}, i + 1, true
			case "\x1b[B":
				return Key{Kind: KeyDown}, i + 1, true
			case "\x1b[C":
				return Key{Kind: KeyRight}, i + 1, true
			case "\x1b[D":
				return Key{Kind: KeyLeft}, i + 1, true
			case "\x1b[H", "\x1b[1~":
				return Key{Kind: KeyHome}, i + 1, true
			case "\x1b[F", "\x1b[4~":
				return Key{Kind: KeyEnd}, i + 1, true
			case "\x1b[3~":
				return Key{Kind: KeyDelete}, i + 1, true
			}
			return Key{Kind: KeyUnknown}, i + 1, true
		}
		return Key{}, 0, false
	case 'O':
		if len(b) < 3 {
			return Key{}, 0, false
		}
		switch b[2] {
		case 'A':
			return Key{Kind: KeyUp}, 3, true
		case 'B':
			return Key{Kind: KeyDown}, 3, true
		case 'C':
			return Key{Kind: KeyRight}, 3, true
		case 'D':
			return Key{Kind: KeyLeft}, 3, true
		case 'H':
			return Key{Kind: KeyHome}, 3, true
		case 'F':
			return Key{Kind: KeyEnd}, 3, true
		}
		return Key{Kind: KeyUnknown}, 3, true
	}
	// An alt-keypress, which the console does not edit with.
	return Key{Kind: KeyUnknown}, 2, true
}
