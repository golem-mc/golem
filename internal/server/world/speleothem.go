package world

import (
	"math"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

type speleothemBlocks struct {
	base, pointed registry.Block
	replaceable   func(registry.Block) bool
}

func parseSpeleothemBlocks(o jobj) speleothemBlocks {
	return speleothemBlocks{registry.BlockOf(compileBlockState(o["base_block"])),
		registry.BlockOf(compileBlockState(o["pointed_block"])), blockSetOf(o["replaceable_blocks"])}
}

func (c speleothemBlocks) isBase(s uint16) bool {
	b := registry.BlockOf(s)
	return b == c.base || c.replaceable(b)
}

func (c speleothemBlocks) placeBase(r *decorRegion, p treePos) bool {
	if !c.replaceable(registry.BlockOf(r.get(p[0], p[1], p[2]))) {
		return false
	}
	r.set(p[0], p[1], p[2], c.base.DefaultState())
	return true
}

func speleothemEmpty(s uint16) bool { return overworldAirBlocks[registry.BlockOf(s)] || isWater(s) }

// grow is SpeleothemUtils.growSpeleothem and buildBaseToTipColumn.
func (c speleothemBlocks) grow(r *decorRegion, p treePos, dy, height int32, merge bool) {
	if !c.isBase(r.get(p[0], p[1]-dy, p[2])) {
		return
	}
	direction := "up"
	if dy < 0 {
		direction = "down"
	}
	for i := range height {
		thickness := "middle"
		switch i {
		case height - 1:
			thickness = "tip"
			if merge {
				thickness = "tip_merge"
			}
		case height - 2:
			thickness = "frustum"
		case 0:
			thickness = "base"
		}
		s, _ := registry.WithProp(c.pointed.DefaultState(), registry.PropVerticalDirection, direction)
		s, _ = registry.WithProp(s, registry.PropThickness, thickness)
		s, _ = registry.WithBool(s, registry.PropWaterlogged, fluidHolding(r.get(p[0], p[1], p[2])).kind == fluidWater)
		r.set(p[0], p[1], p[2], s)
		p[1] += dy
	}
}

// speleothemFeature is SpeleothemFeature.place.
func speleothemFeature(o jobj) featureFn {
	jonly(o, "base_block", "pointed_block", "replaceable_blocks", "chance_of_taller_generation", "chance_of_directional_spread", "chance_of_spread_radius2", "chance_of_spread_radius3")
	c := parseSpeleothemBlocks(o)
	chance := func(key string, def float32) float32 {
		if o.has(key) {
			return float32(o.num(key))
		}
		return def
	}
	taller, spread := chance("chance_of_taller_generation", .2), chance("chance_of_directional_spread", .7)
	radius2, radius3 := chance("chance_of_spread_radius2", .5), chance("chance_of_spread_radius3", .5)
	return func(r *decorRegion, p treePos) bool {
		above, below := c.isBase(r.get(p[0], p[1]+1, p[2])), c.isBase(r.get(p[0], p[1]-1, p[2]))
		dy := int32(1)
		switch {
		case above && below:
			if r.rng.NextBoolean() {
				dy = -1
			}
		case above:
			dy = -1
		case !below:
			return false
		}
		root := treePos{p[0], p[1] - dy, p[2]}
		c.placeBase(r, root)
		for _, d := range horizontalDirs {
			if r.rng.NextFloat() > spread {
				continue
			}
			p1 := at(root, treePos{d[0], 0, d[1]})
			c.placeBase(r, p1)
			if r.rng.NextFloat() > radius2 {
				continue
			}
			p2 := at(p1, buddingOffsets[r.rng.NextIntN(6)])
			c.placeBase(r, p2)
			if r.rng.NextFloat() > radius3 {
				continue
			}
			c.placeBase(r, at(p2, buddingOffsets[r.rng.NextIntN(6)]))
		}
		height := int32(1)
		if r.rng.NextFloat() < taller && speleothemEmpty(r.get(p[0], p[1]+dy, p[2])) {
			height = 2
		}
		c.grow(r, p, dy, height, false)
		return true
	}
}

type speleothemCluster struct {
	speleothemBlocks
	searchRange, maxHeightDiff, heightDeviation, edgeDistance, centerDistance int32
	height, radius, layer                                                     intProvider
	density, wetness                                                          floatProvider
	edgeChance                                                                float32
}

func speleothemClusterFeature(o jobj) featureFn {
	jonly(o, "base_block", "pointed_block", "replaceable_blocks", "floor_to_ceiling_search_range", "height", "radius", "max_stalagmite_stalactite_height_diff", "height_deviation", "speleothem_block_layer_thickness", "density", "wetness", "chance_of_speleothem_at_max_distance_from_center", "max_distance_from_edge_affecting_chance_of_speleothem", "max_distance_from_center_affecting_height_bias")
	c := speleothemCluster{
		speleothemBlocks: parseSpeleothemBlocks(o), searchRange: o.integer("floor_to_ceiling_search_range"),
		maxHeightDiff: o.integer("max_stalagmite_stalactite_height_diff"), heightDeviation: o.integer("height_deviation"),
		edgeDistance: o.integer("max_distance_from_edge_affecting_chance_of_speleothem"), centerDistance: o.integer("max_distance_from_center_affecting_height_bias"),
		height: compileInt(o["height"]), radius: compileInt(o["radius"]), layer: compileInt(o["speleothem_block_layer_thickness"]),
		density: compileFloat(o["density"]), wetness: compileFloat(o["wetness"]), edgeChance: float32(o.num("chance_of_speleothem_at_max_distance_from_center")),
	}
	return c.place
}

// place is SpeleothemClusterFeature.place.
func (c speleothemCluster) place(r *decorRegion, p treePos) bool {
	if !speleothemEmpty(r.get(p[0], p[1], p[2])) {
		return false
	}
	height, wetness, density := c.height(r.rng), c.wetness(r.rng), c.density(r.rng)
	xr, zr := c.radius(r.rng), c.radius(r.rng)
	for dx := -xr; dx <= xr; dx++ {
		for dz := -zr; dz <= zr; dz++ {
			distance := min(xr-absInt32(dx), zr-absInt32(dz))
			// vanilla: Mth.clampedMap(float, float, float, float, float)
			t := float32(distance) / float32(c.edgeDistance)
			chance := float32(1)
			if t <= 1 {
				chance = c.edgeChance + float32(t*(1-c.edgeChance))
			}
			c.column(r, at(p, treePos{dx, 0, dz}), dx, dz, wetness, float64(chance), height, density)
		}
	}
	return true
}

// scanSpeleothemEdge is Column.scanDirection with complementary predicates;
// an edge at searchRange-1 is included, but an empty block there is not.
func scanSpeleothemEdge(r *decorRegion, p treePos, searchRange, dy int32) (int32, bool) {
	for i := int32(1); i < searchRange && speleothemEmpty(r.get(p[0], p[1], p[2])); i++ {
		p[1] += dy
	}
	return p[1], !speleothemEmpty(r.get(p[0], p[1], p[2]))
}

func (c speleothemCluster) pool(r *decorRegion, p treePos) bool {
	s := r.get(p[0], p[1], p[2])
	b := registry.BlockOf(s)
	if isWater(s) || b == c.base || b == c.pointed || fluidHolding(r.get(p[0], p[1]+1, p[2])).kind == fluidWater {
		return false
	}
	adjacent := func(p treePos) bool {
		s := r.get(p[0], p[1], p[2])
		return registry.BlockOf(s).InTag(registry.TagIDBaseStoneOverworld) || fluidHolding(s).kind == fluidWater
	}
	for _, d := range horizontalDirs {
		if !adjacent(at(p, treePos{d[0], 0, d[1]})) {
			return false
		}
	}
	return adjacent(at(p, treePos{0, -1, 0}))
}

func (c speleothemCluster) replaceLayer(r *decorRegion, p treePos, dy int32) {
	n := c.layer(r.rng)
	for range n {
		if !c.placeBase(r, p) {
			return
		}
		p[1] += dy
	}
}

func (c speleothemCluster) sampleHeight(rng *levelgen.WorldgenRandom, dx, dz int32, density float32, height int32) int32 {
	if rng.NextFloat() > density {
		return 0
	}
	// vanilla: getSpeleothemHeight uses the double overload of Mth.clampedMap.
	t := min(max(float64(absInt32(dx)+absInt32(dz))/float64(c.centerDistance), 0), 1)
	mean := float32(float64(height)/2 + t*(0-float64(height)/2))
	return int32(clampedNormal(rng, mean, float32(c.heightDeviation), 0, float32(height)))
}

// column is SpeleothemClusterFeature.placeColumn. Random draws stay in vanilla
// order, including the merge draw when either height is zero.
func (c speleothemCluster) column(r *decorRegion, p treePos, dx, dz int32, wetness float32, chance float64, height int32, density float32) {
	if !speleothemEmpty(r.get(p[0], p[1], p[2])) {
		return
	}
	ceiling, hasCeiling := scanSpeleothemEdge(r, p, c.searchRange, 1)
	floor, hasFloor := scanSpeleothemEdge(r, p, c.searchRange, -1)
	if !hasCeiling && !hasFloor {
		return
	}
	if r.rng.NextFloat() < wetness && hasFloor && c.pool(r, treePos{p[0], floor, p[2]}) {
		r.set(p[0], floor, p[2], registry.BlockWater.DefaultState())
		floor--
	}
	downHeight, upHeight := int32(0), int32(0)
	wantDown := r.rng.NextDouble() < chance
	if hasCeiling && wantDown && registry.BlockOf(r.get(p[0], ceiling, p[2])) != registry.BlockLava {
		c.replaceLayer(r, treePos{p[0], ceiling, p[2]}, 1)
		maxHeight := height
		if hasFloor {
			maxHeight = min(maxHeight, ceiling-floor)
		}
		downHeight = c.sampleHeight(r.rng, dx, dz, density, maxHeight)
	}
	wantUp := r.rng.NextDouble() < chance
	if hasFloor && wantUp && registry.BlockOf(r.get(p[0], floor, p[2])) != registry.BlockLava {
		c.replaceLayer(r, treePos{p[0], floor, p[2]}, -1)
		if hasCeiling {
			upHeight = max(0, downHeight+r.rng.NextIntN(2*c.maxHeightDiff+1)-c.maxHeightDiff)
		} else {
			upHeight = c.sampleHeight(r.rng, dx, dz, density, height)
		}
	}
	if hasCeiling && hasFloor && ceiling-downHeight <= floor+upHeight {
		lo, hi := max(ceiling-downHeight, floor+1), min(floor+upHeight, ceiling-1)
		bottom := lo + r.rng.NextIntN(hi+2-lo)
		downHeight, upHeight = ceiling-bottom, bottom-1-floor
	}
	merge := r.rng.NextBoolean() && downHeight > 0 && upHeight > 0 && hasCeiling && hasFloor && downHeight+upHeight == ceiling-floor-1
	if hasCeiling {
		c.grow(r, treePos{p[0], ceiling - 1, p[2]}, -1, downHeight, merge)
	}
	if hasFloor {
		c.grow(r, treePos{p[0], floor + 1, p[2]}, 1, upHeight, merge)
	}
}

// speleothemHeight is SpeleothemUtils.getSpeleothemHeight.
func speleothemHeight(distance, radius, scale, bluntness float64) float64 {
	distance = max(distance, bluntness)
	r := distance / radius * .384
	part1 := .75 * math.Pow(r, 1.3333333333333333)
	part2 := math.Pow(r, .6666666666666666)
	part3 := .3333333333333333 * math.Log(r)
	return max(scale*(part1-part2-part3), 0) / .384 * radius
}

func speleothemEmptyOrLava(s uint16) bool {
	return speleothemEmpty(s) || registry.BlockOf(s) == registry.BlockLava
}

// speleothemEmbedded is SpeleothemUtils.isCircleMostlyEmbeddedInStone.
func speleothemEmbedded(r *decorRegion, p treePos, radius int32) bool {
	if speleothemEmptyOrLava(r.get(p[0], p[1], p[2])) {
		return false
	}
	increment := float32(6) / float32(radius)
	for angle := float32(0); angle < float32(math.Pi)*2; angle += increment {
		dx := int32(levelgen.MthCos(float64(angle)) * float32(radius))
		dz := int32(levelgen.MthSin(float64(angle)) * float32(radius))
		if speleothemEmptyOrLava(r.get(p[0]+dx, p[1], p[2]+dz)) {
			return false
		}
	}
	return true
}
