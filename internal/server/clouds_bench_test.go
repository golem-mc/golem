package server

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
)

// BenchmarkTickCloudsInHerd ticks one lingering cloud over a flat world's herd.
func BenchmarkTickCloudsInHerd(b *testing.B) {
	for _, cows := range []int{0, 2000} {
		b.Run(strconv.Itoa(cows), func(b *testing.B) {
			cfg := config.Default()
			cfg.OnlineMode = false
			cfg.LevelName = b.TempDir()
			cfg.LevelType = TestFlatLevelType
			s, err := newServer(cfg, 1)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(s.Shutdown)
			dim := s.allDims()[0]
			w := dim.w
			for x := int32(-4); x < 4; x++ {
				for z := int32(-4); z < 4; z++ {
					b.Cleanup(w.HoldEntityTicking(world.ChunkPos{X: x, Z: z}))
				}
			}
			w.FlushLighting()
			for i := range cows {
				x, z := float64(i%50)*2-49.5, float64(i/50)*2-39.5
				s.spawnMobInDim(dim, entity.TypeCow, x, world.GroundLevel, z, int64(i+1), nil)
			}
			s.SpawnCloudEntity(40, world.GroundLevel, 40)

			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				if i%400 == 399 {
					b.StopTimer()
					s.SpawnCloudEntity(40, world.GroundLevel, 40)
					b.StartTimer()
				}
				s.tickClouds()
			}
		})
	}
}
