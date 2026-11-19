package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
)

// BenchmarkTickLocationAdvancements runs a player's per-tick advancement
// checks on a tick that is not a location tick, for a player on foot.
func BenchmarkTickLocationAdvancements(b *testing.B) {
	benchTickLocationAdvancements(b, 1)
}

// BenchmarkTickLocationAdvancementsLocationTick includes the once-a-second
// location predicates with the structure advancements still pending.
func BenchmarkTickLocationAdvancementsLocationTick(b *testing.B) {
	benchTickLocationAdvancements(b, 20)
}

func benchTickLocationAdvancements(b *testing.B, age int) {
	s, _ := newCommandBenchServer(b, 0)
	p := &player.Player{EntityID: s.nextEntity()}
	p.SetPosition(player.Position{X: 2.5, Y: float64(world.GroundLevel), Z: 4.5, OnGround: true})
	p.UpdateVitals(func(v *player.Vitals) { v.Health, v.AgeTicks = 20, age })
	c := &client{srv: s, player: p}
	c.dim.Store(s.allDims()[0])
	c.advancements.Store(advancement.NewProgress(advancement.Current()))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		c.tickLocationAdvancements()
	}
}
