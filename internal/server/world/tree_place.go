package world

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// posSet is a java.util.HashSet<BlockPos>: it keeps what insertion order and
// the final table size give back on iteration, which the tree decorators
// depend on through their stable sort by y.
type posSet struct {
	order []treePos
	seen  map[treePos]struct{}
}

func (s *posSet) add(p treePos) {
	if s.seen == nil {
		s.seen = map[treePos]struct{}{}
	}
	if _, ok := s.seen[p]; ok {
		return
	}
	s.seen[p] = struct{}{}
	s.order = append(s.order, p)
}

func (s *posSet) has(p treePos) bool { _, ok := s.seen[p]; return ok }

func (s *posSet) len() int { return len(s.order) }

// iteration is the elements in the order HashSet iterates them.
// vanilla: Vec3i.hashCode, HashMap.hash
func (s *posSet) iteration() []treePos {
	var h blockPosSet
	for _, p := range s.order {
		h.add(p)
	}
	return h.all()
}

// sortedByY is the decorator context's copy of a set: iteration order, then
// a stable sort by y.
// vanilla: TreeDecorator.Context
func (s *posSet) sortedByY() []treePos {
	out := s.iteration()
	slices.SortStableFunc(out, func(a, b treePos) int { return int(a[1]) - int(b[1]) })
	return out
}

// foliageAttach is a FoliagePlacer.FoliageAttachment.
type foliageAttach struct {
	pos          treePos
	radiusOffset int32
	heightOffset int32
	doubleTrunk  bool
}

// treeBuild is one tree being placed: the sets its blocks go into and the
// region and random it reads.
type treeBuild struct {
	r   *decorRegion
	rng *levelgen.WorldgenRandom
	cfg *treeConfig
	// roots, trunks, foliage and decor are the positions the root placer,
	// trunk placer, foliage placer and decorators set.
	roots, trunks, foliage, decor posSet
}

func (t *treeBuild) put(set *posSet, p treePos, state uint16) {
	set.add(p)
	// Keep a replaced plant's other half until the shape pass: decorators
	// must still see it while drawing their positions.
	t.r.setKnownShape(p[0], p[1], p[2], state)
}

// setOverPlant sets a tree block, first clearing the other half of a two-tall
// plant whose half a feature without a shape pass replaces.
func (r *decorRegion) setOverPlant(p treePos, state uint16) {
	cur := r.get(p[0], p[1], p[2])
	dy := int32(0)
	switch registry.StateValue(cur, registry.PropHalf) {
	case "lower":
		dy = 1
	case "upper":
		dy = -1
	}
	if dy != 0 && registry.BlockOf(r.get(p[0], p[1]+dy, p[2])) == registry.BlockOf(cur) {
		r.set(p[0], p[1]+dy, p[2], registry.BlockAir.DefaultState())
	}
	r.set(p[0], p[1], p[2], state)
}

func (t *treeBuild) state(p treePos) uint16 { return t.r.get(p[0], p[1], p[2]) }

func (t *treeBuild) isAir(p treePos) bool { return t.r.isAir(p[0], p[1], p[2]) }

func (t *treeBuild) is(p treePos, in func(registry.Block) bool) bool {
	return in(registry.BlockOf(t.state(p)))
}

var (
	treeAirOrReplaceable = func(b registry.Block) bool {
		return overworldAirBlocks[b] || b.InTag(registry.TagIDReplaceableByTrees)
	}
	treeLog     = func(b registry.Block) bool { return b.InTag(registry.TagIDLogs) }
	treeAirOrLv = func(b registry.Block) bool { return overworldAirBlocks[b] || b.IsLeaves() }
	treeVine    = func(b registry.Block) bool { return b == registry.BlockVine }
)

// validTreePos is air or a block trees replace.
// vanilla: TreeFeature.validTreePos
func (t *treeBuild) validTreePos(p treePos) bool { return t.is(p, treeAirOrReplaceable) }

