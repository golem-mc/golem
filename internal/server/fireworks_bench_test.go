package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkUpdateFallFlying measures a gentle glide's per-tick checks between
// the ten-tick game events and twenty-tick equipment wear.
func BenchmarkUpdateFallFlying(b *testing.B) {
	p := &player.Player{}
	p.SetPosition(player.Position{Y: 90})
	p.Inventory(func(inv *player.Inventory) {
		inv.Slots[player.ArmorSlot+1] = protocol.ItemStack{ItemID: int32(registry.ItemElytra), Count: 1}
	})
	c := &client{player: p}
	c.fallFlying.Store(true)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		p.UpdateVitals(func(v *player.Vitals) { v.FallDistance = 1.3 })
		c.fallFlyTicks = 0
		c.updateFallFlying()
	}
}
