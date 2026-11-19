package world

import (
	"math"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// horizontalDirs are Direction.Plane.HORIZONTAL in its order: north, east,
// south, west, as x and z steps.
var horizontalDirs = [4][2]int32{{0, -1}, {1, 0}, {0, 1}, {-1, 0}}

// randomHorizontal is Direction.Plane.HORIZONTAL.getRandomDirection.
func randomHorizontal(rng *levelgen.WorldgenRandom) int {
	return int(rng.NextIntN(4))
}

func horizStep(p treePos, dir int, n int32) treePos {
	return treePos{p[0] + horizontalDirs[dir][0]*n, p[1], p[2] + horizontalDirs[dir][1]*n}
}

// trunkPlacer is a TrunkPlacer.
type trunkPlacer interface {
	height(rng *levelgen.WorldgenRandom) int32
	placeTrunk(t *treeBuild, height int32, origin treePos) []foliageAttach
	isFree(t *treeBuild, p treePos) bool
}

// trunkBase holds what every trunk placer shares.
type trunkBase struct {
	base, randA, randB int32
	// growThrough also counts as a valid tree position when set.
	growThrough func(registry.Block) bool
}

func (tb trunkBase) height(rng *levelgen.WorldgenRandom) int32 {
	return tb.base + rng.NextIntN(tb.randA+1) + rng.NextIntN(tb.randB+1)
}

func (tb trunkBase) valid(t *treeBuild, p treePos) bool {
	return t.validTreePos(p) || tb.growThrough != nil && t.is(p, tb.growThrough)
}

func (tb trunkBase) isFree(t *treeBuild, p treePos) bool { return tb.valid(t, p) || t.is(p, treeLog) }

// placeBelow is placeBelowTrunkBlock: the below-trunk provider's block, if it
// gives one.
func (tb trunkBase) placeBelow(t *treeBuild, p treePos) {
	if st, ok := t.cfg.belowTrunk(t.r, p); ok {
		t.put(&t.trunks, p, st)
	}
}

// placeLog sets a log of the trunk provider, changed by mod, where a tree
// may stand.
func (tb trunkBase) placeLog(t *treeBuild, p treePos, mod func(uint16) uint16) bool {
	if !tb.valid(t, p) {
		return false
	}
	st := t.cfg.trunkProvider(t.r, p)
	if mod != nil {
		st = mod(st)
	}
	t.put(&t.trunks, p, st)
	return true
}

func (tb trunkBase) placeLogIfFree(t *treeBuild, p treePos) {
	if tb.isFree(t, p) {
		tb.placeLog(t, p, nil)
	}
}

func withAxis(axis string) func(uint16) uint16 {
	return func(st uint16) uint16 {
		out, _ := registry.WithProp(st, registry.PropAxis, axis)
		return out
	}
}

// straightTrunk is StraightTrunkPlacer.
type straightTrunk struct{ trunkBase }

func (s straightTrunk) placeTrunk(t *treeBuild, height int32, o treePos) []foliageAttach {
	s.placeBelow(t, at(o, treePos{0, -1, 0}))
	for y := range height {
		s.placeLog(t, at(o, treePos{0, y, 0}), nil)
	}
	return []foliageAttach{{pos: at(o, treePos{0, height, 0})}}
}

// forkingTrunk is ForkingTrunkPlacer.
type forkingTrunk struct{ trunkBase }

func (f forkingTrunk) placeTrunk(t *treeBuild, height int32, o treePos) []foliageAttach {
	f.placeBelow(t, at(o, treePos{0, -1, 0}))
	var out []foliageAttach
	lean := randomHorizontal(t.rng)
	leanHeight := height - t.rng.NextIntN(4) - 1
	leanSteps := 3 - t.rng.NextIntN(3)
	tx, tz := o[0], o[2]
	ey, haveEY := int32(0), false
	for yo := range height {
		yy := o[1] + yo
		if yo >= leanHeight && leanSteps > 0 {
			tx += horizontalDirs[lean][0]
			tz += horizontalDirs[lean][1]
			leanSteps--
		}
		if f.placeLog(t, treePos{tx, yy, tz}, nil) {
			ey, haveEY = yy+1, true
		}
	}
	if haveEY {
		out = append(out, foliageAttach{pos: treePos{tx, ey, tz}, radiusOffset: 1})
	}
	tx, tz = o[0], o[2]
	branch := randomHorizontal(t.rng)
	if branch != lean {
		branchPos := leanHeight - t.rng.NextIntN(2) - 1
		branchSteps := 1 + t.rng.NextIntN(3)
		haveEY = false
		for yo := branchPos; yo < height && branchSteps > 0; yo, branchSteps = yo+1, branchSteps-1 {
			if yo < 1 {
				continue
			}
			yy := o[1] + yo
			tx += horizontalDirs[branch][0]
			tz += horizontalDirs[branch][1]
			if f.placeLog(t, treePos{tx, yy, tz}, nil) {
				ey, haveEY = yy+1, true
			}
		}
		if haveEY {
			out = append(out, foliageAttach{pos: treePos{tx, ey, tz}})
		}
	}
	return out
}

// bendingTrunk is BendingTrunkPlacer.
type bendingTrunk struct {
	trunkBase
	minHeightForLeaves int32
	bendLength         intProvider
}

func (b bendingTrunk) placeTrunk(t *treeBuild, height int32, o treePos) []foliageAttach {
	dir := randomHorizontal(t.rng)
	logHeight := height - 1
	b.placeBelow(t, at(o, treePos{0, -1, 0}))
	var out []foliageAttach
	p := o
	for i := range logHeight + 1 {
		if i+1 >= logHeight+t.rng.NextIntN(2) {
			p = horizStep(p, dir, 1)
		}
		b.placeLog(t, p, nil)
		if i >= b.minHeightForLeaves {
			out = append(out, foliageAttach{pos: p})
		}
		p[1]++
	}
	for range b.bendLength(t.rng) + 1 {
		b.placeLog(t, p, nil)
		out = append(out, foliageAttach{pos: p})
		p = horizStep(p, dir, 1)
	}
	return out
}

// giantTrunk is GiantTrunkPlacer; jungle adds MegaJungleTrunkPlacer's
// branches.
type giantTrunk struct {
	trunkBase
	jungle bool
}

func (g giantTrunk) placeTrunk(t *treeBuild, height int32, o treePos) []foliageAttach {
	below := at(o, treePos{0, -1, 0})
	for _, d := range [4]treePos{{}, {1, 0, 0}, {0, 0, 1}, {1, 0, 1}} {
		g.placeBelow(t, at(below, d))
	}
	for hh := range height {
		g.placeLogIfFree(t, at(o, treePos{0, hh, 0}))
		if hh >= height-1 {
			continue
		}
		g.placeLogIfFree(t, at(o, treePos{1, hh, 0}))
		g.placeLogIfFree(t, at(o, treePos{1, hh, 1}))
		g.placeLogIfFree(t, at(o, treePos{0, hh, 1}))
	}
	out := []foliageAttach{{pos: at(o, treePos{0, height, 0}), doubleTrunk: true}}
	if !g.jungle {
		return out
	}
	rng := t.rng
	for bh := height - 2 - rng.NextIntN(4); bh > height/2; bh -= 2 + rng.NextIntN(4) {
		angle := rng.NextFloat() * (math.Pi * 2)
		var bx, bz int32
		for b := range int32(5) {
			bx = int32(1.5 + float32(MthCos(float64(angle)))*float32(b))
			bz = int32(1.5 + float32(MthSin(float64(angle)))*float32(b))
			g.placeLog(t, at(o, treePos{bx, bh - 3 + b/2, bz}), nil)
		}
		out = append(out, foliageAttach{pos: at(o, treePos{bx, bh, bz}), radiusOffset: -2})
	}
	return out
}

// darkOakTrunk is DarkOakTrunkPlacer.
type darkOakTrunk struct{ trunkBase }

func (d darkOakTrunk) placeTrunk(t *treeBuild, height int32, o treePos) []foliageAttach {
	rng := t.rng
	below := at(o, treePos{0, -1, 0})
	for _, dd := range [4]treePos{{}, {1, 0, 0}, {0, 0, 1}, {1, 0, 1}} {
		d.placeBelow(t, at(below, dd))
	}
	lean := randomHorizontal(rng)
	leanHeight := height - rng.NextIntN(4)
	leanSteps := 2 - rng.NextIntN(3)
	x, y, z := o[0], o[1], o[2]
	tx, tz := x, z
	ey := y + height - 1
	for dy := range height {
		if dy >= leanHeight && leanSteps > 0 {
			tx += horizontalDirs[lean][0]
			tz += horizontalDirs[lean][1]
			leanSteps--
		}
		p := treePos{tx, y + dy, tz}
		if !t.is(p, treeAirOrLv) {
			continue
		}
		d.placeLog(t, p, nil)
		d.placeLog(t, at(p, treePos{1, 0, 0}), nil)
		d.placeLog(t, at(p, treePos{0, 0, 1}), nil)
		d.placeLog(t, at(p, treePos{1, 0, 1}), nil)
	}
	out := []foliageAttach{{pos: treePos{tx, ey, tz}, doubleTrunk: true}}
	for ox := int32(-1); ox <= 2; ox++ {
		for oz := int32(-1); oz <= 2; oz++ {
			if ox >= 0 && ox <= 1 && oz >= 0 && oz <= 1 || rng.NextIntN(3) > 0 {
				continue
			}
			length := rng.NextIntN(3) + 2
			for by := range length {
				d.placeLog(t, treePos{x + ox, ey - by - 1, z + oz}, nil)
			}
			out = append(out, foliageAttach{pos: treePos{x + ox, ey, z + oz}})
		}
	}
	return out
}

// fancyTrunk is FancyTrunkPlacer.
type fancyTrunk struct{ trunkBase }

type fancyCoords struct {
	attach     foliageAttach
	branchBase int32
}

func (f fancyTrunk) placeTrunk(t *treeBuild, treeHeight int32, o treePos) []foliageAttach {
	rng := t.rng
	height := treeHeight + 2
	trunkHeight := int32(math.Floor(float64(height) * 0.618))
	f.placeBelow(t, at(o, treePos{0, -1, 0}))
	clustersPerY := min(1, int32(math.Floor(1.382+math.Pow(1.0*float64(height)/13.0, 2.0))))
	trunkTop := o[1] + trunkHeight
	// The first cluster sits at relativeY = height-5, not at the trunk top.
	coords := []fancyCoords{{foliageAttach{pos: at(o, treePos{0, height - 5, 0})}, trunkTop}}
	for relY := height - 5; relY >= 0; relY-- {
		shape := fancyCanopyShape(height, relY)
		if shape < 0 {
			continue
		}
		for range clustersPerY {
			radius := 1.0 * float64(shape) * (float64(rng.NextFloat()) + 0.328)
			angle := float64(rng.NextFloat()*2.0) * math.Pi
			x := radius*math.Sin(angle) + 0.5
			z := radius*math.Cos(angle) + 0.5
			start := at(o, treePos{int32(math.Floor(x)), relY - 1, int32(math.Floor(z))})
			end := at(start, treePos{0, 5, 0})
			if !f.makeLimb(t, start, end, false) {
				continue
			}
			dx, dz := o[0]-start[0], o[2]-start[2]
			branchHeight := float64(start[1]) - math.Sqrt(float64(dx*dx+dz*dz))*0.381
			branchTop := int32(branchHeight)
			if branchHeight > float64(trunkTop) {
				branchTop = trunkTop
			}
			base := treePos{o[0], branchTop, o[2]}
			if !f.makeLimb(t, base, start, false) {
				continue
			}
			coords = append(coords, fancyCoords{foliageAttach{pos: start}, base[1]})
		}
	}
	f.makeLimb(t, o, at(o, treePos{0, trunkHeight, 0}), true)
	for _, c := range coords {
		base := treePos{o[0], c.branchBase, o[2]}
		if base == c.attach.pos || !fancyCanopyTrim(height, c.branchBase-o[1]) {
			continue
		}
		f.makeLimb(t, base, c.attach.pos, true)
	}
	var out []foliageAttach
	for _, c := range coords {
		if fancyCanopyTrim(height, c.branchBase-o[1]) {
			out = append(out, c.attach)
		}
	}
	return out
}

func fancyCanopyTrim(height, localY int32) bool { return float64(localY) >= float64(height)*0.2 }

func fancyCanopyShape(height, y int32) float32 {
	if float32(y) < float32(height)*0.3 {
		return -1
	}
	radius := float32(height) / 2.0
	adjacent := radius - float32(y)
	distance := float32(math.Sqrt(float64(radius*radius - adjacent*adjacent)))
	if adjacent == 0 {
		distance = radius
	} else if math.Abs(float64(adjacent)) >= float64(radius) {
		return 0
	}
	return distance * 0.5
}

func (f fancyTrunk) makeLimb(t *treeBuild, start, end treePos, place bool) bool {
	if !place && start == end {
		return true
	}
	delta := treePos{end[0] - start[0], end[1] - start[1], end[2] - start[2]}
	steps := max(abs32(delta[0]), abs32(delta[1]), abs32(delta[2]))
	dx, dy, dz := float32(delta[0])/float32(steps), float32(delta[1])/float32(steps), float32(delta[2])/float32(steps)
	floor := func(v float32) int32 { return int32(math.Floor(float64(v))) }
	for i := range steps + 1 {
		p := at(start, treePos{floor(0.5 + float32(i)*dx), floor(0.5 + float32(i)*dy), floor(0.5 + float32(i)*dz)})
		if place {
			xd, zd := abs32(p[0]-start[0]), abs32(p[2]-start[2])
			axis := "y"
			if m := max(xd, zd); m > 0 {
				axis = "z"
				if xd == m {
					axis = "x"
				}
			}
			f.placeLog(t, p, withAxis(axis))
			continue
		}
		if !f.isFree(t, p) {
			return false
		}
	}
	return true
}

// cherryTrunk is CherryTrunkPlacer.
type cherryTrunk struct {
	trunkBase
	branchCount, branchLength, branchEnd intProvider
	start, secondStart                   uniformBounds
}

func (c cherryTrunk) placeTrunk(t *treeBuild, height int32, o treePos) []foliageAttach {
	rng := t.rng
	c.placeBelow(t, at(o, treePos{0, -1, 0}))
	sample := func(u uniformBounds) int32 { return rng.NextIntN(u.hi-u.lo+1) + u.lo }
	first := max(0, height-1+sample(c.start))
	second := max(0, height-1+sample(c.secondStart))
	if second >= first {
		second++
	}
	count := c.branchCount(rng)
	middle := count == 3
	both := count >= 2
	trunkHeight := first + 1
	if middle {
		trunkHeight = height
	} else if both {
		trunkHeight = max(first, second) + 1
	}
	for y := range trunkHeight {
		c.placeLog(t, at(o, treePos{0, y, 0}), nil)
	}
	var out []foliageAttach
	if middle {
		out = append(out, foliageAttach{pos: at(o, treePos{0, trunkHeight, 0})})
	}
	dir := randomHorizontal(rng)
	sideways := withAxis(map[bool]string{true: "x", false: "z"}[horizontalDirs[dir][0] != 0])
	out = append(out, c.branch(t, height, o, sideways, dir, first, first < trunkHeight-1))
	if both {
		out = append(out, c.branch(t, height, o, sideways, (dir+2)%4, second, second < trunkHeight-1))
	}
	return out
}

func (c cherryTrunk) branch(t *treeBuild, treeHeight int32, o treePos, sideways func(uint16) uint16, dir int, offset int32, middleUp bool) foliageAttach {
	rng := t.rng
	log := at(o, treePos{0, offset, 0})
	endOffset := treeHeight - 1 + c.branchEnd(rng)
	extend := middleUp || endOffset < offset
	dist := c.branchLength(rng)
	if extend {
		dist++
	}
	end := at(horizStep(o, dir, dist), treePos{0, endOffset, 0})
	horizontal := int32(1)
	if extend {
		horizontal = 2
	}
	for range horizontal {
		log = horizStep(log, dir, 1)
		c.placeLog(t, log, sideways)
	}
	vertical := int32(-1)
	if end[1] > log[1] {
		vertical = 1
	}
	for {
		d := abs32(end[0]-log[0]) + abs32(end[1]-log[1]) + abs32(end[2]-log[2])
		if d == 0 {
			break
		}
		chance := float32(abs32(end[1]-log[1])) / float32(d)
		grow := rng.NextFloat() < chance
		if grow {
			log[1] += vertical
			c.placeLog(t, log, nil)
		} else {
			log = horizStep(log, dir, 1)
			c.placeLog(t, log, sideways)
		}
	}
	return foliageAttach{pos: at(end, treePos{0, 1, 0})}
}

// upwardsBranchingTrunk is UpwardsBranchingTrunkPlacer.
type upwardsBranchingTrunk struct {
	trunkBase
	steps, length intProvider
	branchChance  float32
}

func (u upwardsBranchingTrunk) placeTrunk(t *treeBuild, height int32, o treePos) []foliageAttach {
	rng := t.rng
	var out []foliageAttach
	for hp := range height {
		cur := o[1] + hp
		log := treePos{o[0], cur, o[2]}
		if u.placeLog(t, log, nil) && hp < height-1 && rng.NextFloat() < u.branchChance {
			dir := randomHorizontal(rng)
			bl := u.length(rng)
			bp := max(0, bl-u.length(rng)-1)
			steps := u.steps(rng)
			out = u.placeBranch(t, height, out, log, cur, dir, bp, steps)
		}
		if hp == height-1 {
			out = append(out, foliageAttach{pos: treePos{o[0], cur + 1, o[2]}})
		}
	}
	return out
}

func (u upwardsBranchingTrunk) placeBranch(t *treeBuild, treeHeight int32, out []foliageAttach, log treePos, cur int32, dir int, branchPos, steps int32) []foliageAttach {
	along := cur + branchPos
	lx, lz := log[0], log[2]
	for i := branchPos; i < treeHeight && steps > 0; i, steps = i+1, steps-1 {
		if i < 1 {
			continue
		}
		ph := cur + i
		along = ph
		lx += horizontalDirs[dir][0]
		lz += horizontalDirs[dir][1]
		log = treePos{lx, ph, lz}
		if u.placeLog(t, log, nil) {
			along++
		}
		out = append(out, foliageAttach{pos: log})
	}
	if along-cur > 1 {
		p := treePos{lx, along, lz}
		out = append(out, foliageAttach{pos: p}, foliageAttach{pos: at(p, treePos{0, -2, 0})})
	}
	return out
}

func parseTrunk(o jobj) trunkPlacer {
	tb := trunkBase{base: o.integer("base_height"), randA: o.integer("height_rand_a"), randB: o.integer("height_rand_b")}
	switch o.str("type") {
	case "minecraft:poplar_trunk_placer":
		return poplarTrunk{tb, compileInt(o["trunk_height_above_branches"]), compileInt(o["branch_amount"])}
	case "minecraft:straight_trunk_placer":
		return straightTrunk{tb}
	case "minecraft:forking_trunk_placer":
		return forkingTrunk{tb}
	case "minecraft:giant_trunk_placer":
		return giantTrunk{tb, false}
	case "minecraft:mega_jungle_trunk_placer":
		return giantTrunk{tb, true}
	case "minecraft:dark_oak_trunk_placer":
		return darkOakTrunk{tb}
	case "minecraft:fancy_trunk_placer":
		return fancyTrunk{tb}
	case "minecraft:cherry_trunk_placer":
		start := parseUniformBounds(o["branch_start_offset_from_top"])
		return cherryTrunk{tb, compileInt(o["branch_count"]), compileInt(o["branch_horizontal_length"]),
			compileInt(o["branch_end_offset_from_top"]), start, uniformBounds{start.lo, start.hi - 1}}
	case "minecraft:bending_trunk_placer":
		return bendingTrunk{tb, int32(o.integerOr("min_height_for_leaves", 1)), compileInt(o["bend_length"])}
	case "minecraft:upwards_branching_trunk_placer":
		tb.growThrough = blockSetOf(o["can_grow_through"])
		return upwardsBranchingTrunk{tb, compileInt(o["extra_branch_steps"]), compileInt(o["extra_branch_length"]),
			float32(o.num("place_branch_per_log_probability"))}
	}
	panic("world: tree data: trunk placer " + o.str("type"))
}
