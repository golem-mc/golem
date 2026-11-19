package protocol

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func BenchmarkPutTextNamedItemFeedback(b *testing.B) {
	for name, component := range map[string]Text{
		"modelled": {Text: "prefix", Extra: []Text{{Text: "suffix", Color: "red"}}},
		"list": {Raw: nbt.NewList([]nbt.Tag{
			nbt.String("prefix"), nbt.Compound{"text": nbt.String("suffix"), "bold": nbt.Byte(1)},
		})},
	} {
		b.Run(name, func(b *testing.B) {
			text := Text{Translate: "chat.square_brackets", With: []Text{{Extra: []Text{component}}}}
			var wire Buffer
			b.ReportAllocs()
			for b.Loop() {
				wire.Reset()
				wire.PutText(text)
			}
		})
	}
}
