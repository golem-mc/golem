package world

import (
	"github.com/golem-mc/golem/registry"
)

// The dried ghast random-ticks in every state (randomTicks() is per block), its
// pass gating out a block with neither water nor hydration. A landed die only
// books the block's visit (one per block, the hasScheduledTick gate); the visit
// moves it: waterlogged below the top it climbs a step with the transition
// sound, waterlogged at the top it hatches (the cell left as its water, a baby
// happy ghast stood in it), and dry it dries a step silently.
// vanilla: DriedGhastBlock.randomTick, DriedGhastBlock.tick

const (
	// driedGhastMaxHydration is MAX_HYDRATION_LEVEL, the stage that hatches.
	driedGhastMaxHydration = 3

	// driedGhastHydrationDelay is HYDRATION_TICK_DELAY, the ticks from booking
	// to visit.
	driedGhastHydrationDelay = 5000

	// driedGhastHydrationEvent tags the ghast's own queue entry so a plain
	// visit (the waterlogged cell's fluid visit, a fall, a fire) is not
	// mistaken for it; piston events use the low five bits.
	driedGhastHydrationEvent = 1 << 7
)

// GhastlingSpawn is a hatched dried ghast: the cell to spawn the baby happy
// ghast in (its bottom centre) and the block's facing, whose yaw body and head
// take. The server owns the body.
// vanilla: DriedGhastBlock.spawnGhastling
type GhastlingSpawn struct {
	X, Y, Z int32
	Facing  string
}

// TakeGhastlingSpawns returns and clears the hatches.
func (p *Physics) TakeGhastlingSpawns() []GhastlingSpawn {
	out := p.ghastlings
	p.ghastlings = nil
	return out
}

// driedGhastRandomTick books the visit while it has something to do (water to
// climb with or hydration to dry); the visit waits 5000 ticks.
// vanilla: DriedGhastBlock.randomTick
func (p *Physics) driedGhastRandomTick(x, y, z int32, state uint16) {
	hydration, ok := registry.StateInt(state, registry.PropHydration)
	if !ok {
		return
	}
	if !registry.StateBool(state, registry.PropWaterlogged) && hydration <= 0 {
		return
	}
	p.scheduleDriedGhast(x, y, z, p.now+driedGhastHydrationDelay)
}

// scheduleDriedGhast books the visit at due, one per position, in the ghast's
// own set so the waterlogged cell's fluid visits are not blocked.
func (p *Physics) scheduleDriedGhast(x, y, z int32, due int64) {
	p.scheduleBooked(p.ghastBooked, x, y, z, due, driedGhastHydrationEvent)
}

// driedGhastTick reads the state as found (it may have changed since booking):
// up while waterlogged, hatching at the top, down without water.
// vanilla: DriedGhastBlock.tick
func (p *Physics) driedGhastTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	if registry.BlockOf(state) != registry.BlockDriedGhast {
		return
	}
	hydration, ok := registry.StateInt(state, registry.PropHydration)
	if !ok {
		return
	}
	if !registry.StateBool(state, registry.PropWaterlogged) {
		if hydration <= 0 {
			return
		}
		if next, ok := registry.WithInt(state, registry.PropHydration, hydration-1); ok {
			p.setClientShaped(x, y, z, next, changes)
		}
		return
	}
	if hydration >= driedGhastMaxHydration {
		// The hatch leaves the held water: removeBlock writes the fluid with
		// flag 3, so neighbours wake and the source books its spread. The
		// server spawns the ghastling.
		// vanilla: Level.removeBlock
		p.setShaped(x, y, z, waterByLevel[0], changes)
		p.ghastlings = append(p.ghastlings, GhastlingSpawn{X: x, Y: y, Z: z, Facing: registry.StateValue(state, registry.PropFacing)})
		return
	}
	// One step up, told aloud before it is taken.
	p.effects = append(p.effects, Effect{
		SoundID: registry.SoundBlockDriedGhastTransition, X: x, Y: y, Z: z, Volume: 1, Pitch: 1,
	})
	if next, ok := registry.WithInt(state, registry.PropHydration, hydration+1); ok {
		p.setClientShaped(x, y, z, next, changes)
	}
}
