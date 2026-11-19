package server

import (
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkTickMobsFoxVillage runs 3000 night ticks with sixteen foxes and a
// claimed village. Each batch restarts the foxes' goals and expires POI scans,
// including selection, scoring, navigation and cache refresh in the timing.
func BenchmarkTickMobsFoxVillage(b *testing.B) {
	for _, distant := range []int{0, 128} {
		b.Run(fmt.Sprintf("distant_%d", distant), func(b *testing.B) {
			s, reset, foxes := foxVillageBenchScene(b, distant)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				reset()
				for range 3000 {
					s.raidState.tick++
					s.tickMobs()
				}
			}
			b.StopTimer()
			var x float64
			for _, ent := range foxes {
				x += ent.mob.X - 0.5
			}
			b.ReportMetric(x/float64(len(foxes)), "x-blocks/fox")
		})
	}
}

func foxVillageBenchScene(b testing.TB, distant int) (*Server, func(), []*mobEntity) {
	oldLog := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(oldLog) })
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
	// 8x8 chunks around the origin, the mobs kept to the middle 96 blocks
	// so a random walk does not carry one into an unheld chunk.
	var holds []world.ChunkPos
	for cx := int32(-4); cx < 4; cx++ {
		for cz := int32(-4); cz < 4; cz++ {
			holds = append(holds, world.ChunkPos{X: cx, Z: cz})
		}
	}
	for i := range distant / 2 {
		holds = append(holds, world.ChunkPos{X: int32(64 + i%8), Z: int32(64 + i/8)})
	}
	// Forced, so the foxes entity-tick with no player in the scene.
	for _, pos := range holds {
		w.SetChunkForced(pos, true)
	}
	b.Cleanup(func() {
		for _, pos := range holds {
			w.SetChunkForced(pos, false)
		}
		s.Shutdown()
	})
	w.FlushLighting()

	s.SetDayTime(18000)
	lecternBlock, _ := registry.BlockByName("minecraft:lectern")
	lectern := lecternBlock.DefaultState()
	s.SetBlock(40, 64, 0, lectern)
	villager := s.SpawnVillagerAtJobSite("minecraft:librarian", nil, 40.5, 64, 4.5, 40, 64, 0)
	s.SetMobNoAI(villager)
	for i := range distant {
		x, z := int32(64+(i/2)%8)*16, int32(64+(i/2)/8)*16
		y := int32(64 + (i%2)*16)
		s.SetBlock(x, y, z, lectern)
		id := s.SpawnVillagerAtJobSite("minecraft:librarian", nil, float64(x)+0.5, float64(y), float64(z)+4.5, x, y, z)
		s.SetMobNoAI(id)
	}
	foxes := make([]*mobEntity, 0, 16)
	for i := range 16 {
		id := s.spawnMobInDim(dim, entity.TypeFox, 0.5, 64, float64(i*2-16)+0.5, int64(i+1), nil)
		foxes = append(foxes, s.mobByEntityIDLocked(w, id))
	}
	reset := func() {
		h := s.herdOf(w)
		for i, ent := range foxes {
			old := ent.mob
			fresh := entity.NewMob(entity.TypeFox, old.ID, 0.5, 64, float64(i*2-16)+0.5, int64(i+1))
			// Goals and guard closures point at fresh, so all owners must use it too.
			for j, m := range h.herd {
				if m == old {
					h.herd[j] = fresh
					break
				}
			}
			h.forget(old)
			ent.mob = fresh
			h.remember(ent)
		}
	}
	return s, reset, foxes
}
