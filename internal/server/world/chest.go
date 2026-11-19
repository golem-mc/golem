package world

import (
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// The chests that pair into a double: which half stands where, the shape
// update that pairs and splits them, and the trapped chest's signal.

// IsChestBlock reports whether the block is one of the chests: a wooden chest,
// a trapped chest or a copper chest. The copper chests are read from the live
// tag, so a data pack reload reaches them.
func IsChestBlock(b registry.Block) bool {
	return b == registry.BlockChest || b == registry.BlockTrappedChest || b.InTag(registry.TagIDCopperChests)
}

// ChestsConnect reports whether two chest blocks pair; copper stages may differ.
// vanilla: ChestBlock.chestCanConnectTo
func ChestsConnect(block, partner registry.Block) bool {
	if block.InTag(registry.TagIDCopperChests) {
		return partner.InTag(registry.TagIDCopperChests)
	}
	return block == partner
}

// chestConnectedDir is the side a chest half's partner stands on: clockwise
// of the facing for the left half, counter-clockwise otherwise.
// vanilla: ChestBlock.getConnectedDirection
func chestConnectedDir(state uint16) int {
	if registry.StateValue(state, registry.PropType) == "left" {
		return blocklogic.Clockwise(facingOfState(state))
	}
	return blocklogic.CounterClockwise(facingOfState(state))
}

// ChestPartnerStep is the horizontal step from a chest half to the side its
// partner stands on.
func ChestPartnerStep(state uint16) (dx, dz int32) {
	d := dirDelta[chestConnectedDir(state)]
	return d[0], d[2]
}

// ChestPair returns the two halves of the double chest standing at (x, y,
// z), the right half first, and false where the chest there is a single or
// its partner is not the matching half.
// vanilla: DoubleBlockCombiner.combineWithNeigbour, ChestBlock.getBlockType
func (w *World) ChestPair(x, y, z int32) (first, second [3]int32, ok bool) {
	state := w.BlockAt(x, y, z)
	block := registry.BlockOf(state)
	if !IsChestBlock(block) {
		return first, second, false
	}
	typ := registry.StateValue(state, registry.PropType)
	if typ != "left" && typ != "right" {
		return first, second, false
	}
	dx, dz := ChestPartnerStep(state)
	pstate := w.BlockAt(x+dx, y, z+dz)
	ptyp := registry.StateValue(pstate, registry.PropType)
	if !ChestsConnect(block, registry.BlockOf(pstate)) || ptyp == "single" || ptyp == typ ||
		registry.StateValue(pstate, registry.PropFacing) != registry.StateValue(state, registry.PropFacing) {
		return first, second, false
	}
	here, there := [3]int32{x, y, z}, [3]int32{x + dx, y, z + dz}
	if typ == "right" {
		return here, there, true
	}
	return there, here, true
}

// chestShapeUpdate pairs a single chest with the matching half that just
// formed beside it, and turns a half whose partner went back to a single.
// d is the offset from the block that changed to this chest. It returns the
// new state without writing it, so a copper chest can adopt its partner's block
// before the single write.
// vanilla: ChestBlock.updateShape
func (p *Physics) chestShapeUpdate(x, y, z int32, state uint16, d [3]int32) uint16 {
	toward := blocklogic.Opposite(directionOf(d))
	nstate := p.w.BlockAt(x-d[0], y-d[1], z-d[2])
	cur := registry.StateValue(state, registry.PropType)
	typ := cur
	if ChestsConnect(registry.BlockOf(state), registry.BlockOf(nstate)) && toward != dirUp && toward != dirDown {
		ntyp := registry.StateValue(nstate, registry.PropType)
		if typ == "single" && ntyp != "single" && facingOfState(nstate) == facingOfState(state) &&
			chestConnectedDir(nstate) == blocklogic.Opposite(toward) {
			typ = "left"
			if ntyp == "left" {
				typ = "right"
			}
		}
	} else if chestConnectedDir(state) == toward {
		typ = "single"
	}
	if typ == cur {
		return state
	}
	if next, ok := registry.WithProp(state, registry.PropType, typ); ok {
		return next
	}
	return state
}

// SittingCats answers whether a cat in its sitting pose intersects a box; the
// server owns the mobs and implements it.
type SittingCats interface {
	SittingCatIn(box Box) bool
}

// SetSittingCats sets the mob source for ChestBlockedAt; set once at start-up.
func (w *World) SetSittingCats(c SittingCats) { w.sittingCats = c }

// ChestBlockedAt reports whether a conductor or a sitting cat directly above
// the chest stops its lid; either half blocks a double.
// vanilla: ChestBlock.isChestBlockedAt
func (w *World) ChestBlockedAt(x, y, z int32) bool {
	return w.ChestBlockedAtBy(x, y, z, w.sittingCats)
}

// ChestBlockedAtBy is ChestBlockedAt asking cats about sitting cats, for a
// caller that already holds the lock the registered source takes.
func (w *World) ChestBlockedAtBy(x, y, z int32, cats SittingCats) bool {
	if first, second, ok := w.ChestPair(x, y, z); ok {
		return w.chestBlockedByAbove(first[0], first[1], first[2], cats) ||
			w.chestBlockedByAbove(second[0], second[1], second[2], cats)
	}
	return w.chestBlockedByAbove(x, y, z, cats)
}

// chestBlockedByAbove is isBlockedChestByBlock or isCatSittingOnChest.
func (w *World) chestBlockedByAbove(x, y, z int32, cats SittingCats) bool {
	if w.conductorAbove(x, y, z) {
		return true
	}
	return cats != nil && cats.SittingCatIn(Box{
		MinX: float64(x), MinY: float64(y + 1), MinZ: float64(z),
		MaxX: float64(x + 1), MaxY: float64(y + 2), MaxZ: float64(z + 1)})
}

// EnderChestBlockedAt reports whether a redstone conductor directly above
// the ender chest at (x, y, z) stops it opening.
// vanilla: EnderChestBlock.isChestBlockedAt
func (w *World) EnderChestBlockedAt(x, y, z int32) bool {
	return w.conductorAbove(x, y, z)
}

// conductorAbove is isBlockedChestByBlock: a full, motion-blocking block
// above, which is what isRedstoneConductor takes a solid block to be.
func (w *World) conductorAbove(x, y, z int32) bool {
	return w.ConductorAt(x, y+1, z)
}

// ConductorAt reports whether the block at (x, y, z) is a redstone
// conductor. A shulker box conducts while its lid has zero progress.
// vanilla: BlockBehaviour.isRedstoneConductor, ShulkerBoxBlockEntity.getBoundingBox
func (w *World) ConductorAt(x, y, z int32) bool {
	return w.conductorState(x, y, z, w.BlockAt(x, y, z))
}

// conductorState is ConductorAt for the state read at (x, y, z).
func (w *World) conductorState(x, y, z int32, state uint16) bool {
	if registry.IsShulkerBoxState(state) {
		progress, moving := w.shulkerLidsOpen.Load([3]int32{x, y, z})
		return !moving || progress.(int) == 0
	}
	return registry.IsRedstoneConductorState(state)
}

// trappedChestSignal is the signal a trapped chest gives on every side: how
// many players look in, at most 15. vanilla: TrappedChestBlock.ownSignal
func (p *Physics) trappedChestSignal(x, y, z int32) int {
	be := p.w.BlockEntity(x, y, z)
	if be == nil {
		return 0
	}
	n := 0
	be.Use(func(b *BlockEntity) { n = b.Openers })
	return min(n, 15)
}

// TrappedChestOpenersChanged wakes the neighbours of a trapped chest and of the
// block below (which it strongly powers) after its openers count moved, and
// returns what the wake did.
// vanilla: TrappedChestBlockEntity.signalOpenCount
func (p *Physics) TrappedChestOpenersChanged(x, y, z int32) ([]BlockChange, []Effect, []TNTPrime) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var changes []BlockChange
	for _, at := range [2][3]int32{{x, y, z}, {x, y - 1, z}} {
		for _, d := range updateNeighborOrder {
			nx, ny, nz := at[0]+dirDelta[d][0], at[1]+dirDelta[d][1], at[2]+dirDelta[d][2]
			p.redstoneNeighborChanged(nx, ny, nz, p.w.BlockAt(nx, ny, nz), &changes, false)
		}
	}
	effects, primes := p.takeWake()
	return changes, effects, primes
}