// treeConfig is a TreeConfiguration.
type treeConfig struct {
	trunkProvider, foliageProvider stateProvider
	belowTrunk                     optState
	trunk                          trunkPlacer
	foliage                        foliagePlacer
	root                           rootPlacer
	minSize                        featureSize
	decorators                     []treeDecorator
	ignoreVines                    bool
}

// placeTree is TreeFeature.place: false when the tree does not fit or sets
// nothing.
// vanilla: TreeFeature.place, TreeFeature.doPlace
func (r *decorRegion) placeTree(cfg *treeConfig, origin treePos) bool {
	t := &treeBuild{r: r, rng: r.rng, cfg: cfg}
	if !t.doPlace(origin) || t.trunks.len() == 0 && t.foliage.len() == 0 {
		return false
	}
	if len(cfg.decorators) > 0 {
		ctx := &decoratorCtx{t: t, logs: t.trunks.sortedByY(), leaves: t.foliage.sortedByY(), roots: t.roots.sortedByY()}
		for _, d := range cfg.decorators {
			d.place(ctx)
		}
	}
	t.updateLeaves()
	return true
}

func (t *treeBuild) doPlace(origin treePos) bool {
	cfg, rng := t.cfg, t.rng
	treeHeight := cfg.trunk.height(rng)
	foliageHeight := cfg.foliage.foliageHeight(rng, treeHeight)
	trunkHeight := treeHeight - foliageHeight
	leafRadius := cfg.foliage.foliageRadius(rng, trunkHeight)
	trunkOrigin := origin
	if cfg.root != nil {
		trunkOrigin = cfg.root.trunkOrigin(origin, rng)
	}
	minY := min(origin[1], trunkOrigin[1])
	maxY := max(origin[1], trunkOrigin[1]) + treeHeight + 1
	if minY < t.r.minY+1 || maxY > t.r.minY+t.r.height {
		return false
	}
	clipped := t.maxFreeTreeHeight(treeHeight, trunkOrigin)
	if clipped < treeHeight && (cfg.minSize.minClipped < 0 || clipped < cfg.minSize.minClipped) {
		return false
	}
	if cfg.root != nil && !cfg.root.placeRoots(t, origin, trunkOrigin) {
		return false
	}
	atts := cfg.trunk.placeTrunk(t, clipped, trunkOrigin)
	for _, a := range atts {
		cfg.foliage.createFoliage(t, clipped, a, foliageHeight, leafRadius)
	}
	return true
}

// maxFreeTreeHeight is the height the tree fits in: y-2 at the first layer
// with a blocked cell.
// vanilla: TreeFeature.getMaxFreeTreeHeight
func (t *treeBuild) maxFreeTreeHeight(maxHeight int32, pos treePos) int32 {
	for y := int32(0); y <= maxHeight+1; y++ {
		rad := t.cfg.minSize.sizeAt(maxHeight, y)
		for x := -rad; x <= rad; x++ {
			for z := -rad; z <= rad; z++ {
				p := treePos{pos[0] + x, pos[1] + y, pos[2] + z}
				if t.cfg.trunk.isFree(t, p) && (t.cfg.ignoreVines || !t.is(p, treeVine)) {
					continue
				}
				return y - 2
			}
		}
	}
	return maxHeight
}

