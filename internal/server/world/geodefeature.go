package world

import (
	"math"
	"sync"

	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/internal/server/world/terrain"
	"github.com/golem-mc/golem/registry"
)

// geodeStates are the amethyst_geode configuration's blocks. A crystal is
// indexed by the inner_placements entry, the Direction.values() facing and
// whether the cell it grows into holds a water source.
type geodeStates struct {
	air, amethyst, budding, calcite, basalt uint16
	crystal                                 [4][6][2]uint16
	water                                   registry.Block
}

// geodeDirections are Direction.values() in order, the order a crystal tries
// its budding cell's neighbours.
var geodeDirections = [6]struct {
	d      [3]int32
	facing string
}{{[3]int32{0, -1, 0}, "down"}, {[3]int32{0, 1, 0}, "up"}, {[3]int32{0, 0, -1}, "north"},
	{[3]int32{0, 0, 1}, "south"}, {[3]int32{-1, 0, 0}, "west"}, {[3]int32{1, 0, 0}, "east"}}

var geodeBlocks = sync.OnceValue(func() *geodeStates {
	find := func(name string, props map[string]string) uint16 {
		state, ok := registry.FindState(name, props)
		if !ok {
			panic("the block registry has no " + name)
		}
		return state
	}
	s := &geodeStates{
		air:      airState,
		amethyst: find("minecraft:amethyst_block", nil),
		budding:  find("minecraft:budding_amethyst", nil),
		calcite:  find("minecraft:calcite", nil),
		basalt:   find("minecraft:smooth_basalt", nil),
	}
	s.water, _ = registry.BlockByName("minecraft:water")
	for kind, name := range [4]string{
		"minecraft:small_amethyst_bud", "minecraft:medium_amethyst_bud",
		"minecraft:large_amethyst_bud", "minecraft:amethyst_cluster",
	} {
		for d, dir := range geodeDirections {
			for w, logged := range [2]string{"false", "true"} {
				s.crystal[kind][d][w] = find(name, map[string]string{"facing": dir.facing, "waterlogged": logged})
			}
		}
	}
	return s
})

// geodeNoise is the parity NormalNoise(-4, 1.0) a geode roughens its shells
// with, built from the world seed through LegacyRandomSource.
// vanilla: GeodeFeature.NOISE_PARAMETERS
func (r *decorRegion) geodeNoise() *terrain.NoiseStack {
	if r.geode == nil {
		r.geode = terrain.NewParityNormalNoise(-4, 1.0).Create(levelgen.NewLegacyRandom(r.worldSeed))
	}
	return r.geode
}

