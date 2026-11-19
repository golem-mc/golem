package server

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
)

// BenchmarkLightningBodies measures target selection and a bolt's hurt scan
// with empty lists or 200 of each living fixture, including bodies in range.
func BenchmarkLightningBodies(b *testing.B) {
	for _, count := range []int{0, 200} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			s, w := newTrackerBenchServer(b)
			y := float64(world.GroundLevel + 8)
			for i := range count {
				x, z := trackerBenchSpot(i)
				s.armorStands = append(s.armorStands, &armorStandEntity{
					stand: entity.NewArmorStand(s.nextEntity(), x, y, z), w: w,
				})
				m := entity.NewMannequin(s.nextEntity(), x, y, z)
				// Keep every iteration's population alive without ticking the hurt window.
				m.Invulnerable = true
				s.mannequins = append(s.mannequins, &mannequinEntity{mannequin: m, w: w})
			}
			b.Run("target", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					s.lightningTargetAround(w, 0, 0)
				}
			})
			bolt := &boltEffect{w: w, x: 0.5, y: y, z: 0.5}
			b.Run("hurt", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					s.boltHurt(bolt)
				}
			})
		})
	}
}