// updateLeaves sets the distance of the leaves within reach of the tree's
// logs, a pass over the bounding box of everything placed. The per-distance
// sets drain in HashSet order, which decides which of two visits to a leaf
// writes last. The shape it fills then goes through updateShapeAtEdge.
// vanilla: TreeFeature.updateLeaves
func (t *treeBuild) updateLeaves() {
	const maxDistance = 7
	lo, hi := treePos{math.MaxInt32, math.MaxInt32, math.MaxInt32}, treePos{math.MinInt32, math.MinInt32, math.MinInt32}
	any := false
	for _, set := range []*posSet{&t.roots, &t.trunks, &t.foliage, &t.decor} {
		for _, p := range set.order {
			any = true
			for i := range 3 {
				lo[i], hi[i] = min(lo[i], p[i]), max(hi[i], p[i])
			}
		}
	}
	if !any {
		return
	}
	inside := func(p treePos) bool {
		return p[0] >= lo[0] && p[0] <= hi[0] && p[1] >= lo[1] && p[1] <= hi[1] && p[2] >= lo[2] && p[2] <= hi[2]
	}
	full := newTreeShape(lo, hi)
	for _, set := range []*posSet{&t.decor, &t.roots} {
		for _, p := range set.order {
			full.fill(p)
		}
	}
	toCheck := &t.r.leafLevels
	for i := range toCheck {
		toCheck[i].reset()
	}
	for _, p := range t.trunks.iteration() {
		toCheck[0].add(p)
	}
	dirs := [6]treePos{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}
	smallest := 0
	for smallest < maxDistance {
		p, ok := toCheck[smallest].take()
		if !ok {
			smallest++
			continue
		}
		if !inside(p) {
			continue
		}
		if smallest != 0 {
			st := t.state(p)
			if next, ok := registry.WithInt(st, registry.PropDistance, smallest); ok {
				st = next
			}
			t.r.set(p[0], p[1], p[2], st)
		}
		full.fill(p)
		for _, d := range dirs {
			n := at(p, d)
			if !inside(n) || full.has(n) {
				continue
			}
			dist, ok := leafDistanceAt(t.state(n))
			if !ok {
				continue
			}
			if nd := min(dist, smallest+1); nd < maxDistance {
				toCheck[nd].add(n)
				smallest = min(smallest, nd)
			}
		}
	}
	t.updateShapeAtEdge(full)
}

// leafDistanceAt is the distance a block gives its neighbours: 0 for logs, the
// distance property of leaves, none for anything else.
// vanilla: LeavesBlock.getOptionalDistanceAt
func leafDistanceAt(state uint16) (int, bool) {
	if registry.BlockOf(state).IsPreventsNearbyLeafDecay() {
		return 0, true
	}
	return registry.StateInt(state, registry.PropDistance)
}

// featureSize is a TwoLayersFeatureSize or ThreeLayersFeatureSize.
type featureSize struct {
	// minClipped is min_clipped_height, -1 when absent.
	minClipped                              int32
	three                                   bool
	limit, upperLimit, lower, middle, upper int32
}

// sizeAt is the half-width of the free box at a height of the tree.
// vanilla: TwoLayersFeatureSize.getSizeAtHeight, ThreeLayersFeatureSize.getSizeAtHeight
func (f featureSize) sizeAt(height, y int32) int32 {
	if !f.three {
		if y < f.limit {
			return f.lower
		}
		return f.upper
	}
	if y < f.limit {
		return f.lower
	}
	if y >= height-f.upperLimit {
		return f.upper
	}
	return f.middle
}

func parseFeatureSize(o jobj) featureSize {
	f := featureSize{minClipped: -1}
	if o.has("min_clipped_height") {
		f.minClipped = o.integer("min_clipped_height")
	}
	get := func(k string, def int32) int32 {
		if o.has(k) {
			return o.integer(k)
		}
		return def
	}
	switch o.str("type") {
	case "minecraft:two_layers_feature_size":
		f.limit, f.lower, f.upper = get("limit", 1), get("lower_size", 0), get("upper_size", 1)
	case "minecraft:three_layers_feature_size":
		f.three = true
		f.limit, f.upperLimit = get("limit", 1), get("upper_limit", 1)
		f.lower, f.middle, f.upper = get("lower_size", 0), get("middle_size", 1), get("upper_size", 1)
	default:
		panic("world: tree data: feature size " + o.str("type"))
	}
	return f
}
