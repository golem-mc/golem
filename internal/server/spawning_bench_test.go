package server

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkNaturalSpawning runs every natural spawn category over 36 lit,
// flat stone chunks with eight players; the ground cannot support land spawns.
func BenchmarkNaturalSpawning(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickNaturalSpawning()
	}
}

// BenchmarkNaturalSpawningHerd runs natural spawning beside two thousand
// zombies that count toward the monster cap.
func BenchmarkNaturalSpawningHerd(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	dim := s.allDims()[0]
	for i := range 2000 {
		s.spawnMobInDim(dim, entity.TypeZombie, float64(i%45)*2-45, float64(world.GroundLevel), float64(i/45)*2-45, int64(i), nil)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickNaturalSpawning()
	}
}

// BenchmarkSpawnerRoundBesideHerd runs one mob spawner spawn round beside 0
// or 2000 zombies standing elsewhere in the dimension.
func BenchmarkSpawnerRoundBesideHerd(b *testing.B) {
	for _, herd := range []int{0, 2000} {
		b.Run(strconv.Itoa(herd), func(b *testing.B) {
			s, w := newTrackerBenchServer(b)
			dim := s.allDims()[0]
			sx, sy, sz := int32(-30), int32(world.GroundLevel+1), int32(32)
			w.SetBlock(sx, sy, sz, registry.BlockSpawner.DefaultState())
			be := w.EnsureBlockEntity(sx, sy, sz, world.KindSpawner)
			be.Use(func(e *world.BlockEntity) {
				e.SpawnCount = 4
				e.MaxNearbyEntities = 1000
				e.SpawnData = nbt.Compound{
					"entity": nbt.Compound{"id": nbt.String("minecraft:zombie")},
					"custom_spawn_rules": nbt.Compound{
						"block_light_limit": nbt.List{Elem: nbt.TypeInt, Items: []nbt.Tag{nbt.Int(0), nbt.Int(15)}},
						"sky_light_limit":   nbt.List{Elem: nbt.TypeInt, Items: []nbt.Tag{nbt.Int(0), nbt.Int(15)}},
					},
				}
			})
			for i := range herd {
				s.spawnMobInDim(dim, entity.TypeZombie, float64(i%30)+10, float64(world.GroundLevel), float64((i/30)%30)-40, int64(i), nil)
			}
			b.ReportAllocs()
			b.ResetTimer()
			spawned := 0
			for range b.N {
				be.Use(func(e *world.BlockEntity) { e.Delay = 0 })
				s.tickBlockEntities()
				b.StopTimer()
				s.mobsMu.Lock()
				spawned += len(dim.mobs.herd) - herd
				for _, m := range dim.mobs.herd[herd:] {
					dim.mobs.forget(m)
				}
				for len(dim.mobs.herd) > herd {
					dim.mobs.remove(dim.mobs.herd[len(dim.mobs.herd)-1])
				}
				s.mobsMu.Unlock()
				b.StartTimer()
			}
			b.StopTimer()
			if spawned == 0 {
				b.Fatal("spawner stood up no mobs")
			}
		})
	}
}
