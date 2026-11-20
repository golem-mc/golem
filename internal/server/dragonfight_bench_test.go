package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/google/uuid"
)

// BenchmarkDestroyHealingCrystal includes the dragon's damage and its feedback
// packet construction, without arena generation or the crystal's block blast.
func BenchmarkDestroyHealingCrystal(b *testing.B) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = b.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(s.Shutdown)
	dim := s.dimensionByName(world.DimensionName)
	s.spawnMobInDim(dim, entity.TypeEnderDragon, 0, 128, 0, 1, nil)
	m := dim.mobs.herd[0]
	crystal := s.newEndCrystalEntity(dim.w, 0, 128, 20)
	for range 100 {
		m.DragonCrystals([]entity.EndCrystal{*crystal.crystal})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		m.Health, m.InvulnTicks = 160, 0
		s.destroyEndCrystal(crystal, 0)
	}
}

// BenchmarkTickDragonFight runs the two dragon passes of a tick, the crystal
// choice and the fight. idle is a world nobody took to the End, with
// crystals in the overworld only; dragon has a dragon and ten crystals in an
// unwatched End; watched adds a player in the arena, so the fight reconciles
// its dragon and boss bar every tick.
func BenchmarkTickDragonFight(b *testing.B) {
	for _, bc := range []struct {
		name            string
		dragon, watched bool
	}{{"idle", false, false}, {"dragon", true, false}, {"watched", true, true}} {
		b.Run(bc.name, func(b *testing.B) {
			cfg := config.Default()
			cfg.OnlineMode = false
			cfg.LevelName = b.TempDir()
			cfg.LevelType = TestFlatLevelType
			s, err := newServer(cfg, 1)
			if err != nil {
				b.Fatal(err)
			}
			end := s.dimensionByName(world.EndName)
			b.Cleanup(func() {
				// The watcher has no connection for the shutdown to close.
				s.mu.Lock()
				clear(s.clients)
				s.mu.Unlock()
				s.Shutdown()
			})
			crystals := world.DimensionName
			if bc.dragon {
				crystals = world.EndName
				end.w.Acquire(world.ChunkPos{})
				b.Cleanup(func() { end.w.Release(world.ChunkPos{}) })
				s.spawnMobInDim(end, entity.TypeEnderDragon, 0, 128, 0, 1, nil)
			}
			for i := range 10 {
				s.SpawnEndCrystalInDimension(crystals, float64(i)-5, 100, 8)
			}
			if bc.watched {
				p := &player.Player{UUID: uuid.New(), EntityID: s.nextEntity()}
				p.SetPosition(player.Position{X: 0, Y: 70, Z: 0})
				c := &client{srv: s, player: p}
				c.dim.Store(end)
				s.mu.Lock()
				s.assignPlayerSlot(c)
				s.clients[p.UUID] = c
				s.mu.Unlock()
				// The player scan runs every 20 ticks, then the arena loads
				// on the generation pool.
				for range 20 {
					s.tickDragonFight()
				}
				for !s.DragonArenaSettled() {
				}
				s.tickDragonFight()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.tickDragonCrystals()
				s.tickDragonFight()
			}
		})
	}
}

// BenchmarkDragonRitual includes the adapter's scan of ritual and spike crystals.
func BenchmarkDragonRitual(b *testing.B) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = b.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(s.Shutdown)
	w := s.dimensionByName(world.EndName).w
	var ids []uuid.UUID
	for i := range 14 {
		c := s.newEndCrystalEntity(w, float64(i), 100, 0)
		s.registerEndCrystal(c)
		if i < 4 {
			ids = append(ids, c.uuid)
		}
	}
	f := s.dimensionByName(world.EndName).dragonFight
	b.Run("step", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			f.ritual.Stage, f.ritual.Time, f.ritual.Crystals = "summoning_pillars", 1, ids
			s.stepDragonRespawn(w, f)
		}
	})
	b.Run("rebuild", func(b *testing.B) {
		p := world.EndSpikeTop(1, 0)
		for x := (p[0] - 16) >> 4; x <= (p[0]+16)>>4; x++ {
			for z := (p[2] - 16) >> 4; z <= (p[2]+16)>>4; z++ {
				pos := world.ChunkPos{X: x, Z: z}
				w.Acquire(pos)
				b.Cleanup(func() { w.Release(pos) })
			}
		}
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			s.applyDragonRitual(dragonRitualEffect{w: w, spike: 0})
			s.endCrystalsMu.Lock()
			for _, c := range s.endCrystals {
				if c.crystal.Invulnerable {
					s.removeEndCrystalLocked(c)
				}
			}
			s.endCrystalsMu.Unlock()
		}
	})
}
