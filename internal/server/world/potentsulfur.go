package world

import (
	"math"

	"github.com/golem-mc/golem/registry"
)

// PotentSulfurState is what a potent sulfur block's state must be given the
// cells around it. reset is true when the block is no geyser yet sits over a
// periodic one, so its countdown starts over.
// vanilla: PotentSulfurBlock.validBlockState
func PotentSulfurState(get func(x, y, z int32) uint16, state uint16, x, y, z int32) (next uint16, reset bool) {
	value := "dry"
	if above := fluidHolding(get(x, y+1, z)); above.kind == fluidWater && above.isSource() {
		below := get(x, y-1, z)
		fluid := fluidHolding(below)
		value = "wet"
		if fluid.kind == fluidNone || fluid.isSource() {
			cur := registry.StateValue(state, potentSulfurState)
			switch {
			case registry.BlockOf(below).InTag(continuousGeyser):
				value = "continuous"
			case registry.BlockOf(below).InTag(periodicGeyser):
				reset = cur != "erupting" && cur != "dormant"
				value = "dormant"
				if cur == "erupting" {
					value = "erupting"
				}
			}
		}
	}
	next, _ = registry.WithProp(state, potentSulfurState, value)
	return next, reset
}

// PotentSulfurErupts reports whether a state is a geyser in eruption.
func PotentSulfurErupts(state uint16) bool {
	v := registry.StateValue(state, potentSulfurState)
	return v == "erupting" || v == "continuous"
}

// PotentSulfurGeyserStart is the block event, sound and game event of a block
// that became a geyser in eruption.
// vanilla: PotentSulfurBlock.onPlace
func PotentSulfurGeyserStart(state uint16, x, y, z int32) Effect {
	sound := registry.SoundBlockPotentSulfurGeyserEruption
	if registry.StateValue(state, potentSulfurState) == "continuous" {
		sound = registry.SoundBlockPotentSulfurGeyserContinuousEruption
	}
	return Effect{
		SoundID: sound, X: x, Y: y, Z: z, Volume: 1, Pitch: 1,
		GameEvent:      registry.GameEventBlockActivate,
		HasBlockAction: true, BlockActionBlock: int32(registry.BlockIDs["minecraft:potent_sulfur"]),
	}
}

// potentSulfurShapeUpdate is PotentSulfurBlock.updateShape: any neighbour
// change rederives the state.
func (p *Physics) potentSulfurShapeUpdate(x, y, z int32, state uint16, changes *[]BlockChange) {
	next, reset := PotentSulfurState(p.w.BlockAt, state, x, y, z)
	if reset {
		if be := p.w.BlockEntity(x, y, z); be != nil {
			be.UseLive(func(b *BlockEntity) { b.Countdown = -1 })
		}
	}
	if next == state {
		return
	}
	if p.setClientShaped(x, y, z, next, changes) && PotentSulfurErupts(next) && !PotentSulfurErupts(state) {
		p.effects = append(p.effects, PotentSulfurGeyserStart(next, x, y, z))
	}
}

// potentSulfurReach is how far a gas source reaches a body, in blocks.
// vanilla: PotentSulfurBlockEntity.EFFECT_RANGE
const potentSulfurReach = 3.0

// geyserPassable is a cell gas and a geyser's push pass through.
// vanilla: PotentSulfurBlockEntity.isGeyserPassableBlock
func (w *World) geyserPassable(x, y, z int32) bool {
	state := w.BlockAt(x, y, z)
	if state == airState || registry.BlockOf(state) == registry.BlockWater {
		return true
	}
	return len(CollisionBoxes(state)) == 0
}

// PotentSulfurGasSource is the first cell above the block at (x, y, z), within
// four water blocks, that is no water; ok is false when something solid or too
// much water comes first.
// vanilla: PotentSulfurBlockEntity.findNoxiousGasSourceBlock
func (w *World) PotentSulfurGasSource(x, y, z int32) (sy int32, ok bool) {
	for cy := y + 1; cy <= y+5; cy++ {
		state := w.BlockAt(x, cy, z)
		f := fluidHolding(state)
		water := f.kind == fluidWater && f.isSource()
		if !water || registry.BlockOf(state) != registry.BlockWater && !w.geyserPassable(x, cy, z) {
			if state != airState && !w.geyserPassable(x, cy, z) {
				return 0, false
			}
			return cy, true
		}
	}
	return 0, false
}

// PotentSulfurLaunchHeight is how many cells above the block a geyser pushes:
// 6 per water block, up to the first obstruction.
// vanilla: PotentSulfurBlockEntity.getUnobstructedBlockCount
func (w *World) PotentSulfurLaunchHeight(x, y, z int32, waterBlocks int) int {
	height := 6 * waterBlocks
	for i := range height {
		if !w.geyserPassable(x, y+1+int32(i), z) {
			return i
		}
	}
	return height
}

// PotentSulfurGasReaches reports whether gas from the source cell reaches an
// eye position.
// vanilla: PotentSulfurBlockEntity.canBeReachedByNoxiousGas
func (w *World) PotentSulfurGasReaches(sx, sy, sz int32, ex, ey, ez float64) bool {
	bx, by, bz := int32(math.Floor(ex)), int32(math.Floor(ey)), int32(math.Floor(ez))
	if !w.geyserPassable(bx, by, bz) {
		return false
	}
	dx, dy, dz := ex-(float64(sx)+0.5), ey-(float64(sy)+0.5), ez-(float64(sz)+0.5)
	if dx*dx+dy*dy+dz*dz > potentSulfurReach*potentSulfurReach {
		return false
	}
	wy := ey - 1
	f := fluidHolding(w.BlockAt(bx, int32(math.Floor(wy)), bz))
	if f.kind != fluidWater || !f.isSource() {
		return false
	}
	// Line of sight from the cell below the source's centre, over colliders.
	fx, fy, fz := float64(sx)+0.5, float64(sy)-0.5, float64(sz)+0.5
	rx, ry, rz := ex-fx, wy-fy, ez-fz
	blocked := TraverseBlocks(fx, fy, fz, rx, ry, rz, func(x, y, z, _ int32) bool {
		for _, b := range CollisionBoxes(w.BlockAt(x, y, z)) {
			if _, _, ok := SegmentAABB(fx, fy, fz, rx, ry, rz,
				float64(x)+b.MinX, float64(y)+b.MinY, float64(z)+b.MinZ,
				float64(x)+b.MaxX, float64(y)+b.MaxY, float64(z)+b.MaxZ); ok {
				return true
			}
		}
		return false
	})
	return !blocked
}

// PotentSulfurStateProp is the block's potent_sulfur_state property.
var PotentSulfurStateProp = potentSulfurState
