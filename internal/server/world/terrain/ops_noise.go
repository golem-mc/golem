package terrain

import (
	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

// noiseSampler samples a noise at scaled block coordinates.
// vanilla: NoiseFunction.Sampler
type noiseSampler struct {
	noise Noise
	xz, y float64
}

// NoiseSampler is vanilla: DensityFunctions.noise.
func NoiseSampler(n Noise, xzScale, yScale float64) Sampler { return &noiseSampler{n, xzScale, yScale} }

func (s *noiseSampler) Sample(_ *Context, x, y, z int32) float32 {
	return s.noise.Get3D(float64(x)*s.xz, float64(y)*s.y, float64(z)*s.xz)
}

func (s *noiseSampler) SampleVolume(_ *Context, out []float32, v Volume) {
	for i := range out {
		out[i] = 0
	}
	s.noise.AddToVolume(out, v, s.xz, s.y, 1.0)
}

// MappedNoise is vanilla: DensityFunctions.mappedNoise.
func MappedNoise(n Noise, xzScale, yScale float64, minTarget, maxTarget float32) Sampler {
	return Remap(NoiseSampler(n, xzScale, yScale), -1.0, 1.0, minTarget, maxTarget)
}

// shiftedXZ samples a noise at positions moved by two samplers.
// vanilla: NoiseFunction.ShiftedXzSampler
type shiftedXZ struct {
	shiftX, shiftZ Sampler
	noise          Noise
	xz, y          float64
}

// ShiftedNoise2D is vanilla: DensityFunctions.shiftedNoise2d.
func ShiftedNoise2D(shiftX, shiftZ Sampler, xzScale float64, n Noise) Sampler {
	if k, ok := constOf(shiftX); ok && k == 0 {
		if k, ok := constOf(shiftZ); ok && k == 0 {
			return &noiseSampler{n, xzScale, 0.0}
		}
	}
	return &shiftedXZ{shiftX, shiftZ, n, xzScale, 0.0}
}

func (s *shiftedXZ) Sample(c *Context, x, y, z int32) float32 {
	nx := float64(float64(x)*s.xz) + float64(s.shiftX.Sample(c, x, y, z))
	ny := float64(y) * s.y
	nz := float64(float64(z)*s.xz) + float64(s.shiftZ.Sample(c, x, y, z))
	return s.noise.Get3D(nx, ny, nz)
}

func (s *shiftedXZ) SampleVolume(c *Context, out []float32, v Volume) {
	s.shiftX.SampleVolume(c, out, v)
	zb := c.acquire(len(out))
	s.shiftZ.SampleVolume(c, zb, v)
	index := 0
	for z := range v.SizeZ {
		baseZ := float64(v.BlockZ(z)) * s.xz
		for x := range v.SizeX {
			baseX := float64(v.BlockX(x)) * s.xz
			for y := range v.SizeY {
				nx := baseX + float64(out[index])
				ny := float64(v.BlockY(y)) * s.y
				nz := baseZ + float64(zb[index])
				out[index] = s.noise.Get3D(nx, ny, nz)
				index++
			}
		}
	}
	c.release(zb)
}

// ShiftA is vanilla: DensityFunctions.shiftA.
func ShiftA(n Noise) Sampler { return Mul(&noiseSampler{n, 0.25, 0.0}, constant(4.0)) }

// Shift is vanilla: DensityFunctions.shift.
func Shift(n Noise) Sampler { return Mul(&noiseSampler{n, 0.25, 0.25}, constant(4.0)) }

// shiftB is vanilla: ShiftNoiseFunction.ShiftB.
type shiftB struct{ noise Noise }

// ShiftB is vanilla: DensityFunctions.shiftB.
func ShiftB(n Noise) Sampler { return &shiftB{n} }

func (s *shiftB) Sample(_ *Context, x, _, z int32) float32 {
	return s.noise.Get3D(float64(z)*0.25, float64(x)*0.25, 0.0) * 4.0
}

func (s *shiftB) SampleVolume(c *Context, out []float32, v Volume) {
	tv := Volume{v.SizeZ, v.SizeX, 1, v.MinZ, v.MinX, 0, v.StepZ, v.StepX, 1}
	tb := c.acquire(tv.Size())
	for i := range tb {
		tb[i] = 0
	}
	s.noise.AddToVolume(tb, tv, 0.25, 0.25, 4.0)
	for z := range v.SizeZ {
		for x := range v.SizeX {
			value := tb[tv.Index(z, x, 0)]
			i := v.Index(x, 0, z)
			for k := range v.SizeY {
				out[i+k] = value
			}
		}
	}
	c.release(tb)
}

// NewBlendedNoise builds the terrain base noise, drawing its octaves from r.
// vanilla: BlendedNoise.compileSampler
func NewBlendedNoise(r levelgen.RandomSource, xzScale, yScale, xzFactor, yFactor, smearScaleMultiplier float64) Sampler {
	xzMultiplier := 684.412 * xzScale
	yMultiplier := 684.412 * yScale
	limitSmearScaleY := yMultiplier * smearScaleMultiplier
	mainSmearScaleY := limitSmearScaleY / yFactor
	minLimit := createFbm(r, -15, limitSmearScaleY, 0.9999847412109375)
	maxLimit := createFbm(r, -15, limitSmearScaleY, 0.9999847412109375)
	mainNoise := createFbm(r, -7, mainSmearScaleY, 12.75)
	minS := &noiseSampler{minLimit, xzMultiplier, yMultiplier}
	maxS := &noiseSampler{maxLimit, xzMultiplier, yMultiplier}
	mainS := &noiseSampler{mainNoise, xzMultiplier / xzFactor, yMultiplier / yFactor}
	choice := Clamp(Add(mainS, constant(0.5)), 0.0, 1.0)
	return Lerp(choice, minS, maxS)
}

// createFbm is vanilla: BlendedNoise.createFbm.
func createFbm(r levelgen.RandomSource, firstOctave int, smearScaleY, valueFactor float64) *NoiseStack {
	octaves := -firstOctave + 1
	factor := 1.0
	valueFactor /= pow2(octaves) - 1.0
	var b NoiseStackBuilder
	for range octaves {
		b.Add(NewSmearedPerlinNoise(r, smearScaleY*factor), factor, float32(valueFactor))
		factor /= 2.0
		valueFactor *= 2.0
	}
	return b.Build()
}

func pow2(n int) float64 { return float64(int64(1) << uint(n)) }

// distanceToPoint is vanilla: DistanceToPointFunction.Sampler with the
// euclidean metric.
type distanceToPoint struct{ x, y, z int32 }

// DistanceToPoint is vanilla: DensityFunctions.distanceToPoint(point, EUCLIDEAN).
func DistanceToPoint(x, y, z int32) Sampler { return distanceToPoint{x, y, z} }

func (d distanceToPoint) Sample(_ *Context, x, y, z int32) float32 {
	dx, dy, dz := float32(d.x-x), float32(d.y-y), float32(d.z-z)
	return float32(sqrtF(lengthSquared3(dx, dy, dz)))
}

func (d distanceToPoint) SampleVolume(c *Context, out []float32, v Volume) {
	sampleVolumeNaive(c, out, v, d)
}

// slice samples its input at a fixed coordinate. vanilla: SliceFunction.*Sampler
type slice struct {
	axis  Axis
	coord int32
	input Sampler
}

// Slice is vanilla: DensityFunctions.slice.
func Slice(axis Axis, coordinate int32, input Sampler) Sampler {
	return &slice{axis, coordinate, input}
}

func (s *slice) Sample(c *Context, x, y, z int32) float32 {
	switch s.axis {
	case AxisX:
		return s.input.Sample(c, s.coord, y, z)
	case AxisY:
		return s.input.Sample(c, x, s.coord, z)
	}
	return s.input.Sample(c, x, y, s.coord)
}

func (s *slice) SampleVolume(c *Context, out []float32, v Volume) {
	switch s.axis {
	case AxisX:
		if v.SizeX == 1 && v.MinX == s.coord {
			s.input.SampleVolume(c, out, v)
			return
		}
		iv := Volume{1, v.SizeY, v.SizeZ, s.coord, v.MinY, v.MinZ, v.StepX, v.StepY, v.StepZ}
		ib := c.acquire(iv.Size())
		s.input.SampleVolume(c, ib, iv)
		index := 0
		for z := range v.SizeZ {
			for range v.SizeX {
				for y := range v.SizeY {
					out[index] = ib[iv.Index(0, y, z)]
					index++
				}
			}
		}
		c.release(ib)
	case AxisY:
		if v.SizeY == 1 && v.MinY == s.coord {
			s.input.SampleVolume(c, out, v)
			return
		}
		iv := Volume{v.SizeX, 1, v.SizeZ, v.MinX, s.coord, v.MinZ, v.StepX, v.StepY, v.StepZ}
		ib := c.acquire(iv.Size())
		s.input.SampleVolume(c, ib, iv)
		for z := range v.SizeZ {
			for x := range v.SizeX {
				value := ib[iv.Index(x, 0, z)]
				i := v.Index(x, 0, z)
				for k := range v.SizeY {
					out[i+k] = value
				}
			}
		}
		c.release(ib)
	case AxisZ:
		if v.SizeZ == 1 && v.MinZ == s.coord {
			s.input.SampleVolume(c, out, v)
			return
		}
		iv := Volume{v.SizeX, v.SizeY, 1, v.MinX, v.MinY, s.coord, v.StepX, v.StepY, v.StepZ}
		ib := c.acquire(iv.Size())
		s.input.SampleVolume(c, ib, iv)
		index := 0
		for range v.SizeZ {
			for x := range v.SizeX {
				for y := range v.SizeY {
					out[index] = ib[iv.Index(x, y, 0)]
					index++
				}
			}
		}
		c.release(ib)
	}
}

// contextBound stands in for a sampler a NoiseChunk may replace with a
// blender's; without a blender it is a constant that is not a ConstantFunction.
// vanilla: ContextBoundSampler
type contextBound float32

func (k contextBound) Sample(*Context, int32, int32, int32) float32 { return float32(k) }
func (k contextBound) SampleVolume(_ *Context, out []float32, _ Volume) {
	for i := range out {
		out[i] = float32(k)
	}
}

// BlendAlpha is vanilla: DensityFunctions.blendAlpha with an empty blender.
func BlendAlpha() Sampler { return contextBound(1) }

// BlendOffset is vanilla: DensityFunctions.blendOffset with an empty blender.
func BlendOffset() Sampler { return contextBound(0) }

// BlendDensity is vanilla: DensityFunctions.blendDensity with an empty
// blender, which leaves the density alone.
func BlendDensity(in Sampler) Sampler { return in }
