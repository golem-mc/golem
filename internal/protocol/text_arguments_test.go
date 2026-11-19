package protocol

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// vanilla: TranslatableContents.ARG_CODEC
func TestTextTagPreservesNumericArguments(t *testing.T) {
	t.Parallel()
	number, long, fraction := int64(7), int64(1<<40), float32(1.5)
	for name, arg := range map[string]Text{
		"int": {Number: &number}, "long": {Long: &long}, "float": {Float: &fraction},
	} {
		t.Run(name, func(t *testing.T) {
			for _, with := range [][]Text{
				{arg}, {arg, {Text: "Good"}}, {arg, {Text: "Good", Color: ColorRed}},
				{arg, {Number: &number}, {Long: &long}, {Float: &fraction}},
			} {
				want := Text{Translate: "%s %s", With: with}
				var wire Buffer
				wire.PutText(want)
				wantTag, _, err := nbt.DecodeNetwork(wire.Bytes())
				if err != nil {
					t.Fatal(err)
				}
				file, err := nbt.EncodeNetwork(TextTag(want))
				if err != nil {
					t.Fatal(err)
				}
				gotTag, _, err := nbt.DecodeNetwork(file)
				if err != nil || !nbt.Equal(gotTag, wantTag) {
					t.Fatalf("stored tag = %v (%v), want wire tag %v", gotTag, err, wantTag)
				}
				got, ok := TextOf(gotTag)
				if !ok || !TextEqual(got, want) {
					t.Fatalf("stored arguments = %+v, readable %v, want %+v", got.With, ok, with)
				}
			}
		})
	}
}
