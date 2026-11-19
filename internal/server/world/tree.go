package world

import (
	"math"

	"github.com/golem-mc/golem/registry"
)

// saplingBlocks are SaplingBlock's nine species, all random-ticking whether or
// not a tree is planted for them. The mangrove propagule is a SaplingBlock
// overriding randomTick, so it ticks via mangrovePropaguleTick.
var saplingBlocks = blockSet(
	registry.BlockOakSapling, registry.BlockSpruceSapling, registry.BlockBirchSapling,
	registry.BlockJungleSapling, registry.BlockAcaciaSapling, registry.BlockDarkOakSapling,
	registry.BlockCherrySapling, registry.BlockPaleOakSapling, registry.BlockPoplarSapling,
)

// advanceTree cycles stage 0 to 1, and grows the species' tree from stage 1.
// vanilla: SaplingBlock.advanceTree
func (p *Physics) advanceTree(x, y, z int32, state uint16, changes *[]BlockChange) {
	if registry.StateValue(state, registry.PropStage) == "0" {
		p.writeSaplingStage(x, y, z, state, changes)
		return
	}
	p.growSaplingTree(x, y, z, state, changes)
}

// writeSaplingStage writes the stage with flag 260: no client update (the model
// looks the same at either stage), but the shape pass runs, the only way an
// observer hears it.
func (p *Physics) writeSaplingStage(x, y, z int32, state uint16, changes *[]BlockChange) {
	if next, found := registry.WithInt(state, registry.PropStage, 1); found {
		next, ok := p.veto(HookGrow, x, y, z, next, [3]int32{})
		if !ok {
			return
		}
		p.writeBlockShaped(x, y, z, next, changes)
	}
}

// saplingTick: under raw light 9 or more above, a one-in-seven roll advances
// the tree. Light is checked first, so a sapling under 9 draws no roll.
// vanilla: SaplingBlock.randomTick
func (p *Physics) saplingTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	if p.w.MaxLocalRawLightAt(x, y+1, z) < 9 {
		return
	}
	if p.rng.Intn(7) != 0 {
		return
	}
	p.advanceTree(x, y, z, state, changes)
}

// mangrovePropaguleTick: a hanging propagule ages without a roll until grown, a
// standing one takes the one-in-seven roll. The override skips SaplingBlock's
// light check in both branches.
// vanilla: MangrovePropaguleBlock.randomTick
func (p *Physics) mangrovePropaguleTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	if registry.StateBool(state, registry.PropHanging) {
		age, ok := registry.StateInt(state, registry.PropAge)
		if !ok || age >= 4 {
			return
		}
		if next, ok := registry.WithInt(state, registry.PropAge, age+1); ok {
			p.setClientShaped(x, y, z, next, changes)
		}
		return
	}
	if p.rng.Intn(7) != 0 {
		return
	}
	p.advanceTree(x, y, z, state, changes)
}

// sameSpeciesSapling2x2 reports whether the sapling is one of four of the same
// block in a 2x2, TreeGrower.isTwoByTwoSapling at the four corner offsets.
// Stage is ignored, as vanilla compares blocks, not states.
func (p *Physics) sameSpeciesSapling2x2(x, y, z int32) bool {
	_, _, ok := p.sapling2x2Origin(x, y, z)
	return ok
}

func (p *Physics) sapling2x2Origin(x, y, z int32) (int32, int32, bool) {
	sapling := registry.BlockOf(p.w.BlockAt(x, y, z))
	for xo := int32(0); xo >= -1; xo-- {
		for zo := int32(0); zo >= -1; zo-- {
			match := true
			for dx := int32(0); dx < 2 && match; dx++ {
				for dz := int32(0); dz < 2 && match; dz++ {
					match = registry.BlockOf(p.w.BlockAt(x+xo+dx, y, z+zo+dz)) == sapling
				}
			}
			if match {
				return x + xo, z + zo, true
			}
		}
	}
	return 0, 0, false
}

// treeLeafFree excludes persistent leaves from replaceable canopy cells.
func (p *Physics) treeLeafFree(x, y, z int32) bool {
	if !contains(y) {
		return false
	}
	state := p.w.BlockAt(x, y, z)
	return int(state) < registry.StateCount && !registry.StateBool(state, registry.PropPersistent) && treeAirOrReplaceable(registry.BlockOf(state))
}

