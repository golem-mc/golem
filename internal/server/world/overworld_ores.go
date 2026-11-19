package world

import (
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// undergroundDef is a placed feature of the UNDERGROUND_ORES step: its
// placement modifiers short of the biome filter, and what they feed.
type undergroundDef struct {
	mods  []placementMod
	place func(*decorRegion, [3]int32)
}

// diskConfig is DiskConfiguration.
type diskConfig struct {
	halfHeight int32
	radius     intProvider
	target     blockPred
	state      optState
}

// undergroundFeatures are the step's features by name: ores, blobs, disks
// and underwater magma.
func (g *OverworldGenerator) undergroundFeatures() map[string]undergroundDef {
	const top = worldMinY + worldHeight - 1
	state := func(name string, overrides map[string]string) uint16 {
		s, ok := registry.FindState(name, overrides)
		if !ok {
			panic("the block registry has no " + name)
		}
		return s
	}
	inTag := func(tag registry.BlockTagID) func(uint16, int32) bool {
		return func(s uint16, _ int32) bool { return registry.BlockOf(s).InTag(tag) }
	}
	in := func(names ...string) blockPred {
		t := blocksNamed(names...)
		return func(r *decorRegion, p [3]int32) bool { return t[registry.BlockOf(r.get(p[0], p[1], p[2]))] }
	}
	baseStone := inTag(registry.TagIDBaseStoneOverworld)
	stoneRepl := inTag(registry.TagIDStoneOreReplaceables)
	deepRepl := inTag(registry.TagIDDeepslateOreReplaceables)

	blob := func(name string, size int32) func(*decorRegion, [3]int32) {
		cfg := &oreConfig{size: size, targets: []oreTarget{{baseStone, state(name, nil)}}, probe: true}
		return func(r *decorRegion, p [3]int32) { r.placeOre(p, cfg) }
	}
	ore := func(name string, size int32, discard float32, redstone bool) func(*decorRegion, [3]int32) {
		var props map[string]string
		if redstone {
			props = map[string]string{"lit": "false"}
		}
		cfg := &oreConfig{size: size, discard: discard, probe: true, targets: []oreTarget{
			{stoneRepl, state("minecraft:"+name+"_ore", props)},
			{deepRepl, state("minecraft:deepslate_"+name+"_ore", props)},
		}}
		return func(r *decorRegion, p [3]int32) { r.placeOre(p, cfg) }
	}
	// Heights are absolute; vanilla's above_bottom v is worldMinY+v.
	bottom := int32(worldMinY)
	square := func(count, height placementMod) []placementMod {
		return []placementMod{count, inSquareMod, height}
	}
	rare := func(chance int32, height placementMod) []placementMod {
		return []placementMod{rarityMod(chance), inSquareMod, height}
	}
	uniform := heightUniformMod
	trapezoid := heightTriangleMod

	water := func(r *decorRegion, p [3]int32) bool { return isWaterSource(r.get(p[0], p[1], p[2])) }
	disk := func(cfg *diskConfig) func(*decorRegion, [3]int32) {
		return func(r *decorRegion, p [3]int32) { r.placeDisk(p, cfg) }
	}
	sand, sandstone := state("minecraft:sand", nil), state("minecraft:sandstone", nil)
	grass, dirt := state("minecraft:grass_block", map[string]string{"snowy": "false"}), state("minecraft:dirt", nil)
	constant := func(s uint16) optState {
		return func(*decorRegion, [3]int32) (uint16, bool) { return s, true }
	}
	radius := func(min, span int32) intProvider {
		return func(rng *levelgen.WorldgenRandom) int32 { return rng.NextIntN(span) + min }
	}
	floorWater := []placementMod{inSquareMod, heightmapMod(oceanFloorWGHeight), filterMod(water)}
	mud := blocksNamed("minecraft:mud")

	return map[string]undergroundDef{
		"minecraft:ore_dirt":           {square(countMod(7), uniform(0, 160)), blob("minecraft:dirt", 33)},
		"minecraft:ore_gravel":         {square(countMod(14), uniform(bottom, top)), blob("minecraft:gravel", 33)},
		"minecraft:ore_granite_upper":  {rare(6, uniform(64, 128)), blob("minecraft:granite", 64)},
		"minecraft:ore_granite_lower":  {square(countMod(2), uniform(0, 60)), blob("minecraft:granite", 64)},
		"minecraft:ore_diorite_upper":  {rare(6, uniform(64, 128)), blob("minecraft:diorite", 64)},
		"minecraft:ore_diorite_lower":  {square(countMod(2), uniform(0, 60)), blob("minecraft:diorite", 64)},
		"minecraft:ore_andesite_upper": {rare(6, uniform(64, 128)), blob("minecraft:andesite", 64)},
		"minecraft:ore_andesite_lower": {square(countMod(2), uniform(0, 60)), blob("minecraft:andesite", 64)},
		"minecraft:ore_tuff":           {square(countMod(2), uniform(bottom, 0)), blob("minecraft:tuff", 64)},
		"minecraft:ore_coal_upper":     {square(countMod(30), uniform(136, top)), ore("coal", 17, 0, false)},
		"minecraft:ore_coal_lower":     {square(countMod(20), trapezoid(0, 192)), ore("coal", 17, 0.5, false)},
		"minecraft:ore_iron_upper":     {square(countMod(90), trapezoid(80, 384)), ore("iron", 9, 0, false)},
		"minecraft:ore_iron_middle":    {square(countMod(10), trapezoid(-24, 56)), ore("iron", 9, 0, false)},
		"minecraft:ore_iron_small":     {square(countMod(10), uniform(bottom, 72)), ore("iron", 4, 0, false)},
		"minecraft:ore_gold":           {square(countMod(4), trapezoid(-64, 32)), ore("gold", 9, 0.5, false)},
		"minecraft:ore_gold_lower":     {square(countUniformMod(0, 1), uniform(-64, -48)), ore("gold", 9, 0.5, false)},
		"minecraft:ore_redstone":       {square(countMod(4), uniform(bottom, 15)), ore("redstone", 8, 0, true)},
		"minecraft:ore_redstone_lower": {square(countMod(8), trapezoid(bottom-32, bottom+32)), ore("redstone", 8, 0, true)},
		"minecraft:ore_diamond":        {square(countMod(7), trapezoid(bottom-80, bottom+80)), ore("diamond", 4, 0.5, false)},
		"minecraft:ore_diamond_medium": {square(countMod(2), uniform(-64, -4)), ore("diamond", 8, 0.5, false)},
		"minecraft:ore_diamond_large":  {rare(9, trapezoid(bottom-80, bottom+80)), ore("diamond", 12, 0.7, false)},
		"minecraft:ore_diamond_buried": {square(countMod(4), trapezoid(bottom-80, bottom+80)), ore("diamond", 8, 1, false)},
		"minecraft:ore_lapis":          {square(countMod(2), trapezoid(-32, 32)), ore("lapis", 7, 0, false)},
		"minecraft:ore_lapis_buried":   {square(countMod(4), uniform(bottom, 64)), ore("lapis", 7, 1, false)},
		"minecraft:ore_copper_large":   {square(countMod(16), trapezoid(-16, 112)), ore("copper", 20, 0, false)},
		"minecraft:ore_copper":         {square(countMod(16), trapezoid(-16, 112)), ore("copper", 10, 0, false)},
		"minecraft:underwater_magma": {[]placementMod{
			countUniformMod(44, 52), inSquareMod, uniform(bottom, 256),
			surfaceRelativeMaxMod(oceanFloorWGHeight, -2),
		}, placeUnderwaterMagma(state("minecraft:magma_block", nil))},
		"minecraft:ore_clay":       {square(countMod(46), uniform(bottom, 256)), blob("minecraft:clay", 33)},
		"minecraft:ore_gold_extra": {square(countMod(50), uniform(32, 256)), ore("gold", 9, 0, false)},
		"minecraft:disk_grass": {[]placementMod{
			countMod(1), inSquareMod, heightmapMod(oceanFloorWGHeight),
			func(_ *decorRegion, p [3]int32, next walkNext) { next.call([3]int32{p[0], p[1] - 1, p[2]}) },
			filterMod(func(r *decorRegion, p [3]int32) bool { return mud[registry.BlockOf(r.get(p[0], p[1], p[2]))] }),
		}, disk(&diskConfig{
			halfHeight: 2, radius: radius(2, 5),
			target: in("minecraft:dirt", "minecraft:mud"),
			state: func(r *decorRegion, p [3]int32) (uint16, bool) {
				x, y, z := p[0], p[1], p[2]
				above := r.get(x, y+1, z)
				if registry.IsSolid(above) || isWaterSource(above) {
					return dirt, true
				}
				return grass, true
			},
		})},
		"minecraft:disk_sand": {append([]placementMod{countMod(3)}, floorWater...), disk(&diskConfig{
			halfHeight: 2, radius: radius(2, 5),
			target: in("minecraft:dirt", "minecraft:grass_block"),
			state: func(r *decorRegion, p [3]int32) (uint16, bool) {
				x, y, z := p[0], p[1], p[2]
				if registry.BlockOf(r.get(x, y-1, z)) == registry.BlockAir {
					return sandstone, true
				}
				return sand, true
			},
		})},
		"minecraft:disk_clay": {floorWater, disk(&diskConfig{
			halfHeight: 1, radius: radius(2, 2),
			target: in("minecraft:dirt", "minecraft:clay"),
			state:  constant(state("minecraft:clay", nil)),
		})},
		"minecraft:disk_gravel": {floorWater, disk(&diskConfig{
			halfHeight: 2, radius: radius(2, 4),
			target: in("minecraft:dirt", "minecraft:grass_block"),
			state:  constant(state("minecraft:gravel", nil)),
		})},
		"minecraft:ore_emerald": {square(countMod(100), trapezoid(-16, 480)), ore("emerald", 3, 0, false)},
	}
}

// isWaterSource is the matching_fluids water test on a state.
// vanilla: MatchingFluidsPredicate
func isWaterSource(s uint16) bool {
	f := blocklogic.FluidOf(s)
	return f.Kind == blocklogic.FluidWater && f.Level == 0
}

// placeDisk is DiskFeature: a disc of columns, each block the target accepts
// turned to the provider's state from the top of the column down.
// vanilla: DiskFeature.place, DiskFeature.placeColumn
func (r *decorRegion) placeDisk(p [3]int32, cfg *diskConfig) bool {
	placedAny := false
	top := p[1] + cfg.halfHeight
	bottom := p[1] - cfg.halfHeight - 1
	rad := cfg.radius(r.rng)
	for z := p[2] - rad; z <= p[2]+rad; z++ {
		for x := p[0] - rad; x <= p[0]+rad; x++ {
			xd, zd := x-p[0], z-p[2]
			if xd*xd+zd*zd > rad*rad {
				continue
			}
			placedAbove := false
			for y := top; y > bottom; y-- {
				at := [3]int32{x, y, z}
				if !cfg.target(r, at) {
					placedAbove = false
					continue
				}
				state, ok := cfg.state(r, at)
				if !ok {
					continue
				}
				r.set(x, y, z, state)
				placedAny = true
				if !placedAbove {
					r.markAbove(x, y, z)
				}
				placedAbove = true
			}
		}
	}
	return placedAny
}

// placeUnderwaterMagma is UnderwaterMagmaFeature: magma on the blocks around
// the floor of a short water column that are enclosed on the sides and below.
// vanilla: UnderwaterMagmaFeature.place
func placeUnderwaterMagma(magma uint16) func(*decorRegion, [3]int32) {
	const searchRange, radius = 5, 1
	return func(r *decorRegion, p [3]int32) {
		isWater := func(x, y, z int32) bool { return registry.BlockOf(r.get(x, y, z)) == registry.BlockWater }
		if !isWater(p[0], p[1], p[2]) {
			return
		}
		// vanilla: Column.scanDirection
		y := p[1]
		for i := 1; i < searchRange && isWater(p[0], y, p[2]); i++ {
			y--
		}
		if isWater(p[0], y, p[2]) {
			return
		}
		for z := p[2] - radius; z <= p[2]+radius; z++ {
			for yy := y - radius; yy <= y+radius; yy++ {
				for x := p[0] - radius; x <= p[0]+radius; x++ {
					if r.rng.NextFloat() < 0.5 && r.magmaFits(x, yy, z) {
						r.set(x, yy, z, magma)
					}
				}
			}
		}
	}
}

// magmaFits reports a block that is neither water nor air whose floor and
// four sides are full faces.
// vanilla: UnderwaterMagmaFeature.isValidPlacement
func (r *decorRegion) magmaFits(x, y, z int32) bool {
	s := r.get(x, y, z)
	if registry.BlockOf(s) == registry.BlockWater || r.src.isAir(s) {
		return false
	}
	if !registry.FaceOcclusionFull(r.get(x, y-1, z), registry.LightUp) {
		return false
	}
	return registry.FaceOcclusionFull(r.get(x+1, y, z), registry.LightWest) &&
		registry.FaceOcclusionFull(r.get(x-1, y, z), registry.LightEast) &&
		registry.FaceOcclusionFull(r.get(x, y, z+1), registry.LightNorth) &&
		registry.FaceOcclusionFull(r.get(x, y, z-1), registry.LightSouth)
}
