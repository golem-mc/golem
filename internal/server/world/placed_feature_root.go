package world

import "github.com/golem-mc/golem/registry"

// rootSystem is RootSystemFeature.
func rootSystem(cfg jobj) featureFn {
	jonly(cfg, "feature", "allowed_tree_position", "allowed_vertical_water_for_tree", "required_vertical_space_for_tree",
		"level_test_distance", "max_level_deviation", "root_column_max_height", "root_radius", "root_placement_attempts",
		"root_replaceable", "root_state_provider", "hanging_root_radius", "hanging_roots_vertical_span",
		"hanging_root_placement_attempts", "hanging_root_state_provider")
	tree := placedRef(cfg["feature"])
	allowedPos := compilePred(cfg["allowed_tree_position"])
	allowedWater := cfg.integer("allowed_vertical_water_for_tree")
	space := cfg.integer("required_vertical_space_for_tree")
	testDist, deviation := cfg.integerOr("level_test_distance", 0), cfg.integerOr("max_level_deviation", 0)
	columnMax := cfg.integer("root_column_max_height")
	rootRadius, rootAttempts := cfg.integer("root_radius"), cfg.integer("root_placement_attempts")
	replaceable := blockSetOf(cfg["root_replaceable"])
	rootState := compileState(cfg["root_state_provider"])
	hangRadius, hangSpan := cfg.integer("hanging_root_radius"), cfg.integer("hanging_roots_vertical_span")
	hangAttempts := cfg.integer("hanging_root_placement_attempts")
	hangState := compileState(cfg["hanging_root_state_provider"])

	allowedSpace := func(r *decorRegion, s uint16, aboveOrigin int32) bool {
		if r.src.isAir(s) {
			return true
		}
		return aboveOrigin+1 <= allowedWater && fluidHolding(s).kind == fluidWater
	}
	// vanilla: RootSystemFeature.spaceForTree
	spaceForTree := func(r *decorRegion, p [3]int32) bool {
		for i := int32(1); i <= space; i++ {
			if !allowedSpace(r, r.get(p[0], p[1]+i, p[2]), i) {
				return false
			}
		}
		if testDist > 0 {
			// Direction.from2DDataValue order: south, west, north, east.
			for _, d := range [4][2]int32{{0, 1}, {-1, 0}, {0, -1}, {1, 0}} {
				c := [2]int32{p[0] + d[0]*testDist, p[2] + d[1]*testDist}
				below, above := r.get(c[0], p[1]-deviation, c[1]), r.get(c[0], p[1]+deviation, c[1])
				if r.src.isAir(below) || !r.src.isAir(above) {
					return false
				}
			}
		}
		return true
	}
	return func(r *decorRegion, origin [3]int32) bool {
		if !r.isAir(origin[0], origin[1], origin[2]) {
			return false
		}
		if placed := func() bool {
			w := origin
			for y := range columnMax {
				w[1]++
				if r.heightOf(hmWorldSurface, w[0], w[2]) < w[1] {
					return false
				}
				if !allowedPos(r, w) || !spaceForTree(r, w) {
					continue
				}
				below := r.get(w[0], w[1]-1, w[2])
				if fluidHolding(below).kind == fluidLava || !registry.IsSolid(below) {
					return false
				}
				if !tree(r, w) {
					continue
				}
				// vanilla: RootSystemFeature.placeDirt
				for yy := origin[1]; yy < origin[1]+y; yy++ {
					for range rootAttempts {
						x := origin[0] + r.rng.NextIntN(rootRadius) - r.rng.NextIntN(rootRadius)
						z := origin[2] + r.rng.NextIntN(rootRadius) - r.rng.NextIntN(rootRadius)
						if replaceable(registry.BlockOf(r.get(x, yy, z))) {
							r.set(x, yy, z, rootState(r, [3]int32{x, yy, z}))
						}
					}
				}
				return true
			}
			return false
		}(); placed {
			// vanilla: RootSystemFeature.placeRoots
			for range hangAttempts {
				x := origin[0] + r.rng.NextIntN(hangRadius) - r.rng.NextIntN(hangRadius)
				y := origin[1] + r.rng.NextIntN(hangSpan) - r.rng.NextIntN(hangSpan)
				z := origin[2] + r.rng.NextIntN(hangRadius) - r.rng.NextIntN(hangRadius)
				if !r.isAir(x, y, z) {
					continue
				}
				st := hangState(r, [3]int32{x, y, z})
				if placementSurvives(r, st, x, y, z) && registry.FaceSturdy(r.get(x, y+1, z), dirDown, registry.SupportFull) {
					r.set(x, y, z, st)
				}
			}
		}
		return true
	}
}
