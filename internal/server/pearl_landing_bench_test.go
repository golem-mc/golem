package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
)

// BenchmarkPearlLanding measures the landing effect, including the owner's
// teleport, movement telling and sound, with eight players watching.
func BenchmarkPearlLanding(b *testing.B) {
	for _, riding := range []bool{false, true} {
		name := "walking"
		if riding {
			name = "boat"
		}
		b.Run(name, func(b *testing.B) {
			s, w := newTrackerBenchServer(b)
			var watchers []*client
			for _, c := range s.clients {
				watchers = append(watchers, c)
			}
			owner := watchers[0]
			owner.player.UpdateVitals(func(v *player.Vitals) { v.GameMode = protocol.GameModeCreative })
			for _, c := range watchers {
				if c != owner {
					owner.seenBy.add(c)
				}
			}
			boat := s.spawnBoat(w, "minecraft:oak_boat", 0.5, world.GroundLevel, 0.5, 0, customName{})
			passenger := &boatPassenger{c: owner}
			seat := []*boatPassenger{passenger}
			landing := pearlLanding{
				w: w, id: s.nextEntity(), it: &thrownEntity{}, owner: owner, seenBy: watchers,
				x: 6.5, y: world.GroundLevel, z: 0.5,
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if riding {
					// Restore the seat directly so mounting is outside the measured path.
					seat[0] = passenger
					boat.passengers = seat
					owner.vehicle.Store(boat)
				}
				s.landPearl(landing)
			}
		})
	}
}
