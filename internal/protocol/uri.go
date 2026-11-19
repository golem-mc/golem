package protocol

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// ValidUntrustedURI reports whether s is an http or https URI in Java's
// syntax: a scheme-specific part, at most one fragment marker, and grammar
// characters only.
// vanilla: ExtraCodecs.UNTRUSTED_URI, Util.parseAndValidateUntrustedUri
func ValidUntrustedURI(s string) bool {
	return UntrustedURIMalformed(s) == ""
}

// validURIComponent checks a URI component against Java's grammar: letters,
// digits, reserved and mark characters, escapes, and brackets where allowed.
// Non-ASCII stands unless it is a space or ISO control, which Java refuses.
func validURIComponent(s string, brackets bool) bool {
	for i := 0; i < len(s); {
		switch c := s[i]; {
		case c == '%':
			if i+2 >= len(s) || !isHexDigit(s[i+1]) || !isHexDigit(s[i+2]) {
				return false
			}
			i += 3
		case c < 0x80:
			if brackets && (c == '[' || c == ']') || isURIChar(c) {
				i++
				continue
			}
			return false
		default:
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && size <= 1 {
				return false
			}
			if unicode.IsSpace(r) || unicode.IsControl(r) {
				return false
			}
			i += size
		}
	}
	return true
}

// validURIScheme reports whether a string is a URI scheme: a letter followed
// by letters, digits, +, - or .
func validURIScheme(s string) bool {
	if s == "" || !isURISchemeAlpha(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if isURISchemeAlpha(c) || c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.' {
			continue
		}
		return false
	}
	return true
}

// isURISchemeAlpha reports whether c is an ASCII letter.
func isURISchemeAlpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// isURIChar reports whether c is a character a URI carries unescaped: a
// letter or digit, or one of RFC 2396's reserved and mark characters.
func isURIChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		strings.IndexByte("-_.!~*'();/?:@&=+$,#", c) >= 0
}

// isHexDigit reports whether c is a hexadecimal digit.
func isHexDigit(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}
