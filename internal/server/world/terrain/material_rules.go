package terrain

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

// ruleFn is a compiled rule: the block it places at a position, if any.
// vanilla: RuleEvaluator
type ruleFn func(x, y, z int32) (Material, bool)

// materialRule compiles to a ruleFn for one context. vanilla: MaterialRule
type materialRule func(c *materialContext) ruleFn

// materialCondition compiles to a test of the context's current position.
// vanilla: MaterialCondition
type materialCondition func(c *materialContext) func() bool

// anchor is a vertical position relative to the generation bounds.
// vanilla: VerticalAnchor
type anchor struct {
	kind   anchorKind
	offset int32
}

type anchorKind uint8

const (
	anchorAbsolute anchorKind = iota
	anchorAboveBottom
	anchorBelowTop
)

func absolute(y int32) anchor    { return anchor{anchorAbsolute, y} }
func aboveBottom(n int32) anchor { return anchor{anchorAboveBottom, n} }
func belowTop(n int32) anchor    { return anchor{anchorBelowTop, n} }

// resolve is vanilla: VerticalAnchor.resolveY with the noise settings' bounds.
func (a anchor) resolve(minY, height int32) int32 {
	switch a.kind {
	case anchorAboveBottom:
		return minY + a.offset
	case anchorBelowTop:
		return height - 1 + minY - a.offset
	}
	return a.offset
}

const maxDouble = math.MaxFloat64

// mthMap is vanilla: Mth.map.
func mthMap(v, fromMin, fromMax, toMin, toMax float64) float64 {
	t := (v - fromMin) / (fromMax - fromMin)
	return toMin + float64(t*(toMax-toMin))
}

// block is vanilla: BlockRule.
func block(m Material) materialRule {
	return func(*materialContext) ruleFn { return func(_, _, _ int32) (Material, bool) { return m, true } }
}

// sequence is vanilla: SequenceRule.
func sequence(rules ...materialRule) materialRule {
	return func(c *materialContext) ruleFn {
		if len(rules) == 1 {
			return rules[0](c)
		}
		fns := make([]ruleFn, len(rules))
		for i, r := range rules {
			fns[i] = r(c)
		}
		return func(x, y, z int32) (Material, bool) {
			for _, f := range fns {
				if m, ok := f(x, y, z); ok {
					return m, true
				}
			}
			return 0, false
		}
	}
}

// ifTrue is vanilla: ConditionRule.
func ifTrue(cond materialCondition, then materialRule) materialRule {
	return func(c *materialContext) ruleFn {
		test, run := cond(c), then(c)
		return func(x, y, z int32) (Material, bool) {
			if !test() {
				return 0, false
			}
			return run(x, y, z)
		}
	}
}

// bandlands is vanilla: BandlandsRule.
func bandlands(c *materialContext) ruleFn {
	sample := c.noise(c.sys.clayBandsOffset, false)
	return func(_, y, _ int32) (Material, bool) { return c.sys.bandFromNoise(y, float32(sample())), true }
}

// oreVein is vanilla: OreVeinRule.
func oreVein(pick func(*VeinConfig) Vein, ore, rawOre, filler Material) materialRule {
	const rawOreChance = float32(0.02)
	return func(c *materialContext) ruleFn {
		veins := c.sys.rs.Router.Veins
		vein := pick(veins)
		density := c.bandDensity(vein.Density, vein.MinY, vein.MaxY)
		richness := c.pointDensity(veins.Richness)
		gap := c.pointDensity(veins.Gap)
		factory := c.sys.rs.RandomFactory("minecraft:ore")
		var r levelgen.RandomSource
		return func(x, y, z int32) (Material, bool) {
			if y < vein.MinY || y > vein.MaxY {
				return 0, false
			}
			d := density()
			if d <= 0 {
				return 0, false
			}
			if r == nil {
				r = factory.At(x, y, z)
			} else {
				factory.SeedAt(r, x, y, z)
			}
			if r.NextFloat() > d {
				return 0, false
			}
			if r.NextFloat() < richness() && gap() < 0 {
				if r.NextFloat() < rawOreChance {
					return rawOre, true
				}
				return ore, true
			}
			return filler, true
		}
	}
}

