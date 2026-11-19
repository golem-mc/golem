package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkAdventureProjectileTNTRefusal measures the projectile hit's adventure
// gate; an NBT guard must refuse TNT because it has no block entity.
func BenchmarkAdventureProjectileTNTRefusal(b *testing.B) {
	for _, name := range []string{"plain", "state", "nbt"} {
		b.Run(name, func(b *testing.B) {
			s, w := newTrackerBenchServer(b)
			y := int32(world.GroundLevel)
			w.SetBlock(0, y, 0, registry.BlockTnt.DefaultState())
			hit := projectileBlockHit{
				w: w, block: entity.BlockHit{X: 0, Y: y, Z: 0},
				fire: true, ownerMode: protocol.GameModeAdventure,
			}
			if name != "plain" {
				block := registry.BlockDirt
				if name == "nbt" {
					block = registry.BlockTnt
				}
				var data protocol.Buffer
				data.PutVarInt(1)
				data.PutBool(true)
				data.PutVarInt(2)
				data.PutVarInt(int32(block))
				data.PutBool(false)
				data.PutBool(name == "nbt")
				if name == "nbt" {
					tag, err := nbt.EncodeNetwork(nbt.Compound{})
					if err != nil {
						b.Fatal(err)
					}
					data.PutBytes(tag)
				}
				data.PutVarInt(0)
				data.PutVarInt(0)
				hit.pickupStack = protocol.ItemStack{
					ItemID: int32(registry.ItemArrow), Count: 1,
					Raw: []protocol.RawComponent{{Type: protocol.DataComponentCanBreak, Data: data.Bytes()}},
				}
			}
			hits := []projectileBlockHit{hit}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.hitProjectileBlocks(hits)
			}
		})
	}
}
