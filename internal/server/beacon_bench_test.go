package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkTickBeaconEffects is one 80-tick update of a lit level-one beacon
// with Speed chosen, over twenty players, half inside its reach.
func BenchmarkTickBeaconEffects(b *testing.B) {
	s, w := newBlockBenchServer(b, 0)
	for cx := int32(-5); cx <= 5; cx++ {
		for cz := int32(-5); cz <= 5; cz++ {
			w.Acquire(world.ChunkPos{X: cx, Z: cz})
		}
	}
	y := int32(world.GroundLevel)
	for x := int32(-1); x <= 1; x++ {
		for z := int32(-1); z <= 1; z++ {
			w.SetBlock(x, y, z, registry.BlockIronBlock.DefaultState())
		}
	}
	w.SetBlock(0, y+1, 0, registry.BlockBeacon.DefaultState())
	w.FlushLighting()
	addBlockBenchPlayers(b, s, w, 20, -5, 5)
	i := 0
	for _, c := range s.clientsLocked() {
		// Ten stand at the beacon, ten far outside the reach.
		x := 0.5
		if i++; i%2 == 0 {
			x = 500.5
		}
		c.player.SetPosition(player.Position{X: x, Y: float64(y + 2), Z: 0.5})
	}
	be := w.EnsureBlockEntity(0, y+1, 0, world.KindBeacon)
	if be == nil {
		b.Fatal("no beacon block entity")
	}
	be.Use(func(e *world.BlockEntity) { e.BeaconPrimary = registry.MustMobEffect("minecraft:speed") })
	s.tickBeacon(w, be, 0)
	be.Use(func(e *world.BlockEntity) {
		if e.BeaconLevels != 1 {
			b.Fatalf("beacon level %d, want 1", e.BeaconLevels)
		}
	})
	b.ReportAllocs()
	for b.Loop() {
		s.tickBeacon(w, be, 0)
	}
}
