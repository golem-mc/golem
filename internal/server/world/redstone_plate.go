package world

import "github.com/golem-mc/golem/registry"

// plateKind names what a pressure plate counts: everything, or living things
// only (stone).
func plateKind(block registry.Block) string {
	if block == registry.BlockStonePressurePlate || block == registry.BlockPolishedBlackstonePressurePlate {
		return "mobs"
	}
	return "all"
}

// plateMaxWeight is a weighted plate's entity budget
// (WeightedPressurePlateBlock.maxWeight), zero for an ordinary plate.
func plateMaxWeight(block registry.Block) int {
	switch block {
	case registry.BlockLightWeightedPressurePlate:
		return 15
	case registry.BlockHeavyWeightedPressurePlate:
		return 150
	}
	return 0
}

// plateIsWeighted reports whether the signal follows the entity count rather
// than a flat 15.
func plateIsWeighted(block registry.Block) bool { return plateMaxWeight(block) > 0 }

// plateTick has a pressed plate check what is on it, pressing or releasing, and
// look again 20 ticks out while anything is.
// vanilla: BasePressurePlateBlock.tick
func (p *Physics) plateTick(x, y, z int32, block registry.Block, state uint16, changes *[]BlockChange, effects *[]Effect) {
	signal := plateCurrentSignal(block, state)
	if signal == 0 {
		return
	}
	p.plateCheckPressed(x, y, z, block, signal, changes, effects)
}

// plateCheckPressed compares the new signal with the current one, writes the
// state client-only, wakes its cell and the one below (vanilla's
// updateNeighbours) and clicks.
// vanilla: checkPressed
func (p *Physics) plateCheckPressed(x, y, z int32, block registry.Block, currentSignal int, changes *[]BlockChange, effects *[]Effect) {
	newSignal := p.plateSignalFor(x, y, z, block)
	if newSignal == currentSignal {
		if newSignal > 0 {
			p.schedulePriority(x, y, z, p.now+plateDelay(block), tickPriorityNormal)
		}
		return
	}
	p.setClientShaped(x, y, z, plateWrite(p.w.BlockAt(x, y, z), block, newSignal), changes)
	if (newSignal > 0) != (currentSignal > 0) {
		e := plateEffect(block, newSignal > 0, x, y, z)
		if newSignal > 0 && p.toucher != nil {
			if src := p.toucher.Presser(x, y, z); src != (EventSource{}) {
				e.GameEventSource = &src
			}
		}
		*effects = append(*effects, e)
	}
	p.plateUpdateNeighbours(x, y, z, changes)
	if newSignal > 0 {
		p.schedulePriority(x, y, z, p.now+plateDelay(block), tickPriorityNormal)
	}
}

// plateDelay is the look-again delay: 20 ticks for ordinary plates, 10 for
// weighted.
// vanilla: getPressedTime
func plateDelay(block registry.Block) int64 {
	if plateIsWeighted(block) {
		return weightedPlateTickDelay
	}
	return plateTickDelay
}

func (p *Physics) plateUpdateNeighbours(x, y, z int32, changes *[]BlockChange) {
	p.redstoneNeighborChanged(x, y, z, p.w.BlockAt(x, y, z), changes, false)
	for _, d := range updateNeighborOrder {
		nx, ny, nz := x+dirDelta[d][0], y+dirDelta[d][1], z+dirDelta[d][2]
		p.redstoneNeighborChanged(nx, ny, nz, p.w.BlockAt(nx, ny, nz), changes, false)
	}
	bx, by, bz := x, y-1, z
	p.redstoneNeighborChanged(bx, by, bz, p.w.BlockAt(bx, by, bz), changes, false)
	for _, d := range updateNeighborOrder {
		nx, ny, nz := bx+dirDelta[d][0], by+dirDelta[d][1], bz+dirDelta[d][2]
		p.redstoneNeighborChanged(nx, ny, nz, p.w.BlockAt(nx, ny, nz), changes, false)
	}
}