// settleDistancesFrom walks leaves outward from the growth's logs:
// logs seed level 0, bounded by inside.
func (p *Physics) settleDistancesFrom(seeds [][3]int32, inside func([3]int32) bool) [][3]int32 {
	var levels [7]blockPosSet
	shape := make(map[[3]int32]bool)
	for _, s := range seeds {
		levels[0].add(s)
	}
	var written [][3]int32
	level := 0
	for {
		for level < 7 && levels[level].size == 0 {
			level++
		}
		if level >= 7 {
			return written
		}
		pos, _ := levels[level].take()
		if level != 0 {
			if next, ok := p.leafAtDistance(pos, level); ok {
				p.w.SetBlock(pos[0], pos[1], pos[2], next)
				written = append(written, pos)
			}
		}
		shape[pos] = true
		for _, delta := range [6][3]int32{
			{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0},
		} {
			n := [3]int32{pos[0] + delta[0], pos[1] + delta[1], pos[2] + delta[2]}
			if shape[n] || !inside(n) {
				continue
			}
			ns := p.w.BlockAt(n[0], n[1], n[2])
			if !registry.BlockOf(ns).IsLeaves() {
				continue // the walk runs through leaf blocks alone
			}
			d := 7
			if v, ok := registry.StateInt(ns, registry.PropDistance); ok {
				d = v
			}
			if level+1 < d {
				d = level + 1
			}
			if d >= 7 {
				continue // past the walk's reach the leaf keeps its seven
			}
			levels[d].add(n)
			if d < level {
				level = d
			}
		}
	}
}

// settleGrownDistances is every grower's updateLeaves walk, seeded from the
// logs and bounded by the box the grown logs and leaves draw (bounds.isInside),
// so neighbouring trees' leaves outside it are untouched.
func (p *Physics) settleGrownDistances(logs, leaves [][3]int32) [][3]int32 {
	return p.settleDistancesFrom(logs, boundsOf(logs, leaves))
}

// boundsOf is the logs' and leaves' box as a containment test (vanilla:
// BoundingBox.encapsulatingPositions in TreeFeature.place).
func boundsOf(logs, leaves [][3]int32) func([3]int32) bool {
	lo := [3]int32{math.MaxInt32, math.MaxInt32, math.MaxInt32}
	hi := [3]int32{math.MinInt32, math.MinInt32, math.MinInt32}
	for _, set := range [2][][3]int32{logs, leaves} {
		for _, q := range set {
			for i := range q {
				lo[i], hi[i] = min(lo[i], q[i]), max(hi[i], q[i])
			}
		}
	}
	return func(n [3]int32) bool {
		return n[0] >= lo[0] && n[0] <= hi[0] && n[1] >= lo[1] && n[1] <= hi[1] && n[2] >= lo[2] && n[2] <= hi[2]
	}
}

// leafAtDistance is the position's own leaf with its distance set, the
// dequeue's write, so a neighbouring tree's leaves keep their kind.
func (p *Physics) leafAtDistance(pos [3]int32, distance int) (uint16, bool) {
	state := p.w.BlockAt(pos[0], pos[1], pos[2])
	b := registry.BlockOf(state)
	if !b.IsLeaves() {
		return 0, false
	}
	return registry.WithInt(state, registry.PropDistance, distance)
}

// noteGrownCell records a leaf the tree now owns, the client change with its
// settled state, and gives it the neighbour wake a recorded write runs, which
// the growth's bare writes never did.
func (p *Physics) noteGrownCell(pos [3]int32, noted map[[3]int32]bool, changes *[]BlockChange) {
	if noted[pos] {
		return
	}
	noted[pos] = true
	state := p.w.BlockAt(pos[0], pos[1], pos[2])
	if state == airState {
		return // a leaf outside the loaded chunks never happened
	}
	*changes = append(*changes, physicsChange(pos[0], pos[1], pos[2], airState, state))
	p.neighborUpdate(pos[0], pos[1], pos[2], airState, state, changes)
}

// noteGrownCells records the growth's leaves (placed and rewritten by the
// settle walk) once each, in write order.
func (p *Physics) noteGrownCells(leaves, walked [][3]int32, changes *[]BlockChange) {
	noted := make(map[[3]int32]bool, len(leaves)+len(walked))
	for _, pos := range leaves {
		p.noteGrownCell(pos, noted, changes)
	}
	for _, pos := range walked {
		p.noteGrownCell(pos, noted, changes)
	}
}
