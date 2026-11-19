package protocol_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

func BenchmarkPutRawTextList(b *testing.B) {
	for name, items := range map[string][]nbt.Tag{
		"mixed": {nbt.String("prefix"), nbt.Compound{"selector": nbt.String("@s")}},
		"homogeneous": {
			nbt.Compound{"text": nbt.String("prefix")},
			nbt.Compound{"text": nbt.String("suffix"), "bold": nbt.Byte(1)},
		},
	} {
		b.Run(name, func(b *testing.B) {
			text := protocol.Text{Raw: nbt.List{Elem: nbt.TypeCompound, Items: items}}
			var wire protocol.Buffer
			b.ReportAllocs()
			for b.Loop() {
				wire.Reset()
				wire.PutText(text)
			}
		})
	}
}
