package world

import "github.com/golem-mc/golem/registry"

// The Nether forests' small cover and the vines: roots, fungi and sprouts
// scattered over nylium, the crimson forest's weeping vines and the warped
// forest's twisting vines.

// netherPlantSupport reports whether a plant of the forests (roots, fungus,
// sprouts) can stand on a state. vanilla: BlockTags.SUPPORTS_CRIMSON_ROOTS,
// SUPPORTS_WARPED_ROOTS, SUPPORTS_CRIMSON_FUNGUS, SUPPORTS_NETHER_SPROUTS
func (g *NetherGenerator) netherPlantSupport(state uint16) bool {
	return state == g.crimsonNyliumState || state == g.warpedNyliumState || state == g.soulSoilState
}

// growVine writes body cells from (x, y, z) in direction dir until total
// cells are passed or the next one is taken, then a head with a state drawn
// from heads.
// vanilla: WeepingVinesFeature.placeWeepingVinesColumn, TwistingVinesFeature.placeWeepingVinesColumn
func growVine(r *decorRegion, x, y, z, dir, first, total int32, heads []uint16, plant uint16) {
	for h := first; h <= total; h++ {
		if r.isAir(x, y, z) {
			if h == total || !r.isAir(x, y+dir, z) {
				r.set(x, y, z, heads[r.rng.NextIntN(int32(len(heads)))])
				return
			}
			r.set(x, y, z, plant)
		}
		y += dir
	}
}

// fungusReplaceable are the plants a huge fungus's stem grows through besides
// the replaceable tag, those of its replaceable_blocks list that the Nether
// holds. vanilla: configured_feature/crimson_fungus replaceable_blocks
var fungusReplaceable = blocksNamed("minecraft:brown_mushroom", "minecraft:red_mushroom", "minecraft:nether_wart",
	"minecraft:warped_fungus", "minecraft:crimson_fungus", "minecraft:weeping_vines", "minecraft:weeping_vines_plant",
	"minecraft:twisting_vines", "minecraft:twisting_vines_plant")

// hugeFungus is the configuration of a huge_fungus feature.
type hugeFungus struct {
	base, stem, hat, decor uint16
	// vines is set for the nether wart hat, the only one that hangs vines;
	// vineHeads are the weeping vine tips it hangs, vinePlant their body.
	vines     bool
	vineHeads []uint16
	vinePlant uint16
	// planted is a fungus grown by bone meal: never huge, no height limit, and
	// it clears what it replaces.
	planted bool
	// top is the generation depth: a fungus whose stem would reach it is not
	// placed, unless planted.
	top int32
	// plants tests the cells the stem may replace besides replaceable blocks.
	plants blockPred
}

func (cfg *hugeFungus) canReplace(r *decorRegion, x, y, z int32, plants bool) bool {
	state := r.get(x, y, z)
	if registry.BlockOf(state).IsReplaceable() {
		return true
	}
	return plants && cfg.plants(r, [3]int32{x, y, z})
}

// fungusConfig is the world-generated (not planted) huge fungus of a forest.
func (g *NetherGenerator) fungusConfig(base, stem, hat uint16, vines bool) *hugeFungus {
	return &hugeFungus{
		base: base, stem: stem, hat: hat, decor: g.shroomlightState, vines: vines,
		top: netherTop, vineHeads: g.weepingVineHeads[6:], vinePlant: g.weepingVinesPlantState,
		plants: func(r *decorRegion, p [3]int32) bool {
			return fungusReplaceable[registry.BlockOf(r.get(p[0], p[1], p[2]))]
		},
	}
}

// parseHugeFungus reads a HugeFungusConfiguration.
// vanilla: HugeFungusFeature.CODEC
func parseHugeFungus(c jobj) func(*decorRegion, treePos) bool {
	jonly(c, "valid_base_block", "stem_state", "hat_state", "decor_state", "replaceable_blocks", "planted")
	cfg := &hugeFungus{
		base:      compileBlockState(c["valid_base_block"]),
		stem:      compileBlockState(c["stem_state"]),
		hat:       compileBlockState(c["hat_state"]),
		decor:     compileBlockState(c["decor_state"]),
		planted:   c.flag("planted"),
		top:       worldMinY + worldHeight,
		plants:    compilePred(c["replaceable_blocks"]),
		vinePlant: registry.BlockWeepingVinesPlant.DefaultState(),
	}
	cfg.vines = registry.BlockOf(cfg.hat) == registry.BlockNetherWartBlock
	for age := 23; age <= 25; age++ { // vanilla: HugeFungusFeature.tryPlaceWeepingVines
		head, _ := registry.WithInt(registry.BlockWeepingVines.DefaultState(), registry.PropAge, age)
		cfg.vineHeads = append(cfg.vineHeads, head)
	}
	return func(r *decorRegion, p treePos) bool { return placeHugeFungus(r, p, cfg) }
}

