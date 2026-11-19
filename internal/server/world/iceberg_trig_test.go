package world

import (
	"math"
	"testing"
)

func TestIcebergSinCos(t *testing.T) {
	// Java 25 Math.sin/cos, including the ellipse angle for caseSeed(4).
	for _, tt := range []struct{ angle, sin, cos float64 }{
		{0, 0, 1},
		{math.Pi / 4, 0x1.6a09e667f3bccp-1, 0x1.6a09e667f3bcdp-1},
		{math.Pi / 2, 1, 0x1.1a62633145c07p-54},
		{math.Pi, 0x1.1a62633145c07p-53, -1},
		{3 * math.Pi / 2, -1, -0x1.a79394c9e8a0ap-53},
		{2 * math.Pi, -0x1.1a62633145c07p-52, 1},
		{5 * math.Pi / 2, 1, 0x1.60fafbfd97309p-52},
		{0x1.8978bc084386bp0, 0x1.ffb52557137f4p-1, 0x1.14d1a933e216bp-5},
	} {
		sin, cos := icebergSinCos(tt.angle)
		if math.Float64bits(sin) != math.Float64bits(tt.sin) || math.Float64bits(cos) != math.Float64bits(tt.cos) {
			t.Errorf("angle %x: sin/cos %x %x, want %x %x", tt.angle, sin, cos, tt.sin, tt.cos)
		}
	}
	sin, cos := icebergSinCos(0x1.8978bc084386bp0)
	if d := (icebergRotation{sin, cos}).distance(-4, 0, 4, 4); d != -0x1p-53 {
		t.Errorf("ellipse boundary distance = %x, want -0x1p-53", d)
	}
}
