package terrain

import (
	"math"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

// gradient is a lattice gradient vector. vanilla: GradientNoise.Gradient
type gradient struct{ x, y, z int32 }

// gradients is vanilla: GradientNoise.GRADIENT.
var gradients = [16]gradient{
	{1, 1, 0}, {-1, 1, 0}, {1, -1, 0}, {-1, -1, 0},
	{1, 0, 1}, {-1, 0, 1}, {1, 0, -1}, {-1, 0, -1},
	{0, 1, 1}, {0, -1, 1}, {0, 1, -1}, {0, -1, -1},
	{1, 1, 0}, {0, -1, 1}, {-1, 1, 0}, {0, -1, -1},
}

// dot is vanilla: Gradient.dot(float, float, float).
func (g gradient) dot(x, y, z float32) float32 {
	return float32(float32(g.x)*x) + float32(float32(g.y)*y) + float32(float32(g.z)*z)
}

// dotXZ is vanilla: Gradient.dotXz.
func (g gradient) dotXZ(x, z float32) float32 {
	return float32(float32(g.x)*x) + float32(float32(g.z)*z)
}

// dotDouble is vanilla: Gradient.dot(double, double, double).
func (g gradient) dotDouble(x, y, z float64) float64 {
	return float64(float64(g.x)*x) + float64(float64(g.y)*y) + float64(float64(g.z)*z)
}

// halfRoundOff is vanilla: GradientNoise.HALF_ROUND_OFF.
var halfRoundOff = math.Nextafter(1.6777216e7, 0)

// wrap folds a coordinate back near the origin so that float relative
// coordinates keep their precision. vanilla: GradientNoise.wrap
func wrap(x float64) float64 {
	if x >= -halfRoundOff && x < halfRoundOff {
		return x
	}
	return x - math.Floor(x/3.3554432e7+0.5)*3.3554432e7
}

// gradientNoise is the permutation and offsets Perlin and simplex noise share.
// vanilla: GradientNoise
type gradientNoise struct {
	perms                     [256]uint8
	offsetX, offsetY, offsetZ float64
}

// newGradientNoise draws the offsets and then shuffles the permutation.
// vanilla: GradientNoise.<init>
func newGradientNoise(r levelgen.RandomSource, offsetScale float64) gradientNoise {
	var n gradientNoise
	n.offsetX = r.NextDouble() * offsetScale
	n.offsetY = r.NextDouble() * offsetScale
	n.offsetZ = r.NextDouble() * offsetScale
	for i := range n.perms {
		n.perms[i] = uint8(i)
	}
	for i := range int32(256) {
		off := r.NextIntN(256 - i)
		n.perms[i], n.perms[off+i] = n.perms[off+i], n.perms[i]
	}
	return n
}

func (n *gradientNoise) permute(x int32) int32 { return int32(n.perms[x&0xFF]) }

func (n *gradientNoise) permuteToGrad(x int32) gradient { return gradients[n.permute(x)&0xF] }
