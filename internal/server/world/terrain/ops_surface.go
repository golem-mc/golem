package terrain

import (
	"math"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

// findTopSurface is the height of the highest cell of a column whose density
// is positive. vanilla: FindTopSurfaceFunction.Sampler
type findTopSurface struct {
	density, upperBound Sampler
	lowerBound          int32
	cellHeight          int32
}

// FindTopSurface is vanilla: DensityFunctions.findTopSurface; the result does
// not depend on y. vanilla: SliceFunction.YSampler
func FindTopSurface(density, upperBound Sampler, lowerBound, stepSize int32) Sampler {
	return Slice(AxisY, 0, &findTopSurface{density, upperBound, lowerBound, stepSize})
}

func (s *findTopSurface) Sample(c *Context, x, y, z int32) float32 {
	return s.findSurfaceFrom(c, x, z, s.upperBound.Sample(c, x, y, z))
}

func (s *findTopSurface) SampleVolume(c *Context, out []float32, v Volume) {
	if v.SizeY != 1 {
		panic("terrain: cannot sample a surface with sizeY != 1")
	}
	s.upperBound.SampleVolume(c, out, v)
	index := 0
	for z := range v.SizeZ {
		bz := v.BlockZ(z)
		for x := range v.SizeX {
			out[index] = s.findSurfaceFrom(c, v.BlockX(x), bz, out[index])
			index++
		}
	}
}

func (s *findTopSurface) findSurfaceFrom(c *Context, x, z int32, upperBound float32) float32 {
	topY := floorInt(float64(upperBound/float32(s.cellHeight))) * s.cellHeight
	if topY <= s.lowerBound {
		return float32(s.lowerBound)
	}
	for probe := topY; probe >= s.lowerBound; probe -= s.cellHeight {
		if s.density.Sample(c, x, probe, z) > 0.0 {
			return float32(probe)
		}
	}
	return float32(s.lowerBound)
}

// endIslands is the end's outer islands. vanilla: EndIslandFunction.Sampler
type endIslands struct{ noise *SimplexNoise }

// EndOuterIslands is vanilla: DensityFunctions.endOuterIslands; r is the
// world seed's legacy random source.
func EndOuterIslands(r levelgen.RandomSource) Sampler {
	r.ConsumeCount(17292)
	return &endIslands{&SimplexNoise{newGradientNoise(r, 0.0)}}
}

func (s *endIslands) Sample(_ *Context, x, _, z int32) float32 {
	return (s.heightValue(x/8, z/8) - 8.0) / 128.0
}

func (s *endIslands) SampleVolume(c *Context, out []float32, v Volume) {
	for z := range v.SizeZ {
		bz := v.BlockZ(z)
		for x := range v.SizeX {
			value := s.Sample(c, v.BlockX(x), 0, bz)
			i := v.Index(x, 0, z)
			for k := range v.SizeY {
				out[i+k] = value
			}
		}
	}
}

// heightValue is vanilla: EndIslandFunction.getHeightValue.
func (s *endIslands) heightValue(sectionX, sectionZ int32) float32 {
	chunkX := sectionX / 2
	chunkZ := sectionZ / 2
	subX := sectionX % 2
	subZ := sectionZ % 2
	doffs := float32(-100.0)
	for xo := int32(-12); xo <= 12; xo++ {
		for zo := int32(-12); zo <= 12; zo++ {
			tx := int64(chunkX + xo)
			tz := int64(chunkZ + zo)
			if tx*tx+tz*tz <= 4096 || !(s.noise.Get2D(float64(tx), float64(tz)) < -0.9) {
				continue
			}
			sum := float32(float32(math.Abs(float64(tx)))*3439.0) + float32(float32(math.Abs(float64(tz)))*147.0)
			islandSize := float32(math.Mod(float64(sum), 13.0)) + 9.0
			xd := float32(subX - xo*2)
			zd := float32(subZ - zo*2)
			newDoffs := 100.0 - float32(sqrtF(float32(xd*xd)+float32(zd*zd))*islandSize)
			newDoffs = clampF(newDoffs, -100.0, 80.0)
			doffs = javaMax(doffs, newDoffs)
		}
	}
	return doffs
}
