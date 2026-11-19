package command

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestPrimitiveTagReadsOnlyNumbersAsNumbers pins the bare-token reader: a
// token becomes a number only where it carries a digit, so the words Go's
// ParseFloat would still take — inf, infinity and nan, with either suffix —
// stay strings. The boolean and suffixed forms around them keep their widths.
// vanilla: SnbtGrammar.canStartNumber
func TestPrimitiveTagReadsOnlyNumbersAsNumbers(t *testing.T) {
	cases := []struct {
		text string
		want nbt.Tag
	}{
		{"true", nbt.Byte(1)},
		{"false", nbt.Byte(0)},
		{"5", nbt.Int(5)},
		{"-100", nbt.Int(-100)},
		{"70000", nbt.Int(70000)},
		{"5b", nbt.Byte(5)},
		{"5s", nbt.Short(5)},
		{"5l", nbt.Long(5)},
		{"2.7f", nbt.Float(2.7)},
		{"1.5d", nbt.Double(1.5)},
		{"1.5", nbt.Double(1.5)},
		{"inf", nbt.String("inf")},
		{"Inf", nbt.String("Inf")},
		{"infinity", nbt.String("infinity")},
		{"nan", nbt.String("nan")},
		{"NaN", nbt.String("NaN")},
		{"inff", nbt.String("inff")},
		{"nand", nbt.String("nand")},
		{"infinityd", nbt.String("infinityd")},
	}
	for _, tc := range cases {
		if got, _, err := nbt.ParseValueTag(tc.text); err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("primitiveTag(%q) = %#v (%T), want %#v", tc.text, got, got, tc.want)
		}
	}
}
