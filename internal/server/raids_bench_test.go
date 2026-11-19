package server

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/raid"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkTickRaids runs the raid tick while a raid is live over a village
// with a bell and beds, with forty raiders enrolled in it.
func BenchmarkTickRaids(b *testing.B) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = b.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		b.Fatal(err)
	}
	dim := s.allDims()[0]
	w := dim.w
	var holds []world.ChunkPos
	for cx := int32(-4); cx < 4; cx++ {
		for cz := int32(-4); cz < 4; cz++ {
			holds = append(holds, world.ChunkPos{X: cx, Z: cz})
		}
	}
	for _, pos := range holds {
		w.Acquire(pos)
	}
	b.Cleanup(func() {
		for _, pos := range holds {
			w.Release(pos)
		}
		s.Shutdown()
	})
	w.FlushLighting()

	y := int32(world.GroundLevel)
	w.SetBlock(0, y, 0, registry.MustState("minecraft:bell", nil))
	for i := range int32(6) {
		w.SetBlock(-10+4*i, y, 6, registry.MustState("minecraft:red_bed", map[string]string{"part": "head"}))
	}
	// A librarian claims a lectern beside the bell so the raid's village exists.
	s.SetBlock(2, y, 0, registry.MustState("minecraft:lectern", nil))
	s.SpawnVillagerAtJobSite("minecraft:librarian", nil, 2.5, float64(y), 1.5, 2, y, 0)
	for range 400 {
		s.tickMobs()
		if dim.isVillage(0, int(y), 0) {
			break
		}
	}
	if !dim.isVillage(0, int(y), 0) {
		b.Fatal("the villager never claimed the bell")
	}
	at := raid.Pos{X: 0, Y: int(y), Z: 0}
	dim.raids.CreateOrExtend(raid.Player{UUID: uuid.New(), Alive: true}, 0, at, s.villagePOIsAround(dim, at, 64))
	for i := range 40 {
		s.spawnMobInDim(dim, entity.TypePillager, float64(i%10)*3-15, float64(y), float64(i/10)*3-6, int64(i), nil)
	}
	for range 40 {
		s.tickMobs()
		s.tickRaids()
	}
	if len(dim.raids.All()) == 0 {
		b.Fatal("the raid ended during the settling ticks")
	}
	b.ReportAllocs()
	b.ResetTimer()
	var worst time.Duration
	for range b.N {
		start := time.Now()
		s.tickRaids()
		worst = max(worst, time.Since(start))
	}
	b.ReportMetric(float64(worst.Microseconds()), "worst-us")
}
