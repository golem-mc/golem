package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
	"testing"
)

// BenchmarkPillagerCrossbowCharge measures the charge and load portion of a mob tick.
func BenchmarkPillagerCrossbowCharge(b *testing.B) {
	w := newTestWorld()
	m := NewMob(TypePillager, 1, .5, 64, .5, 3)
	quick, _ := registry.EnchantmentID("minecraft:quick_charge")
	m.MainHand = protocol.ItemStack{ItemID: int32(registry.ItemCrossbow), Count: 1, Enchantments: map[int32]int32{quick: 1}}
	g := &crossbowAttackGoal{m: m}
	view := &tickView{w: w}
	target := &Player{X: 8.5, Y: 64, Z: .5}
	b.ReportAllocs()
	for b.Loop() {
		view.events = view.events[:0]
		if g.state != crossbowCharging {
			g.state = crossbowCharging
			g.drawn = 0
			m.UsingItem = true
			m.MainHand.SetChargedProjectiles(nil)
		}
		g.stepCrossbow(view, target, true, true)
	}
}
