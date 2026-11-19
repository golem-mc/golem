package world

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkChunkPacketPluginSigns includes private data on update-tag-bearing
// entities, both on a cache hit and when assembling a new packet.
func BenchmarkChunkPacketPluginSigns(b *testing.B) {
	for _, populated := range []bool{false, true} {
		b.Run(fmt.Sprintf("populated=%t", populated), func(b *testing.B) {
			w, err := NewWorld(b.TempDir(), NewFlatGenerator())
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = w.Close() })
			chunk, _ := w.Acquire(ChunkPos{})
			if chunk == nil {
				b.Fatal("chunk not loaded")
			}
			for i := range int32(10) {
				x, z := i%4, i/4
				w.SetBlock(x, GroundLevel, z, registry.BlockOakSign.DefaultState())
				tag := nbt.Compound{}
				if populated {
					tag["plugins"] = nbt.Compound{
						"bench:counter": nbt.Int(42),
						"bench:nested": nbt.Compound{"payload": nbt.Compound{
							"bytes": nbt.ByteArray(make([]byte, 4096)),
							"ints":  nbt.IntArray(make([]int32, 1024)),
							"longs": nbt.LongArray(make([]int64, 512)),
						}},
					}
				}
				if _, ok := w.SetBlockEntityTag(x, GroundLevel, z, KindSign.Name(), tag); !ok {
					b.Fatal("sign not loaded")
				}
			}
			w.CachedChunkPacket(chunk)
			for _, cached := range []bool{true, false} {
				b.Run(fmt.Sprintf("cached=%t", cached), func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						if !cached {
							chunk.packet = chunkPacketCache{}
						}
						w.CachedChunkPacket(chunk)
					}
				})
			}
		})
	}
}
