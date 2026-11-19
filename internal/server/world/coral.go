package world

import (
	"strings"

	"github.com/golem-mc/golem/registry"
)

const (
	// coralDieDelayBase and coralDieDelayDice give the die tick's span:
	// 60 + nextInt(40) game ticks.
	// vanilla: BaseCoralPlantTypeBlock.tryScheduleDieTick
	coralDieDelayBase = 60
	coralDieDelayDice = 40

	// coralTickEvent tags a living coral's die tick: the survival and leaf bits
	// together, since every single high bit is taken and piston events use the
	// low five.
	coralTickEvent = 1<<5 | 1<<6
)

var coralBlocks = registry.BlockTagOf("minecraft:coral_blocks")

// isLivingCoral reports a living coral plant, fan, wall fan or block by the
// tags in force, so a data pack's coral tags reach it.
func isLivingCoral(b registry.Block) bool {
	return b.InTag(registry.TagIDCorals) || b.InTag(registry.TagIDWallCorals) || b.InTag(registry.TagIDCoralBlocks)
}

// deadCoralTwins maps living coral to its dead block; absent twins stay unchanged.
var deadCoralTwins = func() (twins [registry.BlockCount]registry.Block) {
	for b := range registry.Block(registry.BlockCount) {
		if dead, ok := registry.BlockByName("minecraft:dead_" + strings.TrimPrefix(b.Name(), "minecraft:")); ok {
			twins[b] = dead
		}
	}
	return twins
}()

// coralHasWater reports whether a coral keeps living: plants and fans by their
// own waterlogging, the block by its six neighbours.
// vanilla: BaseCoralPlantTypeBlock.scanForWater, CoralBlock.scanForWater
func (p *Physics) coralHasWater(x, y, z int32, state uint16) bool {
	if !registry.BlockOf(state).InTag(coralBlocks) &&
		registry.StateBool(state, registry.PropWaterlogged) {
		return true
	}
	for _, d := range dirDelta {
		if HoldsWater(p.w.BlockAt(x+d[0], y+d[1], z+d[2])) {
			return true
		}
	}
	return false
}

// scheduleCoralDie books a dry living coral's die tick, one per position.
// vanilla: BaseCoralPlantTypeBlock.tryScheduleDieTick, CoralBlock.updateShape
func (p *Physics) scheduleCoralDie(x, y, z int32, state uint16) {
	if p.coralHasWater(x, y, z, state) {
		return
	}
	p.scheduleBooked(p.coralBooked, x, y, z, p.now+coralDieDelayBase+int64(p.rng.Intn(coralDieDelayDice)), coralTickEvent)
}

// coralTick replaces a still-dry coral with its dead twin, dry and facing as it
// was, flag 2.
// vanilla: CoralPlantBlock.tick, CoralWallFanBlock.tick, CoralBlock.tick
func (p *Physics) coralTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	b := registry.BlockOf(state)
	if !isLivingCoral(b) || p.coralHasWater(x, y, z, state) {
		return
	}
	dead := deadCoralTwins[b]
	if dead == registry.BlockAir {
		return
	}
	next := dead.DefaultState()
	if facing := registry.StateValue(state, registry.PropFacing); facing != "" {
		next, _ = registry.WithProp(next, registry.PropFacing, facing)
	}
	if !b.InTag(coralBlocks) {
		next, _ = registry.WithBool(next, registry.PropWaterlogged, false)
	}
	if next, ok := p.veto(HookFade, x, y, z, next, [3]int32{}); ok {
		p.setClientShaped(x, y, z, next, changes)
	}
}
