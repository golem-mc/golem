package server

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
)

func BenchmarkTrialHostMob(b *testing.B) {
	for _, cows := range []int{0, 2000} {
		b.Run("cows="+strconv.Itoa(cows), func(b *testing.B) {
			s, w := newBlockBenchServer(b, 4)
			dim := s.allDims()[0]
			for i := range cows {
				x, z := float64(i%64)+0.5, float64(i/64)+0.5
				s.spawnMobInDim(dim, entity.TypeCow, x, world.GroundLevel, z, int64(i+1), nil)
			}
			id := s.spawnMobInDim(dim, entity.TypeZombie, 0.5, world.GroundLevel, 32.5, int64(cows+1), nil)
			zombie := s.mobByEntityID(w, id).uuid

			b.ReportAllocs()
			for b.Loop() {
				host := &trialHost{s: s, dim: dim}
				if _, ok := host.Mob(zombie); !ok {
					b.Fatal("trial spawner could not find its zombie")
				}
			}
		})
	}
}
