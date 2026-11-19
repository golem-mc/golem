package world

import (
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// rootPlacer is a RootPlacer.
type rootPlacer interface {
	trunkOrigin(origin treePos, rng *levelgen.WorldgenRandom) treePos
	placeRoots(t *treeBuild, origin, trunkOrigin treePos) bool
}

// mangroveRoots is a MangroveRootPlacer.
type mangroveRoots struct {
	trunkOffsetY         intProvider
	root                 stateProvider
	aboveChance          float32
	above                stateProvider
	growThrough, muddyIn func(registry.Block) bool
	muddy                stateProvider
	maxWidth, maxLength  int32
	skewChance           float32
}

func (m *mangroveRoots) trunkOrigin(origin treePos, rng *levelgen.WorldgenRandom) treePos {
	return at(origin, treePos{0, m.trunkOffsetY(rng), 0})
}

func (m *mangroveRoots) canPlace(t *treeBuild, p treePos) bool {
	return t.validTreePos(p) || t.is(p, m.growThrough)
}

// waterlogged sets waterlogged from the water at p.
// vanilla: RootPlacer.getPotentiallyWaterloggedState
func waterlogged(t *treeBuild, p treePos, st uint16) uint16 {
	if _, has := registry.WithBool(st, registry.PropWaterlogged, false); !has {
		return st
	}
	st, _ = registry.WithBool(st, registry.PropWaterlogged, fluidHolding(t.state(p)).kind == fluidWater)
	return st
}

func (m *mangroveRoots) placeRoots(t *treeBuild, origin, trunkOrigin treePos) bool {
	var roots []treePos
	for y := origin[1]; y < trunkOrigin[1]; y++ {
		if !m.canPlace(t, treePos{origin[0], y, origin[2]}) {
			return false
		}
	}
	roots = append(roots, at(trunkOrigin, treePos{0, -1, 0}))
	for dir := range 4 {
		var inDir []treePos
		if !m.simulate(t, horizStep(trunkOrigin, dir, 1), dir, trunkOrigin, &inDir, 0) {
			return false
		}
		roots = append(roots, inDir...)
		roots = append(roots, horizStep(trunkOrigin, dir, 1))
	}
	for _, p := range roots {
		m.placeRoot(t, p)
	}
	return true
}

func (m *mangroveRoots) simulate(t *treeBuild, rootPos treePos, dir int, rootOrigin treePos, out *[]treePos, layer int32) bool {
	if layer == m.maxLength || int32(len(*out)) > m.maxLength {
		return false
	}
	for _, p := range m.potential(t, rootPos, dir, rootOrigin) {
		if !m.canPlace(t, p) {
			continue
		}
		*out = append(*out, p)
		if !m.simulate(t, p, dir, rootOrigin, out, layer+1) {
			return false
		}
	}
	return true
}

func (m *mangroveRoots) potential(t *treeBuild, pos treePos, prev int, rootOrigin treePos) []treePos {
	rng := t.rng
	below := at(pos, treePos{0, -1, 0})
	nextTo := horizStep(pos, prev, 1)
	width := abs32(pos[0]-rootOrigin[0]) + abs32(pos[1]-rootOrigin[1]) + abs32(pos[2]-rootOrigin[2])
	switch {
	case width > m.maxWidth-3 && width <= m.maxWidth:
		if rng.NextFloat() < m.skewChance {
			return []treePos{below, at(nextTo, treePos{0, -1, 0})}
		}
		return []treePos{below}
	case width > m.maxWidth:
		return []treePos{below}
	}
	if rng.NextFloat() < m.skewChance {
		return []treePos{below}
	}
	if rng.NextBoolean() {
		return []treePos{nextTo}
	}
	return []treePos{below}
}

func (m *mangroveRoots) placeRoot(t *treeBuild, p treePos) {
	if t.is(p, m.muddyIn) {
		t.put(&t.roots, p, waterlogged(t, p, m.muddy(t.r, p)))
		return
	}
	if !m.canPlace(t, p) {
		return
	}
	t.put(&t.roots, p, waterlogged(t, p, m.root(t.r, p)))
	if m.above == nil {
		return
	}
	up := at(p, treePos{0, 1, 0})
	if t.rng.NextFloat() < m.aboveChance && t.isAir(up) {
		t.put(&t.roots, up, waterlogged(t, up, m.above(t.r, up)))
	}
}

func parseRoots(o jobj) rootPlacer {
	if o.str("type") != "minecraft:mangrove_root_placer" {
		panic("world: tree data: root placer " + o.str("type"))
	}
	mp := o.obj("mangrove_root_placement")
	m := &mangroveRoots{
		trunkOffsetY: compileInt(o["trunk_offset_y"]),
		root:         compileState(o["root_provider"]),
		growThrough:  blockSetOf(mp["can_grow_through"]),
		muddyIn:      blockSetOf(mp["muddy_roots_in"]),
		muddy:        compileState(mp["muddy_roots_provider"]),
		maxWidth:     mp.integer("max_root_width"),
		maxLength:    mp.integer("max_root_length"),
		skewChance:   float32(mp.num("random_skew_chance")),
	}
	if o.has("above_root_placement") {
		a := o.obj("above_root_placement")
		sp := compileState(a["above_root_provider"])
		m.above, m.aboveChance = sp, float32(a.num("above_root_placement_chance"))
	}
	return m
}
