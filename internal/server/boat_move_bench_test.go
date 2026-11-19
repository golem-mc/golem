package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
)

// BenchmarkMoveBoatWithPlayers includes the move check and both riders' seats.
func BenchmarkMoveBoatWithPlayers(b *testing.B) {
	s, w := newCommandBenchServer(b, 0)
	s.allowFlight.Store(true)
	y := float64(world.GroundLevel)
	boat := s.spawnBoat(w, "minecraft:oak_boat", 4.5, y, 4.5, 0, customName{})
	for range 2 {
		p := &player.Player{EntityID: s.nextEntity()}
		p.SetPosition(player.Position{X: 4.5, Y: y, Z: 4.5})
		c := &client{srv: s, player: p}
		c.dim.Store(s.allDims()[0])
		c.vehicle.Store(boat)
		boat.passengers = append(boat.passengers, &boatPassenger{c: c})
	}
	driver := boat.driver()
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		s.moveBoat(driver, boat, 4.5+float64(i%2)*0.1, y, 4.5, float32(i%360), true)
	}
}
