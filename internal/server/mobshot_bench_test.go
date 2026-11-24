package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkMobCrossbowShotCollect measures the mob tick's collection of a
// loaded crossbow shot into the effects collector.
func BenchmarkMobCrossbowShotCollect(b *testing.B) {
	m := entity.NewMob(entity.TypePillager, 1, .5, 64, .5, 3)
	crossbow := protocol.ItemStack{ItemID: int32(registry.ItemCrossbow), Count: 1}
	ammo := protocol.ItemStack{ItemID: int32(registry.ItemSpectralArrow), Count: 1}
	ev := entity.MobEvent{Mob: m, Kind: entity.MobShotArrow, Crossbow: true, Stack: crossbow, Ammo: &ammo, Damage: 2}
	t := &mobDimTick{h: &mobHerd{}}
	fx := &mobEffects{}
	b.ReportAllocs()
	for b.Loop() {
		fx.shots = fx.shots[:0]
		t.shotArrow(fx, ev)
	}
	if len(fx.shots) != 1 {
		b.Fatal("no shot collected")
	}
}