// placeGeode is GeodeFeature with the amethyst_geode configuration.
// vanilla: GeodeFeature.place
func (r *decorRegion) placeGeode(p [3]int32) bool {
	const (
		outerLo, outerHi = 4, 6 // outer_wall_distance
		layerFilling     = 1.7
		layerInner       = 2.2
		layerMiddle      = 3.2
		layerOuter       = 4.2
		crackChance      = 0.95
		crackBase        = 2.0
		crackPointOffset = 2
		clusterChance    = 0.35  // use_potential_placements_chance
		altChance        = 0.083 // use_alternate_layer0_chance
		reach            = 16    // min_gen_offset, max_gen_offset
		noiseMultiplier  = 0.05
		invalidThreshold = 1
	)
	st := geodeBlocks()
	rng := r.rng
	ox, oy, oz := p[0], p[1], p[2]
	numPoints := rng.NextIntN(2) + 3
	noise := r.geodeNoise()
	adjust := float64(numPoints) / outerHi
	innerAir := 1 / math.Sqrt(layerFilling)
	innermost := 1 / math.Sqrt(layerInner+adjust)
	innerCrust := 1 / math.Sqrt(layerMiddle+adjust)
	outerCrust := 1 / math.Sqrt(layerOuter+adjust)
	extra := 0.0
	if numPoints > 3 {
		extra = adjust
	}
	crackSize := 1 / math.Sqrt(crackBase+rng.NextDouble()/2+extra)
	hasCrack := float64(rng.NextFloat()) < crackChance

	type point struct {
		x, y, z int32
		offset  float64
	}
	points := make([]point, 0, numPoints)
	invalid := 0
	wall := func() int32 { return rng.NextIntN(outerHi-outerLo+1) + outerLo }
	for range numPoints {
		x := ox + wall()
		y := oy + wall()
		z := oz + wall()
		state := r.get(x, y, z)
		if (r.src.isAir(state) || stateInTag(state, registry.TagIDGeodeInvalidBlocks)) && func() bool { invalid++; return invalid > invalidThreshold }() {
			return false
		}
		points = append(points, point{x, y, z, float64(rng.NextIntN(2) + 1)})
	}
	var crackPoints [3][3]int32
	if hasCrack {
		n := numPoints*2 + 1
		var dx, dz int32
		switch rng.NextIntN(4) {
		case 0:
			dx = n
		case 1:
			dz = n
		case 2:
			dx, dz = n, n
		}
		crackPoints = [3][3]int32{{ox + dx, oy + 7, oz + dz}, {ox + dx, oy + 5, oz + dz}, {ox + dx, oy + 1, oz + dz}}
	}

	set := func(x, y, z int32, state uint16) {
		if !stateInTag(r.get(x, y, z), registry.TagIDFeaturesCannotReplace) {
			r.set(x, y, z, state)
		}
	}
	distSq := func(x, y, z int32, px, py, pz int32) float64 {
		dx, dy, dz := float64(x-px), float64(y-py), float64(z-pz)
		return dx*dx + dy*dy + dz*dz
	}
	var crystals [][3]int32
	// BlockPos.betweenClosed: x fastest, then y, then z.
	for z := oz - reach; z <= oz+reach; z++ {
		for y := oy - reach; y <= oy+reach; y++ {
			for x := ox - reach; x <= ox+reach; x++ {
				wobble := float64(noise.Get3D(float64(x), float64(y), float64(z))) * noiseMultiplier
				shell, crack := 0.0, 0.0
				for _, pt := range points {
					shell += 1/math.Sqrt(distSq(x, y, z, pt.x, pt.y, pt.z)+pt.offset) + wobble
				}
				if hasCrack {
					for _, c := range crackPoints {
						crack += 1/math.Sqrt(distSq(x, y, z, c[0], c[1], c[2])+crackPointOffset) + wobble
					}
				}
				switch {
				case shell < outerCrust:
				case hasCrack && crack >= crackSize && shell < innerAir:
					set(x, y, z, st.air)
					for _, dir := range geodeDirections {
						ax, ay, az := x+dir.d[0], y+dir.d[1], z+dir.d[2]
						if blocklogic.FluidOf(r.get(ax, ay, az)).Kind != blocklogic.FluidNone {
							r.scheduleFluidTick(ax, ay, az)
						}
					}
				case shell >= innerAir:
					set(x, y, z, st.air)
				case shell >= innermost:
					alt := float64(rng.NextFloat()) < altChance
					if alt {
						set(x, y, z, st.budding)
					} else {
						set(x, y, z, st.amethyst)
					}
					if alt && float64(rng.NextFloat()) < clusterChance {
						crystals = append(crystals, [3]int32{x, y, z})
					}
				case shell >= innerCrust:
					set(x, y, z, st.calcite)
				default:
					set(x, y, z, st.basalt)
				}
			}
		}
	}
	for _, c := range crystals {
		kind := rng.NextIntN(4)
		for d, dir := range geodeDirections {
			px, py, pz := c[0]+dir.d[0], c[1]+dir.d[1], c[2]+dir.d[2]
			here := r.get(px, py, pz)
			w := 0
			if blocklogic.FluidOf(here).IsSource() {
				w = 1
			}
			if !r.src.isAir(here) && (registry.BlockOf(here) != st.water || !blocklogic.FluidOf(here).IsFullWater()) {
				continue
			}
			set(px, py, pz, st.crystal[kind][d][w])
			break
		}
	}
	return true
}

// scheduleFluidTick records a fluid tick at a block inside the decorated 3x3.
// vanilla: WorldGenRegion.scheduleTick
func (r *decorRegion) scheduleFluidTick(x, y, z int32) {
	if y < r.minY || y >= r.minY+r.height || absInt32(x>>4-r.nx) > 1 || absInt32(z>>4-r.nz) > 1 {
		return
	}
	ps := r.protoAt(x, z)
	ps.sources = append(ps.sources, posKey{x & 15, y - r.minY, z & 15})
}
