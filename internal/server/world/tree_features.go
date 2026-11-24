package world

import (
	"sync"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// placedTree is a PlacedFeature of the tree table: its modifiers and the
// feature they feed. place reports whether any position placed.
type placedTree struct {
	mods  []placementMod
	place func(r *decorRegion, p treePos) bool
}

// run is PlacedFeature.placeWithContext.
func (f *placedTree) run(r *decorRegion, p treePos) bool {
	placed := false
	r.walk(f.mods, p, func(r *decorRegion, q [3]int32) {
		if f.place(r, q) {
			placed = true
		}
	})
	return placed
}

var treeTable struct {
	mu         sync.Mutex
	placed     map[string]*placedTree
	configured map[string]func(*decorRegion, treePos) bool
}

// treeCompile returns a cached compiled feature of the tree table, building it
// outside the lock: two workers may build one, the results being identical.
func treeCompile[T any](cache *map[string]T, name string, build func() T) T {
	treeTable.mu.Lock()
	if v, ok := (*cache)[name]; ok {
		treeTable.mu.Unlock()
		return v
	}
	treeTable.mu.Unlock()
	v := build()
	treeTable.mu.Lock()
	if *cache == nil {
		*cache = map[string]T{}
	}
	(*cache)[name] = v
	treeTable.mu.Unlock()
	return v
}

// treeFeature compiles a placed feature of the overworld tree table, with the
// biome filter reading mask (none when 0). ok is false for a feature this port
// does not place.
func treeFeature(name string, mask uint64) (f *placedTree, ok bool) {
	if _, has := levelgen.OverworldTreeFeature(true, name); !has {
		return nil, false
	}
	defer func() {
		if e := recover(); e != nil {
			if _, is := e.(unported); !is {
				panic(e)
			}
			f, ok = nil, false
		}
	}()
	return treePlaced(name, mask), true
}

func treePlaced(name string, mask uint64) *placedTree {
	build := func() *placedTree {
		src, ok := levelgen.OverworldTreeFeature(true, name)
		if !ok {
			modelFail("placed feature %s", name)
		}
		doc := parseJSON(src)
		mods, place := compilePlaced(doc, mask, true)
		return &placedTree{mods: mods, place: place}
	}
	if mask != 0 {
		return build() // root features differ by biome mask: not shared
	}
	return treeCompile(&treeTable.placed, name, build)
}

// canWrite is WorldGenRegion.ensureCanWrite for a feature's origin.
func (r *decorRegion) canWrite(p treePos) bool {
	return absInt32(p[0]>>4-r.nx) <= 1 && absInt32(p[2]>>4-r.nz) <= 1
}

func compileConfigured(name string) func(*decorRegion, treePos) bool {
	return treeCompile(&treeTable.configured, name, func() func(*decorRegion, treePos) bool {
		src, ok := levelgen.OverworldTreeFeature(false, name)
		if !ok {
			modelFail("configured feature %s", name)
		}
		place := compileFeature(parseJSON(src))
		return func(r *decorRegion, p treePos) bool { return r.canWrite(p) && place(r, p) }
	})
}

func parseTreeConfig(c jobj) *treeConfig {
	tc := &treeConfig{
		trunkProvider:   compileState(c["trunk_provider"]),
		foliageProvider: compileState(c["foliage_provider"]),
		trunk:           parseTrunk(c.obj("trunk_placer")),
		foliage:         parseFoliage(c.obj("foliage_placer")),
		minSize:         parseFeatureSize(c.obj("minimum_size")),
		ignoreVines:     c.flag("ignore_vines"),
	}
	if c.has("below_trunk_provider") {
		tc.belowTrunk = compileOptState(c["below_trunk_provider"])
	} else {
		dirt := registry.BlockDirt.DefaultState()
		tc.belowTrunk = func(*decorRegion, treePos) (uint16, bool) { return dirt, true }
	}
	if c.has("root_placer") {
		tc.root = parseRoots(c.obj("root_placer"))
	}
	for _, d := range c.arr("decorators") {
		tc.decorators = append(tc.decorators, parseDecoratorOrSkip(jobjOf(d)))
	}
	return tc
}

func parseDecoratorOrSkip(o jobj) treeDecorator {
	d := parseDecorator(o)
	if d == nil {
		panic(unported(o.str("type")))
	}
	return d
}

// parseFallenTree reads a FallenTreeConfiguration.
// vanilla: FallenTreeFeature
func parseFallenTree(c jobj) func(*decorRegion, treePos) bool {
	trunk, length := compileState(c["trunk_provider"]), compileInt(c["log_length"])
	var stump, logs []treeDecorator
	for _, d := range c.arr("stump_decorators") {
		stump = append(stump, parseDecoratorOrSkip(jobjOf(d)))
	}
	for _, d := range c.arr("log_decorators") {
		logs = append(logs, parseDecoratorOrSkip(jobjOf(d)))
	}
	return func(r *decorRegion, origin treePos) bool {
		t := &treeBuild{r: r, rng: r.rng}
		log := func(p treePos, mod func(uint16) uint16) {
			st := trunk(r, p)
			if mod != nil {
				st = mod(st)
			}
			r.setOverPlant(p, st)
		}
		decorate := func(set []treePos, ds []treeDecorator) {
			if len(ds) == 0 {
				return
			}
			var ps posSet
			for _, p := range set {
				ps.add(p)
			}
			ctx := &decoratorCtx{t: t, logs: ps.sortedByY()}
			for _, d := range ds {
				d.place(ctx)
			}
		}
		validTree := func(p treePos) bool { return t.validTreePos(p) }
		overSolid := func(p treePos) bool {
			return registry.FaceSturdy(r.get(p[0], p[1]-1, p[2]), dirUp, registry.SupportFull)
		}
		log(origin, nil)
		decorate([]treePos{origin}, stump)
		dir := randomHorizontal(r.rng)
		logLength := length(r.rng) - 2
		start := horizStep(origin, dir, 2+r.rng.NextIntN(2))
		start[1]++
		for range 6 {
			if validTree(start) && overSolid(start) {
				break
			}
			start[1]--
		}
		gap := int32(0)
		cur := start
		for range logLength {
			if !validTree(cur) {
				return true
			}
			if !overSolid(cur) {
				if gap++; gap > 2 {
					return true
				}
			} else {
				gap = 0
			}
			cur = horizStep(cur, dir, 1)
		}
		axis := "z"
		if horizontalDirs[dir][0] != 0 {
			axis = "x"
		}
		var placed []treePos
		cur = start
		for range logLength {
			log(cur, func(st uint16) uint16 {
				out, _ := registry.WithProp(st, registry.PropAxis, axis)
				return out
			})
			placed = append(placed, cur)
			cur = horizStep(cur, dir, 1)
		}
		decorate(placed, logs)
		return true
	}
}

// hugeMushroomRadius reads the foliage radius of a HugeMushroomFeatureConfiguration.
func hugeMushroomRadius(c jobj) int32 { return c.integerOr("foliage_radius", 2) }

// bonemealMushroomRadius returns the foliage radius of the huge mushroom
// feature key, or false when the feature is missing or not a huge mushroom.
// vanilla: MushroomBlock.isValidBonemealTarget
func bonemealMushroomRadius(key string) (int32, bool) {
	src, ok := levelgen.OverworldTreeFeature(false, "minecraft:"+key)
	if !ok {
		return 0, false
	}
	o := parseJSON(src)
	if t := jtype(o); t != "huge_red_mushroom" && t != "huge_brown_mushroom" {
		return 0, false
	}
	return hugeMushroomRadius(o), true
}

// parseHugeMushroom reads a HugeMushroomFeatureConfiguration.
// vanilla: AbstractHugeMushroomFeature, HugeRedMushroomFeature, HugeBrownMushroomFeature
func parseHugeMushroom(red bool, c jobj) func(*decorRegion, treePos) bool {
	canPlaceOn, cap, stem := compilePred(c["can_place_on"]), compileState(c["cap_provider"]), compileState(c["stem_provider"])
	radius := hugeMushroomRadius(c)
	radiusAt := func(height, yo int32) int32 {
		if red {
			if yo < height && yo >= height-3 || yo == height {
				return radius
			}
			return 0
		}
		if yo <= 3 {
			return 0
		}
		return radius
	}
	return func(r *decorRegion, origin treePos) bool {
		rng := r.rng
		height := rng.NextIntN(3) + 4
		if rng.NextIntN(12) == 0 {
			height *= 2
		}
		if origin[1] < r.minY+1 || origin[1]+height+1 > r.minY+r.height-1 || !canPlaceOn(r, at(origin, treePos{0, -1, 0})) {
			return false
		}
		for dy := range height + 1 {
			// vanilla: AbstractHugeMushroomFeature.isValidPosition passes -1 as the height.
			rad := radiusAt(-1, dy)
			for dx := -rad; dx <= rad; dx++ {
				for dz := -rad; dz <= rad; dz++ {
					b := registry.BlockOf(r.get(origin[0]+dx, origin[1]+dy, origin[2]+dz))
					if !overworldAirBlocks[b] && !b.IsLeaves() {
						return false
					}
				}
			}
		}
		put := func(p treePos, st uint16) {
			b := registry.BlockOf(r.get(p[0], p[1], p[2]))
			if overworldAirBlocks[b] || b.InTag(registry.TagIDReplaceableByMushrooms) {
				r.setOverPlant(p, st)
			}
		}
		capState := func(sides [5]bool) uint16 {
			st := cap(r, origin)
			props := [5]registry.Prop{registry.PropWest, registry.PropEast, registry.PropNorth, registry.PropSouth, registry.PropUp}
			n := 4
			if red {
				n = 5
			}
			for i := range n {
				st, _ = registry.WithBool(st, props[i], sides[i])
			}
			return st
		}
		if red {
			for dy := height - 3; dy <= height; dy++ {
				rad, centre := radius, radius-2
				if dy >= height {
					rad--
				}
				for dx := -rad; dx <= rad; dx++ {
					for dz := -rad; dz <= rad; dz++ {
						xEdge, zEdge := dx == -rad || dx == rad, dz == -rad || dz == rad
						if dy < height && xEdge == zEdge {
							continue
						}
						put(at(origin, treePos{dx, dy, dz}), capState([5]bool{dx < -centre, dx > centre, dz < -centre, dz > centre, dy >= height-1}))
					}
				}
			}
		} else {
			for dx := -radius; dx <= radius; dx++ {
				for dz := -radius; dz <= radius; dz++ {
					minX, maxX, minZ, maxZ := dx == -radius, dx == radius, dz == -radius, dz == radius
					xEdge, zEdge := minX || maxX, minZ || maxZ
					if xEdge && zEdge {
						continue
					}
					put(at(origin, treePos{dx, height, dz}), capState([5]bool{
						minX || zEdge && dx == 1-radius, maxX || zEdge && dx == radius-1,
						minZ || xEdge && dz == 1-radius, maxZ || xEdge && dz == radius-1, false}))
				}
			}
		}
		for dy := range height {
			put(at(origin, treePos{0, dy, 0}), stem(r, origin))
		}
		return true
	}
}
