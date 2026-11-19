package protocol

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func BenchmarkNativeStackTextTag(b *testing.B) {
	for _, fixture := range []struct {
		name string
		text Text
	}{
		{"typed", Text{Text: "label", Color: "red", Italic: true, Extra: []Text{{Text: "extra", Color: "blue"}}, HoverText: &Text{Text: "hover", Color: "yellow"}}},
		{"raw", Text{Raw: nbt.Compound{"text": nbt.String("label"), "bold": nbt.Byte(1), "italic": nbt.Byte(0), "extra": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("extra")}}}}},
	} {
		b.Run(fixture.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				TextTag(fixture.text)
			}
		})
	}
}
