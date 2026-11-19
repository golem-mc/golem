package world

import "github.com/golem-mc/golem/registry"

// The engine actions the server's use path and entity pass call.

// UseBlock runs the use reaction of the cell's block and reports whether there
// was one; false sends the caller on to whatever else the click means. facing
// is the player's horizontal look direction, empty when nobody stands there (a
// fence gate swings away from it). The primes are TNT the click lit, for the
// caller to spawn.
func (p *Physics) UseBlock(x, y, z int32, facing string) (bool, []BlockChange, []Effect, []TNTPrime) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.w.BlockAt(x, y, z)
	block := registry.BlockOf(state)
	var changes []BlockChange
	var used bool
	switch {
	case block == registry.BlockLever:
		p.pullLever(x, y, z, state, true, &changes, &p.effects)
		used = true
	case isButton[block]:
		if !registry.StatePowered(state) {
			p.pressButton(x, y, z, block, state, true, &changes, &p.effects)
			used = true
		} else {
			// A pressed button consumes the click without acting
			// (InteractionResult.CONSUME).
			used = true
		}
	case block == registry.BlockRepeater:
		p.cycleRepeater(x, y, z, state, &changes)
		used = true
	case block == registry.BlockComparator:
		p.comparatorUsed(x, y, z, state, &changes, &p.effects)
		used = true
	case isDaylightDetector[block]:
		p.daylightDetectorUsed(x, y, z, state, &changes)
		used = true
	case block == registry.BlockNoteBlock:
		p.noteBlockUsed(x, y, z, state, &changes)
		used = true
	case isWire[block]:
		if wireIsCross(state) || wireIsDot(state) {
			used = p.clickWire(x, y, z, state, &changes)
		}
	case isDoorLike[block]:
		p.toggleDoor(x, y, z, block, facing, state, &changes, &p.effects)
		used = doorOpensByHand(block)
	}
	effects, primes := p.takeWake()
	return used, changes, effects, primes
}

// ExplosionTrigger is a wind charge's burst at the cell: a lever pulls, an
// unpowered button presses, an unpowered non-iron door (lower half), trapdoor
// or fence gate swings, a lit candle or candle cake goes out, a bell rings
// toward its facing.
// vanilla: LeverBlock.onExplosionHit, ButtonBlock.onExplosionHit,
// DoorBlock.onExplosionHit, TrapDoorBlock.onExplosionHit,
// FenceGateBlock.onExplosionHit, AbstractCandleBlock.onExplosionHit,
// BellBlock.onExplosionHit
func (p *Physics) ExplosionTrigger(x, y, z int32) ([]BlockChange, []Effect, []TNTPrime) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.w.BlockAt(x, y, z)
	block := registry.BlockOf(state)
	var changes []BlockChange
	switch {
	case block == registry.BlockLever:
		p.pullLever(x, y, z, state, false, &changes, &p.effects)
	case isButton[block]:
		if !registry.StatePowered(state) {
			p.pressButton(x, y, z, block, state, false, &changes, &p.effects)
		}
	case isDoorLike[block]:
		if !registry.StatePowered(state) && registry.StateValue(state, registry.PropHalf) != "upper" {
			p.toggleDoor(x, y, z, block, "", state, &changes, &p.effects)
		}
	case block.InTag(registry.TagIDCandles) || block.InTag(registry.TagIDCandleCakes):
		if registry.StateBool(state, registry.PropLit) {
			next, _ := registry.WithBool(state, registry.PropLit, false)
			p.setShaped(x, y, z, next, &changes)
			p.effects = append(p.effects, Effect{
				SoundID: registry.SoundBlockCandleExtinguish, GameEvent: gameEventBlockChange, X: x, Y: y, Z: z, Volume: 1, Pitch: 1,
			})
		}
	case block == registry.BlockBell:
		p.ringBell(x, y, z, facingOfState(state))
	}
	effects, primes := p.takeWake()
	return changes, effects, primes
}
