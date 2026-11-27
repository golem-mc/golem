package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
)

// BenchmarkTickMobsTeam times tickMobs over a herd of cows, with and without
// the whole herd on one team.
func BenchmarkTickMobsTeam(b *testing.B) {
	const herd = 2000
	for _, team := range []bool{false, true} {
		name := "no-team"
		if team {
			name = "team"
		}
		b.Run(name, func(b *testing.B) {
			s, _ := newTrackerBenchServer(b)
			dim := s.allDims()[0]
			for i := range herd {
				x := float64(-40 + (i%40)*2)
				z := float64(-40 + (i/40)*2)
				id := s.spawnMobInDim(dim, entity.TypeCow, x, float64(world.GroundLevel), z, int64(i), nil)
				s.mobsMu.Lock()
				ent := s.mobByEntityIDLocked(dim.w, id)
				ent.mob.NoAI, ent.mob.Persistence = true, true
				s.mobsMu.Unlock()
			}
			if team {
				s.RunLine("team add herd")
				s.RunLine("team join herd @e[type=cow]")
			}
			for range 2 {
				s.tickMobs()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.tickMobs()
			}
		})
	}
}
