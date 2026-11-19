package terrain

import (
	"math"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

// The skew factors of 2D simplex noise. vanilla: SimplexNoise.F2, SimplexNoise.G2
var (
	simplexF2 = 0.5 * (math.Sqrt(3.0) - 1.0)
	simplexG2 = (3.0 - math.Sqrt(3.0)) / 6.0
)

// SimplexNoise is vanilla: SimplexNoise.
type SimplexNoise struct{ gradientNoise }

// NewSimplexNoise draws the offsets and then shuffles the permutation.
// vanilla: SimplexNoise.<init>
func NewSimplexNoise(r levelgen.RandomSource) *SimplexNoise {
	return &SimplexNoise{newGradientNoise(r, 256.0)}
}

// NewSimplexNoiseWithoutOffset draws the offsets but discards them.
// vanilla: SimplexNoise.<init>(RandomSource, true)
func NewSimplexNoiseWithoutOffset(r levelgen.RandomSource) *SimplexNoise {
	return &SimplexNoise{newGradientNoise(r, 0.0)}
}

// cornerNoise3D is vanilla: SimplexNoise.getCornerNoise3D.
func cornerNoise3D(index int32, x, y, z, base float64) float64 {
	t := base - float64(x*x) - float64(y*y) - float64(z*z)
	if t < 0.0 {
		return 0.0
	}
	t *= t
	return t * t * gradients[index].dotDouble(x, y, z)
}

// Get2D is vanilla: SimplexNoise.get(double, double).
func (n *SimplexNoise) Get2D(xIn, yIn float64) float32 {
	xin := xIn + n.offsetX
	yin := yIn + n.offsetY
	s := (xin + yin) * simplexF2
	i := floorInt(xin + s)
	j := floorInt(yin + s)
	t := float64(i+j) * simplexG2
	x0 := xin - (float64(i) - t)
	y0 := yin - (float64(j) - t)
	var i1, j1 int32
	if x0 > y0 {
		i1 = 1
	} else {
		j1 = 1
	}
	x1 := x0 - float64(i1) + simplexG2
	y1 := y0 - float64(j1) + simplexG2
	x2 := x0 - 1.0 + float64(2.0*simplexG2)
	y2 := y0 - 1.0 + float64(2.0*simplexG2)
	ii := i & 0xFF
	jj := j & 0xFF
	gi0 := n.permute(ii+n.permute(jj)) % 12
	gi1 := n.permute(ii+i1+n.permute(jj+j1)) % 12
	gi2 := n.permute(ii+1+n.permute(jj+1)) % 12
	n0 := cornerNoise3D(gi0, x0, y0, 0.0, 0.5)
	n1 := cornerNoise3D(gi1, x1, y1, 0.0, 0.5)
	n2 := cornerNoise3D(gi2, x2, y2, 0.0, 0.5)
	return float32(70.0 * (n0 + n1 + n2))
}

// Get3D is vanilla: SimplexNoise.get(double, double, double).
func (n *SimplexNoise) Get3D(xIn, yIn, zIn float64) float32 {
	xin := xIn + n.offsetX
	yin := yIn + n.offsetY
	zin := zIn + n.offsetZ
	s := (xin + yin + zin) * 0.3333333333333333
	i := floorInt(xin + s)
	j := floorInt(yin + s)
	k := floorInt(zin + s)
	t := float64(i+j+k) * 0.16666666666666666
	x0 := xin - (float64(i) - t)
	y0 := yin - (float64(j) - t)
	z0 := zin - (float64(k) - t)
	var i1, j1, k1, i2, j2, k2 int32
	switch {
	case x0 >= y0 && y0 >= z0:
		i1, i2, j2 = 1, 1, 1
	case x0 >= y0 && x0 >= z0:
		i1, i2, k2 = 1, 1, 1
	case x0 >= y0:
		k1, i2, k2 = 1, 1, 1
	case y0 < z0:
		k1, j2, k2 = 1, 1, 1
	case x0 < z0:
		j1, j2, k2 = 1, 1, 1
	default:
		j1, i2, j2 = 1, 1, 1
	}
	x1 := x0 - float64(i1) + 0.16666666666666666
	y1 := y0 - float64(j1) + 0.16666666666666666
	z1 := z0 - float64(k1) + 0.16666666666666666
	x2 := x0 - float64(i2) + 0.3333333333333333
	y2 := y0 - float64(j2) + 0.3333333333333333
	z2 := z0 - float64(k2) + 0.3333333333333333
	x3 := x0 - 1.0 + 0.5
	y3 := y0 - 1.0 + 0.5
	z3 := z0 - 1.0 + 0.5
	ii, jj, kk := i&0xFF, j&0xFF, k&0xFF
	gi0 := n.permute(ii+n.permute(jj+n.permute(kk))) % 12
	gi1 := n.permute(ii+i1+n.permute(jj+j1+n.permute(kk+k1))) % 12
	gi2 := n.permute(ii+i2+n.permute(jj+j2+n.permute(kk+k2))) % 12
	gi3 := n.permute(ii+1+n.permute(jj+1+n.permute(kk+1))) % 12
	n0 := cornerNoise3D(gi0, x0, y0, z0, 0.6)
	n1 := cornerNoise3D(gi1, x1, y1, z1, 0.6)
	n2 := cornerNoise3D(gi2, x2, y2, z2, 0.6)
	n3 := cornerNoise3D(gi3, x3, y3, z3, 0.6)
	return float32(32.0 * (n0 + n1 + n2 + n3))
}

// AddToVolume is vanilla: Noise.addToVolume.
func (n *SimplexNoise) AddToVolume(out []float32, v Volume, xzScale, yScale float64, amplitude float32) {
	addToVolumeNaive(n, out, v, xzScale, yScale, amplitude)
}

// addToVolumeNaive is vanilla: Noise.addToVolume.
func addToVolumeNaive(n Noise, out []float32, v Volume, xzScale, yScale float64, amplitude float32) {
	index := 0
	for iz := range v.SizeZ {
		z := float64(v.BlockZ(iz)) * xzScale
		for ix := range v.SizeX {
			x := float64(v.BlockX(ix)) * xzScale
			for iy := range v.SizeY {
				y := float64(v.BlockY(iy)) * yScale
				out[index] += float32(amplitude * n.Get3D(x, y, z))
				index++
			}
		}
	}
}