// placeHugeFungus is HugeFungusFeature: a stem, huge one time in 17, and a hat
// of wart blocks, shroomlights and vines. A planted fungus differs from
// vanilla in not dropping the items of the blocks it replaces.
// vanilla: HugeFungusFeature.place
func placeHugeFungus(r *decorRegion, p [3]int32, cfg *hugeFungus) bool {
	if r.get(p[0], p[1]-1, p[2]) != cfg.base {
		return false
	}
	total := 4 + r.rng.NextIntN(10)
	if r.rng.NextIntN(12) == 0 {
		total *= 2
	}
	if !cfg.planted && p[1]+total+1 >= cfg.top {
		return false
	}
	huge := !cfg.planted && r.rng.NextFloat() < 0.06
	r.set(p[0], p[1], p[2], airState)
	radius := int32(0)
	if huge {
		radius = 1
	}
	for dx := -radius; dx <= radius; dx++ {
		for dz := -radius; dz <= radius; dz++ {
			corner := huge && absInt32(dx) == radius && absInt32(dz) == radius
			for dy := range total {
				x, y, z := p[0]+dx, p[1]+dy, p[2]+dz
				if !cfg.canReplace(r, x, y, z, true) {
					continue
				}
				if !cfg.planted && corner && !(r.rng.NextFloat() < 0.1) {
					continue
				}
				r.set(x, y, z, cfg.stem)
			}
		}
	}
	placeFungusHat(r, p, total, huge, cfg)
	return true
}

func placeFungusHat(r *decorRegion, p [3]int32, total int32, huge bool, cfg *hugeFungus) {
	hatHeight := min(r.rng.NextIntN(1+total/3)+5, total)
	start := total - hatHeight
	for dy := start; dy <= total; dy++ {
		radius := int32(1)
		if dy < total-r.rng.NextIntN(3) {
			radius = 2
		}
		if hatHeight > 8 && dy < start+4 {
			radius = 3
		}
		if huge {
			radius++
		}
		for dx := -radius; dx <= radius; dx++ {
			for dz := -radius; dz <= radius; dz++ {
				edgeX, edgeZ := dx == -radius || dx == radius, dz == -radius || dz == radius
				inside := !edgeX && !edgeZ && dy != total
				corner := edgeX && edgeZ
				x, y, z := p[0]+dx, p[1]+dy, p[2]+dz
				if !cfg.canReplace(r, x, y, z, false) {
					continue
				}
				switch {
				case dy < start+3:
					if !inside {
						placeHatDrop(r, x, y, z, cfg)
					}
				case inside:
					placeHatBlock(r, x, y, z, cfg, 0.1, 0.2, 0.1)
				case corner:
					placeHatBlock(r, x, y, z, cfg, 0.01, 0.7, 0.083)
				default:
					placeHatBlock(r, x, y, z, cfg, 5.0e-4, 0.98, 0.07)
				}
			}
		}
	}
}

// placeHatBlock draws a shroomlight, else a hat block that may hang vines;
// the vine chance is 0 for a hat without vines, and is still drawn.
// vanilla: HugeFungusFeature.placeHatBlock
func placeHatBlock(r *decorRegion, x, y, z int32, cfg *hugeFungus, decor, hat, vines float32) {
	if r.rng.NextFloat() < decor {
		r.set(x, y, z, cfg.decor)
	} else if r.rng.NextFloat() < hat {
		r.set(x, y, z, cfg.hat)
		if !cfg.vines {
			vines = 0
		}
		if r.rng.NextFloat() < vines {
			hangFungusVines(r, x, y, z, cfg)
		}
	}
}

// placeHatDrop is the hat's underside: a block over a hat block continues
// it, else 15% of the cells start one.
// vanilla: HugeFungusFeature.placeHatDropBlock
func placeHatDrop(r *decorRegion, x, y, z int32, cfg *hugeFungus) {
	if r.get(x, y-1, z) == cfg.hat {
		r.set(x, y, z, cfg.hat)
	} else if float64(r.rng.NextFloat()) < 0.15 {
		r.set(x, y, z, cfg.hat)
		if cfg.vines && r.rng.NextIntN(11) == 0 {
			hangFungusVines(r, x, y, z, cfg)
		}
	}
}

// hangFungusVines hangs a weeping vine of age 23..25 under a hat block.
// vanilla: HugeFungusFeature.tryPlaceWeepingVines
func hangFungusVines(r *decorRegion, x, y, z int32, cfg *hugeFungus) {
	if !r.isAir(x, y-1, z) {
		return
	}
	total := 1 + r.rng.NextIntN(5)
	if r.rng.NextIntN(7) == 0 {
		total *= 2
	}
	growVine(r, x, y-1, z, -1, 0, total, cfg.vineHeads, cfg.vinePlant)
}
