package world

import "github.com/golem-mc/golem/registry"

// redstoneOreBlocks marks the two ores that light on touch.
var redstoneOreBlocks = blockSet(registry.BlockRedstoneOre, registry.BlockDeepslateRedstoneOre)

// writeRedstoneOreLit writes LIT with a flag-3 block write.
func (p *Physics) writeRedstoneOreLit(x, y, z int32, state uint16, lit bool, changes *[]BlockChange) {
	if registry.StateLit(state) == lit {
		return
	}
	if id, ok := registry.WithBool(state, registry.PropLit, lit); ok {
		p.setShaped(x, y, z, id, changes)
	}
}

// redstoneOreRandomTick unlights a lit ore.
// vanilla: RedStoneOreBlock.randomTick
func (p *Physics) redstoneOreRandomTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	p.writeRedstoneOreLit(x, y, z, state, false, changes)
}

// TouchRedstoneOre lights redstone ore at the cell, if any, and returns the
// change; a click, hit or step comes here.
// vanilla: RedStoneOreBlock.interact
func (p *Physics) TouchRedstoneOre(x, y, z int32) []BlockChange {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.w.BlockAt(x, y, z)
	if !redstoneOreBlocks[registry.BlockOf(state)] {
		return nil
	}
	var changes []BlockChange
	p.writeRedstoneOreLit(x, y, z, state, true, &changes)
	return changes
}
