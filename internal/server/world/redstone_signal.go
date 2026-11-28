package world

import (
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// Signal queries. Vanilla's getSignal(receiver, dir) asks the block at
// receiver.relative(dir), dir pointing from receiver to giver; here a source is
// asked for the signal it delivers through a named face of its own.

// signalThrough returns the weak signal the block delivers through its face d.
// vanilla: BlockState.getSignal (direction turned around)
func (p *Physics) signalThrough(x, y, z int32, d int) int {
	state := p.w.BlockAt(x, y, z)
	kind := signalKindOf(state)
	var s int
	switch kind {
	case kindComparator:
		// The comparator's strength lives in its block entity, so it needs the
		// position the state lookup below lacks.
		s = p.comparatorSignal(x, y, z, state, d)
	case kindTrappedChest:
		s = p.trappedChestSignal(x, y, z)
	case kindJukebox:
		s = p.jukeboxSignal(x, y, z)
	case kindTestBlock:
		s = p.testBlockSignal(x, y, z, state)
	default:
		s = p.signalThroughState(kind, state, d)
	}
	// vanilla: SignalGetter.getSignal — a strongly powered conductor delivers
	// that strength through every face, which is how dust on a block reaches
	// the blocks beside it.
	if s < 15 && p.w.conductorState(x, y, z, state) {
		if strong := p.directSignalTo(x, y, z); strong > s {
			return strong
		}
	}
	return s
}

// signalThroughState is signalThrough for a state in hand.
func (p *Physics) signalThroughState(kind signalKind, state uint16, d int) int {
	switch kind {
	case kindWire:
		// vanilla: RedStoneWireBlock.getSignal — a wire muted during a
		// block-signal query answers nothing, so it cannot read its own power
		// back through the conductor it powers.
		if !p.wireShouldSignal {
			return 0
		}
		power := registry.StatePower(state)
		// A receiver above the wire gets nothing, the block below gets the
		// power, and a side receiver gets it through a connection facing it.
		if d == dirUp {
			return 0
		}
		if d == dirDown {
			return power
		}
		if registry.StateWireSide(state, blocklogic.WireSideIndex(d)) != registry.WireSideNone {
			return power
		}
		return 0
	case kindTorch:
		if !registry.StateLit(state) {
			return 0
		}
		// Only the wall torch has a facing.
		if registry.StateHasFacing(state) {
			// A wall torch's back, the block it hangs on, gets nothing.
			if facing := facingOfState(state); d == blocklogic.Opposite(facing) {
				return 0
			}
			return 15
		}
		// A standing torch reads down, so its down face is dark.
		if d == dirDown {
			return 0
		}
		return 15
	case kindRepeater:
		// A repeater reads at FACING and emits out the back, the face opposite
		// FACING.
		if d == blocklogic.Opposite(facingOfState(state)) {
			if registry.StatePowered(state) {
				return 15
			}
			return 0
		}
		return 0
	case kindObserver:
		// An observer watches FACING and emits out the back, as a repeater
		// does.
		if d == blocklogic.Opposite(facingOfState(state)) {
			if registry.StatePowered(state) {
				return 15
			}
			return 0
		}
		return 0
	case kindRedstoneBlock:
		return 15
	case kindPressurePlate:
		return plateCurrentSignal(registry.BlockOf(state), state)
	case kindButton:
		if registry.StatePowered(state) {
			return 15
		}
		return 0
	case kindDaylightDetector:
		// The power is the detector's signal out of every face, none of them
		// strong.
		// vanilla: DaylightDetectorBlock.ownSignal, BlockBehaviour.getDirectSignal
		return registry.StatePower(state)
	case kindTripHook:
		// vanilla: TripWireHookBlock.ownSignal — a powered hook answers weakly
		// out of every face, attached or bare.
		if registry.StatePowered(state) {
			return 15
		}
		return 0
	case kindTarget:
		// Like the daylight detector: the power a projectile's hit left.
		// vanilla: TargetBlock.ownSignal, BlockBehaviour.getDirectSignal
		return registry.StatePower(state)
	case kindDetectorRail:
		// POWERED is the rail's signal out of every face, none of them strong.
		// vanilla: DetectorRailBlock.ownSignal, BlockBehaviour.getDirectSignal
		if registry.StatePowered(state) {
			return 15
		}
		return 0
	case kindLectern:
		// vanilla: LecternBlock.ownSignal
		if registry.StatePowered(state) {
			return 15
		}
		return 0
	case kindSculkSensor:
		// The calibrated sensor stays silent out of its back face.
		// vanilla: SculkSensorBlock.ownSignal, CalibratedSculkSensorBlock.getSignal
		if registry.BlockOf(state) == registry.BlockCalibratedSculkSensor && d == blocklogic.Opposite(facingOfState(state)) {
			return 0
		}
		return registry.StatePower(state)
	case kindLightningRod:
		// vanilla: LightningRodBlock.ownSignal
		if registry.StatePowered(state) {
			return 15
		}
		return 0
	}
	if kind == kindLever {
		if registry.StatePowered(state) {
			return 15
		}
		return 0
	}
	return 0
}

// directThrough returns the strong signal the block delivers through its face
// d; only the faces a block strongly powers answer.
// vanilla: BlockState.getDirectSignal
func (p *Physics) directThrough(x, y, z int32, d int) int {
	state := p.w.BlockAt(x, y, z)
	kind := signalKindOf(state)
	switch kind {
	case kindWire:
		// A wire's strong signal is its weak one
		// (RedStoneWireBlock.getDirectSignal delegates to getSignal).
		return p.signalThroughState(kind, state, d)
	case kindTorch:
		// Strong only into the block above, the receiver asking from DOWN.
		if d == dirUp {
			return p.signalThroughState(kind, state, d)
		}
		return 0
	case kindLever, kindButton:
		attached := attachedFace(state)
		if d == attached {
			return p.signalThroughState(kind, state, d)
		}
		return 0
	case kindPressurePlate:
		if d == dirDown {
			return p.signalThroughState(kind, state, d)
		}
		return 0
	case kindRepeater, kindObserver:
		return p.signalThroughState(kind, state, d)
	case kindComparator:
		return p.comparatorSignal(x, y, z, state, d)
	case kindTrappedChest:
		// Strong only into the block below. vanilla: TrappedChestBlock.getDirectSignal
		if d == dirDown {
			return p.trappedChestSignal(x, y, z)
		}
		return 0
	case kindDetectorRail:
		// Strong into the block it sits on.
		// vanilla: DetectorRailBlock.getDirectSignal
		if d == dirDown && registry.StatePowered(state) {
			return 15
		}
		return 0
	case kindLectern:
		// Strong into the block below. vanilla: LecternBlock.getDirectSignal
		if d == dirDown && registry.StatePowered(state) {
			return 15
		}
		return 0
	case kindSculkSensor:
		// Strong into the block below. vanilla: SculkSensorBlock.getDirectSignal
		if d == dirDown {
			return registry.StatePower(state)
		}
		return 0
	case kindLightningRod:
		// Strong into the block the rod points away from.
		// vanilla: LightningRodBlock.getDirectSignal
		if registry.StatePowered(state) && d == blocklogic.Opposite(facingOfState(state)) {
			return 15
		}
		return 0
	case kindTripHook:
		// vanilla: TripWireHookBlock.getDirectSignal — strong only into the
		// block the hook hangs on.
		if registry.StatePowered(state) && d == blocklogic.Opposite(facingOfState(state)) {
			return 15
		}
		return 0
	}
	return 0
}

// attachedFace is the face a lever or button strongly powers through: the face
// of its mount, down for a floor one and up for a ceiling one.
func attachedFace(state uint16) int {
	face := registry.StateValue(state, registry.PropFace)
	if b := registry.BlockOf(state); b == registry.BlockLever || isButton[b] {
		switch face {
		case "ceiling":
			return dirUp
		case "wall":
			return blocklogic.Opposite(facingOfState(state))
		default:
			return dirDown
		}
	}
	return dirDown
}

// signalAt returns the signal the neighbour in direction d delivers toward (x,
// y, z).
// vanilla: Level.getSignal
func (p *Physics) signalAt(x, y, z int32, d int) int {
	gx, gy, gz := x+dirDelta[d][0], y+dirDelta[d][1], z+dirDelta[d][2]
	return p.signalThrough(gx, gy, gz, blocklogic.Opposite(d))
}

// directAt returns the strong signal the neighbour in direction d delivers
// toward (x, y, z).
// vanilla: Level.getDirectSignal
func (p *Physics) directAt(x, y, z int32, d int) int {
	gx, gy, gz := x+dirDelta[d][0], y+dirDelta[d][1], z+dirDelta[d][2]
	return p.directThrough(gx, gy, gz, blocklogic.Opposite(d))
}

// directSignalTo returns the strongest signal the six neighbours deliver into
// (x, y, z), what a conductor passes on; it stops at 15.
// vanilla: SignalGetter.getDirectSignalTo
func (p *Physics) directSignalTo(x, y, z int32) int {
	best := 0
	for _, d := range updateNeighborOrder {
		if s := p.directAt(x, y, z, d); s > best {
			best = s
			if best >= 15 {
				break
			}
		}
	}
	return best
}

// hasNeighborSignal reports whether any neighbour signals toward (x, y, z).
// vanilla: SignalGetter.hasNeighborSignal
func (p *Physics) hasNeighborSignal(x, y, z int32) bool {
	for d := range dirCount {
		if p.signalAt(x, y, z, d) > 0 {
			return true
		}
	}
	return false
}

// HasNeighborSignal is hasNeighborSignal for callers outside the engine (a door
// placed beside a live source). It takes the engine's lock, which
// hasNeighborSignal cannot under the neighbour updates it runs in.
func (p *Physics) HasNeighborSignal(x, y, z int32) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hasNeighborSignal(x, y, z)
}

