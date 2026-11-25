package terrain

import (
	"math"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

// surfaceDepthBelowPreliminary is how far under the preliminary surface the
// rules still build surface. vanilla: MaterialRuleContext.HOW_FAR_BELOW_PRELIMINARY_SURFACE_LEVEL_TO_BUILD_SURFACE
const surfaceDepthBelowPreliminary = 8

// noWater marks a block with no fluid above it.
const noWater = math.MinInt32

// materialContext is the column and block the rules are evaluated for, with
// the lazily computed values they share. vanilla: MaterialRuleContext
//
// updXZ and updY count column and block changes; a lazy value is stale when
// its stamp differs from the counter.
type materialContext struct {
	sys            *MaterialSystem
	ctx            *Context
	volume         Volume
	biomes         Biomes
	possibleBiomes []int
	// Carving only evaluates the exposed dirt blocks in a chunk.
	sampleBandsAtPoint bool
	// prefilled is the volumes densities sampled, released by close.
	prefilled [][]float32
	updXZ     int64
	updY      int64
	x, z      int32
	gradX     int32
	gradZ     int32
	depth     int32
	y         int32
	water     int32
	below     int32
	above     int32
	biome     int
	hasBiom   bool

	// rnd is the scratch stream surfaceDepth reseeds per column.
	rnd       levelgen.RandomSource
	prelimVol Volume
	prelimBuf []float32
	secStamp  int64
	secondary float64
	minStamp  int64
	minSurf   int32
	noises    map[materialNoiseKey]func() float64
}

type materialNoiseKey struct {
	noise *NoiseStack
	is3d  bool
}

func newMaterialContext(s *MaterialSystem, nc *NoiseChunk, volume Volume, biomes Biomes, possibleBiomes []int) *materialContext {
	return &materialContext{
		sys: s, ctx: nc.ctx, volume: volume, biomes: biomes, possibleBiomes: possibleBiomes,
		prelimVol: NewVolume(volume.SizeX, 1, volume.SizeZ, volume.MinX, 0, volume.MinZ),
		secStamp:  -1, minStamp: -1, rnd: s.rs.random.At(0, 0, 0),
	}
}

// updateXZ moves to a column. vanilla: MaterialRuleContext.updateXZ
func (c *materialContext) updateXZ(x, z, gradX, gradZ int32) {
	c.updXZ++
	c.updY++
	c.x, c.z, c.gradX, c.gradZ = x, z, gradX, gradZ
	c.depth = c.sys.surfaceDepth(x, z, c.rnd)
}

// updateY moves to a block of the column. vanilla: MaterialRuleContext.updateY
func (c *materialContext) updateY(above, below, water, y int32) {
	c.updY++
	c.hasBiom = false
	c.y, c.water, c.below, c.above = y, water, below, above
}

func (c *materialContext) surfaceSecondary() float64 {
	if c.secStamp != c.updXZ {
		c.secStamp = c.updXZ
		c.secondary = float64(c.sys.surfaceSecondary.Get3D(float64(c.x), 0, float64(c.z)))
	}
	return c.secondary
}

func (c *materialContext) getBiome() int {
	if !c.hasBiom {
		c.biome, c.hasBiom = c.biomes.At(c.x, c.y, c.z), true
	}
	return c.biome
}

// minSurfaceLevel is the preliminary surface under the column less its depth
// and 8. vanilla: MaterialRuleContext.getMinSurfaceLevel
func (c *materialContext) minSurfaceLevel() int32 {
	if c.minStamp != c.updXZ {
		c.minStamp = c.updXZ
		var level float32
		if i := c.prelimVol.IndexOfBlock(c.x, 0, c.z); i != -1 {
			if c.prelimBuf == nil {
				c.prelimBuf = make([]float32, c.prelimVol.Size())
				c.sys.preliminary.SampleVolume(c.ctx, c.prelimBuf, c.prelimVol)
			}
			level = c.prelimBuf[i]
		} else {
			level = c.sys.preliminary.Sample(c.ctx, c.x, 0, c.z)
		}
		c.minSurf = floorInt(float64(level)) + c.depth - surfaceDepthBelowPreliminary
	}
	return c.minSurf
}

// pointDensity reads a density at the current block. vanilla: MaterialRuleContext.getDensitiesInChunk
func (c *materialContext) pointDensity(s Sampler) func() float32 {
	return func() float32 { return s.Sample(c.ctx, c.x, c.y, c.z) }
}

// bandDensity reads a density that is only wanted for blocks with minY <= y <=
// maxY (the caller skips the rest), sampled up front over that band of the
// chunk's volume.
func (c *materialContext) bandDensity(s Sampler, minY, maxY int32) func() float32 {
	if c.sampleBandsAtPoint {
		return c.pointDensity(s)
	}
	band := c.volume
	lo, hi := max(minY, c.volume.MinY), min(maxY, c.volume.MinY+c.volume.SizeY-1)
	if hi < lo {
		return c.pointDensity(s)
	}
	band.MinY, band.SizeY = lo, hi-lo+1
	buf := c.ctx.acquire(band.Size())
	c.prefilled = append(c.prefilled, buf)
	s.SampleVolume(c.ctx, buf, band)
	return func() float32 {
		if i := band.IndexOfBlock(c.x, c.y, c.z); i != -1 {
			return buf[i]
		}
		return s.Sample(c.ctx, c.x, c.y, c.z)
	}
}

// close hands the prefilled volumes back to the sampler context.
func (c *materialContext) close() {
	for _, b := range c.prefilled {
		c.ctx.release(b)
	}
	c.prefilled = nil
}

// lazyY caches a condition until the block changes. vanilla: MaterialRuleContext.LazyYCondition
func (c *materialContext) lazyY(f func() bool) func() bool {
	last, result := c.updY-1, false
	return func() bool {
		if c.updY != last {
			last, result = c.updY, f()
		}
		return result
	}
}

// lazyXZ caches a condition until the column changes. vanilla: MaterialRuleContext.LazyXZCondition
func (c *materialContext) lazyXZ(f func() bool) func() bool {
	last, result := c.updXZ-1, false
	return func() bool {
		if c.updXZ != last {
			last, result = c.updXZ, f()
		}
		return result
	}
}

// noise reads a noise at the column (2D) or block (3D), once per update.
// vanilla: MaterialRuleContext.getNoiseSampler
func (c *materialContext) noise(n *NoiseStack, is3d bool) func() float64 {
	key := materialNoiseKey{n, is3d}
	if sample, ok := c.noises[key]; ok {
		return sample
	}
	if c.noises == nil {
		c.noises = make(map[materialNoiseKey]func() float64)
	}
	var sample func() float64
	if is3d {
		last, v := c.updY-1, 0.0
		sample = func() float64 {
			if c.updY != last {
				last, v = c.updY, float64(n.Get3D(float64(c.x), float64(c.y), float64(c.z)))
			}
			return v
		}
	} else {
		last, v := c.updXZ-1, 0.0
		sample = func() float64 {
			if c.updXZ != last {
				last, v = c.updXZ, float64(n.Get3D(float64(c.x), 0, float64(c.z)))
			}
			return v
		}
	}
	c.noises[key] = sample
	return sample
}
