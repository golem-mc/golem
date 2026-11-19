package nbt

import (
	"errors"
	"strings"
	"testing"
)

// TestParseSNBTDepthMatchesDecode checks SNBT nests exactly as deep as
// Decode reads back, and no deeper, so a command cannot store a tree whose
// save the next read refuses.
func TestParseSNBTDepthMatchesDecode(t *testing.T) {
	// nested is a root compound holding lists nested n deep under a, the
	// innermost holding inner.
	nested := func(n int, inner string) string {
		return "{a:" + strings.Repeat("[", n) + inner + strings.Repeat("]", n) + "}"
	}

	// A typed array's numbers are no deeper than the array, which Decode
	// reads as one tag.
	for _, text := range []string{nested(maxDepth, ""), nested(maxDepth-1, "1b"), nested(maxDepth-1, "{}"), nested(maxDepth-1, "[I;1,2]")} {
		tree, err := ParseSNBT(text)
		if err != nil {
			t.Fatalf("ParseSNBT at the limit: %v", err)
		}
		data, err := Encode("", tree)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		if _, _, err := Decode(data); err != nil {
			t.Fatalf("Decode what ParseSNBT read at the limit: %v", err)
		}
	}

	for _, text := range []string{nested(maxDepth+1, ""), nested(maxDepth, "1b"), nested(maxDepth, "{}")} {
		_, err := ParseSNBT(text)
		var pe *ParseError
		if !errors.As(err, &pe) || pe.Key != errTooDeep {
			t.Errorf("ParseSNBT one past the limit = %v, want the %s error", err, errTooDeep)
		}
	}
}
