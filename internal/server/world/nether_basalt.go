package world

import (
	"slices"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// The basalt deltas' features: the lava-and-magma delta, the basalt columns
// and the soul sand valley's basalt pillars.

// deltaCannotReplace and basaltColumnCannotPlaceOn are the blocks the delta
// and the columns leave alone. vanilla: DeltaFeature.CANNOT_REPLACE, BasaltColumnsFeature.CANNOT_PLACE_ON
var (
	deltaCannotReplace = [...]registry.Block{
		registry.BlockBedrock, registry.BlockNetherBricks, registry.BlockNetherBrickFence,
		registry.BlockNetherBrickStairs, registry.BlockNetherWart, registry.BlockChest, registry.BlockSpawner,
	}
	basaltColumnCannotPlaceOn = [...]registry.Block{
		registry.BlockLava, registry.BlockBedrock, registry.BlockMagmaBlock, registry.BlockSoulSand,
		registry.BlockNetherBricks, registry.BlockNetherBrickFence, registry.BlockNetherBrickStairs,
		registry.BlockNetherWart, registry.BlockChest, registry.BlockSpawner,
	}
)

func blockIn(list []registry.Block, state uint16) bool {
	b := registry.BlockOf(state)
	return slices.Contains(list, b)
}

// countOnEveryLayerMod is CountOnEveryLayerPlacement with a constant count:
// positions on the floor of each cave layer under the heightmap, layer after
// layer until one has none. Every position is found before the first feature
// is placed. vanilla: CountOnEveryLayerPlacement.getPositions
func countOnEveryLayerMod(count int32) placementMod {
	return func(r *decorRegion, p [3]int32, next walkNext) {
		var found [][3]int32
		for layer := 0; ; layer++ {
			any := false
			for range count {
				x := r.rng.NextIntN(16) + p[0]
				z := r.rng.NextIntN(16) + p[2]
				y := r.layerFloor(x, r.motionBlockingHeight(x, z), z, layer)
				if y < 0 {
					continue
				}
				found = append(found, [3]int32{x, y, z})
				any = true
			}
			if !any {
				break
			}
		}
		for _, q := range found {
			next.call(q)
		}
	}
}

// layerFloor is the y of the air above the layer-th floor found scanning down
// from startY, or -1. vanilla: CountOnEveryLayerPlacement.findOnGroundYPosition
func (r *decorRegion) layerFloor(x, startY, z int32, layer int) int32 {
	g := r.g
	empty := func(s uint16) bool { return g.isAir(s) || s == g.lavaState }
	cur := r.get(x, startY, z)
	n := 0
	for y := startY; y >= 1; y-- {
		below := r.get(x, y-1, z)
		if !empty(below) && empty(cur) && below != g.bedrockState {
			if n == layer {
				return y
			}
			n++
		}
		cur = below
	}
	return -1
}

func manhattan(x, y, z, ox, oy, oz int32) int32 {
	return absInt32(x-ox) + absInt32(y-oy) + absInt32(z-oz)
}

// withinManhattan walks the offsets of BlockPos.withinManhattan(origin, rx, 0,
// rz) in its order until visit returns false.
// vanilla: BlockPos.withinManhattan
func withinManhattan(origin [3]int32, rx, rz int32, visit func(x, y, z int32) bool) {
	maxDepth := rx + rz
	for depth := int32(0); depth <= maxDepth; depth++ {
		maxX := min(rx, depth)
		for x := -maxX; x <= maxX; x++ {
			maxY := min(0, depth-absInt32(x))
			for y := -maxY; y <= maxY; y++ {
				z := depth - absInt32(x) - absInt32(y)
				if z > rz {
					continue
				}
				if !visit(origin[0]+x, origin[1]+y, origin[2]+z) {
					return
				}
				if z != 0 && !visit(origin[0]+x, origin[1]+y, origin[2]-z) {
					return
				}
			}
		}
	}
}

// placeDelta is DeltaFeature with the nether's configuration: lava inside a
// rim of magma, radii of 3 to 7 and a rim offset of 0 to 2.
// vanilla: DeltaFeature.place, NetherFeatures.DELTA
func (g *NetherGenerator) placeDelta(r *decorRegion, p [3]int32) {
	rng := r.rng
	rimX, rimZ := int32(0), int32(0)
	spawnRim := rng.NextDouble() < 0.9
	if spawnRim {
		rimX = rng.NextIntN(3)
		rimZ = rng.NextIntN(3)
	}
	hasRim := spawnRim && rimX != 0 && rimZ != 0
	radiusX := rng.NextIntN(5) + 3
	radiusZ := rng.NextIntN(5) + 3
	limit := max(radiusX, radiusZ)
	lava := registry.BlockLava
	withinManhattan(p, radiusX, radiusZ, func(x, y, z int32) bool {
		if manhattan(x, y, z, p[0], p[1], p[2]) > limit {
			return false
		}
		if !g.deltaClear(r, x, y, z, lava) {
			return true
		}
		if hasRim {
			r.set(x, y, z, g.magmaState)
		}
		if g.deltaClear(r, x+rimX, y, z+rimZ, lava) {
			r.set(x+rimX, y, z+rimZ, g.lavaState)
		}
		return true
	})
}

// deltaClear is DeltaFeature.isClear: not the contents, not a protected
// block, air above and solid on the other five sides.
func (g *NetherGenerator) deltaClear(r *decorRegion, x, y, z int32, contents registry.Block) bool {
	s := r.get(x, y, z)
	if registry.BlockOf(s) == contents || blockIn(deltaCannotReplace[:], s) {
		return false
	}
	// vanilla: Direction.values() order, DOWN first; any mismatch fails.
	if r.isAir(x, y-1, z) || !r.isAir(x, y+1, z) {
		return false
	}
	return !r.isAir(x, y, z-1) && !r.isAir(x, y, z+1) && !r.isAir(x-1, y, z) && !r.isAir(x+1, y, z)
}

// airOrLavaSea is the basalt columns' can_replace predicate.
// vanilla: small_basalt_columns / large_basalt_columns feature definitions
func (g *NetherGenerator) airOrLavaSea(r *decorRegion, x, y, z int32) bool {
	s := r.get(x, y, z)
	return g.isAir(s) || registry.BlockOf(s) == registry.BlockLava && y <= netherSeaLevel
}

// columnSupport is BasaltColumnsFeature.canPlaceAt.
func (g *NetherGenerator) columnSupport(r *decorRegion, x, y, z int32) bool {
	if !g.airOrLavaSea(r, x, y, z) {
		return false
	}
	below := r.get(x, y-1, z)
	return !g.isAir(below) && !blockIn(basaltColumnCannotPlaceOn[:], below)
}

// placeBasaltColumns is BasaltColumnsFeature: clusters of basalt columns whose
// height falls off with the distance from the origin. height and reach are
// the configured int providers as inclusive ranges.
// vanilla: WeightedRandomSelectorFeature.place, SteppedColumnClusterFeature.place
func (g *NetherGenerator) placeBasaltColumns(r *decorRegion, p [3]int32, heightLo, heightHi, reachLo, reachHi int32) {
	rng := r.rng
	spread, count := int32(8), 15
	if rng.NextIntN(10) < 9 {
		spread, count = 5, 50
	}
	x0, y0, z0 := p[0], p[1], p[2]
	if !g.columnSupport(r, x0, y0, z0) {
		return
	}
	height := sampleInt(rng, heightLo, heightHi)
	spread = min(height, spread)
	for range count {
		x := x0 - spread + rng.NextIntN(2*spread+1)
		y := y0 + rng.NextIntN(1)
		z := z0 - spread + rng.NextIntN(2*spread+1)
		blocks := height - manhattan(x, y, z, x0, y0, z0)
		if blocks < 0 {
			continue
		}
		g.placeBasaltColumn(r, x, y, z, blocks, sampleInt(rng, reachLo, reachHi))
	}
}

// sampleInt samples a UniformInt, or a ConstantInt (no draw) when lo == hi.
func sampleInt(rng *levelgen.WorldgenRandom, lo, hi int32) int32 {
	if lo == hi {
		return lo
	}
	return rng.NextIntN(hi-lo+1) + lo
}

// placeBasaltColumn raises a column at every cell of the square of the given
// reach around (ox, oy, oz). vanilla: BasaltColumnsFeature.placeColumn
func (g *NetherGenerator) placeBasaltColumn(r *decorRegion, ox, oy, oz, height, reach int32) {
	for z := oz - reach; z <= oz+reach; z++ {
	next:
		for x := ox - reach; x <= ox+reach; x++ {
			step := manhattan(x, oy, z, ox, oy, oz)
			cy, ok := int32(0), false
			if g.airOrLavaSea(r, x, oy, z) {
				cy, ok = g.findColumnSurface(r, x, oy, z, step)
			} else {
				cy, ok = g.findColumnAir(r, x, oy, z, step)
			}
			if !ok {
				continue
			}
			for n := height - step/2; n >= 0; n-- {
				if g.airOrLavaSea(r, x, cy, z) {
					r.set(x, cy, z, g.basaltState)
				} else if r.get(x, cy, z) != g.basaltState {
					continue next
				}
				cy++
			}
		}
	}
}

func (g *NetherGenerator) findColumnSurface(r *decorRegion, x, y, z, limit int32) (int32, bool) {
	for ; y > 1 && limit > 0; y-- {
		limit--
		if g.columnSupport(r, x, y, z) {
			return y, true
		}
	}
	return 0, false
}

func (g *NetherGenerator) findColumnAir(r *decorRegion, x, y, z, limit int32) (int32, bool) {
	for ; y < netherGenHeight && limit > 0; y++ {
		limit--
		s := r.get(x, y, z)
		if blockIn(basaltColumnCannotPlaceOn[:], s) {
			return 0, false
		}
		if g.isAir(s) {
			return y, true
		}
	}
	return 0, false
}
