package nbt

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// TestParseSNBTEmptyKeyBehindATrailingComma checks an empty name fails its
// compound whatever follows its value, and leaves nothing behind that fails
// a compound read after it.
func TestParseSNBTEmptyKeyBehindATrailingComma(t *testing.T) {
	for _, text := range []string{
		`{"":1b,}`,
		`{'':1b,}`,
		`{a:{"":1b,}}`,
		`{a:[{"":1b,}]}`,
		`{"":1b,a:2b}`,
	} {
		if got, err := ParseSNBT(text); err == nil {
			t.Errorf("ParseSNBT(%s) = %v, want an error", text, got)
		}
	}

	for _, tc := range []struct {
		text string
		want Compound
	}{
		{`{a:1b,}`, Compound{"a": Byte(1)}},
		{`{a:[{b:1b,},{c:2b}]}`, Compound{"a": List{Elem: TypeCompound, Items: []Tag{
			Compound{"b": Byte(1)}, Compound{"c": Byte(2)},
		}}}},
	} {
		got, err := ParseSNBT(tc.text)
		if err != nil {
			t.Errorf("ParseSNBT(%s): %v", tc.text, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ParseSNBT(%s) = %#v, want %#v", tc.text, got, tc.want)
		}
	}

	// A value read after a refused one starts over.
	if _, _, pe := ParseValueTag(`{"":1b,}`); pe == nil {
		t.Fatal(`ParseValueTag({"":1b,}) succeeded, want an error`)
	}
	if _, err := ParseSNBT(`{a:{b:1b}}`); err != nil {
		t.Errorf("ParseSNBT after a refused empty key: %v", err)
	}
}

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
