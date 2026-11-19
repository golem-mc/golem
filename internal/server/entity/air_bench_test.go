package entity

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkMobAirStep measures submerged breath with and without oxygen bonus.
func BenchmarkMobAirStep(b *testing.B) {
	for _, bonus := range []float64{0, 3, 1024} {
		b.Run(fmt.Sprint(bonus), func(b *testing.B) {
			m := NewMob(TypePig, 1, .5, 66, .5, 1)
			m.SetAttributes(m.BuildAttributes(nbt.List{}))
			m.attrs.SetBase(registry.AttributeOxygenBonus, bonus)
			m.waterFluid.eyesIn = true
			b.ReportAllocs()
			for b.Loop() {
				m.AirSupply = 100
				m.airStep(nil)
			}
		})
	}
}
