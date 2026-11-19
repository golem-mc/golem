package world

import (
	"math"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// maxOreSize bounds oreConfig.size, which sizes the scratch a vein is drawn in.
const maxOreSize = 64

// oreTarget is one of OreConfiguration's target states: a vein turns a block
// the test accepts into state, the first target that accepts it winning.
type oreTarget struct {
	test  func(uint16, int32) bool
	state uint16
}

// oreConfig is OreConfiguration.
type oreConfig struct {
	size int32
	// discard is discard_chance_on_air_exposure.
	discard float32
	targets []oreTarget
	// probe is OreFeature.place's check that the vein's box reaches the
	// terrain; the Nether's roof is bedrock, so its veins always do.
	probe bool
}

// oceanFloorReaches reports whether the OCEAN_FLOOR_WG height of a column is
// at least y. Its terrain heights stay fixed throughout decoration.
// vanilla: Heightmap.Types.OCEAN_FLOOR_WG
func (r *decorRegion) oceanFloorReaches(x, z, y int32) bool {
	if y <= r.minY {
		return true
	}
	i := r.slot(x, z)
	if i < 0 {
		return r.oceanFloorHeight(x, z) >= y
	}
	if r.floors == nil {
		r.floors = make([]int32, decorRegionSpan*decorRegionSpan*256)
	}
	k := i*256 + int(z&15)*16 + int(x&15)
	if r.floors[k] == 0 {
		r.floors[k] = r.oceanFloorHeight(x, z) - r.minY + 1
	}
	return r.floors[k]-1+r.minY >= y
}

// oceanFloorHeight is one above the highest motion-blocking non-fluid block.
func (r *decorRegion) oceanFloorHeight(x, z int32) int32 {
	return r.wgHeight(hmOceanFloorWG, x, z)
}

// adjacentToAir reports air in any of the six neighbours of a block.
// vanilla: OreFeature.isAdjacentToAir
func (r *decorRegion) adjacentToAir(x, y, z int32) bool {
	for _, d := range [6][3]int32{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
		if r.isAir(x+d[0], y+d[1], z+d[2]) {
			return true
		}
	}
	return false
}

// placeOre is OreFeature: overlapping spheres along a line, each block a
// target accepts turned to its state. It reports whether a block was placed.
// vanilla: OreFeature.place, OreFeature.doPlace
func (r *decorRegion) placeOre(p [3]int32, cfg *oreConfig) bool {
	rng := r.rng
	size := cfg.size
	dir := rng.NextFloat() * float32(math.Pi)
	spreadXY := float32(size) / 8.0
	maxRadius := int32(math.Ceil(float64((float32(size)/16.0*2.0 + 1.0) / 2.0)))
	sinD, cosD := math.Sin(float64(dir)), math.Cos(float64(dir))
	x0 := float64(p[0]) + sinD*float64(spreadXY)
	x1 := float64(p[0]) - sinD*float64(spreadXY)
	z0 := float64(p[2]) + cosD*float64(spreadXY)
	z1 := float64(p[2]) - cosD*float64(spreadXY)
	y0 := float64(p[1] + rng.NextIntN(3) - 2)
	y1 := float64(p[1] + rng.NextIntN(3) - 2)
	xStart := p[0] - int32(math.Ceil(float64(spreadXY))) - maxRadius
	yStart := p[1] - 2 - maxRadius
	zStart := p[2] - int32(math.Ceil(float64(spreadXY))) - maxRadius
	sizeXZ := 2 * (int32(math.Ceil(float64(spreadXY))) + maxRadius)
	sizeY := 2 * (2 + maxRadius)
	if cfg.probe {
		reached := false
		for x := xStart; x <= xStart+sizeXZ && !reached; x++ {
			for z := zStart; z <= zStart+sizeXZ; z++ {
				if r.oceanFloorReaches(x, z, yStart) {
					reached = true
					break
				}
			}
		}
		if !reached {
			return false
		}
	}

	placed := false
	var data [maxOreSize * 4]float64
	n := int(size)
	for i := range n {
		step := float32(i) / float32(size)
		ss := rng.NextDouble() * float64(size) / 16.0
		data[i*4+0] = x0 + float64(step)*(x1-x0)
		data[i*4+1] = y0 + float64(step)*(y1-y0)
		data[i*4+2] = z0 + float64(step)*(z1-z0)
		data[i*4+3] = (float64(levelgen.MthSin(float64(float32(math.Pi)*step))+1.0)*ss + 1.0) / 2.0
	}
	for i1 := 0; i1 < n-1; i1++ {
		if data[i1*4+3] <= 0 {
			continue
		}
		for i2 := i1 + 1; i2 < n; i2++ {
			if data[i2*4+3] <= 0 {
				continue
			}
			dr := data[i1*4+3] - data[i2*4+3]
			dx, dy, dz := data[i1*4]-data[i2*4], data[i1*4+1]-data[i2*4+1], data[i1*4+2]-data[i2*4+2]
			if dr*dr > dx*dx+dy*dy+dz*dz {
				if dr > 0 {
					data[i2*4+3] = -1
				} else {
					data[i1*4+3] = -1
				}
			}
		}
	}

	// Vanilla indexes its visited set by sizeXZ though x runs one past it and
	// aliases into the next row; the set is sized for that.
	words := int(sizeXZ+1) + int(sizeY+1)*int(sizeXZ) + int(sizeXZ+1)*int(sizeXZ)*int(sizeY) + 1
	if cap(r.tested) < words {
		r.tested = make([]bool, words)
	}
	tested := r.tested[:words]
	clear(tested)
	for i := range n {
		rad := data[i*4+3]
		if rad < 0 {
			continue
		}
		xx, yy, zz := data[i*4], data[i*4+1], data[i*4+2]
		xMin := max(int32(math.Floor(xx-rad)), xStart)
		yMin := max(int32(math.Floor(yy-rad)), yStart)
		zMin := max(int32(math.Floor(zz-rad)), zStart)
		xMax := max(int32(math.Floor(xx+rad)), xMin)
		yMax := max(int32(math.Floor(yy+rad)), yMin)
		zMax := max(int32(math.Floor(zz+rad)), zMin)
		for x := xMin; x <= xMax; x++ {
			xd := (float64(x) + 0.5 - xx) / rad
			if !(xd*xd < 1.0) {
				continue
			}
			for y := yMin; y <= yMax; y++ {
				yd := (float64(y) + 0.5 - yy) / rad
				if !(xd*xd+yd*yd < 1.0) {
					continue
				}
				for z := zMin; z <= zMax; z++ {
					zd := (float64(z) + 0.5 - zz) / rad
					if !(xd*xd+yd*yd+zd*zd < 1.0) || y < r.minY || y >= r.minY+r.height {
						continue
					}
					idx := (x - xStart) + (y-yStart)*sizeXZ + (z-zStart)*sizeXZ*sizeY
					if tested[idx] {
						continue
					}
					tested[idx] = true
					if r.tryOre(x, y, z, cfg) {
						placed = true
					}
				}
			}
		}
	}
	return placed
}

// tryOre replaces the block at a position by the first target that accepts
// it, unless the air-exposure roll refuses, and reports whether it did.
// vanilla: OreFeature.canPlaceOre, OreFeature.shouldSkipAirCheck
func (r *decorRegion) tryOre(x, y, z int32, cfg *oreConfig) bool {
	cur := r.get(x, y, z)
	for _, t := range cfg.targets {
		if !t.test(cur, y) {
			continue
		}
		skipAirCheck := cfg.discard <= 0 || cfg.discard < 1 && r.rng.NextFloat() >= cfg.discard
		if skipAirCheck || !r.adjacentToAir(x, y, z) {
			r.setKnownShape(x, y, z, t.state)
			return true
		}
	}
	return false
}

// oreFeature reads an ore feature's configuration. A target's rule test is a
// tag_match, the only one the generator lets through.
// vanilla: OreConfiguration.CODEC, TagMatchTest
func oreFeature(cfg jobj) featureFn {
	jonly(cfg, "size", "discard_chance_on_air_exposure", "targets")
	c := &oreConfig{size: cfg.integer("size"), discard: float32(cfg.num("discard_chance_on_air_exposure")), probe: true}
	for _, t := range cfg.arr("targets") {
		t := jobjOf(t)
		c.targets = append(c.targets, oreTarget{test: compileOreRule(t.obj("target")), state: compileBlockState(t["state"])})
	}
	return func(r *decorRegion, p [3]int32) bool { return r.placeOre(p, c) }
}

// compileOreRule reads the state and height tests used by ore target rules.
// vanilla: RuleTest.CODEC, HeightMatchTest.test, AllOfTest.test, AnyOfTest.test
func compileOreRule(o jobj) func(uint16, int32) bool {
	switch jtype := o.str("predicate_type"); jtype {
	case "minecraft:tag_match":
		jonly(o, "predicate_type", "tag")
		tag := registry.BlockTagOf(o.str("tag"))
		return func(s uint16, _ int32) bool { return registry.BlockOf(s).InTag(tag) }
	case "minecraft:height_match":
		jonly(o, "predicate_type", "min_inclusive", "max_inclusive")
		lo, hi := o.integer("min_inclusive"), o.integer("max_inclusive")
		return func(_ uint16, y int32) bool { return y >= lo && y <= hi }
	case "minecraft:not":
		jonly(o, "predicate_type", "rule")
		rule := compileOreRule(o.obj("rule"))
		return func(s uint16, y int32) bool { return !rule(s, y) }
	case "minecraft:all_of", "minecraft:any_of":
		jonly(o, "predicate_type", "rules")
		var rules []func(uint16, int32) bool
		for _, raw := range o.arr("rules") {
			rules = append(rules, compileOreRule(jobjOf(raw)))
		}
		all := jtype == "minecraft:all_of"
		return func(s uint16, y int32) bool {
			for _, rule := range rules {
				if rule(s, y) != all {
					return !all
				}
			}
			return all
		}
	default:
		modelFail("unknown ore rule %q", jtype)
	}
	return nil
}
