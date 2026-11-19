package world

import (
	"strings"

	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// featureFn places a configured feature at a position and reports whether it
// placed anything.
type featureFn func(r *decorRegion, p [3]int32) bool

// compileFeature reads a configured feature.
func compileFeature(o jobj) featureFn {
	cfg, ok := asJobj(o["config"])
	if !ok {
		cfg = o
	}
	switch jtype(o) {
	case "simple_block":
		jonly(cfg, "to_place", "schedule_tick")
		tick, _ := cfg["schedule_tick"].(bool)
		return simpleBlock(compileState(cfg["to_place"]), tick)
	case "block_column":
		return blockColumn(cfg)
	case "block_pile":
		jonly(cfg, "state_provider")
		return blockPile(compileState(cfg["state_provider"]))
	case "sea_pickle":
		jonly(cfg, "count")
		return seaPickle(compileInt(cfg["count"]))
	case "vines":
		jonly(cfg)
		return vines
	case "multiface_growth":
		return multifaceGrowth(cfg)
	case "bamboo":
		jonly(cfg, "probability")
		return bamboo(jfloat(cfg["probability"]))
	case "vegetation_patch", "waterlogged_vegetation_patch":
		return vegetationPatch(cfg, jtype(o) == "waterlogged_vegetation_patch")
	case "coral_tree":
		jonly(cfg, "feature")
		return coralTree(placedRef(cfg["feature"]))
	case "coral_claw":
		jonly(cfg, "feature")
		return coralClaw(placedRef(cfg["feature"]))
	case "large_dripstone":
		return largeDripstoneFeature(cfg)
	case "speleothem":
		return speleothemFeature(cfg)
	case "speleothem_cluster":
		return speleothemClusterFeature(cfg)
	case "sculk_patch":
		jonly(cfg, "charge_count", "amount_per_charge", "spread_attempts", "growth_rounds", "spread_rounds")
		c := sculkPatchConfig{
			chargeCount: int(cfg.integer("charge_count")), amountPerCharge: int(cfg.integer("amount_per_charge")),
			spreadAttempts: int(cfg.integer("spread_attempts")), growthRounds: int(cfg.integer("growth_rounds")),
			spreadRounds: int(cfg.integer("spread_rounds")),
		}
		return func(r *decorRegion, p [3]int32) bool { return c.place(r, p[0], p[1], p[2]) }
	case "fossil":
		return fossilFeature(cfg)
	case "template":
		return templateFeature(cfg)
	case "lake":
		jonly(cfg, "fluid", "barrier", "can_place_feature", "can_replace_with_air_or_fluid", "can_replace_with_barrier")
		c := lakeConfig{
			fluid: compileState(cfg["fluid"]), barrier: compileState(cfg["barrier"]),
			canPlace:       compilePred(cfg["can_place_feature"]),
			replaceFluid:   compilePred(cfg["can_replace_with_air_or_fluid"]),
			replaceBarrier: compilePred(cfg["can_replace_with_barrier"]),
		}
		return func(r *decorRegion, p [3]int32) bool { return r.placeLake(c, p) }
	case "ore":
		return oreFeature(cfg)
	case "iceberg":
		jonly(cfg, "state")
		return icebergFeature(compileBlockState(cfg["state"]))
	case "spike":
		return spikeFeature(cfg)
	case "disk":
		jonly(cfg, "half_height", "radius", "target", "state_provider")
		c := diskConfig{
			halfHeight: cfg.integer("half_height"), radius: compileInt(cfg["radius"]),
			target: compilePred(cfg["target"]), state: compileOptState(cfg["state_provider"]),
		}
		return func(r *decorRegion, p [3]int32) bool { return r.placeDisk(p, &c) }
	case "desert_well":
		jonly(cfg)
		return (*decorRegion).placeDesertWell
	case "root_system":
		return rootSystem(cfg)
	case "tree":
		tc := parseTreeConfig(cfg)
		return func(r *decorRegion, p [3]int32) bool { return r.placeTree(tc, p) }
	case "fallen_tree":
		return parseFallenTree(cfg)
	case "huge_red_mushroom", "huge_brown_mushroom":
		return parseHugeMushroom(jtype(o) == "huge_red_mushroom", cfg)
	case "huge_fungus":
		return parseHugeFungus(cfg)
	case "single_block_pillar":
		return singleBlockPillar(cfg)
	case "projected_random_patchy_square":
		return projectedRandomPatchySquare(cfg)
	case "random_neighbor_spread":
		return randomNeighborSpread(cfg)
	case "overlay":
		jonly(cfg, "features")
		var features []featureFn
		list, ok := cfg["features"].([]any)
		if !ok { // vanilla: PlacedFeature.LIST_CODEC permits a single holder too.
			list = []any{cfg["features"]}
		}
		for _, v := range list {
			features = append(features, placedRef(v))
		}
		return func(r *decorRegion, p [3]int32) bool {
			placed := false
			for _, f := range features {
				if f(r, p) {
					placed = true
				}
			}
			return placed
		}
	case "sequence":
		jonly(cfg, "features")
		var features []featureFn
		for _, x := range cfg.arr("features") {
			features = append(features, placedRef(x))
		}
		// vanilla: SequenceFeature.place
		return func(r *decorRegion, p [3]int32) bool {
			for _, f := range features {
				if !f(r, p) {
					return false
				}
			}
			return true
		}
	case "weighted_random_selector":
		jonly(cfg, "features")
		var features []featureFn
		var weights []int32
		var total int32
		for _, x := range cfg.arr("features") {
			e := jobjOf(x)
			jonly(e, "data", "weight")
			features = append(features, placedRef(e["data"]))
			weights = append(weights, e.integer("weight"))
			total += e.integer("weight")
		}
		// vanilla: WeightedRandomSelectorFeature.place
		return func(r *decorRegion, p [3]int32) bool {
			return total > 0 && features[pickWeighted(r.rng, weights)](r, p)
		}
	case "random_selector":
		jonly(cfg, "features", "default")
		type entry struct {
			chance  float32
			feature featureFn
		}
		var entries []entry
		for _, e := range cfg.arr("features") {
			e := jobjOf(e)
			entries = append(entries, entry{float32(e.num("chance")), placedRef(e["feature"])})
		}
		def := placedRef(cfg["default"])
		// vanilla: RandomSelectorFeature.place
		return func(r *decorRegion, p [3]int32) bool {
			for _, e := range entries {
				if r.rng.NextFloat() < e.chance {
					return e.feature(r, p)
				}
			}
			return def(r, p)
		}
	case "simple_random_selector":
		jonly(cfg, "features")
		var list []featureFn
		for _, x := range cfg["features"].([]any) {
			list = append(list, nestedPlaced(jobjOf(x)))
		}
		return func(r *decorRegion, p [3]int32) bool { return list[r.rng.NextIntN(int32(len(list)))](r, p) }
	case "random_boolean_selector":
		jonly(cfg, "feature_true", "feature_false")
		yes, no := placedRef(cfg["feature_true"]), placedRef(cfg["feature_false"])
		// vanilla: RandomBooleanSelectorFeature.place
		return func(r *decorRegion, p [3]int32) bool {
			if r.rng.NextBoolean() {
				return yes(r, p)
			}
			return no(r, p)
		}
	case "no_op":
		jonly(cfg)
		return func(*decorRegion, [3]int32) bool { return true }
	default:
		modelFail("unhandled feature %q", jtype(o))
		return nil
	}
}

// stateSurvives is BlockState.canSurvive: a two-tall plant's half asks its own
// rule, the rest the shared table.
func stateSurvives(r BlockReader, s uint16, p [3]int32) bool {
	if registry.BlockOf(s).IsDoublePlant() {
		return doublePlantSurvives(r, s, p[0], p[1], p[2])
	}
	return placementSurvives(r, s, p[0], p[1], p[2])
}

// isWater is state.is(Blocks.WATER): the block, whatever its level.
func isWater(s uint16) bool { return registry.BlockOf(s) == registry.BlockWater }

// simpleBlock is SimpleBlockFeature.
func simpleBlock(provider stateProvider, scheduleTick bool) featureFn {
	return func(r *decorRegion, p [3]int32) bool {
		s := provider(r, p)
		if !stateSurvives(r, s, p) {
			return false
		}
		if registry.BlockOf(s).IsDoublePlant() {
			// The upper half may also take a replaceable cell of the plant's own fluid.
			if above := r.get(p[0], p[1]+1, p[2]); !r.isAir(p[0], p[1]+1, p[2]) &&
				(fluidHolding(s) != fluidHolding(above) || !registry.BlockOf(above).IsReplaceable()) {
				return false
			}
			// vanilla: DoublePlantBlock.placeAt
			lower, _ := registry.WithProp(s, registry.PropHalf, "lower")
			upper, _ := registry.WithProp(s, registry.PropHalf, "upper")
			r.set(p[0], p[1], p[2], lower)
			r.set(p[0], p[1]+1, p[2], upper)
			return true
		}
		if registry.BlockOf(s) == registry.BlockPaleMossCarpet {
			// vanilla: MossyCarpetBlock.placeAt
			carpet := MossyCarpetState(r, s, p[0], p[1], p[2], true)
			r.set(p[0], p[1], p[2], carpet)
			if topper, ok := MossyCarpetTopper(r, p[0], p[1], p[2], r.regionRandom().NextBoolean); ok {
				r.set(p[0], p[1]+1, p[2], topper)
				r.set(p[0], p[1], p[2], MossyCarpetState(r, carpet, p[0], p[1], p[2], true))
			}
		} else {
			r.set(p[0], p[1], p[2], s)
		}
		if scheduleTick {
			r.scheduleTick(p[0], p[1], p[2])
		}
		return true
	}
}

// blockColumn is BlockColumnFeature.
func blockColumn(cfg jobj) featureFn {
	jonly(cfg, "layers", "direction", "allowed_placement", "prioritize_tip")
	type layer struct {
		height intProvider
		state  stateProvider
	}
	var layers []layer
	for _, x := range cfg["layers"].([]any) {
		l := jobjOf(x)
		layers = append(layers, layer{compileInt(l["height"]), compileState(l["provider"])})
	}
	dy := dirDelta[dirIndex(cfg["direction"].(string))][1]
	allowed := compilePred(cfg["allowed_placement"])
	tip, _ := cfg["prioritize_tip"].(bool)
	return func(r *decorRegion, p [3]int32) bool {
		heights := make([]int32, len(layers))
		var total int32
		for i, l := range layers {
			heights[i] = l.height(r.rng)
			total += heights[i]
		}
		if total == 0 {
			return false
		}
		next := p
		for y := range total {
			next[1] += dy
			if !allowed(r, next) {
				truncateColumn(heights, total, y, tip)
				break
			}
		}
		at := p
		for i, l := range layers {
			for range heights[i] {
				r.set(at[0], at[1], at[2], l.state(r, at))
				at[1] += dy
			}
		}
		return true
	}
}

// truncateColumn removes the blocks past newHeight from the layers, from the
// tip's end when it has priority.
// vanilla: BlockColumnFeature.truncate
func truncateColumn(heights []int32, total, newHeight int32, tip bool) {
	remove := total - newHeight
	step, start, end := int32(1), 0, len(heights)
	if !tip {
		step, start, end = -1, len(heights)-1, -1
	}
	for i := int32(start); i != int32(end) && remove > 0; i += step {
		n := min(heights[i], remove)
		heights[i] -= n
		remove -= n
	}
}

// reefSpread is the offset of a sea pickle from its origin.
func reefSpread(r *decorRegion) (int32, int32) {
	x := diff(r.rng, 8)
	return x, diff(r.rng, 8)
}

// diff is nextInt(n) - nextInt(n), the first draw first.
func diff(rng *levelgen.WorldgenRandom, n int32) int32 {
	a := rng.NextIntN(n)
	return a - rng.NextIntN(n)
}

var propPickles, _ = registry.PropNamed("pickles")

// seaPickle is SeaPickleFeature.
func seaPickle(count intProvider) featureFn {
	return func(r *decorRegion, p [3]int32) bool {
		placed := false
		for range count(r.rng) {
			dx, dz := reefSpread(r)
			x, z := p[0]+dx, p[2]+dz
			pos := [3]int32{x, r.heightOf(hmOceanFloor, x, z), z}
			s, _ := registry.WithInt(registry.BlockSeaPickle.DefaultState(), propPickles, int(r.rng.NextIntN(4))+1)
			if isWater(r.get(pos[0], pos[1], pos[2])) && stateSurvives(r, s, pos) {
				r.set(pos[0], pos[1], pos[2], s)
				placed = true
			}
		}
		return placed
	}
}

// vineFaceOrder is the order VinesFeature tries the faces, skipping down.
// vanilla: Direction.values
var vineFaceOrder = [5]int{dirUp, dirNorth, dirSouth, dirWest, dirEast}

// vines is VinesFeature: a vine on the first face with something to hold it.
func vines(r *decorRegion, p [3]int32) bool {
	if !r.isAir(p[0], p[1], p[2]) {
		return false
	}
	for _, d := range vineFaceOrder {
		n := dirDelta[d]
		if !registry.FaceAttachable(r.get(p[0]+n[0], p[1]+n[1], p[2]+n[2]), blocklogic.Opposite(d)) {
			continue
		}
		var faces vineFaceSet
		faces[d] = true
		r.set(p[0], p[1], p[2], vineState(faces))
		return true
	}
	return false
}

// multifaceGrowth is MultifaceGrowthFeature: a face of the block on a host
// next to the origin or the cell beside it, which may then spread once.
func multifaceGrowth(cfg jobj) featureFn {
	jonly(cfg, "block", "search_range", "can_place_on_floor", "can_place_on_ceiling",
		"can_place_on_wall", "chance_of_spreading", "can_be_placed_on")
	block, ok := registry.BlockByName(cfg.str("block"))
	if !ok {
		modelFail("unknown block %q", cfg["block"])
	}
	names := jstrings(cfg["can_be_placed_on"])
	for _, n := range names {
		if strings.HasPrefix(n, "#") {
			modelFail("multiface_growth: block tag %q", n)
		}
	}
	m := multiface{block: block, hosts: blocksNamed(names...), chance: float32(0.5)}
	if cfg.has("chance_of_spreading") {
		m.chance = float32(cfg.num("chance_of_spreading"))
	}
	// vanilla: MultifaceGrowthConfiguration, Direction.Plane.HORIZONTAL order
	if cfg.flag("can_place_on_ceiling") {
		m.dirs = append(m.dirs, dirUp)
	}
	if cfg.flag("can_place_on_floor") {
		m.dirs = append(m.dirs, dirDown)
	}
	if cfg.flag("can_place_on_wall") {
		m.dirs = append(m.dirs, dirNorth, dirEast, dirSouth, dirWest)
	}
	// search_range only repeats the same cell: vanilla's loop offsets from the
	// origin each pass, so one try is the whole search. The read only rejects
	// a non-integer value.
	_ = cfg.integerOr("search_range", 10)
	return m.place
}

type multiface struct {
	block  registry.Block
	hosts  *[registry.BlockCount]bool
	chance float32
	dirs   []int // the valid directions, in the configuration's order
}

func airOrWater(r *decorRegion, s uint16) bool { return r.src.isAir(s) || isWater(s) }

// vanilla: MultifaceGrowthFeature.place
func (m multiface) place(r *decorRegion, p [3]int32) bool {
	old := r.get(p[0], p[1], p[2])
	if !airOrWater(r, old) {
		return false
	}
	var searchBuf, facesBuf [dirCount]int
	search := m.shuffled(r, -1, &searchBuf)
	if m.grow(r, p, old, search) {
		return true
	}
	for _, sd := range search {
		faces := m.shuffled(r, blocklogic.Opposite(sd), &facesBuf)
		d := dirDelta[sd]
		q := [3]int32{p[0] + d[0], p[1] + d[1], p[2] + d[2]}
		s := r.get(q[0], q[1], q[2])
		if !airOrWater(r, s) && registry.BlockOf(s) != m.block {
			continue
		}
		if m.grow(r, q, s, faces) {
			return true
		}
	}
	return false
}

// shuffled is the valid directions but except, shuffled into buf.
// vanilla: Util.toShuffledList
func (m multiface) shuffled(r *decorRegion, except int, buf *[dirCount]int) []int {
	out := buf[:0]
	for _, d := range m.dirs {
		if d != except {
			out = append(out, d)
		}
	}
	shuffle(out, r.rng)
	return out
}

// grow places a face toward the first of dirs whose neighbour is a host;
// false when there is none or that face cannot take it.
// vanilla: MultifaceGrowthFeature.placeGrowthIfPossible
func (m multiface) grow(r *decorRegion, p [3]int32, old uint16, dirs []int) bool {
	for _, d := range dirs {
		n := dirDelta[d]
		if !m.hosts[registry.BlockOf(r.get(p[0]+n[0], p[1]+n[1], p[2]+n[2]))] {
			continue
		}
		s, ok := multifaceSpreader{m.block, r.get}.stateFor(old, p, d)
		if !ok {
			return false
		}
		r.set(p[0], p[1], p[2], s)
		r.markPost(p[0], p[1], p[2])
		if fluidHolding(s).kind != fluidNone {
			r.scheduleTick(p[0], p[1], p[2])
		}
		if r.rng.NextFloat() < m.chance {
			m.spread(r, s, p, d)
		}
		return true
	}
	return false
}

// spread grows the face on from to the first shuffled direction that takes
// it.
// vanilla: MultifaceSpreader.spreadFromFaceTowardRandomDirection
func (m multiface) spread(r *decorRegion, s uint16, p [3]int32, from int) {
	order := [dirCount]int{dirDown, dirUp, dirNorth, dirSouth, dirWest, dirEast}
	shuffle(order[:], r.rng)
	sp := multifaceSpreader{m.block, r.get}
	for _, d := range order {
		if pos, ns, ok := sp.target(s, p, from, d); ok {
			r.set(pos[0], pos[1], pos[2], ns)
			r.markPost(pos[0], pos[1], pos[2])
			if fluidHolding(ns).kind != fluidNone {
				r.scheduleTick(pos[0], pos[1], pos[2])
			}
			return
		}
	}
}

// vanilla: RandomNeighborSpreadFeature.place
func randomNeighborSpread(cfg jobj) featureFn {
	jonly(cfg, "block", "accepted_neighbors", "can_replace", "attempts", "xz_offset", "y_offset")
	block := compileState(cfg["block"])
	neighbors := compilePred(jobj{"type": "matching_blocks", "blocks": cfg["accepted_neighbors"]})
	replace := compilePred(cfg["can_replace"])
	attempts, xz, y := compileInt(cfg["attempts"]), compileInt(cfg["xz_offset"]), compileInt(cfg["y_offset"])
	return func(r *decorRegion, p [3]int32) bool {
		r.set(p[0], p[1], p[2], block(r, p))
		for n := attempts(r.rng); n > 0; n-- {
			q := [3]int32{p[0] + xz(r.rng), p[1] + y(r.rng), p[2] + xz(r.rng)}
			if !replace(r, q) {
				continue
			}
			count := 0
			for _, d := range [6][3]int32{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
				if neighbors(r, [3]int32{q[0] + d[0], q[1] + d[1], q[2] + d[2]}) {
					count++
				}
				if count > 1 {
					break
				}
			}
			if count == 1 {
				r.set(q[0], q[1], q[2], block(r, q))
			}
		}
		return true
	}
}

// vanilla: SingleBlockPillarFeature.place
func singleBlockPillar(cfg jobj) featureFn {
	jonly(cfg, "block", "can_replace", "direction", "chance_to_continue", "cap_feature")
	block := compileState(cfg["block"])
	replace := compilePred(jobj{"type": "true"})
	if cfg.has("can_replace") {
		replace = compilePred(cfg["can_replace"])
	}
	dy := int32(1)
	if cfg.str("direction") == "down" {
		dy = -1
	}
	chance := float32(1)
	if cfg.has("chance_to_continue") {
		chance = float32(cfg.num("chance_to_continue"))
	}
	var capFeature featureFn
	if cfg.has("cap_feature") {
		capFeature = placedRef(cfg["cap_feature"])
	}
	return func(r *decorRegion, p [3]int32) bool {
		for replace(r, p) && r.rng.NextFloat() < chance && p[1] >= r.minY && p[1] < r.minY+r.height {
			r.set(p[0], p[1], p[2], block(r, p))
			p[1] += dy
		}
		p[1] -= dy
		if capFeature != nil {
			capFeature(r, p)
		}
		return true
	}
}

// vanilla: ProjectedRandomPatchySquare.place
func projectedRandomPatchySquare(cfg jobj) featureFn {
	jonly(cfg, "block", "project_through", "size", "max_projection_height")
	block := compileOptState(cfg["block"])
	through := compilePred(cfg["project_through"])
	size := compileInt(cfg["size"])
	maxDrop := cfg.integer("max_projection_height")
	return func(r *decorRegion, p [3]int32) bool {
		size := size(r.rng)
		bound := size*size + 1
		for dx := -size; dx <= size; dx++ {
			for dz := -size; dz <= size; dz++ {
				if r.rng.NextIntN(bound) >= bound-absInt32(dx)*absInt32(dz) {
					continue
				}
				q := [3]int32{p[0] + dx, p[1], p[2] + dz}
				drop := maxDrop
				for through(r, [3]int32{q[0], q[1] - 1, q[2]}) {
					q[1]--
					drop--
					if drop <= 0 {
						break
					}
				}
				if s, ok := block(r, q); ok {
					r.set(q[0], q[1], q[2], s)
				}
			}
		}
		return true
	}
}
