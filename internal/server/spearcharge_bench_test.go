package server

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
)

// BenchmarkPiercingTargets runs the per-tick candidate scan of a charging
// spear holder over a herd of cows spread across the held chunks, with a few
// of them on the look ray.
func BenchmarkPiercingTargets(b *testing.B) {
	for _, mobs := range []int{0, 2000} {
		b.Run(strconv.Itoa(mobs), func(b *testing.B) {
			cfg := config.Default()
			cfg.OnlineMode = false
			cfg.LevelName = b.TempDir()
			cfg.LevelType = TestFlatLevelType
			s, err := newServer(cfg, 1)
			if err != nil {
				b.Fatal(err)
			}
			s.SetSpawning(false)
			w := s.world
			var holds []world.ChunkPos
			for cx := int32(-3); cx < 3; cx++ {
				for cz := int32(-3); cz < 3; cz++ {
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
			y := float64(world.GroundLevel)
			for i := range mobs {
				s.spawnMobInDim(s.allDims()[0], entity.TypeCow, float64(i%45)*2-44, y, float64(i/45%45)*2-44, int64(i), nil)
			}
			p := &player.Player{EntityID: s.nextEntity()}
			p.SetPosition(player.Position{X: 0.5, Y: y, Z: 0.5, OnGround: true})
			c := &client{srv: s, player: p}
			c.dim.Store(s.allDims()[0])
			ray := entity.PiercingRay{
				EyeX: 0.5, EyeY: y + 1.62, EyeZ: 0.5, LookZ: 1,
				MinReach: 0, MaxReach: 4.5, Margin: 0.3,
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				c.piercingTargets(w, ray)
			}
		})
	}
}
