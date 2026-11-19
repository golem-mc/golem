package world

import "github.com/golem-mc/golem/registry"

// dragonEggAttempts is how many random cells the egg tries.
// vanilla: DragonEggBlock.teleport
const dragonEggAttempts = 1000

// DragonEggTeleport moves the egg at (x, y, z) to a random air cell within
// 16x8x16 above a non-air block, inside the border (within) and build height,
// or leaves it after 1000 failed tries. It returns the changes and the cell
// the egg moved to; no changes if there is no egg or no try fit.
// vanilla: DragonEggBlock.teleport
func (p *Physics) DragonEggTeleport(x, y, z int32, within func(x, z int32) bool) ([]BlockChange, [3]int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockDragonEgg {
		return nil, [3]int32{}
	}
	minY, maxY := p.w.MinY(), p.w.MinY()+p.w.Height()
	for range dragonEggAttempts {
		tx := x + int32(p.rng.Intn(16)-p.rng.Intn(16))
		ty := y + int32(p.rng.Intn(8)-p.rng.Intn(8))
		tz := z + int32(p.rng.Intn(16)-p.rng.Intn(16))
		if ty < minY || ty >= maxY || !within(tx, tz) {
			continue
		}
		if !p.w.IsAirAt(tx, ty, tz) || p.w.IsAirAt(tx, ty-1, tz) {
			continue
		}
		var changes []BlockChange
		p.setPlacedShaped(tx, ty, tz, state, &changes)
		p.setShaped(x, y, z, airState, &changes)
		return changes, [3]int32{tx, ty, tz}
	}
	return nil, [3]int32{}
}
