package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// lodestoneBenchClient is a player holding 36 tracked compasses pointing into
// saved, unloaded chunks: all at a lodestone, or half at stone. stacks are the
// starting inventory.
func lodestoneBenchClient(b *testing.B, allKept bool) (*client, []protocol.ItemStack) {
	s, _ := newCommandBenchServer(b, 0)
	lodestone := registry.BlockLodestone.DefaultState()
	p := &player.Player{EntityID: s.nextEntity()}
	stacks := make([]protocol.ItemStack, 36)
	for i := range stacks {
		x, z := int32(20000+i*16), int32(20000)
		if allKept || i%2 == 0 {
			s.SetLoadedBlock(x, 63, z, lodestone)
		} else {
			s.SetLoadedBlock(x, 62, z, registry.BlockStone.DefaultState())
		}
		stacks[i] = protocol.ItemStack{ItemID: int32(registry.ItemCompass), Count: 1}
		stacks[i].SetLodestoneTracker(protocol.LodestoneTracker{
			Target:  &protocol.LodestoneTarget{Dimension: "minecraft:overworld", X: x, Y: 63, Z: z},
			Tracked: true,
		})
	}
	c := &client{srv: s, player: p}
	c.dim.Store(s.allDims()[0])
	return c, stacks
}

func setLodestoneBenchStacks(c *client, stacks []protocol.ItemStack) {
	c.player.Inventory(func(inv *player.Inventory) {
		for i, stack := range stacks {
			inv.Set(i, stack)
		}
	})
}

// BenchmarkTickLodestoneCompasses is a player's compass tick with 36 tracked
// compasses pointing into saved chunks. Warm keeps every target, after the
// first tick has learned the answers; cold starts each tick from the original
// stacks with no answer kept, half of them lost.
func BenchmarkTickLodestoneCompasses(b *testing.B) {
	b.Run("warm", func(b *testing.B) {
		c, stacks := lodestoneBenchClient(b, true)
		setLodestoneBenchStacks(c, stacks)
		c.tickLodestoneCompasses()
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			c.tickLodestoneCompasses()
		}
	})
	b.Run("cold", func(b *testing.B) {
		c, stacks := lodestoneBenchClient(b, false)
		b.ReportAllocs()
		for range b.N {
			b.StopTimer()
			setLodestoneBenchStacks(c, stacks)
			c.srv.ForgetLodestoneAnswers()
			b.StartTimer()
			c.tickLodestoneCompasses()
		}
	})
}
