package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/google/uuid"
)

// BenchmarkTickRandomTicks runs a random tick pass with one player at the
// default simulation distance, every chunk of its square loaded.
func BenchmarkTickRandomTicks(b *testing.B) {
	benchTickRandomTicks(b, false)
}

func BenchmarkTickRandomTicksDisabled(b *testing.B) {
	benchTickRandomTicks(b, true)
}

func benchTickRandomTicks(b *testing.B, disabled bool) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = b.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		b.Fatal(err)
	}
	s.SetSpawning(false)
	defer func() {
		b.StopTimer()
		s.Shutdown()
	}()
	w := s.world
	if disabled {
		w.SetIntRule(world.RuleRandomTickSpeed, 0)
	}
	reach := int32(cfg.SimulationDistance)
	for x := -reach; x <= reach; x++ {
		for z := -reach; z <= reach; z++ {
			w.Acquire(world.ChunkPos{X: x, Z: z})
		}
	}
	w.FlushLighting()
	p := &player.Player{EntityID: s.nextEntity()}
	p.SetPosition(player.Position{X: 8.5, Y: float64(world.GroundLevel), Z: 8.5, OnGround: true})
	c := &client{srv: s, player: p}
	c.dim.Store(s.allDims()[0])
	id := uuid.New()
	s.assignPlayerSlot(c)
	s.clients[id] = c
	defer delete(s.clients, id)
	// Warm the tick's scratch buffers before timing.
	s.tickRandomTicks(s.allDims()[0], 0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		s.tickRandomTicks(s.allDims()[0], int64(i))
	}
}
