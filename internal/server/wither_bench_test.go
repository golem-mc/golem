package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkWitherSmashing resets a stone box, arms a wither with an accepted
// hurt and ticks its full countdown, including the resulting destruction pass.
func BenchmarkWitherSmashing(b *testing.B) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = b.TempDir()
	cfg.LevelType = TestFlatLevelType
	cfg.Difficulty = "easy" // Easy keeps idle heads from firing stray skulls.
	s, err := newServer(cfg, 1)
	if err != nil {
		b.Fatal(err)
	}
	w := s.world
	pos := world.ChunkPos{X: 0, Z: 0}
	w.Acquire(pos)
	b.Cleanup(func() {
		w.Release(pos)
		s.Shutdown()
	})
	// Keep the timed workload bounded without accumulating item entities.
	w.SetBoolRule(world.RuleBlockDrops, false)
	w.SetBoolRule(world.RuleMobGriefing, true)
	id := s.spawnMobInDim(s.allDims()[0], entity.TypeWither, 8.5, 64, 8.5, 1, nil)
	m := s.mobByEntityIDLocked(w, id).mob
	stone := registry.BlockStone.DefaultState()
	w.FlushLighting()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for x := int32(7); x <= 9; x++ {
			for z := int32(7); z <= 9; z++ {
				for y := int32(64); y <= 67; y++ {
					if x != 8 || z != 8 {
						s.SetBlock(x, y, z, stone)
					}
				}
			}
		}
		m.Health = m.MaxHealth
		if landed, _ := m.HurtByWorld(1, rules.DamageTypeGeneric); !landed {
			b.Fatal("wither refused the arming hit")
		}
		for range 20 {
			m.X, m.Y, m.Z = 8.5, 64, 8.5
			m.VX, m.VY, m.VZ = 0, 0, 0
			s.tickMobs()
		}
	}
}
