package protocol

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// vanilla: ComponentSerialization.createFromList
func TestTextOfListAppendsToFirstComponent(t *testing.T) {
	t.Parallel()
	first := nbt.Compound{
		"text": nbt.String("prefix"), "color": nbt.String("red"),
		"extra": nbt.NewList([]nbt.Tag{nbt.String("existing")}),
	}
	for name, tag := range map[string]nbt.Tag{
		"single": nbt.NewList([]nbt.Tag{first}),
		"mixed":  nbt.NewList([]nbt.Tag{first, nbt.String("suffix")}),
		"nested": nbt.NewList([]nbt.Tag{first, nbt.NewList([]nbt.Tag{nbt.String("suffix")})}),
	} {
		t.Run(name, func(t *testing.T) {
			want := Text{Text: "prefix", Color: "red", Extra: []Text{{Text: "existing"}}}
			if name != "single" {
				want.Extra = append(want.Extra, Text{Text: "suffix"})
			}
			got, ok := TextOf(tag)
			if !ok || !TextEqual(got, want) {
				t.Fatalf("TextOf = %+v, %v, want %+v", got, ok, want)
			}
			var wire Buffer
			wire.PutText(got)
			read, ok := ReadText(NewReader(wire.Bytes()))
			if !ok || !TextEqual(read, want) {
				t.Fatalf("ReadText = %+v, %v, want %+v", read, ok, want)
			}
		})
	}
}

func TestStyledTextOfListKeepsStyleAndRejectsInvalidEntries(t *testing.T) {
	t.Parallel()
	tag := nbt.NewList([]nbt.Tag{
		nbt.String("prefix"), nbt.Compound{"text": nbt.String("suffix"), "bold": nbt.Byte(1)},
	})
	got, ok := StyledTextOf(tag)
	if !ok || !nbt.Equal(got.Raw, tag) {
		t.Fatalf("StyledTextOf = %+v, %v, want the list and its bold suffix", got, ok)
	}
	for _, invalid := range []nbt.Tag{
		nbt.NewList(nil),
		nbt.NewList([]nbt.Tag{nbt.Int(1)}),
		nbt.NewList([]nbt.Tag{nbt.String("prefix"), nbt.Int(1)}),
		nbt.NewList([]nbt.Tag{nbt.String("prefix"), nbt.NewList(nil)}),
		nbt.NewList([]nbt.Tag{nbt.String("prefix"), nbt.Compound{"text": nbt.String("suffix"), "bold": nbt.String("true")}}),
	} {
		if text, ok := StyledTextOf(invalid); ok {
			t.Errorf("StyledTextOf(%v) = %+v, want invalid component refused", invalid, text)
		}
	}
}

func TestRawTextListElementsSurviveModelledParents(t *testing.T) {
	t.Parallel()
	suffix := nbt.Compound{"text": nbt.String("suffix"), "bold": nbt.Byte(1)}
	for _, tag := range []nbt.Tag{
		suffix,
		nbt.NewList([]nbt.Tag{nbt.String("prefix"), suffix}),
	} {
		raw := Text{Raw: tag}
		for _, parent := range []Text{
			{Text: "parent", Extra: []Text{raw}},
			{Translate: "chat.square_brackets", With: []Text{raw}},
		} {
			var wire Buffer
			wire.PutText(parent)
			got, _, err := nbt.DecodeNetwork(wire.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			// Decode the file form too: both readers remove mixed-list wrappers.
			file, err := nbt.EncodeNetwork(TextTag(parent))
			if err != nil {
				t.Fatal(err)
			}
			want, _, err := nbt.DecodeNetwork(file)
			if err != nil {
				t.Fatal(err)
			}
			if !nbt.Equal(got, want) {
				t.Errorf("parent with raw %v = %v, want %v", tag, got, want)
			}
		}
	}
}
