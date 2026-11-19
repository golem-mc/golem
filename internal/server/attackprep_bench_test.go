package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
)

func BenchmarkAttackBoat(b *testing.B) {
	s, w := newCommandBenchServer(b, 0)
	p := &player.Player{EntityID: s.nextEntity()}
	p.SetPosition(player.Position{X: 2.5, Y: float64(world.GroundLevel), Z: 4.5, OnGround: true})
	c := &client{srv: s, player: p}
	c.dim.Store(s.allDims()[0])
	boat := s.spawnBoat(w, "minecraft:oak_boat", 4, float64(world.GroundLevel), 4.5, 0, customName{})
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		boat.boat.HurtDamage = -1e6
		c.attackStrengthTicks.Store(20)
		c.attackBoat(boat.boat.ID)
	}
}
