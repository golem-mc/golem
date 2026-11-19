package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/nbt"
)

// BenchmarkMobDeathName measures the killer snapshot taken for a mob's blow.
func BenchmarkMobDeathName(b *testing.B) {
	for _, team := range []bool{false, true} {
		name := "unaffiliated"
		if team {
			name = "team"
		}
		b.Run(name, func(b *testing.B) {
			s, w := newCommandBenchServer(b, 0)
			id := s.spawnMobInDim(s.allDims()[0], entity.TypeZombie, 0.5, 64, 0.5, 1, nil)
			if team {
				ent := s.mobByEntityIDLocked(w, id)
				s.scoreboard.AddTeam("red", nbt.String("red"))
				s.scoreboard.JoinTeam("red", []string{ent.uuid.String()})
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				killer, ok := s.mobKillerByID(w, id)
				if !ok || killer.Name.Plain() == "" {
					b.Fatal("missing mob killer name")
				}
			}
		})
	}
}
