package world

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkCarriedBlockEntities covers a gameplay field write and chunk sending,
// including cached packets whose carried update tags must still be checked.
func BenchmarkCarriedBlockEntities(b *testing.B) {
	for _, populated := range []bool{false, true} {
		b.Run(fmt.Sprintf("plugins=%t", populated), func(b *testing.B) {
			w := newTestWorld(b, NewFlatGenerator())
			chunk, _ := w.Acquire(ChunkPos{})
			b.Cleanup(func() { w.Release(ChunkPos{}) })
			for x := range int32(16) {
				w.SetBlock(x, GroundLevel+1, 0, registry.BlockPlayerHead.DefaultState())
				tag := nbt.Compound{"profile": nbt.Compound{"name": nbt.String("Tester")}}
				if populated {
					tag["plugins"] = nbt.Compound{"bench:payload": nbt.ByteArray(make([]byte, 4096))}
				}
				w.SetBlockEntityTag(x, GroundLevel+1, 0, KindSkull.Name(), tag)
			}
			b.Run("field", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					w.SetCarriedField(0, GroundLevel+1, 0, "note_block_sound", nbt.String("minecraft:block.note_block.bell"))
				}
			})
			for _, cached := range []bool{false, true} {
				b.Run(fmt.Sprintf("packet/cached=%t", cached), func(b *testing.B) {
					w.CachedChunkPacket(chunk)
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
