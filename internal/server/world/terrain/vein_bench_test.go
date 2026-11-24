package terrain

import "testing"

func BenchmarkVeinRangeChoice(b *testing.B) {
	veins := NewRandomState(Overworld, 12345).Router.Veins
	for _, test := range []struct {
		name string
		vein Vein
	}{
		{"copper", veins.Copper},
		{"iron", veins.Iron},
	} {
		b.Run(test.name, func(b *testing.B) {
			vein := test.vein
			volume := NewVolume(16, vein.MaxY-vein.MinY+1, 16, 0, vein.MinY, 0)
			out := make([]float32, volume.Size())
			b.ReportAllocs()
			for b.Loop() {
				ctx := newContext(nil, 1)
				vein.Density.SampleVolume(ctx, out, volume)
				ctx.recycle()
			}
		})
	}
}
