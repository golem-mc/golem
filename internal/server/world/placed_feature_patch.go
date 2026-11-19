package world

import (
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

var tagBeneathBambooPodzolReplaceable = registry.BlockTagOf("minecraft:beneath_bamboo_podzol_replaceable")

// bambooStates are BambooFeature's four stalk states: trunk, final large,
// top large and top small.
var bambooStates = func() (trunk, finalLarge, topLarge, topSmall uint16) {
	with := func(s uint16, leaves string, stage int) uint16 {
		s, _ = registry.WithProp(s, registry.PropLeaves, leaves)
		s, _ = registry.WithInt(s, registry.PropStage, stage)
		return s
	}
	base, _ := registry.WithInt(registry.BlockBamboo.DefaultState(), registry.PropAge, 1)
	trunk = with(base, "none", 0)
	return trunk, with(trunk, "large", 1), with(trunk, "large", 0), with(trunk, "small", 0)
}

// bamboo is BambooFeature.
func bamboo(probability float64) featureFn {
	trunk, finalLarge, topLarge, topSmall := bambooStates()
	sapling := registry.BlockBamboo.DefaultState()
	return func(r *decorRegion, origin [3]int32) bool {
		if !r.isAir(origin[0], origin[1], origin[2]) {
			return false
		}
		if placementSurvives(r, sapling, origin[0], origin[1], origin[2]) {
			height := r.rng.NextIntN(12) + 5
			if float64(r.rng.NextFloat()) < probability {
				rad := r.rng.NextIntN(4) + 1
				for x := origin[0] - rad; x <= origin[0]+rad; x++ {
					for z := origin[2] - rad; z <= origin[2]+rad; z++ {
						xd, zd := x-origin[0], z-origin[2]
						if xd*xd+zd*zd > rad*rad {
							continue
						}
						y := r.heightOf(hmWorldSurface, x, z) - 1
						if registry.BlockOf(r.get(x, y, z)).InTag(tagBeneathBambooPodzolReplaceable) {
							r.set(x, y, z, registry.BlockPodzol.DefaultState())
						}
					}
				}
			}
			y := origin[1]
			for i := int32(0); i < height && r.isAir(origin[0], y, origin[2]); i++ {
				r.set(origin[0], y, origin[2], trunk)
				y++
			}
			if y-origin[1] >= 3 {
				r.set(origin[0], y, origin[2], finalLarge)
				r.set(origin[0], y-1, origin[2], topLarge)
				r.set(origin[0], y-2, origin[2], topSmall)
			}
		}
		return true
	}
}

// vegetationPatch is VegetationPatchFeature, and WaterloggedVegetationPatchFeature
// when waterlogged.
func vegetationPatch(cfg jobj, waterlogged bool) featureFn {
	jonly(cfg, "replaceable", "ground_state", "vegetation_feature", "surface", "depth",
		"extra_bottom_block_chance", "vertical_range", "vegetation_chance", "xz_radius", "extra_edge_column_chance")
	replaceable := blockSetOf(cfg["replaceable"])
	ground := compileState(cfg["ground_state"])
	vegetation := nestedPlaced(jobjOf(cfg["vegetation_feature"]))
	inwards := int32(-1) // the surface's direction: down for a floor
	switch cfg.str("surface") {
	case "floor":
	case "ceiling":
		inwards = 1
	default:
		modelFail("vegetation patch surface %q", cfg.str("surface"))
	}
	depth := compileInt(cfg["depth"])
	extraBottom := float32(cfg.num("extra_bottom_block_chance"))
	vertical := cfg.integer("vertical_range")
	vegChance := float32(cfg.num("vegetation_chance"))
	radius := compileInt(cfg["xz_radius"])
	extraEdge := float32(cfg.num("extra_edge_column_chance"))
	faceTowards := dirUp // the face of the ground that must be sturdy
	if inwards > 0 {
		faceTowards = dirDown
	}

	placeGround := func(r *decorRegion, p [3]int32, depth int32) bool {
		for i := range depth {
			state := ground(r, p)
			below := r.get(p[0], p[1], p[2])
			if registry.BlockOf(state) == registry.BlockOf(below) {
				continue
			}
			if !replaceable(registry.BlockOf(below)) {
				return i != 0
			}
			r.set(p[0], p[1], p[2], state)
			p[1] += inwards
		}
		return true
	}
	// vanilla: VegetationPatchFeature.placeGroundPatch
	groundPatch := func(r *decorRegion, origin [3]int32, xr, zr int32) [][3]int32 {
		var surface [][3]int32
		for dx := -xr; dx <= xr; dx++ {
			xEdge := dx == -xr || dx == xr
			for dz := -zr; dz <= zr; dz++ {
				zEdge := dz == -zr || dz == zr
				corner := xEdge && zEdge
				edge := xEdge || zEdge
				if corner || edge && (extraEdge == 0 || r.rng.NextFloat() > extraEdge) {
					continue
				}
				x, y, z := origin[0]+dx, origin[1], origin[2]+dz
				for off := int32(0); r.isAir(x, y, z) && off < vertical; off++ {
					y += inwards
				}
				for off := int32(0); !r.isAir(x, y, z) && off < vertical; off++ {
					y -= inwards
				}
				below := y + inwards
				if !r.isAir(x, y, z) || !registry.FaceSturdy(r.get(x, below, z), faceTowards, registry.SupportFull) {
					continue
				}
				d := depth(r.rng)
				if extraBottom > 0 && r.rng.NextFloat() < extraBottom {
					d++
				}
				if placeGround(r, [3]int32{x, below, z}, d) {
					surface = append(surface, [3]int32{x, below, z})
				}
			}
		}
		return surface
	}
	// vanilla: WaterloggedVegetationPatchFeature.placeGroundPatch
	waterPatch := func(r *decorRegion, surface [][3]int32) [][3]int32 {
		var water [][3]int32
		for _, p := range surface {
			exposed := false
			for _, d := range [...]int{dirNorth, dirEast, dirSouth, dirWest, dirDown} {
				o := [3]int32{p[0] + dirDelta[d][0], p[1] + dirDelta[d][1], p[2] + dirDelta[d][2]}
				if !registry.FaceSturdy(r.get(o[0], o[1], o[2]), blocklogic.Opposite(d), registry.SupportFull) {
					exposed = true
					break
				}
			}
			if !exposed {
				water = append(water, p)
			}
		}
		water = blockPosSetOrder(r, water)
		for _, p := range water {
			r.set(p[0], p[1], p[2], registry.BlockWater.DefaultState())
		}
		return water
	}
	return func(r *decorRegion, origin [3]int32) bool {
		xr, zr := radius(r.rng)+1, radius(r.rng)+1
		surface := blockPosSetOrder(r, groundPatch(r, origin, xr, zr))
		if waterlogged {
			surface = waterPatch(r, surface)
		}
		for _, p := range surface {
			if vegChance > 0 && r.rng.NextFloat() < vegChance {
				if !waterlogged {
					vegetation(r, [3]int32{p[0], p[1] - inwards, p[2]})
				} else if vegetation(r, [3]int32{p[0], p[1] - 1 - inwards, p[2]}) {
					// vanilla: WaterloggedVegetationPatchFeature.placeVegetation
					s := r.get(p[0], p[1], p[2])
					if registry.StateHas(s, registry.PropWaterlogged) && !registry.StateBool(s, registry.PropWaterlogged) {
						s, _ = registry.WithBool(s, registry.PropWaterlogged, true)
						r.set(p[0], p[1], p[2], s)
					}
				}
			}
		}
		return len(surface) > 0
	}
}
