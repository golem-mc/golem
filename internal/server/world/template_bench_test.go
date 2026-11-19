package world

import (
	"bytes"
	"compress/gzip"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func BenchmarkParseTemplateDataFix(b *testing.B) {
	for _, tc := range []struct {
		name    string
		version int32
	}{{"current", 4903}, {"older", 4440}} {
		b.Run(tc.name, func(b *testing.B) {
			blocks := nbt.List{Elem: nbt.TypeCompound}
			for i := range int32(256) {
				blocks.Items = append(blocks.Items, nbt.Compound{"state": nbt.Int(0), "pos": intList(i%16, 0, i/16)})
			}
			blocks.Items = append(blocks.Items, nbt.Compound{"state": nbt.Int(1), "pos": intList(0, 1, 0), "nbt": nbt.Compound{
				"id": nbt.String("minecraft:chest"), "Items": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"id": nbt.String("minecraft:chain"), "count": nbt.Int(1)}}},
			}})
			root := nbt.Compound{
				"DataVersion": nbt.Int(tc.version), "size": intList(16, 2, 16), "blocks": blocks,
				"palette": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"Name": nbt.String("minecraft:stone")}, nbt.Compound{"Name": nbt.String("minecraft:chest")}}},
			}
			raw, err := nbt.Encode("", root)
			if err != nil {
				b.Fatal(err)
			}
			var compressed bytes.Buffer
			zw := gzip.NewWriter(&compressed)
			if _, err := zw.Write(raw); err != nil {
				b.Fatal(err)
			}
			if err := zw.Close(); err != nil {
				b.Fatal(err)
			}
			data := compressed.Bytes()
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for range b.N {
				if _, err := ParseTemplate(data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
