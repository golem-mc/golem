package protocol

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// EscapeControls writes the control characters of s other than the newline
// as Go escapes (\x1b, \r, \u0085), and any byte that is not UTF-8 as \x9b,
// so that text a player wrote cannot drive the terminal it is printed on: a
// lone 0x9B is the 8-bit CSI to a terminal outside UTF-8 mode.
func EscapeControls(s string) string {
	if utf8.ValidString(s) && !strings.ContainsFunc(s, escaped) {
		return s
	}
	const hex = "0123456789abcdef"
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			b.WriteString(`\x`)
			b.WriteByte(hex[s[i]>>4])
			b.WriteByte(hex[s[i]&0xF])
		case escaped(r):
			q := strconv.QuoteRune(r)
			b.WriteString(q[1 : len(q)-1])
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

func escaped(r rune) bool { return r != '\n' && unicode.IsControl(r) }
