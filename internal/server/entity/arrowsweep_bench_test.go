package entity

import "testing"

// BenchmarkArrowStepFlying is one full-power arrow's flight tick, three
// blocks sideways: the clip, the body sweep and the block effects of the
// cells it passes, through open air and through the powder snow field's top
// layer, where each snow cell crossed is a snowflake event.
func BenchmarkArrowStepFlying(b *testing.B) {
	w := snowField(b)
	for _, bc := range []struct {
		name string
		z    float64
	}{{"air", 5.5}, {"powder snow", 0.5}} {
		b.Run(bc.name, func(b *testing.B) {
			a := NewArrow(1, -1.5, 63.2, bc.z, 3, 0, 0, false, ArrowBaseDamage, PickupAllowed, 0, 3)
			b.ReportAllocs()
			for b.Loop() {
				a.X, a.Y, a.Z = -1.5, 63.2, bc.z
				a.VX, a.VY, a.VZ = 3, 0, 0
				a.Step(w, nil, nil)
			}
		})
	}
}
