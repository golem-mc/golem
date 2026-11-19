package server

import (
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/raid"
)

// BenchmarkRaiderEnrollmentRestoration measures membership changes in a populated
// wave, including cleanup so neither revision accumulates raiders or health.
func BenchmarkRaiderEnrollmentRestoration(b *testing.B) {
	for _, restore := range []bool{false, true} {
		name := "Enrollment"
		if restore {
			name = "Restoration"
		}
		b.Run(name, func(b *testing.B) {
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
			r := dim.raids.CreateOrExtend(raid.Player{UUID: uuid.New(), Alive: true}, 0, raid.Pos{Y: 64}, nil)
			newRef := func(id uuid.UUID) *raiderRef {
				return &raiderRef{s: s, ent: &mobEntity{uuid: id,
					mob: entity.NewMob(entity.TypePillager, 1, 0.5, 64, 0.5, 1)}}
			}
			var previous *raiderRef
			for range 40 {
				previous = newRef(uuid.New())
				r.Join(1, previous)
			}
			next := newRef(uuid.New())
			if restore {
				next = newRef(previous.ent.uuid)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if restore {
					next.savedRaid, next.wave = r.ID(), 1
					s.restoreRaider(dim, next)
					// Cleanup also bounds implementations that append the replacement.
					// With UUID replacement this is a no-op; the wave stays at 40.
					r.RemoveFromRaid(1, previous, true)
					previous, next = next, previous
				} else {
					r.Join(1, next)
					r.RemoveFromRaid(1, next, true)
				}
			}
			b.StopTimer()
			if r.Alive() != 40 || r.Progress() != 1 {
				b.Fatalf("wave after measurement: count %d, progress %v; want 40, 1", r.Alive(), r.Progress())
			}
		})
	}
}
