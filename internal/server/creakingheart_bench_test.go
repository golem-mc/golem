package server

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func BenchmarkCreakingHeartLinked(b *testing.B) {
	for _, cows := range []int{0, 2000} {
		b.Run(fmt.Sprintf("%dCows", cows), func(b *testing.B) {
			s, w, creaking := newMobLookupBenchServer(b, cows, entity.TypeCreaking)
			w.SetBlock(0, mobLookupBenchY, 0, registry.BlockCreakingHeart.DefaultState())
			be := w.EnsureBlockEntity(0, mobLookupBenchY, 0, world.KindCreakingHeart)
			be.UseLive(func(be *world.BlockEntity) { be.Creaking = creaking.uuid })
			if body, found := s.creakingBodyOf(w, be); !found || body.ent != creaking {
				b.Fatal("the heart could not find its linked creaking")
			}
			for b.Loop() {
				s.creakingBodyOf(w, be)
			}
		})
	}
}

func BenchmarkConduitTargetLookup(b *testing.B) {
	for _, cows := range []int{0, 2000} {
		b.Run(fmt.Sprintf("%dCows", cows), func(b *testing.B) {
			s, w, zombie := newMobLookupBenchServer(b, cows, entity.TypeZombie)
			s.mobsMu.Lock()
			defer s.mobsMu.Unlock()
			herd := s.herdOf(w)
			if conduitTargetEntity(herd, zombie.uuid, 0, mobLookupBenchY, 0) != zombie {
				b.Fatal("the conduit could not keep its target")
			}
			for b.Loop() {
				conduitTargetEntity(herd, zombie.uuid, 0, mobLookupBenchY, 0)
			}
		})
	}
}

const mobLookupBenchY = world.GroundLevel + 3

func newMobLookupBenchServer(b *testing.B, cows int, target entity.MobType) (*Server, *world.World, *mobEntity) {
	b.Helper()
	s, w := newBlockBenchServer(b, 4)
	dim := s.allDims()[0]
	for i := range cows {
		x, z := float64(i%64)+0.5, float64(i/64)+0.5
		s.spawnMobInDim(dim, entity.TypeCow, x, mobLookupBenchY, z, int64(i+1), nil)
	}
	id := s.spawnMobInDim(dim, target, 1.5, mobLookupBenchY, 0.5, 1, nil)
	return s, w, s.mobByEntityID(w, id)
}
