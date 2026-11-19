package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// BenchmarkTickArrowsImpact lands three hundred loaded arrows on the flat
// world's ground per tick, including the deferred impact sounds. Resetting
// the flight before each tick keeps the same arrows and held chunk in use.
func BenchmarkTickArrowsImpact(b *testing.B) {
	for _, sound := range []string{"entity.arrow.hit", "block.bell.use"} {
		b.Run(sound, func(b *testing.B) {
			cfg := config.Default()
			cfg.OnlineMode = false
			cfg.LevelName = b.TempDir()
			cfg.LevelType = TestFlatLevelType
			s, err := newServer(cfg, 1)
			if err != nil {
				b.Fatal(err)
			}
			w := s.world
			pos := world.ChunkPos{}
			w.Acquire(pos)
			b.Cleanup(func() {
				w.Release(pos)
				s.Shutdown()
			})
			w.FlushLighting()

			type flight struct {
				arrow *entity.Arrow
				start entity.Arrow
			}
			var flights []flight
			for range 300 {
				it := newArrowEntity(s, w, 0.5, world.GroundLevel+2, 0.5, 0, 0, 0, -4, 0,
					false, entity.ArrowBaseDamage, entity.PickupDisallowed, 0, uuid.Nil,
					protocol.ItemStack{}, 0)
				applyArrowTag(it, nbt.Compound{"SoundEvent": nbt.String(sound), "NoGravity": nbt.Byte(1)})
				it.arrow.VY = -4
				flights = append(flights, flight{it.arrow, *it.arrow})
				s.registerArrow(it)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				for _, f := range flights {
					*f.arrow = f.start
				}
				s.tickArrows()
			}
			b.StopTimer()
			for _, f := range flights {
				if !f.arrow.InGround {
					b.Fatal("arrow did not land")
				}
			}
		})
	}
}
