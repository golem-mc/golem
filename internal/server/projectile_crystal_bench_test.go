package server

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// BenchmarkTickProjectilesCrystals runs the arrow tick and the thrown tick
// over hanging projectiles beside live end crystals; the zero-projectile
// cases are the cost of the crystal snapshot alone.
func BenchmarkTickProjectilesCrystals(b *testing.B) {
	for _, crystals := range []int{0, 20} {
		for _, bodies := range []int{0, 100} {
			b.Run(fmt.Sprintf("crystals=%d/projectiles=%d", crystals, bodies), func(b *testing.B) {
				s, w := newTrackerBenchServer(b)
				for i := range crystals {
					x, z := trackerBenchSpot(i)
					s.registerEndCrystal(s.newEndCrystalEntity(w, x, world.GroundLevel+30, z))
				}
				item, _ := registry.ItemID("minecraft:snowball")
				for i := range bodies {
					x, z := trackerBenchSpot(i)
					it := newArrowEntity(s, w, x, world.GroundLevel+10, z, 0, 0, 0, 0, 0,
						false, entity.ArrowBaseDamage, entity.PickupDisallowed, 0, uuid.Nil,
						protocol.ItemStack{}, 0)
					it.arrow.NoGravity = true
					it.arrow.Life = -1 << 30
					s.registerArrow(it)
					s.standThrown(entity.ThrownSnowball, w, x, world.GroundLevel+12, z, 0, 0, 0, 0, 0, 0, uuid.Nil,
						int64(i), entity.NewRolls(int64(i)), protocol.ItemStack{ItemID: item, Count: 1})
				}
				for _, it := range s.thrown {
					it.throwable.NoGravity = true
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					s.tickArrows()
					s.tickThrowables()
				}
			})
		}
	}
}