// plateSignalFor turns what stands on the plate into a signal: 15 for ordinary
// plates, the weighted ones' share of their budget (light 15, heavy 150).
// vanilla: WeightedPressurePlateBlock.getSignalStrength
func (p *Physics) plateSignalFor(x, y, z int32, block registry.Block) int {
	living, all := p.touchTouching(x, y, z)
	count := all
	if plateKind(block) == "mobs" {
		count = living
	}
	if count == 0 {
		return 0
	}
	if budget := plateMaxWeight(block); budget > 0 {
		count = min(count, budget)
		// ceil(count / budget * 15), the vanilla float math in integer form.
		return (count*15 + budget - 1) / budget
	}
	return 15
}

// plateEffect is the BlockSetType's pressurePlateClickOn/Off at volume and
// pitch 1.
// vanilla: BasePressurePlateBlock.checkPressed
func plateEffect(block registry.Block, on bool, x, y, z int32) Effect {
	return Effect{SoundID: clickSound(plateSounds[block], on), X: x, Y: y, Z: z, Volume: 1, Pitch: 1, GameEvent: activateGameEvent(on)}
}

// plateCurrentSignal reads the signal a plate's state carries (vanilla
// getSignalForState): POWERED for ordinary plates, POWER for weighted.
func plateCurrentSignal(block registry.Block, state uint16) int {
	if plateIsWeighted(block) {
		return registry.StatePower(state)
	}
	if registry.StatePowered(state) {
		return 15
	}
	return 0
}

// plateWrite writes the signal into the state, POWERED or POWER as for
// plateCurrentSignal.
func plateWrite(state uint16, block registry.Block, signal int) uint16 {
	if plateIsWeighted(block) {
		state, _ = registry.WithInt(state, registry.PropPower, signal)
		return state
	}
	state, _ = registry.WithBool(state, registry.PropPowered, signal > 0)
	return state
}

// vanilla: BlockSetType.pressurePlateClickOn, BlockSetType.pressurePlateClickOff
var plateSounds = func() [registry.BlockCount][2]registry.Sound {
	sounds := [registry.BlockCount][2]registry.Sound{
		registry.BlockLightWeightedPressurePlate:      {registry.SoundBlockMetalPressurePlateClickOff, registry.SoundBlockMetalPressurePlateClickOn},
		registry.BlockHeavyWeightedPressurePlate:      {registry.SoundBlockMetalPressurePlateClickOff, registry.SoundBlockMetalPressurePlateClickOn},
		registry.BlockStonePressurePlate:              {registry.SoundBlockStonePressurePlateClickOff, registry.SoundBlockStonePressurePlateClickOn},
		registry.BlockPolishedBlackstonePressurePlate: {registry.SoundBlockStonePressurePlateClickOff, registry.SoundBlockStonePressurePlateClickOn},
		registry.BlockCherryPressurePlate:             {registry.SoundBlockCherryWoodPressurePlateClickOff, registry.SoundBlockCherryWoodPressurePlateClickOn},
		registry.BlockBambooPressurePlate:             {registry.SoundBlockBambooWoodPressurePlateClickOff, registry.SoundBlockBambooWoodPressurePlateClickOn},
		registry.BlockCrimsonPressurePlate:            {registry.SoundBlockNetherWoodPressurePlateClickOff, registry.SoundBlockNetherWoodPressurePlateClickOn},
		registry.BlockWarpedPressurePlate:             {registry.SoundBlockNetherWoodPressurePlateClickOff, registry.SoundBlockNetherWoodPressurePlateClickOn},
	}
	for block := range sounds {
		if sounds[block][0] == registry.SoundNone && registry.Block(block).InGroup(registry.GroupPressurePlate) {
			sounds[block] = [2]registry.Sound{registry.SoundBlockWoodenPressurePlateClickOff, registry.SoundBlockWoodenPressurePlateClickOn}
		}
	}
	return sounds
}()
