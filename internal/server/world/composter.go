package world

import "github.com/golem-mc/golem/registry"

// composterReadyDelay is the 20 ticks a full composter takes to turn ready.
const composterReadyDelay = 20

// composterOnPlace books the ready tick of a composter that arrives at
// level 7. vanilla: ComposterBlock.onPlace
func (p *Physics) composterOnPlace(x, y, z int32, state uint16) {
	if registry.StateValue(state, registry.PropLevel) == "7" {
		p.schedulePriority(x, y, z, p.now+composterReadyDelay, tickPriorityNormal)
	}
}

// composterTick turns a level-7 composter ready (level 8) with the ready
// sound. vanilla: ComposterBlock.tick
func (p *Physics) composterTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	if registry.StateValue(state, registry.PropLevel) != "7" {
		return
	}
	old := p.w.BlockAt(x, y, z)
	if next, ok := registry.WithInt(state, registry.PropLevel, 8); ok {
		p.setClient(x, y, z, next, changes)
		p.neighborUpdate(x, y, z, old, p.w.BlockAt(x, y, z), changes)
		p.shapeUpdateNeighbors(x, y, z, changes)
		p.effects = append(p.effects, Effect{SoundID: registry.SoundBlockComposterReady, X: x, Y: y, Z: z, Volume: 1, Pitch: 1})
	}
}
