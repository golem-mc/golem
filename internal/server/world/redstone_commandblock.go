package world

import (
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// Command blocks: impulse runs on a rising edge, repeating every tick while
// powered or always active, chain from the walk of the block behind. The visits
// live in the scheduled queue; the run is the server's (it owns the command
// tree), so they report themselves like dispenses.
// vanilla: CommandBlock

// commandBlockDelay is the ticks from a rise to its visit, and between a
// repeating block's visits.
// vanilla: CommandBlock.tick, CommandBlockEntity.scheduleTick
const commandBlockDelay = 1

// commandBlockPowerChanged writes the block entity's powered flag in every
// mode, and a rise books the visit only in the redstone modes; always-active
// loops and chain walks book their own.
// vanilla: CommandBlock.neighborChanged, setPoweredAndUpdate
func (p *Physics) commandBlockPowerChanged(x, y, z int32, block registry.Block, state uint16) {
	be := p.w.BlockEntity(x, y, z)
	if be == nil || be.Kind != KindCommandBlock {
		return
	}
	var book bool
	be.Use(func(b *BlockEntity) {
		powered := p.hasNeighborSignal(x, y, z)
		if b.Powered == powered {
			return
		}
		b.Powered = powered
		book = powered && !b.Automatic && block != registry.BlockChainCommandBlock
	})
	if book {
		p.commandBlockMark(x, y, z, state)
	}
}

// commandBlockMark records the condition on the block entity and books the
// visit a tick out.
// vanilla: CommandBlockEntity.markConditionMet, CommandBlockEntity.scheduleTick
func (p *Physics) commandBlockMark(x, y, z int32, state uint16) {
	met := p.commandConditionMet(x, y, z, state)
	if be := p.w.BlockEntity(x, y, z); be != nil && be.Kind == KindCommandBlock {
		be.Use(func(b *BlockEntity) { b.ConditionMet = met })
	}
	p.schedule(x, y, z, p.now+commandBlockDelay)
}

// commandConditionMet is true for an unconditional block, else whether the
// command block behind it (opposite its facing) last succeeded. Entity locks
// are taken one at a time, so two conditional blocks facing each other cannot
// deadlock.
// vanilla: CommandBlockEntity.markConditionMet
func (p *Physics) commandConditionMet(x, y, z int32, state uint16) bool {
	if !registry.StateBool(state, registry.PropConditional) {
		return true
	}
	back := blocklogic.Opposite(facingOfState(state))
	bx, by, bz := x+dirDelta[back][0], y+dirDelta[back][1], z+dirDelta[back][2]
	if !isCommandBlock[registry.BlockOf(p.w.BlockAt(bx, by, bz))] {
		return false
	}
	be := p.w.BlockEntity(bx, by, bz)
	if be == nil || be.Kind != KindCommandBlock {
		return false
	}
	met := false
	be.Use(func(b *BlockEntity) { met = b.SuccessCount > 0 })
	return met
}

// commandBlockTick is CommandBlock.tick without the run: chain mode does
// nothing, repeating re-marks and books the next visit, and the run happens if
// the booked condition held; a failed conditional zeroes the success count a
// comparator may read. The comparators are woken last.
// vanilla: CommandBlock.tick
func (p *Physics) commandBlockTick(x, y, z int32, state uint16) {
	block := registry.BlockOf(state)
	be := p.w.BlockEntity(x, y, z)
	if be == nil || be.Kind != KindCommandBlock {
		return
	}
	// A tick booked under another mode does nothing in chain mode:
	// CommandBlock.tick handles only AUTO and REDSTONE, then wakes the
	// comparators.
	if block == registry.BlockChainCommandBlock {
		p.analogOutputChanged(x, y, z, p.now)
		return
	}
	met := p.commandConditionMet(x, y, z, state)
	var run, reschedule bool
	be.Use(func(b *BlockEntity) {
		wasMet := b.ConditionMet
		if block == registry.BlockRepeatingCommandBlock {
			b.ConditionMet = met
			reschedule = b.Powered || b.Automatic
		}
		if wasMet {
			run = true
		} else if registry.StateBool(state, registry.PropConditional) {
			b.SuccessCount = 0
		}
	})
	if run {
		p.commandRuns = append(p.commandRuns, CommandRun{X: x, Y: y, Z: z})
	}
	if reschedule {
		p.schedule(x, y, z, p.now+commandBlockDelay)
	}
	p.analogOutputChanged(x, y, z, p.now)
}

// CommandConditionMet is the condition check for the server's chain walk and
// programming packet, which run outside the engine's tick.
// vanilla: CommandBlockEntity.markConditionMet
func (p *Physics) CommandConditionMet(x, y, z int32, state uint16) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.commandConditionMet(x, y, z, state)
}

// ScheduleCommandBlock marks the condition and books the visit a programming
// change or placement owes: always-active switched on, mode switched to
// repeating, or a rise the placement read.
// vanilla: CommandBlockEntity.scheduleTick
func (p *Physics) ScheduleCommandBlock(x, y, z int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.w.BlockAt(x, y, z)
	if !isCommandBlock[registry.BlockOf(state)] {
		return
	}
	p.commandBlockMark(x, y, z, state)
}
