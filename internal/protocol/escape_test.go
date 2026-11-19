package protocol

import "testing"

func TestEscapeControls(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"plain 日本 \"quoted\" \\ ok", "plain 日本 \"quoted\" \\ ok"},
		{"two\nlines", "two\nlines"},
		{"\x1b]0;pwned\x07\x1b[2K\rFAKE LINE", `\x1b]0;pwned\a\x1b[2K\rFAKE LINE`},
		{"tab\tdel\x7f c1\u0085", `tab\tdel\x7f c1\u0085`},
		// Not UTF-8: a lone 0x9B is the 8-bit CSI. A real U+FFFD is text.
		{"me \x9b31mC1RED \x9b2J", `me \x9b31mC1RED \x9b2J`},
		{"replaced \uFFFD", "replaced \uFFFD"},
	} {
		if got := EscapeControls(tc.in); got != tc.want {
			t.Errorf("EscapeControls(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
