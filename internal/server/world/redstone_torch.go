package world

import (
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// torchShouldBeLit reads the signal a standing torch's support delivers up, or
// a wall torch's wall block.
// vanilla: RedstoneTorchBlock.hasNeighborSignal
func (p *Physics) torchShouldBeLit(x, y, z int32, block registry.Block, state uint16) bool {
	if block == registry.BlockRedstoneWallTorch {
		f := facingOfState(state)
		bx, by, bz := x+dirDelta[blocklogic.Opposite(f)][0], y+dirDelta[blocklogic.Opposite(f)][1], z+dirDelta[blocklogic.Opposite(f)][2]
		return p.signalThrough(bx, by, bz, blocklogic.Opposite(blocklogic.Opposite(f))) > 0
	}
	return p.signalAt(x, y, z, dirDown) > 0
}

// torchTooFrequent reports a burn-out: 8 toggles inside 60 ticks (vanilla
// RECENT_TOGGLES), recording now if record. The toggles live on the engine, not
// a package map, so parallel worlds never share them.
func (p *Physics) torchTooFrequent(x, y, z int32, now int64, record bool) bool {
	key := blockKey{x, y, z}
	list := p.torchToggles[key]
	if record {
		list = append(list, now)
		p.torchToggles[key] = list
	}
	return len(list) >= 8
}

// pruneTorchToggles drops toggles older than 60 ticks, before either branch of
// the tick reads them, so a burnt-out torch can relight.
// vanilla: RedstoneTorchBlock.tick
func (p *Physics) pruneTorchToggles(x, y, z int32, now int64) {
	key := blockKey{x, y, z}
	list := p.torchToggles[key]
	for len(list) > 0 && now-list[0] > 60 {
		list = list[1:]
	}
	if len(list) == 0 {
		delete(p.torchToggles, key)
		return
	}
	p.torchToggles[key] = list
}

// torchTick turns a lit torch that should be off off, counting the toggle
// toward burn-out; a burnt-out torch waits 160 ticks; an unlit torch that
// should be lit lights.
// vanilla: RedstoneTorchBlock.tick
func (p *Physics) torchTick(x, y, z int32, block registry.Block, state uint16, changes *[]BlockChange, effects *[]Effect) {
	p.pruneTorchToggles(x, y, z, p.now)
	should := p.torchShouldBeLit(x, y, z, block, state)
	lit := registry.StateLit(state)
	// A flag-3 write: the wire beside re-reads, and the second ring reaches
	// what the block above fed.
	wake := func() { p.redstoneWakeAround(x, y, z, changes) }
	if lit && should {
		if p.redstonePower(x, y, z, 15, 0) > 0 {
			return
		}
		off, _ := registry.WithBool(state, registry.PropLit, false)
		p.setClientShaped(x, y, z, off, changes)
		wake()
		if p.torchTooFrequent(x, y, z, p.now, true) {
			*effects = append(*effects, Effect{LevelEvent: 1502, X: x, Y: y, Z: z})
			p.schedulePriority(x, y, z, p.now+torchBurnoutFuse, tickPriorityNormal)
		}
		return
	}
	if !lit && !should && !p.torchTooFrequent(x, y, z, p.now, false) {
		if p.redstonePower(x, y, z, 0, 15) == 0 {
			return
		}
		on, _ := registry.WithBool(state, registry.PropLit, true)
		p.setClientShaped(x, y, z, on, changes)
		wake()
	}
}

// redstoneWakeAround wakes a torch's cell and the six neighbours of each
// neighbour; the second ring reaches a lamp on the block the torch strongly
// powers.
// vanilla: RedstoneTorchBlock.notifyNeighbors
func (p *Physics) redstoneWakeAround(x, y, z int32, changes *[]BlockChange) {
	p.wireWakeNeighbors(x, y, z, changes)
	for _, d := range updateNeighborOrder {
		p.wireWakeNeighbors(x+dirDelta[d][0], y+dirDelta[d][1], z+dirDelta[d][2], changes)
	}
}
