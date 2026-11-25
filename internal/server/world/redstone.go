package world

import (
	"strconv"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// The redstone power network: wire carrying power 0-15, the sources that feed
// it and the blocks that listen, with the tick delays, face rules and wake
// order taken from the vanilla bytecode.
//
// Shared state calculations live in internal/server/blocklogic; physics owns writes,
// scheduling and neighbor wake order.

// The six directions, indexed so tables from the bytecode copy across directly.
// vanilla: Direction.values
const (
	dirDown = iota
	dirUp
	dirNorth
	dirSouth
	dirWest
	dirEast
	dirCount
)

var dirDelta = [dirCount][3]int32{
	{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0},
}

// updateNeighborOrder is the order a neighbour update walks the six directions
// (NeighborUpdater.UPDATE_ORDER).
var updateNeighborOrder = [6]int{dirWest, dirEast, dirDown, dirUp, dirNorth, dirSouth}

// The redstone blocks this engine answers for, indexed by Block once.
var (
	isWire             = blockSet(registry.BlockRedstoneWire)
	isTorch            = blockSet(registry.BlockRedstoneTorch, registry.BlockRedstoneWallTorch)
	diodeBlocks        = blockSet(registry.BlockRepeater, registry.BlockComparator)
	isObserver         = blockSet(registry.BlockObserver)
	isLamp             = blockSet(registry.BlockRedstoneLamp)
	isRedstoneBlk      = blockSet(registry.BlockRedstoneBlock)
	isTntBlock         = blockSet(registry.BlockTnt)
	pistonBlocks       = blockSet(registry.BlockPiston, registry.BlockStickyPiston)
	isDispenser        = blockSet(registry.BlockDispenser)
	isDropper          = blockSet(registry.BlockDropper)
	isCommandBlock     = blockSet(registry.BlockCommandBlock, registry.BlockChainCommandBlock, registry.BlockRepeatingCommandBlock)
	isHopper           = blockSet(registry.BlockHopper)
	isBell             = blockSet(registry.BlockBell)
	isDaylightDetector = blockSet(registry.BlockDaylightDetector)
	isTripwire         = blockSet(registry.BlockTripwire)
	isTripHook         = blockSet(registry.BlockTripwireHook)
	isSpeleothem       = blockSet(registry.BlockPointedDripstone, registry.BlockSulfurSpike)
	shelfBlocks        = registry.BlocksOf(func(b registry.Block) bool { return b.Type() == registry.BlockTypeShelf })
)

// isPressurePlate and isButton mark the blocks of each kind: the registry's
// signal sources minus the ones with their own branches above.
var (
	isPressurePlate = signalSourcesInGroup(registry.GroupPressurePlate)
	isButton        = signalSourcesInGroup(registry.GroupButton)
	isDoorLike      = registry.BlocksOf(func(b registry.Block) bool {
		return b.InGroup(registry.GroupDoor) || b.InGroup(registry.GroupTrapdoor) || b.InGroup(registry.GroupFenceGate)
	})
	// vanilla: AbstractBedBlock, the colours and the straw bed
	isBed = registry.BlocksInGroup(registry.GroupBed)
)

func init() {
	buildSignalKinds()
	buildReactKinds()
}

// signalSourcesInGroup marks the signal sources in a class family,
// minus the ones with their own wake branches.
func signalSourcesInGroup(group registry.BlockGroup) *[registry.BlockCount]bool {
	return registry.BlocksOf(func(b registry.Block) bool {
		switch {
		case isWire[b], isTorch[b], diodeBlocks[b], isLamp[b], isRedstoneBlk[b]:
			return false
		}
		return b.IsRedstoneSignalSource() && b.InGroup(group)
	})
}

// blocksNamed marks the blocks named; an unknown name is a typo and panics at
// init.
func blocksNamed(names ...string) *[registry.BlockCount]bool {
	t := new([registry.BlockCount]bool)
	for _, name := range names {
		b, ok := registry.BlockByName(name)
		if !ok {
			panic("world: unknown block " + name)
		}
		t[b] = true
	}
	return t
}

// atoiOr reads a numeric state property, or def when it is missing or not a
// number.
func atoiOr(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

// stateIntOr is the state's numeric property, def for a state without it.
func stateIntOr(state uint16, prop registry.Prop, def int) int {
	if n, ok := registry.StateInt(state, prop); ok {
		return n
	}
	return def
}

// stateInTag is Block.InTag for a state; an ID that is no state is in no tag,
// whatever a data pack adds to air's.
func stateInTag(state uint16, t registry.BlockTagID) bool {
	return int(state) < registry.StateCount && registry.BlockOf(state).InTag(t)
}

// crafterFront is the direction of a crafter's orientation ("front_top" before
// the underscore).
func crafterFront(state uint16) int {
	front, _, _ := strings.Cut(registry.StateValue(state, registry.PropOrientation), "_")
	return registry.FacingIndex(front)
}

// facingOfState reads a state's facing as a direction; no facing reads as east.
func facingOfState(state uint16) int { return registry.StateFacing(state) }

// redstoneTickEvent tags a big dripleaf's or detector rail's block tick, so a
// waterlogged cell's fluid visit neither eats nor blocks it.
const redstoneTickEvent = 1<<5 | 1<<6 | 1<<7 | 2

// The tick delays, in game ticks, the vanilla blocks run on.
const (
	torchTickDelay   = 2
	lampOffDelay     = 4
	plateTickDelay   = 20
	buttonTicksStone = 20
	buttonTicksWood  = 30
	torchBurnoutFuse = 160
	repeaterDelayMax = 4
	dispenserDelay   = 4

	// crafterDelay is CrafterBlock.setPlacedBy's scheduleTick span, in game
	// ticks.
	crafterDelay = 4

	// weightedPlateTickDelay is WeightedPressurePlateBlock.getPressedTime (the
	// ordinary plate's is 20).
	weightedPlateTickDelay = 10
)

// The wake hooks the physics neighbour update calls into.

// redstoneAfterRemoval is the affect-neighbours-after-removal half of the wake:
// a removed wire, torch or powered lever, button or plate wakes what it fed.
// byPiston is vanilla's movedByPiston: true skips the source wake (a piston's
// destroy passes false, like every other removal). entityReplaced says the
// block write already removed a ride's block entity and may have loaded a
// replacement's since, so the ride's cleanup must not run again.
func (p *Physics) redstoneAfterRemoval(x, y, z int32, oldState uint16, byPiston, entityReplaced bool, changes *[]BlockChange) {
	if int(oldState) >= registry.StateCount {
		return
	}
	oldBlock := registry.BlockOf(oldState)
	switch {
	case isWire[oldBlock]:
		p.wireAfterRemoval(x, y, z, oldState, changes)
	case isTorch[oldBlock]:
		p.redstoneWakeAround(x, y, z, changes)
	case railBlocks[oldBlock]:
		// vanilla: BaseRailBlock.affectNeighborsAfterRemoval — a powered,
		// activator or detector rail wakes the cell above a slope, then its own
		// cell and its support (whose neighbours reach a rail stepping up to
		// it); a plain rail wakes only the cell above a slope.
		if !byPiston {
			if _, slope := ascendingTo[registry.StateValue(oldState, registry.PropShape)]; slope {
				p.wireWakeNeighbors(x, y+1, z, changes)
			}
			if oldBlock != registry.BlockRail {
				p.wireWakeNeighbors(x, y, z, changes)
				p.wireWakeNeighbors(x, y-1, z, changes)
			}
		}
	// A source that carried a signal wakes its own cell and the block it hangs
	// on, each with its six neighbours.
	// vanilla: affectNeighborsAfterRemoval
	case isPressurePlate[oldBlock]:
		if !byPiston && plateCurrentSignal(oldBlock, oldState) > 0 {
			p.plateUpdateNeighbours(x, y, z, changes)
		}
	case isButton[oldBlock]:
		if !byPiston && registry.StatePowered(oldState) {
			p.buttonUpdateNeighbours(x, y, z, oldState, changes)
		}
	case oldBlock == registry.BlockLever:
		if !byPiston && registry.StatePowered(oldState) {
			p.levitateNeighbours(x, y, z, oldState, changes)
		}
	case isTripwire[oldBlock]:
		// The hooks hear the removal with the string still powered, so a break
		// fires the line unless shears disarmed it first. A write of the same
		// block over itself is no block change and runs no wake.
		// vanilla: TripWireBlock.affectNeighborsAfterRemoval, LevelChunk.setBlockState
		if !byPiston && registry.BlockOf(p.w.BlockAt(x, y, z)) != oldBlock {
			if state, ok := registry.WithBool(oldState, registry.PropPowered, true); ok {
				p.tripwireUpdateSource(x, y, z, state, changes, &p.effects)
			}
		}
	case oldBlock == registry.BlockLectern:
		// vanilla: LecternBlock.affectNeighborsAfterRemoval
		if registry.BlockOf(p.w.BlockAt(x, y, z)) != oldBlock && registry.StatePowered(oldState) {
			p.wireWakeNeighbors(x, y-1, z, changes)
		}
	case isLightningRod(oldState):
		// vanilla: LightningRodBlock.affectNeighborsAfterRemoval
		if registry.BlockOf(p.w.BlockAt(x, y, z)) != oldBlock && registry.StatePowered(oldState) {
			p.lightningRodUpdateNeighbours(x, y, z, facingOfState(oldState), changes)
		}
	case isSculkSensor(oldBlock):
		// vanilla: SculkSensorBlock.affectNeighborsAfterRemoval
		if registry.BlockOf(p.w.BlockAt(x, y, z)) != oldBlock && registry.StateValue(oldState, registry.PropSculkSensorPhase) == "active" {
			p.sculkUpdateNeighbours(x, y, z, changes)
		}
	case isTripHook[oldBlock]:
		// vanilla: TripWireHookBlock.affectNeighborsAfterRemoval, LevelChunk.setBlockState
		if !byPiston && registry.BlockOf(p.w.BlockAt(x, y, z)) != oldBlock {
			p.tripHookOnRemoved(x, y, z, facingOfState(oldState), tripHookAttached(oldState), registry.StatePowered(oldState), changes, &p.effects)
		}
	case oldBlock == registry.BlockComparator:
		// The fed block and its neighbours learn the output is gone, using the
		// comparator's old facing.
		// vanilla: affectNeighborsAfterRemoval
		p.repeaterWakeFront(x, y, z, facingOfState(oldState), changes)
	case isObserver[oldBlock]:
		p.observerAfterRemoval(x, y, z, oldState, changes)
	case oldBlock == registry.BlockPistonHead:
		// The base the head still fits on goes with it, dropping, whatever took
		// the head.
		// vanilla: PistonHeadBlock.affectNeighborsAfterRemoval
		headFacing := facingOfState(oldState)
		bx, by, bz := pistonShift(x, y, z, blocklogic.Opposite(headFacing), 1)
		if p.pistonFittingBase(bx, by, bz, headFacing, registry.StateValue(oldState, registry.PropType) == "sticky") {
			p.breakWithDrop(bx, by, bz, changes)
		}
	case oldBlock == registry.BlockMovingPiston:
		// A removed ride does not land: its block entity vanishes, and the
		// still-extended base of a head's ride goes too, as a player break's
		// destroy hook leaves no extended base. No landing write: the section
		// write that removed the ride ran first, so the final tick's
		// moving-piston check finds the cell changed.
		if !entityReplaced {
			p.w.RemoveBlockEntity(x, y, z)
		}
		bx, by, bz := pistonShift(x, y, z, blocklogic.Opposite(facingOfState(oldState)), 1)
		if bs := p.w.BlockAt(bx, by, bz); pistonBlocks[registry.BlockOf(bs)] && registry.StateBool(bs, registry.PropExtended) {
			p.set(bx, by, bz, airState, changes)
			p.shapeUpdateNeighbors(bx, by, bz, changes)
		}
	case shelfBlocks[oldBlock]:
		// containerRemoved wakes the comparators the shelf fed.
		// vanilla: ShelfBlock.affectNeighborsAfterRemoval
		p.shelfPowerDownNeighbors(x, y, z, facingOfState(oldState), changes)
	}
}

// redstoneOnPlace is the on-place half of the wake (each block's onPlace).
// oldState is the cell's previous state, which the piston reads to skip its own
// state moving under it.
func (p *Physics) redstoneOnPlace(x, y, z int32, oldState uint16, changes *[]BlockChange) {
	state := p.w.BlockAt(x, y, z)
	block := registry.BlockOf(state)
	switch {
	case isWire[block]:
		p.wireOnPlace(x, y, z, changes)
	case isTripwire[block]:
		// Only a wire's arrival, not a change of its own state, tells the hook
		// behind it.
		// vanilla: TripWireBlock.onPlace
		if !isTripwire[registry.BlockOf(oldState)] {
			p.tripwireOnPlace(x, y, z, state, changes, &p.effects)
		}
	case isTorch[block]:
		p.redstoneWakeAround(x, y, z, changes)
	case block == registry.BlockRepeater, block == registry.BlockComparator:
		p.repeaterWakeFront(x, y, z, facingOfState(state), changes)
	case isObserver[block]:
		p.observerOnPlace(x, y, z, state, changes)
	case pistonBlocks[block]:
		// A placed piston reads its power at once, except when this is its own
		// state changing or the cell holds a block entity (the ride its own
		// event is writing).
		// vanilla: PistonBaseBlock.onPlace
		if registry.BlockOf(oldState) == block || p.w.BlockEntity(x, y, z) != nil {
			return
		}
		p.pistonCheckIfExtend(x, y, z, state, changes)
	case isHopper[block]:
		// A hopper placed in a powered circuit starts disabled rather than
		// running until its first neighbour change.
		// vanilla: HopperBlock.checkPoweredState
		enabled := !p.hasNeighborSignal(x, y, z)
		if registry.StateEnabled(state) != enabled {
			want, _ := registry.WithBool(state, registry.PropEnabled, enabled)
			p.setClientShaped(x, y, z, want, changes)
		}
	case isCopperBulb(block):
		// Only a bulb arriving over a different block reads power; a write over
		// the same block keeps the state it was handed.
		// vanilla: CopperBulbBlock.onPlace
		if registry.BlockOf(oldState) != block {
			p.copperBulbFlip(x, y, z, state, changes, false)
		}
	case block == registry.BlockComposter:
		p.composterOnPlace(x, y, z, state)
	case block == registry.BlockTarget:
		p.targetOnPlace(x, y, z, state, oldState, changes)
	case isSculkSensor(block):
		p.sculkOnPlace(x, y, z, state, oldState, changes)
	case isLightningRod(state):
		p.lightningRodOnPlace(x, y, z, state, oldState)
	case shelfBlocks[block]:
		// A powered shelf arriving links with the powered shelves beside it; an
		// unpowered one unlinks them.
		// vanilla: ShelfBlock.onPlace
		if registry.StatePowered(state) {
			p.shelfPowerUp(x, y, z, oldState, changes)
		} else {
			p.shelfPowerDownNeighbors(x, y, z, facingOfState(state), changes)
		}
	}
}

// CrafterPlacement reports whether the neighbour signal would make a crafter
// placed here triggered. It books nothing, so a refused placement leaves no
// visit; ScheduleCrafter books it once the block is in the world.
// vanilla: CrafterBlock.getStateForPlacement
func (p *Physics) CrafterPlacement(x, y, z int32) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hasNeighborSignal(x, y, z)
}

// ScheduleCrafter books a triggered crafter's craft visit; the craft itself is
// not modelled.
// vanilla: CrafterBlock.setPlacedBy
func (p *Physics) ScheduleCrafter(x, y, z int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.schedule(x, y, z, p.now+crafterDelay)
}

// setCrafterTriggered updates the entity's saved flag on a power edge.
// Triggered is the saved source of truth, so /data can edit it independently
// of the block state until the next power edge.
// vanilla: CrafterBlock.setBlockEntityTriggered
func (p *Physics) setCrafterTriggered(x, y, z int32, triggered bool) {
	if be := p.w.BlockEntity(x, y, z); be != nil && be.Kind == KindCrafter {
		be.Use(func(b *BlockEntity) {
			b.Triggered = triggered
			p.w.TouchEntity(b)
		})
	}
}

// redstoneNeighborChanged is the neighbour-changed reaction of every redstone
// block. placed marks the visit for the block whose own cell just changed,
// which vanilla's neighbour pass never gives the written block; a case that
// reads no power at placement ignores it.
func (p *Physics) redstoneNeighborChanged(x, y, z int32, state uint16, changes *[]BlockChange, placed bool) {
	cur := p.w.BlockAt(x, y, z)
	kind := reactKindOf(cur)
	if placed {
		// Own-cell visit decisions from the 26.3 classes' onPlace:
		// kind                 class / power read
		// Wire                 RedstoneWireBlock.updatePowerStrength
		// Torch                RedstoneTorchBlock.notifyNeighbors (returns here)
		// Rail                 BaseRailBlock.updateState -> neighborChanged
		// TNT                  TntBlock.hasNeighborSignal
		// Hopper               HopperBlock.checkPoweredState
		// Piston               PistonBaseBlock.checkIfExtend
		// Repeater, Comparator DiodeBlock: wakes front only; setPlacedBy checks
		// CopperBulb           CopperBulbBlock: handled by redstoneOnPlace
		// Shelf                ShelfBlock: links only, handled by redstoneOnPlace
		// Lamp                 RedstoneLampBlock: inherited no-op
		// Dispenser            DispenserBlock / DropperBlock: inherited no-op
		// DoorLike             DoorBlock / TrapDoorBlock / FenceGateBlock: no-op
		// Bell, NoteBlock      BellBlock / NoteBlock: inherited no-op
		// Crafter, CommandBlock CrafterBlock / CommandBlock: inherited no-op
		// Skull, BigDripleaf   AbstractSkullBlock / BigDripleafBlock: no-op
		// PlateOrButton        BasePressurePlateBlock / ButtonBlock: no-op
		// StructureBlock, TestBlock StructureBlock / TestBlock: inherited no-op
		// PistonHead           PistonHeadBlock: inherited no-op
		// None                 no reaction (includes LecternBlock: no-op)
		// vanilla: BlockBehaviour.onPlace
		switch kind {
		case reactWire, reactTorch, reactRail, reactTNT, reactHopper, reactPiston:
		default:
			return
		}
	}
	switch kind {
	case reactWire:
		if !p.wireCanSurvive(x, y, z) {
			p.dropBlock(x, y, z, state, changes)
			return
		}
		p.wireUpdatePower(x, y, z, changes)
	case reactTorch:
		lit := registry.StateLit(cur)
		if lit == p.torchShouldBeLit(x, y, z, registry.BlockOf(cur), cur) && !p.torchWillTick(x, y, z) {
			p.schedulePriority(x, y, z, p.now+torchTickDelay, tickPriorityNormal)
		}
	case reactRail:
		// Direct wakes skip updateAsNeighbor's survival pass, so the rail's own
		// survival check runs here before the powered family re-reads its
		// power.
		// vanilla: BaseRailBlock.neighborChanged, PoweredRailBlock.updateState
		if !canSurvive(p.w, x, y, z, state) {
			p.dropBlock(x, y, z, state, changes)
			return
		}
		p.railUpdatePowered(x, y, z, changes)
	case reactRepeater:
		if p.repeaterLocked(x, y, z, facingOfState(cur)) {
			return
		}
		p.repeaterCheckTickOnNeighbor(x, y, z, registry.StatePowered(cur), repeaterDelayOf(cur), facingOfState(cur))
	case reactComparator:
		p.comparatorCheckTickOnNeighbor(x, y, z, cur, p.now)
	case reactLamp:
		lit := registry.StateLit(cur)
		if lit != p.hasNeighborSignal(x, y, z) {
			if lit {
				p.schedulePriority(x, y, z, p.now+lampOffDelay, tickPriorityNormal)
			} else if p.redstonePower(x, y, z, 0, 15) > 0 {
				p.setClientShaped(x, y, z, lampLit, changes)
			}
		}
	case reactTNT:
		if p.hasNeighborSignal(x, y, z) && p.w.TNTExplodes() {
			// A flags-3 removal: p.set is the neighbour update, and the shape
			// update is what a wire or door resting on the block reacts to.
			// vanilla: TntBlock.neighborChanged
			p.set(x, y, z, airState, changes)
			p.shapeUpdateNeighbors(x, y, z, changes)
			p.primeTNT(x, y, z)
		}
	case reactNoteBlock:
		p.noteBlockNeighborChanged(x, y, z, cur, changes, placed)
	case reactSkull:
		// A flag-2 write: clients hear it and the shape pass runs, neighbours get no update.
		// vanilla: AbstractSkullBlock.neighborChanged
		if !placed {
			if signal := p.hasNeighborSignal(x, y, z); registry.StatePowered(cur) != signal {
				want, _ := registry.WithBool(cur, registry.PropPowered, signal)
				p.setClientShaped(x, y, z, want, changes)
			}
		}
	case reactBigDripleaf:
		// vanilla: BigDripleafBlock.neighborChanged
		if !placed && p.hasNeighborSignal(x, y, z) {
			p.dripleafReset(x, y, z, cur, changes)
		}
	case reactPlateOrButton:
		// A plate or button that lost its support is the shape update's
		// business, not this.
	case reactDoorLike:
		p.doorLikeNeighborChanged(x, y, z, registry.BlockOf(cur), cur, changes, p.effectsBuffer())
	case reactBell:
		// The rising edge rings the bell along its facing; either edge writes
		// powered.
		// vanilla: BellBlock.neighborChanged
		powered := p.hasNeighborSignal(x, y, z)
		if registry.StatePowered(cur) != powered {
			if powered {
				p.ringBell(x, y, z, facingOfState(cur))
			}
			want, _ := registry.WithBool(cur, registry.PropPowered, powered)
			p.setClientShaped(x, y, z, want, changes)
		}
	case reactCopperBulb:
		// The bulb's own arrival is the on-place read's; vanilla gives the
		// written block no neighbour change.
		if placed {
			return
		}
		p.copperBulbFlip(x, y, z, cur, changes, true)
	case reactShelf:
		// POWERED follows the signal's edges, each fall unlinking the side
		// chain, each edge playing its sound. The placed return keeps a
		// /setblock'd shelf at its commanded state; since the write runs inside
		// the wake, the on-place link vanilla would run is called here instead.
		// vanilla: ShelfBlock.neighborChanged
		if placed {
			return
		}
		powered := p.hasNeighborSignal(x, y, z)
		if registry.StatePowered(cur) != powered {
			want, _ := registry.WithBool(cur, registry.PropPowered, powered)
			if !powered {
				want, _ = registry.WithProp(want, registry.PropSideChain, "unconnected")
			}
			p.setClientShaped(x, y, z, want, changes)
			if powered {
				p.shelfPowerUp(x, y, z, state, changes)
			} else {
				p.shelfPowerDownNeighbors(x, y, z, facingOfState(cur), changes)
			}
			sound := registry.SoundBlockShelfDeactivate
			if powered {
				sound = registry.SoundBlockShelfActivate
			}
			p.effects = append(p.effects, Effect{SoundID: sound, X: x, Y: y, Z: z, Volume: 1, Pitch: 1})
		}
	case reactDispenser:
		// Power counts at the dispenser or the block above it: a wire on its
		// roof counts, which hasNeighborSignal alone misses since the block
		// above does not feed down. A rise books the dispense four ticks out
		// and sets triggered; a fall clears it.
		// vanilla: DispenserBlock.neighborChanged
		powered := p.hasNeighborSignal(x, y, z) || p.hasNeighborSignal(x, y+1, z)
		triggered := registry.StateBool(cur, registry.PropTriggered)
		if powered && !triggered {
			p.schedulePriority(x, y, z, p.now+dispenserDelay, tickPriorityNormal)
			want, _ := registry.WithBool(cur, registry.PropTriggered, true)
			p.setClientShaped(x, y, z, want, changes)
		} else if !powered && triggered {
			want, _ := registry.WithBool(cur, registry.PropTriggered, false)
			p.setClientShaped(x, y, z, want, changes)
		}
	case reactCrafter:
		// A rise books the craft visit and sets TRIGGERED; a fall clears
		// TRIGGERED and CRAFTING. The placement path owns the power read, so
		// placed skips a second one.
		// vanilla: CrafterBlock.neighborChanged
		if placed {
			return
		}
		powered := p.hasNeighborSignal(x, y, z)
		triggered := registry.StateBool(cur, registry.PropTriggered)
		switch {
		case powered && !triggered:
			p.schedule(x, y, z, p.now+crafterDelay)
			want, _ := registry.WithBool(cur, registry.PropTriggered, true)
			p.setClientShaped(x, y, z, want, changes)
			p.setCrafterTriggered(x, y, z, true)
		case !powered && triggered:
			want, _ := registry.WithProps(cur, registry.PropValue{P: registry.PropTriggered, V: "false"}, registry.PropValue{P: registry.PropCrafting, V: "false"})
			p.setClientShaped(x, y, z, want, changes)
			p.setCrafterTriggered(x, y, z, false)
		}
	case reactCommandBlock:
		// vanilla: CommandBlock.neighborChanged
		p.commandBlockPowerChanged(x, y, z, registry.BlockOf(cur), state)
	case reactStructureBlock:
		p.structureBlockPowerChanged(x, y, z)
	case reactTestBlock:
		p.testBlockNeighborChanged(x, y, z, cur)
	case reactHopper:
		// Enabled while nothing powers it. Only the hopper's six neighbours
		// count: unlike the dispenser, a wire on its roof leaves it alone.
		// vanilla: HopperBlock.checkPoweredState
		enabled := !p.hasNeighborSignal(x, y, z)
		if registry.StateEnabled(cur) != enabled {
			want, _ := registry.WithBool(cur, registry.PropEnabled, enabled)
			p.setClientShaped(x, y, z, want, changes)
		}
	case reactPiston:
		// vanilla: PistonBaseBlock.neighborChanged
		p.pistonCheckIfExtend(x, y, z, cur, changes)
	case reactPistonHead:
		// A surviving head forwards the change to the piston behind it, whose
		// power it may have altered; a head that no longer survives is the
		// shape update's to take down.
		// vanilla: PistonHeadBlock.neighborChanged
		if p.pistonHeadCanSurvive(x, y, z, cur) {
			bx, by, bz := pistonShift(x, y, z, blocklogic.Opposite(facingOfState(cur)), 1)
			p.redstoneNeighborChanged(bx, by, bz, p.w.BlockAt(bx, by, bz), changes, false)
		}
	}
}

// effectsBuffer is the wake's effect sink, a pointer so a reaction can add a
// click without every helper carrying the slice.
func (p *Physics) effectsBuffer() *[]Effect {
	return &p.effects
}

// ringBell queues the block event that swings the bell away from dir, and its
// sound at volume 2.
// vanilla: BellBlock.attemptToRing, BellBlockEntity.onHit
func (p *Physics) ringBell(x, y, z int32, dir int) {
	p.effects = append(p.effects, Effect{
		SoundID: registry.SoundBlockBellUse, GameEvent: gameEventBlockChange,
		X: x, Y: y, Z: z, Volume: 2,
		Pitch:            1,
		HasBlockAction:   true,
		BlockAction:      1, // vanilla: BellBlock.EVENT_BELL_RING
		BlockActionData:  uint8(dir),
		BlockActionBlock: protocol.BlockBell,
	})
}

// shapeUpdate answers one neighbour's shape update for the blocks that care.
// vanilla: BlockBehaviour.updateShape
func (p *Physics) shapeUpdate(x, y, z int32, d [3]int32, changes *[]BlockChange) {
	if !p.chainStart(x, y, z) {
		return
	}
	p.shapeUpdateBody(x, y, z, d, changes)
	p.chainEnd()
}

func (p *Physics) shapeUpdateBody(x, y, z int32, d [3]int32, changes *[]BlockChange) {
	state := p.w.BlockAt(x, y, z)
	block := registry.BlockOf(state)
	p.bubbleShapeUpdate(x, y, z, state, blocklogic.Opposite(directionOf(d)))
	if plantBodyBlocks[block] {
		toward := blocklogic.Opposite(directionOf(d))
		if toward != directionOf(growingPlantOf(block).dir) {
			return
		}
		next := plantBodyShapeUpdate(p.rng, state, toward, p.w.BlockAt(x-d[0], y-d[1], z-d[2]))
		if next != state {
			p.setClientShaped(x, y, z, next, changes)
		}
		return
	}
	if block == registry.BlockBambooSapling {
		// vanilla: BambooSaplingBlock.updateShape — a stalk placed on top turns
		// the sapling into a stalk.
		if blocklogic.Opposite(directionOf(d)) == dirUp && registry.BlockOf(p.w.BlockAt(x, y+1, z)) == registry.BlockBamboo {
			p.setClientShaped(x, y, z, registry.BlockBamboo.DefaultState(), changes)
		}
		return
	}
	if block == registry.BlockCreakingHeart {
		// vanilla: CreakingHeartBlock.updateShape — any neighbour change books
		// the heart's reread for the next tick.
		p.schedule(x, y, z, p.now+1)
		return
	}
	if block == registry.BlockPotentSulfur {
		p.potentSulfurShapeUpdate(x, y, z, state, changes)
		return
	}
	if isSpeleothem[block] {
		p.speleothemShapeUpdate(x, y, z, d, state, changes)
		return
	}
	if isWire[block] {
		// Vanilla's shape-update direction points from the updated block toward
		// the changed one (BlockBehaviour.updateNeighbourShapes); the walk
		// delta points the other way.
		p.wireUpdateShape(x, y, z, blocklogic.Opposite(directionOf(d)), changes)
		return
	}
	if block.InTag(registry.TagIDCampfires) {
		p.campfireShapeUpdate(x, y, z, d, state, changes)
		return
	}
	if block == registry.BlockRepeater {
		p.repeaterShapeUpdate(x, y, z, d, state, changes)
		return
	}
	if isObserver[block] {
		// Only a change at the watched face counts, and only while unpowered;
		// the schedule's deduplication is the hasScheduledTick guard.
		// vanilla: ObserverBlock.updateShape
		if !registry.StatePowered(state) {
			if vd := blocklogic.Opposite(directionOf(d)); vd == facingOfState(state) {
				p.schedulePriority(x, y, z, p.now+observerDelay, tickPriorityNormal)
			}
		}
		return
	}
	if block == registry.BlockPistonHead {
		// A change from the base's side takes the head down to air where
		// nothing fits under it; the head's loot table gives nothing.
		// vanilla: PistonHeadBlock.updateShape
		if directionOf(d) == facingOfState(state) && !p.pistonHeadCanSurvive(x, y, z, state) {
			p.breakWithDrop(x, y, z, changes)
		}
		return
	}
	if isTripwire[block] {
		// A horizontal change re-derives that side's connection; a vertical one
		// asks nothing, as the wire survives anywhere.
		// vanilla: TripWireBlock.updateShape
		if vd := blocklogic.Opposite(directionOf(d)); vd != dirUp && vd != dirDown {
			connected := blocklogic.TripwireConnects(p.w.BlockAt(x+dirDelta[vd][0], y, z+dirDelta[vd][2]), vd)
			if connected != registry.StateBool(state, blocklogic.SideProp(vd)) {
				np, _ := registry.WithBool(state, blocklogic.SideProp(vd), connected)
				p.setClientShaped(x, y, z, np, changes)
			}
		}
		return
	}
	if block == registry.BlockNoteBlock {
		p.noteBlockShapeUpdate(x, y, z, d, state, changes)
		return
	}
	if block == registry.BlockBell {
		p.bellShapeUpdate(x, y, z, d, state, changes)
		return
	}
	if block == registry.BlockChorusPlant {
		p.chorusPlantShapeUpdate(x, y, z, d, changes)
		return
	}
	if block == registry.BlockPaleHangingMoss {
		// vanilla: HangingMossBlock.updateShape
		if next := hangingMossShapeUpdate(state, p.w.BlockAt(x, y-1, z)); next != state {
			p.setClientShaped(x, y, z, next, changes)
		}
		return
	}
	if p.doublePlantUpdate(x, y, z, state, [3]int32{-d[0], -d[1], -d[2]}, changes) {
		return
	}
	if isDoorLike[block] {
		p.doorLikeShapeUpdate(x, y, z, d, changes)
	}
	if isBed[block] {
		p.bedShapeUpdate(x, y, z, d, changes)
	}
	if IsChestBlock(block) {
		next := p.chestShapeUpdate(x, y, z, state, d)
		if block.InTag(registry.TagIDCopperChests) {
			next = p.copperChestShapeUpdate(x, y, z, next, d)
		}
		p.setShaped(x, y, z, next, changes)
	}
}

// speleothemShapeUpdate is SpeleothemBlock.updateShape's thickness half: a
// vertical change recomputes this block's thickness, and the write's shape pass
// carries it along the column.
func (p *Physics) speleothemShapeUpdate(x, y, z int32, d [3]int32, state uint16, changes *[]BlockChange) {
	tip := dirIndex(registry.StateValue(state, registry.PropVerticalDirection))
	// vanilla: directionToNeighbour points from this block toward the changed
	// one; the walk delta points the other way.
	toward := blocklogic.Opposite(directionOf(d))
	if toward != dirUp && toward != dirDown {
		return
	}
	// A stalactite with a fall booked keeps the state the fall will lay down.
	if tip == dirDown && p.fallBooked[blockKey{x, y, z}] {
		return
	}
	if next := speleothemShapeState(p.w, state, toward, x, y, z); next != state {
		p.setClientShaped(x, y, z, next, changes)
	}
}

// directionOf turns a delta back into a direction index.
func directionOf(d [3]int32) int {
	switch {
	case d[1] == -1:
		return dirDown
	case d[1] == 1:
		return dirUp
	case d[2] == -1:
		return dirNorth
	case d[2] == 1:
		return dirSouth
	case d[0] == -1:
		return dirWest
	default:
		return dirEast
	}
}

// redstoneTick dispatches scheduled ticks for the redstone blocks and reports
// whether the visit was one of them; its sounds go to the effect sink.
func (p *Physics) redstoneTick(x, y, z int32, state uint16, changes *[]BlockChange) bool {
	cur := p.w.BlockAt(x, y, z)
	// block is the booked state's, curBlock the cell's as it stands now.
	block, curBlock := registry.BlockOf(state), registry.BlockOf(cur)
	switch {
	case curBlock == registry.BlockComposter:
		p.composterTick(x, y, z, cur, changes)
		return true
	case isTorch[curBlock]:
		p.torchTick(x, y, z, curBlock, state, changes, &p.effects)
		return true
	case block == registry.BlockRepeater:
		p.repeaterTick(x, y, z, state, changes)
		return true
	case block == registry.BlockComparator:
		p.comparatorTick(x, y, z, state, facingOfState(state), changes)
		return true
	case isObserver[curBlock]:
		p.observerTick(x, y, z, state, changes)
		return true
	case isLamp[curBlock]:
		p.lampTick(x, y, z, cur, changes)
		return true
	case isPressurePlate[curBlock]:
		p.plateTick(x, y, z, curBlock, cur, changes, &p.effects)
		return true
	case isButton[curBlock]:
		p.buttonTick(x, y, z, curBlock, state, changes, &p.effects)
		return true
	case isTripHook[curBlock]:
		p.tripHookTick(x, y, z, state, changes, &p.effects)
		return true
	case isTripwire[curBlock]:
		p.tripwireTick(x, y, z, state, changes, &p.effects)
		return true
	case curBlock == registry.BlockDetectorRail:
		p.detectorRailTick(x, y, z, changes)
		return true
	case curBlock == registry.BlockTarget:
		p.targetTick(x, y, z, cur, changes)
		return true
	case curBlock == registry.BlockBigDripleaf:
		p.dripleafTick(x, y, z, cur, changes)
		return true
	case curBlock == registry.BlockLectern:
		p.lecternSetPowered(x, y, z, cur, false, changes)
		return true
	case isDispenser[curBlock], isDropper[curBlock]:
		// The dispense the trigger booked is reported to the server, which owns
		// the contents. A block that stopped being a dispenser since the
		// booking falls through the cases above and counts for nothing.
		p.dispenses = append(p.dispenses, Dispense{
			X: x, Y: y, Z: z, Dir: facingOfState(state),
		})
		return true
	case curBlock == registry.BlockCrafter:
		// vanilla: CrafterBlock.tick
		p.dispenses = append(p.dispenses, Dispense{
			X: x, Y: y, Z: z, Dir: crafterFront(cur),
		})
		return true
	case isCommandBlock[curBlock]:
		// The command is the server's to run, so the visit reports itself like
		// a dispense.
		p.commandBlockTick(x, y, z, state)
		return true
	case block == registry.BlockMovingPiston:
		// The ride's landing (PistonMovingBlockEntity.tick) runs in the
		// block-entity pass, outside the handling tick, so a wake it books asks
		// for a plain retraction, never the same-tick abort, and its events
		// wait for the next tick.
		p.eventDelay = 1
		p.handlingTick = false
		p.movingPistonFinalize(x, y, z, changes)
		p.handlingTick = true
		p.eventDelay = 0
		return true
	}
	return false
}

// primeTNT queues the primed TNT for the cell in the wake sink; the block write
// is the caller's and the fuse the server's.
// vanilla: TntBlock.explode
func (p *Physics) primeTNT(x, y, z int32) {
	p.wakePrimes = append(p.wakePrimes, TNTPrime{X: x, Y: y, Z: z})
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// breakWithDrop is a shape update's destruction: a flags-3 write of the held
// fluid, the 2001 event, and the loot rolled against the column as it stood,
// since a tall grass half drops only beside its partner. That is how one item
// comes out of a pair whose halves refuse their own loot.
// vanilla: Level.destroyBlock
func (p *Physics) breakWithDrop(x, y, z int32, changes *[]BlockChange) {
	state := p.w.BlockAt(x, y, z)
	if state == airState {
		return
	}
	column := [2]uint16{p.w.BlockAt(x, y-1, z), p.w.BlockAt(x, y+1, z)}
	before := len(*changes)
	p.set(x, y, z, RemovalLeaves(state), changes)
	if len(*changes) == before {
		return // the write never landed; there is nothing to announce
	}
	p.shapeUpdateNeighbors(x, y, z, changes)
	e := destroyBlockEffect(state, nil, x, y, z)
	e.BreakColumn = &column
	p.effects = append(p.effects, e)
}
