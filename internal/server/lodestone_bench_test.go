package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkTickLodestoneCompasses is a player's compass tick with 36 tracked
// compasses pointing into saved, unloaded chunks, half with a lodestone, after
// the first tick has learned the answers.
func BenchmarkTickLodestoneCompasses(b *testing.B) {
	s, _ := newCommandBenchServer(b, 0)
	lodestone := registry.BlockLodestone.DefaultState()
	p := &player.Player{EntityID: s.nextEntity()}
	for i := range 36 {
		x, z := int32(20000+i*16), int32(20000)
		if i%2 == 0 {
			s.SetLoadedBlock(x, 63, z, lodestone)
		} else {
			s.SetLoadedBlock(x, 62, z, registry.BlockStone.DefaultState())
		}
		stack := protocol.ItemStack{ItemID: int32(registry.ItemCompass), Count: 1}
		stack.SetLodestoneTracker(protocol.LodestoneTracker{
			Target:  &protocol.LodestoneTarget{Dimension: "minecraft:overworld", X: x, Y: 63, Z: z},
			Tracked: true,
		})
		p.Inventory(func(inv *player.Inventory) { inv.Set(i, stack) })
	}
	c := &client{srv: s, player: p}
	c.dim.Store(s.allDims()[0])
	c.tickLodestoneCompasses()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		c.tickLodestoneCompasses()
	}
}
