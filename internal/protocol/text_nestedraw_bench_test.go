package protocol

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func BenchmarkPutTextWithItemName(b *testing.B) {
	name := Text{Raw: nbt.Compound{
		"text": nbt.String("Named"), "color": nbt.String("green"),
		"click_event": nbt.Compound{
			"action": nbt.String("show_dialog"),
			"dialog": nbt.Compound{"type": nbt.String("notice"), "title": nbt.String("Title")},
		},
	}}
	for label, inner := range map[string]Text{
		"modelled": {Text: "Named", Color: "green", Italic: true},
		"dialog":   {Italic: true, Extra: []Text{name}},
	} {
		b.Run(label, func(b *testing.B) {
			text := Text{Translate: "chat.square_brackets", With: []Text{{Extra: []Text{inner}}}}
			b.ReportAllocs()
			var wire Buffer
			for b.Loop() {
				wire.Reset()
				wire.PutText(text)
			}
		})
	}
}
