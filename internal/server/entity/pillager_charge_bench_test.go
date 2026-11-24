package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkPillagerCrossbowCharge measures a crossbow mob's whole cycle: charge,
// load from a held stack of arrows, and the shot dispatch.
func BenchmarkPillagerCrossbowCharge(b *testing.B) {
	w := newTestWorld()
	m := NewMob(TypePillager, 1, .5, 64, .5, 3)
	quick, _ := registry.EnchantmentID("minecraft:quick_charge")
	m.MainHand = protocol.ItemStack{ItemID: int32(registry.ItemCrossbow), Count: 1, Enchantments: map[int32]int32{quick: 1}}
	g := &crossbowAttackGoal{m: m}
	view := &tickView{w: w}
	target := &Player{X: 8.5, Y: 64, Z: .5}
	b.ReportAllocs()
	shots := 0
	for b.Loop() {
		view.events = view.events[:0]
		if m.OffHand.Empty() {
			m.OffHand = protocol.ItemStack{ItemID: int32(registry.ItemSpectralArrow), Count: 64}
		}
		g.stepCrossbow(view, target, true, true)
		for _, ev := range view.events {
			if ev.Kind == MobShotArrow {
				shots++
			}
		}
	}
	if shots == 0 {
		b.Fatal("no shot dispatched")
	}
}
