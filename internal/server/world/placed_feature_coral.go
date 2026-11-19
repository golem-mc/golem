package world

import (
	"fmt"

	"github.com/golem-mc/golem/registry"
)

// orderedTagStates is the block tags a random_block provider draws from, in
// the data pack's order, which the draw indexes: nested tags are expanded in
// place.
// vanilla: data/minecraft/tags/block/{corals,wall_corals}.json
var orderedTagStates = map[string][]uint16{
	"minecraft:corals":      append(coralStates("minecraft:%s_coral"), coralStates("minecraft:%s_coral_fan")...),
	"minecraft:wall_corals": coralStates("minecraft:%s_coral_wall_fan"),
}

func coralStates(format string) []uint16 {
	var out []uint16
	for _, kind := range [...]string{"tube", "brain", "bubble", "fire", "horn"} {
		b, ok := registry.BlockByName(fmt.Sprintf(format, kind))
		if !ok {
			panic("world: no block " + fmt.Sprintf(format, kind))
		}
		out = append(out, b.DefaultState())
	}
	return out
}

// coralTree is CoralTreeFeature: block places one coral block of the shape.
func coralTree(block featureFn) featureFn {
	return func(r *decorRegion, o [3]int32) bool {
		rng, p := r.rng, o
		for range rng.NextIntN(3) + 1 {
			if !block(r, p) {
				return true
			}
			p[1]++
		}
		top := p
		branches := int(rng.NextIntN(3)) + 2
		dirs := []int{0, 1, 2, 3}
		shuffle(dirs, rng)
		for _, dir := range dirs[:branches] {
			p = horizStep(top, dir, 1)
			height, segment := rng.NextIntN(5)+2, 0
			for j := int32(0); j < height && block(r, p); j++ {
				p[1]++
				if j != 0 {
					segment++
					if segment < 2 || !(rng.NextFloat() < 0.25) {
						continue
					}
				}
				p = horizStep(p, dir, 1)
				segment = 0
			}
		}
		return true
	}
}

// coralClaw is CoralClawFeature.
func coralClaw(block featureFn) featureFn {
	return func(r *decorRegion, o [3]int32) bool {
		rng := r.rng
		if !block(r, o) {
			return false
		}
		claw := randomHorizontal(rng)
		branches := int(rng.NextIntN(2)) + 2
		dirs := []int{claw, (claw + 1) % 4, (claw + 3) % 4}
		shuffle(dirs, rng)
	branch:
		for _, dir := range dirs[:branches] {
			p := o
			sideways := rng.NextIntN(2) + 1
			p = horizStep(p, dir, 1)
			var segment func(treePos) treePos // moves one step along the segment direction
			var back func(treePos) treePos
			var inway int32
			if dir == claw {
				segment = func(q treePos) treePos { return horizStep(q, dir, 1) }
				back = func(q treePos) treePos { return horizStep(q, dir, -1) }
				inway = rng.NextIntN(3) + 2
			} else {
				p[1]++
				up := func(q treePos) treePos { q[1]++; return q }
				down := func(q treePos) treePos { q[1]--; return q }
				if rng.NextIntN(2) == 0 { // Util.getRandom({branchDirection, UP})
					segment = func(q treePos) treePos { return horizStep(q, dir, 1) }
					back = func(q treePos) treePos { return horizStep(q, dir, -1) }
				} else {
					segment, back = up, down
				}
				inway = rng.NextIntN(3) + 3
			}
			for i := int32(0); i < sideways && block(r, p); i++ {
				p = segment(p)
			}
			p = back(p)
			p[1]++
			for range inway {
				p = horizStep(p, claw, 1)
				if !block(r, p) {
					continue branch
				}
				if rng.NextFloat() < 0.25 {
					p[1]++
				}
			}
		}
		return true
	}
}
