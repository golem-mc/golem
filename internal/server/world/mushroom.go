package world

import "github.com/golem-mc/golem/registry"

// hugeMushroomBlocks marks the mushroom blocks and the stems.
var hugeMushroomBlocks = registry.BlocksOf(func(b registry.Block) bool {
	return b.Type() == registry.BlockTypeHugeMushroom
})

// vanilla: HugeMushroomBlock.updateShape
func hugeMushroomShapeUpdate(state uint16, toward int, neighbor uint16) uint16 {
	// Removing a neighbour never restores a hidden face.
	if registry.BlockOf(neighbor) != registry.BlockOf(state) {
		return state
	}
	next, _ := registry.WithBool(state, facePropIDs[toward], false)
	return next
}

const (
	// mushroomSpreadDie is the one-in-N chance a landed die acts; mushroomCrowd
	// is the count of its kind in the box (itself included) that stops it.
	// vanilla: MushroomBlock.randomTick
	mushroomSpreadDie = 25
	mushroomCrowd     = 5
)

// mushroomTick: on a one-in-25 roll, unless five of its kind are in the 9x3x9
// box, it walks four steps over empty cells it could survive in and plants a
// copy at the end, a flag-2 write with no neighbour update.
// vanilla: MushroomBlock.randomTick
func (p *Physics) mushroomTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	if p.rng.Intn(mushroomSpreadDie) != 0 {
		return
	}
	left := mushroomCrowd
	for dx := int32(-4); dx <= 4; dx++ {
		for dy := int32(-1); dy <= 1; dy++ {
			for dz := int32(-4); dz <= 4; dz++ {
				if p.w.BlockAt(x+dx, y+dy, z+dz) != state {
					continue
				}
				if left--; left <= 0 {
					return
				}
			}
		}
	}
	// Java evaluates the offset's three draws in this order.
	step := func(x, y, z int32) (int32, int32, int32) {
		dx := int32(p.rng.Intn(3)) - 1
		up := int32(p.rng.Intn(2))
		dy := up - int32(p.rng.Intn(2))
		dz := int32(p.rng.Intn(3)) - 1
		return x + dx, y + dy, z + dz
	}
	fits := func(x, y, z int32) bool {
		return p.airAt(x, y, z) && canSurvive(p.w, x, y, z, state)
	}
	ox, oy, oz := step(x, y, z)
	for range 4 {
		if fits(ox, oy, oz) {
			x, y, z = ox, oy, oz
		}
		ox, oy, oz = step(x, y, z)
	}
	if fits(ox, oy, oz) {
		if next, ok := p.veto(HookSpread, ox, oy, oz, state, [3]int32{x, y, z}); ok {
			p.setClientShaped(ox, oy, oz, next, changes)
		}
	}
}
