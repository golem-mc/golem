package nbt

import (
	"errors"
	"testing"
)

// vanilla: SnbtGrammar.string_hex_8, string_escape_sequence
func TestSNBTUnicodeCodepointEscapes(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`"\U00000041"`, "A"},
		{`"\U0001F600"`, "😀"},
		{`"\U0010FFFF"`, "\U0010ffff"},
	} {
		tag, end, err := ParseValueTag(tc.input)
		if err != nil || tag != String(tc.want) || end != len(tc.input) {
			t.Errorf("parse %q = %v, %d, %v", tc.input, tag, end, err)
		}
	}
	for _, input := range []string{`"\U00110000"`, `"\UFFFFFFFF"`} {
		_, _, err := ParseValueTag(input)
		var pe *ParseError
		if !errors.As(err, &pe) || pe.Key != errInvalidCodepoint {
			t.Errorf("parse %q = %v, want invalid code point", input, err)
		}
	}
}