func not(target materialCondition) materialCondition {
	return func(c *materialContext) func() bool {
		t := target(c)
		return func() bool { return !t() }
	}
}

// isBiome is vanilla: BiomeCondition.
func isBiome(paths ...string) materialCondition {
	return func(c *materialContext) func() bool {
		ids := make([]int, 0, len(paths))
		for _, p := range paths {
			if i := c.biomes.Index(p); i >= 0 {
				ids = append(ids, i)
			}
		}
		if c.possibleBiomes != nil {
			matches := 0
			for _, b := range c.possibleBiomes {
				if slices.Contains(ids, b) {
					matches++
				}
			}
			if matches == 0 {
				return func() bool { return false }
			}
			if matches == len(c.possibleBiomes) {
				return func() bool { return true }
			}
		}
		return c.lazyY(func() bool {
			b := c.getBiome()
			return slices.Contains(ids, b)
		})
	}
}

// stoneDepth is vanilla: StoneDepthCondition.
func stoneDepth(offset int32, addSurfaceDepth bool, secondaryRange int32, ceiling bool) materialCondition {
	return func(c *materialContext) func() bool {
		return c.lazyY(func() bool {
			depth := c.above
			if ceiling {
				depth = c.below
			}
			var surface, secondary int32
			if addSurfaceDepth {
				surface = c.depth
			}
			if secondaryRange != 0 {
				secondary = int32(mthMap(c.surfaceSecondary(), -1.0, 1.0, 0.0, float64(secondaryRange)))
			}
			return depth <= 1+offset+surface+secondary
		})
	}
}

// yAbove is vanilla: YCondition.
func yAbove(a anchor, surfaceDepthMultiplier int32, addStoneDepth bool) materialCondition {
	return func(c *materialContext) func() bool {
		return c.lazyY(func() bool {
			y := c.y
			if addStoneDepth {
				y += c.above
			}
			return y >= a.resolve(c.sys.rs.MinY, c.sys.rs.Height)+c.depth*surfaceDepthMultiplier
		})
	}
}

// water is vanilla: WaterCondition.
func water(offset, surfaceDepthMultiplier int32, addStoneDepth bool) materialCondition {
	return func(c *materialContext) func() bool {
		return c.lazyY(func() bool {
			if c.water == noWater {
				return true
			}
			y := c.y
			if addStoneDepth {
				y += c.above
			}
			return y >= c.water+offset+c.depth*surfaceDepthMultiplier
		})
	}
}

// noiseThreshold is vanilla: NoiseThresholdCondition.
func noiseThreshold(name string, lo, hi float64, is3d bool) materialCondition {
	return func(c *materialContext) func() bool {
		n := c.noise(c.sys.rs.Noise("minecraft:"+name), is3d)
		return func() bool {
			v := n()
			return v >= lo && v <= hi
		}
	}
}

// verticalGradient is vanilla: VerticalGradientCondition.
func verticalGradient(name string, trueAtAndBelow, falseAtAndAbove anchor) materialCondition {
	return func(c *materialContext) func() bool {
		lo := trueAtAndBelow.resolve(c.sys.rs.MinY, c.sys.rs.Height)
		hi := falseAtAndAbove.resolve(c.sys.rs.MinY, c.sys.rs.Height)
		factory := c.sys.rs.RandomFactory("minecraft:" + name)
		rnd := factory.At(0, 0, 0)
		return c.lazyY(func() bool {
			y := c.y
			if y <= lo {
				return true
			}
			if y >= hi {
				return false
			}
			p := mthMap(float64(y), float64(lo), float64(hi), 1.0, 0.0)
			factory.SeedAt(rnd, c.x, y, c.z)
			return float64(rnd.NextFloat()) < p
		})
	}
}

// steep is vanilla: SteepCondition.
func steep(c *materialContext) func() bool {
	return c.lazyXZ(func() bool { return c.gradX <= -4 || c.gradZ >= 4 })
}

// hole is vanilla: HoleCondition.
func hole(c *materialContext) func() bool {
	return c.lazyXZ(func() bool { return c.depth <= 0 })
}

// abovePreliminarySurface is vanilla: AbovePreliminarySurfaceCondition.
func abovePreliminarySurface(c *materialContext) func() bool {
	return func() bool { return c.y >= c.minSurfaceLevel() }
}
