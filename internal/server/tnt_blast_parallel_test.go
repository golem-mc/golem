package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// TestBlastTNTParallelMatchesSerial runs one blast among a crowd of primed TNT
// twice, once with the parallel path forced and once with the serial path, and
// asserts identical velocities for every TNT.
func TestBlastTNTParallelMatchesSerial(t *testing.T) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	w := s.world
	holds := []world.ChunkPos{{X: 0, Z: 0}, {X: 1, Z: 0}, {X: 0, Z: 1}, {X: 1, Z: 1}}
	for _, pos := range holds {
		w.Acquire(pos)
	}
	t.Cleanup(func() {
		for _, pos := range holds {
			w.Release(pos)
		}
		s.Shutdown()
	})

	// Place stone pillars and slabs around the blast so exposure rays hit a mix
	// of blocked, partially blocked, and clear sightlines.
	stone := registry.BlockStone.DefaultState()
	for y := int32(64); y <= 66; y++ {
		w.SetBlock(6, y, 8, stone)
		w.SetBlock(10, y, 8, stone)
		w.SetBlock(8, y, 6, stone)
		w.SetBlock(8, y, 10, stone)
	}
	w.SetBlock(7, 64, 7, stone)
	w.SetBlock(9, 65, 9, stone)

	type vel [3]float64
	run := func(parallelMin, workers int) []vel {
		s.tntMu.Lock()
		s.tnts = s.tnts[:0]
		for i := range 256 {
			x := float64(i%16)*0.75 + 2.5
			y := 64 + float64(i/128)*1.5
			z := float64((i%128)/16)*1.25 + 2.5
			ent := s.newTNTEntity(w, x, y, z, 80, 0, uuid.Nil, false)
			// Mark a few dead to verify filtering matches.
			if i%37 == 0 {
				ent.tnt.Dead = true
			}
			s.tnts = append(s.tnts, ent)
		}
		s.tntBlasts.build(s.tnts)
		s.tntMu.Unlock()

		b := blast{
			w:      w,
			x:      8.5,
			y:      world.GroundLevel + entity.TNTHeight/16,
			z:      8.5,
			radius: entity.TNTExplosionPower,
		}
		in := blastInputs{
			blocks: w.SnapshotBlocks(),
			at:     rules.Blast{X: b.x, Y: b.y, Z: b.z, Radius: b.radius},
		}
		var fx blastEffects
		s.blastTNT(b, &in, &fx, parallelMin, workers)
		in.blocks.Release()

		s.tntMu.Lock()
		out := make([]vel, len(s.tnts))
		for i, ent := range s.tnts {
			out[i] = vel{ent.tnt.VX, ent.tnt.VY, ent.tnt.VZ}
		}
		s.tntBlasts.drop()
		s.tntMu.Unlock()
		return out
	}

	serial := run(1<<30, 1)
	parallel := run(1, 4)

	pushed := 0
	for i := range serial {
		if serial[i] != parallel[i] {
			t.Fatalf("tnt %d velocity mismatch: serial=%v parallel=%v", i, serial[i], parallel[i])
		}
		if serial[i] != (vel{}) {
			pushed++
		}
	}
	if pushed < 64 {
		t.Fatalf("expected at least 64 pushed TNT, got %d", pushed)
	}
}
