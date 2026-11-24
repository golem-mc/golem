package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
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

// BenchmarkMobCrossbowRocketShot measures a loaded rocket's whole shot: the
// collector, then the spawn into the world with the loaded stack's effects.
// The rockets are cleared out again every 64 shots, untimed.
func BenchmarkMobCrossbowRocketShot(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	m := entity.NewMob(entity.TypePillager, 1, .5, 64, .5, 3)
	crossbow := protocol.ItemStack{ItemID: int32(registry.ItemCrossbow), Count: 1}
	rocket := protocol.ItemStack{ItemID: int32(registry.ItemFireworkRocket), Count: 1}
	ev := entity.MobEvent{Mob: m, Kind: entity.MobShotArrow, Crossbow: true, Stack: crossbow, Ammo: &rocket,
		X: .5, Y: float64(world.GroundLevel) + 1, Z: .5, VX: 1.6, VY: .1, VZ: 0, Damage: 2}
	t := &mobDimTick{h: &mobHerd{book: map[*entity.Mob]*mobEntity{}}, w: w}
	fx := &mobEffects{}
	clearRockets := func() {
		s.fireworksMu.Lock()
		for _, it := range s.fireworks {
			fireworkTracker.unfile(it)
		}
		s.fireworks = nil
		s.fireworksMu.Unlock()
	}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		if i%64 == 63 {
			b.StopTimer()
			clearRockets()
			b.StartTimer()
		}
		fx.shots = fx.shots[:0]
		t.shotArrow(fx, ev)
		s.spawnMobShot(fx.shots[0])
	}
}
