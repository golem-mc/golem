package world

import "math"

// Ken Perlin's improved gradient noise, written here rather than imported
// because terrain must be reproducible forever: the same seed and coordinates
// must give the same numbers on any machine, or a regenerated chunk would not
// match the one it replaces.
//
// The permutation table is shuffled from the seed with splitmix64, so different
// seeds give unrelated worlds and the same seed identical ones.

// splitmix64 shuffles a permutation table: short, well understood, and well
// distributed for every 64-bit seed, 0 included.
type splitmix64 struct{ state uint64 }

// next returns the next value of the stream.
func (s *splitmix64) next() uint64 {
	s.state += 0x9E3779B97F4A7C15
	z := s.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// perlin is one noise field, its permutation from the world seed and a salt, so
// the fields terrain needs (continents, hills, caves, temperature) are
// independent but all deterministic in the seed.
type perlin struct {
	perm [512]uint8
}

// newPerlin shuffles the permutation from seed and salt, both mixed by
// splitmix64 so nearby seeds give unrelated tables; Fisher-Yates keeps each
// gradient exactly once.
func newPerlin(seed, salt int64) *perlin {
	p := &perlin{}
	rng := splitmix64{state: uint64(seed) ^ uint64(salt)*0xD6E8F8E4A3D2C1B0}
	var perm [256]uint8
	for i := range perm {
		perm[i] = uint8(i)
	}
	for i := 255; i > 0; i-- {
		j := rng.next() % uint64(i+1)
		perm[i], perm[j] = perm[j], perm[i]
	}
	for i := range p.perm {
		p.perm[i] = perm[i&255]
	}
	return p
}

// fade is Perlin's ease curve, flattening interpolation at lattice points so
// the first derivative is continuous.
func fade(t float64) float64 { return t * t * t * (t*(t*6-15) + 10) }

// grad2 is one octave's 2D gradient dot product over the eight axis and
// diagonal directions.
func grad2(hash uint8, x, y float64) float64 {
	switch hash & 7 {
	case 0:
		return x + y
	case 1:
		return x - y
	case 2:
		return -x + y
	case 3:
		return -x - y
	case 4:
		return x
	case 5:
		return -x
	case 6:
		return y
	default:
		return -y
	}
}

// grad3 is Perlin's improved 3D gradient: the hash picks one of a cube's twelve
// edges, and its two offsets are dotted with the position.
func grad3(hash uint8, x, y, z float64) float64 {
	h := hash & 15
	u := x
	if h&8 != 0 {
		u = y
	}
	v := y
	if h < 8 {
		v = z
	}
	if h&1 != 0 {
		u = -u
	}
	if h&2 != 0 {
		v = -v
	}
	return u + v
}

// noise2 evaluates the 2D field at (x, y), within about ±0.71 for a unit
// lattice cell.
func (p *perlin) noise2(x, y float64) float64 {
	xi := floorInt(x)
	yi := floorInt(y)
	xf := x - float64(xi)
	yf := y - float64(yi)

	// Wrapped into the table's range, the field tiles every 256 cells, which
	// the summed octaves hide at world scale.
	X := uint8(xi)
	Y := uint8(yi)

	u := fade(xf)
	v := fade(yf)

	aa := p.perm[p.perm[X]+Y]
	ab := p.perm[p.perm[X]+Y+1]
	ba := p.perm[p.perm[X+1]+Y]
	bb := p.perm[p.perm[X+1]+Y+1]

	x1 := grad2(aa, xf, yf) + u*(grad2(ba, xf-1, yf)-grad2(aa, xf, yf))
	x2 := grad2(ab, xf, yf-1) + u*(grad2(bb, xf-1, yf-1)-grad2(ab, xf, yf-1))
	return x1 + v*(x2-x1)
}

// noise3 evaluates the 3D field at (x, y, z), blending eight corners, for cave
// carving.
func (p *perlin) noise3(x, y, z float64) float64 {
	xi := floorInt(x)
	yi := floorInt(y)
	zi := floorInt(z)
	xf := x - float64(xi)
	yf := y - float64(yi)
	zf := z - float64(zi)

	X := uint8(xi)
	Y := uint8(yi)
	Z := uint8(zi)

	u, v, w := fade(xf), fade(yf), fade(zf)

	A := p.perm[X] + Y
	AA := p.perm[A] + Z
	AB := p.perm[A+1] + Z
	B := p.perm[X+1] + Y
	BA := p.perm[B] + Z
	BB := p.perm[B+1] + Z

	x1 := lerp(grad3(p.perm[AA], xf, yf, zf), grad3(p.perm[BA], xf-1, yf, zf), u)
	x2 := lerp(grad3(p.perm[AB], xf, yf-1, zf), grad3(p.perm[BB], xf-1, yf-1, zf), u)
	y1 := lerp(x1, x2, v)
	x3 := lerp(grad3(p.perm[AA+1], xf, yf, zf-1), grad3(p.perm[BA+1], xf-1, yf, zf-1), u)
	x4 := lerp(grad3(p.perm[AB+1], xf, yf-1, zf-1), grad3(p.perm[BB+1], xf-1, yf-1, zf-1), u)
	y2 := lerp(x3, x4, v)
	return lerp(y1, y2, w)
}

// fbm2 sums 2D octaves at halving wavelengths, giving terrain both shape and
// roughness, normalised by total amplitude to roughly ±1.
func (p *perlin) fbm2(x, z float64, freq float64, octaves int) float64 {
	var sum, norm float64
	amp := 1.0
	for range octaves {
		sum += p.noise2(x*freq, z*freq) * amp
		norm += amp
		freq *= 2
		amp *= 0.5
	}
	return sum / norm
}

func lerp(a, b, t float64) float64 { return a + t*(b-a) }

// floorInt is math.Floor returning an int, keeping the noise's integer paths
// free of conversions.
func floorInt(v float64) int {
	if v >= 0 {
		return int(v)
	}
	return int(math.Floor(v))
}
