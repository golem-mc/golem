package world

import (
	"github.com/golem-mc/golem/registry"
)

// Weathering copper, aged by ChangeOverTimeBlock on random ticks: each stage is
// the same shape one oxidation step on, every property kept. Only a 0.05688889
// roll asks the neighbourhood (the family within Manhattan distance 4): a less
// oxidised member blocks the change, otherwise the chance is ((more + 1) /
// (more + same + 1))², times 0.75 for the unaffected stage.
// vanilla: ChangeOverTimeBlock.changeOverTime, getNextState
// vanilla: WeatheringCopper.getChanceModifier

const (
	// copperChangeChance is the share of random ticks that ask the
	// neighbourhood at all.
	// vanilla: ChangeOverTimeBlock.changeOverTime
	copperChangeChance = 0.05688889

	// copperUnaffectedChance is the unaffected stage's multiplier.
	// vanilla: WeatheringCopper.getChanceModifier
	copperUnaffectedChance = 0.75

	// copperScanDistance is the neighbourhood scan's Manhattan reach.
	// vanilla: ChangeOverTimeBlock.SCAN_DISTANCE
	copperScanDistance = 4
)

// copperNext is WeatheringCopper.NEXT_BY_BLOCK, inverted from the registry's
// scraped PREVIOUS_BY_BLOCK (the two halves of vanilla's one BiMap). Only the
// three stages before oxidized have a next; waxed blocks, which vanilla never
// ages, carry BlockAir.
var copperNext = func() *[registry.BlockCount]registry.Block {
	next := new([registry.BlockCount]registry.Block)
	for more := range registry.Block(registry.BlockCount) {
		if less, ok := more.Scraped(); ok {
			next[less] = more
		}
	}
	return next
}()

// copperNoStage marks a block outside the weathering family in copperStages.
const copperNoStage = -1

// copperStages numbers the family's blocks by stage, 0 unaffected to 3
// oxidized, for both a block's own verdict and its neighbours'; copperNoStage
// marks the rest. The oxidized block ends the chain but still counts in a
// neighbour's scan.
var copperStages = func() *[registry.BlockCount]int8 {
	stages := new([registry.BlockCount]int8)
	for b := range stages {
		stages[b] = copperNoStage
	}
	for more := range registry.Block(registry.BlockCount) {
		less, ok := more.Scraped()
		if !ok {
			continue
		}
		stage := int8(1)
		for previous, ok := less.Scraped(); ok; previous, ok = previous.Scraped() {
			stage++
		}
		stages[more], stages[less] = stage, stage-1
	}
	return stages
}()

// copperStageOf is the state's copperStages entry.
func copperStageOf(state uint16) int8 {
	if int(state) >= registry.StateCount {
		return copperNoStage
	}
	return copperStages[registry.BlockOf(state)]
}

// mustBlock is the named block; an unknown name is a table typo.
func mustBlock(name string) registry.Block {
	b, ok := registry.BlockByName(name)
	if !ok {
		panic("world: unknown block " + name)
	}
	return b
}

// copperRolls reports whether a state spends its own die: doors' upper halves
// and chests' right halves follow their partner's shape update, though
// isRandomlyTicking still counts them.
// vanilla: WeatheringCopperDoorBlock.randomTick, WeatheringCopperChestBlock.randomTick
func copperRolls(state uint16) bool {
	// Only the doors carry half=upper; a trapdoor's half is top or bottom.
	if registry.StateValue(state, registry.PropHalf) == "upper" {
		return false
	}
	if registry.BlockOf(state).InTag(registry.TagIDCopperChests) && registry.StateValue(state, registry.PropType) == "right" {
		return false
	}
	return true
}

// CopperChestViewers answers whether anyone has the copper chest at a cell open
// (the ChestBlockEntity opener count's zero test). The server implements it;
// tests can stub it.
type CopperChestViewers interface {
	// ChestViewed reports whether a player has the container at the cell open.
	ChestViewed(x, y, z int32) bool
}

// SetCopperChestViewers sets the player source; without one nobody is ever
// looking in, and open chests weather.
func (p *Physics) SetCopperChestViewers(v CopperChestViewers) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.copperChestViewers = v
}

func (p *Physics) copperChestViewed(x, y, z int32) bool {
	if p.copperChestViewers == nil {
		return false
	}
	return p.copperChestViewers.ChestViewed(x, y, z)
}

// copperOxidationTick: only the lower door half and left chest half roll (the
// others follow by shape update), and an open copper chest holds its stage. The
// changeOverTime gate, then the neighbourhood's verdict, then the stage swap
// with every property kept, and the shape update that carries a paired half
// along.
// vanilla: ChangeOverTimeBlock.changeOverTime, getNextState
func (p *Physics) copperOxidationTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	if !copperRolls(state) {
		return
	}
	b := registry.BlockOf(state)
	if b.InTag(registry.TagIDCopperChests) && p.copperChestViewed(x, y, z) {
		return
	}
	if p.rng.Float32() >= copperChangeChance {
		return
	}
	stage := copperStages[b]
	more, same := 0, 0
	for dx := int32(-copperScanDistance); dx <= copperScanDistance; dx++ {
		for dy := -copperScanDistance + absI32(dx); dy <= copperScanDistance-absI32(dx); dy++ {
			for dz := -copperScanDistance + absI32(dx) + absI32(dy); dz <= copperScanDistance-absI32(dx)-absI32(dy); dz++ {
				if dx == 0 && dy == 0 && dz == 0 {
					continue
				}
				other := copperStageOf(p.w.BlockAt(x+dx, y+dy, z+dz))
				if other == copperNoStage {
					continue
				}
				switch {
				case other < stage:
					return // one younger neighbour vetoes the change
				case other > stage:
					more++
				default:
					same++
				}
			}
		}
	}
	chance := float32(more+1) / float32(more+same+1)
	chance *= chance
	if stage == 0 {
		chance *= copperUnaffectedChance
	}
	if p.rng.Float32() >= chance {
		return
	}
	next, ok := registry.WithPropertiesOf(copperNext[b], state)
	if !ok {
		return
	}
	p.set(x, y, z, next, changes)
	// The flags-3 shape pass carries a door's other half and a chest's partner
	// along.
	p.shapeUpdateNeighbors(x, y, z, changes)
}

func absI32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
