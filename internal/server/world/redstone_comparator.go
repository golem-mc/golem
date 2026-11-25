package world

import (
	"math"

	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// The comparator: a diode that reads its input's strength, waits two game ticks
// and keeps its last output in a block entity.
// vanilla: ComparatorBlock

// comparatorDelay is ComparatorBlock.getDelay in game ticks; unlike a
// repeater's it is not doubled.
const comparatorDelay = 2

// comparatorSignal is a comparator's output: its last tick's strength, out the
// back face, only while powered. A comparator whose tick never ran answers
// zero, like a fresh block entity.
func (p *Physics) comparatorSignal(x, y, z int32, state uint16, d int) int {
	if d != blocklogic.Opposite(facingOfState(state)) {
		return 0
	}
	if !registry.StatePowered(state) {
		return 0
	}
	return p.comparatorOutput(x, y, z)
}

// comparatorOutput reads the block entity's output signal; no entity reads
// zero, vanilla's null check.
func (p *Physics) comparatorOutput(x, y, z int32) int {
	be := p.w.BlockEntity(x, y, z)
	if be == nil || be.Kind != KindComparator {
		return 0
	}
	out := 0
	be.Use(func(b *BlockEntity) { out = b.Output })
	return out
}

// analogOutputChanged wakes comparators beside a position or beyond a conductor
// next to it. tick is the base of the comparator's delay.
// vanilla: Level.updateNeighbourForOutputSignal
func (p *Physics) analogOutputChanged(x, y, z int32, tick int64) {
	for _, d := range []int{dirNorth, dirSouth, dirEast, dirWest} {
		nx, ny, nz := x+dirDelta[d][0], y+dirDelta[d][1], z+dirDelta[d][2]
		if p.wakeReadingComparator(nx, ny, nz, tick) {
			continue
		}
		if p.w.ConductorAt(nx, ny, nz) {
			p.wakeReadingComparator(nx+dirDelta[d][0], ny+dirDelta[d][1], nz+dirDelta[d][2], tick)
		}
	}
}

// shulkerBoxBlocks follows the block class rather than reloadable tags.
var shulkerBoxBlocks = registry.BlocksOf(func(b registry.Block) bool { return b.Type() == registry.BlockTypeShulkerBox })

// isFullnessContainer names the blocks whose output is their inventory's
// fullness.
func isFullnessContainer(block registry.Block) bool {
	switch block {
	case registry.BlockDecoratedPot, registry.BlockFurnace, registry.BlockBlastFurnace,
		registry.BlockSmoker, registry.BlockBrewingStand, registry.BlockBarrel,
		registry.BlockDispenser, registry.BlockDropper, registry.BlockHopper,
		registry.BlockShulkerBox:
		return true
	}
	return IsChestBlock(block) || shulkerBoxBlocks[block]
}

// readsOwnContainer names the other blocks whose removal wakes their
// comparators: those with an output of their own.
// vanilla: Containers.updateNeighboursAfterDestroy callers
func readsOwnContainer(block registry.Block) bool {
	switch block {
	case registry.BlockJukebox, registry.BlockChiseledBookshelf, registry.BlockCrafter,
		registry.BlockCreakingHeart, registry.BlockCopperGolemStatue:
		return true
	}
	return block.InGroup(registry.GroupShelf | registry.GroupCopperGolemStatue)
}

// containerRemoved wakes the comparators reading a container replaced by a
// different block.
// vanilla: Containers.updateNeighboursAfterDestroy, LevelChunk.setBlockState
func (p *Physics) containerRemoved(x, y, z int32, oldState, newState uint16) {
	block := registry.BlockOf(oldState)
	if registry.BlockOf(newState) == block {
		return
	}
	if isFullnessContainer(block) || readsOwnContainer(block) {
		p.analogOutputChanged(x, y, z, p.now)
	}
}

// wakeReadingComparator asks a comparator at the position to re-check,
// reporting whether there was one.
func (p *Physics) wakeReadingComparator(x, y, z int32, tick int64) bool {
	state := p.w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockComparator {
		return false
	}
	p.comparatorCheckTickOnNeighbor(x, y, z, state, tick)
	return true
}

// comparatorInput is the front read (a wire's raw power standing in),
// overridden by the analog output of the block ahead, or, when that is a
// conductor, of the block beyond it: the chest read through a wall.
// vanilla: ComparatorBlock.getInputSignal
func (p *Physics) comparatorInput(x, y, z int32, f int) int {
	in := p.repeaterInput(x, y, z, f)
	ix, iy, iz := x+dirDelta[f][0], y+dirDelta[f][1], z+dirDelta[f][2]
	istate := p.w.BlockAt(ix, iy, iz)
	if out, ok := p.analogOutputOf(istate, ix, iy, iz, blocklogic.Opposite(f)); ok {
		return out
	}
	if in < 15 && p.w.conductorState(ix, iy, iz, istate) {
		jx, jy, jz := ix+dirDelta[f][0], iy+dirDelta[f][1], iz+dirDelta[f][2]
		jstate := p.w.BlockAt(jx, jy, jz)
		out, ok := p.analogOutputOf(jstate, jx, jy, jz, blocklogic.Opposite(f))
		// A frame in the cell beyond the conductor facing the comparator's way
		// is read beside the block there, the larger winning. Even an empty
		// frame overrides the conductor's own power, so the read reports
		// whether a frame was found.
		// vanilla: ComparatorBlock.getInputSignal
		if frame, found := p.entityOutputAt(int32(f), jx, jy, jz); found || ok {
			return max(frame, out)
		}
	}
	return in
}

// copperGolemPoseSignal is a statue's output by pose: its ordinal plus one.
var copperGolemPoseSignal = map[string]int{"standing": 1, "sitting": 2, "running": 3, "star": 4}

// analogOutputOf computes the analog output of a block the engine can model,
// false where vanilla would ask data this server does not keep. d is the face
// read through (from block toward reader); only the shelf uses it. A container
// reads AbstractContainerMenu.getRedstoneSignalFromContainer: each stack's
// count over its max size, summed over the slot count, then floor(p*14) plus
// one if anything is there.
func (p *Physics) analogOutputOf(state uint16, x, y, z int32, d int) (int, bool) {
	block := registry.BlockOf(state)
	switch {
	case block == registry.BlockComposter:
		// vanilla: ComposterBlock.getAnalogOutputSignal
		return stateIntOr(state, registry.PropLevel, 0), true
	case block == registry.BlockCake:
		// Each of the seven slices is worth two signal, a full cake 14.
		// vanilla: CakeBlock.getAnalogOutputSignal
		return (7 - stateIntOr(state, registry.PropBites, 0)) * 2, true
	case block.InTag(registry.TagIDCandleCakes):
		// A candle cake is always a full cake, lit or not.
		// vanilla: CandleCakeBlock.getAnalogOutputSignal
		return 14, true
	case block == registry.BlockWaterCauldron, block == registry.BlockPowderSnowCauldron:
		// vanilla: LayeredCauldronBlock.getAnalogOutputSignal
		return stateIntOr(state, registry.PropLevel, 0), true
	case block == registry.BlockCauldron:
		return 0, true
	case block == registry.BlockLavaCauldron:
		// vanilla: LavaCauldronBlock.getAnalogOutputSignal
		return 3, true
	case block == registry.BlockBeehive, block == registry.BlockBeeNest:
		// vanilla: BeehiveBlock.getAnalogOutputSignal
		return stateIntOr(state, registry.PropHoneyLevel, 0), true
	case block == registry.BlockRespawnAnchor:
		// Charges 0-4 read 0, 3, 7, 11, 15.
		// vanilla: RespawnAnchorBlock.getAnalogOutputSignal
		return stateIntOr(state, registry.PropCharges, 0) * 15 / 4, true
	case isSculkSensor(block):
		// The last vibration's frequency while active, zero otherwise.
		// vanilla: SculkSensorBlock.getAnalogOutputSignal
		if be := p.w.BlockEntity(x, y, z); be != nil && registry.StateValue(state, registry.PropSculkSensorPhase) == "active" {
			n := 0
			be.Use(func(b *BlockEntity) { n = b.SculkFrequency })
			return n, true
		}
		return 0, true
	case block == registry.BlockCreakingHeart:
		// vanilla: CreakingHeartBlock.getAnalogOutputSignal
		if be := p.w.BlockEntity(x, y, z); be != nil && be.Kind == KindCreakingHeart &&
			registry.StateValue(state, registry.PropCreakingHeartState) != "uprooted" {
			n := 0
			be.Use(func(b *BlockEntity) { n = b.CreakingOutput })
			return n, true
		}
		return 0, true
	case block.InGroup(registry.GroupCopperGolemStatue):
		// vanilla: CopperGolemStatueBlock.getAnalogOutputSignal
		return copperGolemPoseSignal[registry.StateValue(state, registry.PropCopperGolemPose)], true
	case isCopperBulb(block):
		// vanilla: CopperBulbBlock.getAnalogOutputSignal
		if registry.StateLit(state) {
			return 15, true
		}
		return 0, true
	case block == registry.BlockEndPortalFrame:
		// vanilla: EndPortalFrameBlock.getAnalogOutputSignal
		if registry.StateBool(state, registry.PropEye) {
			return 15, true
		}
		return 0, true
	case block == registry.BlockDetectorRail:
		// vanilla: DetectorRailBlock.getAnalogOutputSignal
		if registry.StatePowered(state) && p.railCarts != nil {
			if sig, ok := p.railCarts.CartContainerSignal(detectorRailSearchBox(x, y, z)); ok {
				return sig, true
			}
		}
		return 0, true
	case block == registry.BlockLectern:
		// vanilla: LecternBlock.getAnalogOutputSignal
		if !registry.StateBool(state, registry.PropHasBook) {
			return 0, true
		}
		be := p.w.BlockEntity(x, y, z)
		if be == nil || be.Kind != KindLectern {
			// A lectern without its entity reads as holding no book.
			return 14, true
		}
		out := 0
		be.Use(func(b *BlockEntity) { out = b.LecternSignal() })
		return out, true
	case block == registry.BlockCrafter:
		// A crafter counts the slots that are disabled or occupied, not its
		// fullness.
		// vanilla: CrafterBlock.getAnalogOutputSignal
		be := p.w.BlockEntity(x, y, z)
		if be == nil || be.Kind != KindCrafter {
			return 0, true
		}
		out := 0
		be.Use(func(b *BlockEntity) {
			for i := range b.Slots {
				if b.SlotDisabled(i) || !b.Slots[i].Empty() {
					out++
				}
			}
		})
		return out, true
	case block == registry.BlockJukebox:
		// The stored disc's song output, which does not fall when the song
		// ends.
		// vanilla: JukeboxBlock.getAnalogOutputSignal
		be := p.w.BlockEntity(x, y, z)
		if be == nil || be.Kind != KindJukebox {
			return 0, true
		}
		out := 0
		be.Use(func(b *BlockEntity) { out = b.JukeboxComparatorOutput() })
		return out, true
	case shelfBlocks[block]:
		// A shelf answers only through the face opposite its own, with a
		// bitmask of the occupied slots.
		// vanilla: ShelfBlock.getAnalogOutputSignal
		if d != blocklogic.Opposite(facingOfState(state)) {
			return 0, true
		}
		be := p.w.BlockEntity(x, y, z)
		if be == nil || be.Kind != KindShelf {
			return 0, true
		}
		out := 0
		be.Use(func(b *BlockEntity) {
			for i := range b.Slots {
				if !b.Slots[i].Empty() {
					out |= 1 << i
				}
			}
		})
		return out, true
	case block == registry.BlockChiseledBookshelf:
		// The last slot touched plus one, from any side; it lives in the block
		// entity, as the state names only the occupied slots.
		// vanilla: ChiseledBookShelfBlock.getAnalogOutputSignal
		be := p.w.BlockEntity(x, y, z)
		if be == nil || be.Kind != KindChiseledBookshelf {
			return 0, true
		}
		out := 0
		be.Use(func(b *BlockEntity) { out = b.LastInteractedSlot + 1 })
		return out, true
	case isCommandBlock[block]:
		// The last run's success count, clamped to 15; a missing entity is a
		// fresh one's zero.
		// vanilla: CommandBlock.getAnalogOutputSignal
		be := p.w.BlockEntity(x, y, z)
		if be == nil || be.Kind != KindCommandBlock {
			return 0, true
		}
		out := 0
		be.Use(func(b *BlockEntity) { out = b.SuccessCount })
		return min(out, 15), true
	case isFullnessContainer(block):
	default:
		return 0, false
	}
	// A chest half reads as its double chest whole.
	// vanilla: ChestBlock.getAnalogOutputSignal
	halves := [][3]int32{{x, y, z}}
	if IsChestBlock(block) {
		// A blocked half hides the whole container.
		// vanilla: ChestBlock.getAnalogOutputSignal, DoubleBlockCombiner.combineWithNeigbour
		if p.w.ChestBlockedAt(x, y, z) {
			return 0, true
		}
		if first, second, ok := p.w.ChestPair(x, y, z); ok {
			halves = [][3]int32{first, second}
		}
	}
	total, slots := 0.0, 0
	for _, h := range halves {
		be := p.w.BlockEntity(h[0], h[1], h[2])
		if be == nil {
			continue
		}
		be.Use(func(b *BlockEntity) {
			p.w.UnpackLoot(b)
			slots += len(b.Slots)
			for _, stack := range b.Slots {
				if stack.Empty() {
					continue
				}
				size := registry.StackMaxStackSize(stack)
				if size <= 0 || size > 64 {
					size = 64
				}
				total += float64(stack.Count) / float64(size)
			}
		})
	}
	if slots == 0 || total <= 0 {
		return 0, true
	}
	return int(math.Floor(total/float64(slots)*14.0)) + 1, true
}

// comparatorSide is the larger side input, each read like
// SignalGetter.getControlInputSignal without diodes required: a redstone block
// 15, a wire its raw power whatever its shape, any other source its strong
// signal (a diode only when its output points this way, a torch never
// sideways).
// vanilla: getAlternateSignal
func (p *Physics) comparatorSide(x, y, z int32, f int) int {
	best := 0
	for _, d := range []int{blocklogic.Clockwise(f), blocklogic.CounterClockwise(f)} {
		sx, sy, sz := x+dirDelta[d][0], y+dirDelta[d][1], z+dirDelta[d][2]
		sstate := p.w.BlockAt(sx, sy, sz)
		block := registry.BlockOf(sstate)
		var s int
		switch {
		case block == registry.BlockRedstoneBlock:
			s = 15
		case isWire[block]:
			s = registry.StatePower(sstate)
		case registry.IsRedstoneSignalSourceState(sstate):
			s = p.directAt(x, y, z, d)
		}
		if s > best {
			best = s
		}
	}
	return best
}

// comparatorCalculate is zero when the side input is stronger, else the
// difference in subtract mode or the input in compare mode.
// vanilla: calculateOutputSignal
func (p *Physics) comparatorCalculate(x, y, z int32, f int, subtract bool) int {
	in := p.comparatorInput(x, y, z, f)
	if in == 0 {
		return 0
	}
	side := p.comparatorSide(x, y, z, f)
	if side > in {
		return 0
	}
	if subtract {
		return in - side
	}
	return in
}

// comparatorShouldTurnOn is the calculation's sign, with the bytecode's
// subtlety: an input equal to the side turns a compare-mode comparator on and a
// subtract-mode one off.
// vanilla: shouldTurnOn
func (p *Physics) comparatorShouldTurnOn(x, y, z int32, f int, subtract bool) bool {
	in := p.comparatorInput(x, y, z, f)
	if in == 0 {
		return false
	}
	side := p.comparatorSide(x, y, z, f)
	if side > in {
		return false
	}
	if in == side {
		return !subtract
	}
	return true
}

// comparatorTick stores the calculation in the block entity first (what later
// reads answer), then writes powered to match as a client-only setBlock, and
// wakes the front when the strength changes or the mode is compare.
// vanilla: ComparatorBlock.refreshOutputState
func (p *Physics) comparatorTick(x, y, z int32, state uint16, f int, changes *[]BlockChange) {
	subtract := registry.StateValue(state, registry.PropMode) == "subtract"
	calculated := p.comparatorCalculate(x, y, z, f, subtract)
	old := 0
	if be := p.w.BlockEntity(x, y, z); be != nil && be.Kind == KindComparator {
		be.Use(func(b *BlockEntity) {
			old = b.Output
			b.Output = calculated
		})
	}
	if old == calculated && subtract {
		// Subtract mode skips an unchanged strength; compare mode always
		// re-checks powered and wakes the front.
		return
	}
	if p.comparatorShouldTurnOn(x, y, z, f, subtract) {
		if !registry.StatePowered(state) {
			on, _ := registry.WithBool(state, registry.PropPowered, true)
			p.comparatorWrite(x, y, z, on, f, changes)
			return
		}
	} else if registry.StatePowered(state) {
		off, _ := registry.WithBool(state, registry.PropPowered, false)
		p.comparatorWrite(x, y, z, off, f, changes)
		return
	}
	p.repeaterWakeFront(x, y, z, f, changes)
}

// comparatorWrite is the tick's flag-2 write plus its two passes: the shape
// update (an observer at the front sees it) and the on-place front wake.
func (p *Physics) comparatorWrite(x, y, z int32, state uint16, f int, changes *[]BlockChange) {
	p.setClient(x, y, z, state, changes)
	p.shapeUpdateNeighbors(x, y, z, changes)
	p.repeaterWakeFront(x, y, z, f, changes)
}

// comparatorCheckTickOnNeighbor schedules a two-tick visit when the stored
// output or powered disagrees with the inputs, unless one is due this instant,
// at HIGH priority when shouldPrioritize says so.
// vanilla: ComparatorBlock.checkTickOnNeighbor
func (p *Physics) comparatorCheckTickOnNeighbor(x, y, z int32, state uint16, tick int64) {
	if p.hasTickDueAt(blockKey{x, y, z}, tick) {
		return
	}
	f := facingOfState(state)
	subtract := registry.StateValue(state, registry.PropMode) == "subtract"
	calculated := p.comparatorCalculate(x, y, z, f, subtract)
	if calculated != p.comparatorOutput(x, y, z) ||
		registry.StatePowered(state) != p.comparatorShouldTurnOn(x, y, z, f, subtract) {
		priority := tickPriorityNormal
		if p.comparatorShouldPrioritize(x, y, z, f) {
			priority = tickPriorityHigh
		}
		p.schedulePriority(x, y, z, tick+comparatorDelay, priority)
	}
}

// comparatorShouldPrioritize reports a diode on the output side that is not
// feeding back into this one (its facing is not the output direction), so this
// tick may cut ahead of the neighbour's. Shared by repeaters and comparators.
// vanilla: shouldPrioritize
func (p *Physics) comparatorShouldPrioritize(x, y, z int32, f int) bool {
	ox, oy, oz := x+dirDelta[blocklogic.Opposite(f)][0], y+dirDelta[blocklogic.Opposite(f)][1], z+dirDelta[blocklogic.Opposite(f)][2]
	sstate := p.w.BlockAt(ox, oy, oz)
	if !diodeBlocks[registry.BlockOf(sstate)] {
		return false
	}
	return facingOfState(sstate) != blocklogic.Opposite(f)
}

// comparatorUsed cycles compare and subtract with a click (higher for
// subtract), writes the state as a client-only setBlock and refreshes the
// output.
// vanilla: useWithoutItem
func (p *Physics) comparatorUsed(x, y, z int32, state uint16, changes *[]BlockChange, effects *[]Effect) {
	mode := "subtract"
	if registry.StateValue(state, registry.PropMode) == "subtract" {
		mode = "compare"
	}
	*effects = append(*effects, Effect{
		SoundID: registry.SoundBlockComparatorClick, X: x, Y: y, Z: z,
		Volume: 0.3, Pitch: 0.5, ExcludeUser: true,
	})
	if mode == "subtract" {
		(*effects)[len(*effects)-1].Pitch = 0.55
	}
	next, _ := registry.WithProp(state, registry.PropMode, mode)
	p.setClientShaped(x, y, z, next, changes)
	p.comparatorTick(x, y, z, next, facingOfState(state), changes)
}

// EntityAnalogOutput is the comparator's read of a cell's entities: here an
// item frame's held item, from the one frame facing the comparator's way. The
// server implements it; tests can stub it.
type EntityAnalogOutput interface {
	// AnalogOutputAt reports the output of the frame in the cell facing dir
	// (zero if empty) and whether one is there. dir is the world's direction
	// numbering.
	AnalogOutputAt(dir, x, y, z int32) (int, bool)
}

// SetEntityAnalogOutput sets the engine's entity source; without one, frames
// are worth nothing.
func (p *Physics) SetEntityAnalogOutput(o EntityAnalogOutput) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entityOutput = o
}

// EntityOutputMissing reports whether no entity source is set.
func (p *Physics) EntityOutputMissing() bool { return p.entityOutput == nil }

// entityOutputAt is the frame read of a cell; no source reads as no frame.
func (p *Physics) entityOutputAt(dir, x, y, z int32) (int, bool) {
	if p.entityOutput == nil {
		return 0, false
	}
	return p.entityOutput.AnalogOutputAt(dir, x, y, z)
}

// EntityOutputChanged wakes the comparators reading a cell's entities (a
// frame's item or rotation changed), the same wake as a block's own output
// change, since they read it through a conductor too.
// vanilla: ItemFrame.setItem, Level.updateNeighbourForOutputSignal
func (p *Physics) EntityOutputChanged(x, y, z int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.analogOutputChanged(x, y, z, p.now)
}