// bestNeighborSignal returns the strongest signal any neighbour delivers toward
// (x, y, z).
// vanilla: SignalGetter.getBestNeighborSignal
func (p *Physics) bestNeighborSignal(x, y, z int32) int {
	best := 0
	for d := range dirCount {
		signal := p.signalAt(x, y, z, d)
		if signal >= 15 {
			return 15
		}
		best = max(best, signal)
	}
	return best
}

// NeighborSignal is bestNeighborSignal for callers outside the engine.
func (p *Physics) NeighborSignal(x, y, z int32) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.bestNeighborSignal(x, y, z)
}

// HasOwnOrNeighborSignal includes a source occupying the queried cell.
// vanilla: SignalGetter.getBestOwnOrNeighbourSignal
func (p *Physics) HasOwnOrNeighborSignal(x, y, z int32) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.w.BlockAt(x, y, z)
	kind := signalKindOf(state)
	if kind == kindOther {
		return p.hasNeighborSignal(x, y, z)
	}
	// Wire ownSignal does not depend on its directional connections.
	if kind == kindWire && registry.StatePower(state) > 0 {
		return true
	}
	for _, d := range updateNeighborOrder {
		var signal int
		switch kind {
		case kindComparator:
			signal = p.comparatorSignal(x, y, z, state, d)
		case kindTrappedChest:
			signal = p.trappedChestSignal(x, y, z)
		case kindJukebox:
			signal = p.jukeboxSignal(x, y, z)
		case kindTestBlock:
			signal = p.testBlockSignal(x, y, z, state)
		default:
			signal = p.signalThroughState(kind, state, d)
		}
		if signal > 0 {
			return true
		}
	}
	return p.hasNeighborSignal(x, y, z)
}
