package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkTickInventoryAdvancements runs a player's per-tick inventory read
// over a full hotbar that does not change between ticks, the common case.
func BenchmarkTickInventoryAdvancements(b *testing.B) {
	benchTickInventoryAdvancements(b, false)
}

// BenchmarkTickInventoryAdvancementsInWater is the same for a player
// standing in water.
func BenchmarkTickInventoryAdvancementsInWater(b *testing.B) {
	benchTickInventoryAdvancements(b, true)
}

func benchTickInventoryAdvancements(b *testing.B, water bool) {
	s, _ := newCommandBenchServer(b, 0)
	p := &player.Player{EntityID: s.nextEntity()}
	y := world.GroundLevel
	p.SetPosition(player.Position{X: 2.5, Y: float64(y), Z: 4.5, OnGround: true})
	p.UpdateVitals(func(v *player.Vitals) { v.Health = 20 })
	p.Inventory(func(inv *player.Inventory) {
		for i, item := range []registry.Item{registry.ItemCobblestone, registry.ItemOakLog, registry.ItemOakPlanks,
			registry.ItemStick, registry.ItemTorch, registry.ItemIronIngot, registry.ItemCoal, registry.ItemBread, registry.ItemStonePickaxe} {
			inv.Slots[player.HotbarSlot+i] = protocol.ItemStack{ItemID: int32(item), Count: 1}
		}
	})
	c := &client{srv: s, player: p}
	c.dim.Store(s.allDims()[0])
	if water {
		c.theWorld().SetBlock(2, int32(y), 4, registry.BlockWater.DefaultState())
	}
	c.advancements.Store(advancement.NewProgress(advancement.Current()))
	benchTickInventory(c) // the first read fires and grants; the rest find no change
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchTickInventory(c)
	}
}

func benchTickInventory(c *client) { c.tickInventoryAdvancements() }
