package world

import (
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// The tripwire. A hook scans its facing up to WIRE_DIST_MAX-1 cells, holes
// included, for string and a partner hook; both hooks take attached and powered
// from the scan, and the string takes attached when the pair's flips. A body
// crossing the string's band powers it, which the hooks copy: weak out of every
// face, strong into their support.
// vanilla: TripWireHookBlock, TripWireBlock

// tripwireSearchMax is the exclusive bound of both scans; both re-check ten
// ticks out.
// vanilla: TripWireHookBlock.WIRE_DIST_MAX, RECHECK_PERIOD
const (
	tripwireSearchMax = 42
	tripwireRecheck   = 10
)

// tripHookState is the state of a hook with that facing and flags.
func tripHookState(facing string, attached, power bool) uint16 {
	id, _ := registry.WithProps(registry.BlockTripwireHook.DefaultState(),
		registry.PropValue{P: registry.PropFacing, V: facing},
		registry.PropValue{P: registry.PropAttached, V: boolStr(attached)},
		registry.PropValue{P: registry.PropPowered, V: boolStr(power)})
	return id
}

// TripwireHookPlaced scans a player-placed hook's line at once; /setblock never
// runs it.
// vanilla: TripWireHookBlock.setPlacedBy
func (p *Physics) TripwireHookPlaced(x, y, z int32) ([]BlockChange, []Effect, []TNTPrime) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.w.BlockAt(x, y, z)
	if !isTripHook[registry.BlockOf(state)] {
		return nil, nil, nil
	}
	var changes []BlockChange
	p.effects, p.wakePrimes = nil, nil
	p.tripHookCalculate(x, y, z, facingOfState(state), tripHookAttached(state), registry.StatePowered(state), false, false, -1, 0, &changes, &p.effects)
	effects, primes := p.takeWake()
	return changes, effects, primes
}

// tripHookCalculate re-reads the hook's line and sets its attached and powered,
// the partner's, and the string's attached. The hooks' write takes attached
// from before the receiver check narrows it and powered after, the evaluation
// order of vanilla's trySetValue arguments, so a hook no partner reaches still
// parks ATTACHED on itself. wireSource is the string cell that triggered the
// visit, whose vanished state wireSourceState stands in for.
// vanilla: TripWireHookBlock.calculateState
func (p *Physics) tripHookCalculate(x, y, z int32, d int, wasAttached, wasPower, isBeingDestroyed, canUpdate bool, wireSource int, wireSourceState uint16, changes *[]BlockChange, effects *[]Effect) {
	attached := !isBeingDestroyed
	power := false
	receiverPos := 0
	var wireStates [tripwireSearchMax]uint16
	for i := 1; i < tripwireSearchMax; i++ {
		wState := p.w.BlockAt(x+dirDelta[d][0]*int32(i), y, z+dirDelta[d][2]*int32(i))
		if i == wireSource && wireSourceState != 0 {
			wState = wireSourceState
		}
		wBlock := registry.BlockOf(wState)
		if isTripHook[wBlock] {
			// The partner must face back; the scan stops at the first hook
			// either way.
			if facingOfState(wState) != blocklogic.Opposite(d) {
				break
			}
			receiverPos = i
			break
		}
		if isTripwire[wBlock] || i == wireSource {
			armed := !registry.StateBool(wState, registry.PropDisarmed)
			power = power || armed && registry.StatePowered(wState)
			wireStates[i] = wState
			if i == wireSource {
				p.schedulePriority(x, y, z, p.now+tripwireRecheck, tickPriorityNormal)
				attached = attached && armed
			}
			continue
		}
		attached = false
	}
	newAttached := attached
	attached = attached && receiverPos > 1
	power = power && attached
	if receiverPos > 0 {
		rx, rz := x+dirDelta[d][0]*int32(receiverPos), z+dirDelta[d][2]*int32(receiverPos)
		p.setShaped(rx, y, rz, tripHookState(blocklogic.FaceName(blocklogic.Opposite(d)), newAttached, power), changes)
		p.tripHookNotifyNeighbors(rx, y, rz, blocklogic.Opposite(d), changes)
		if !isTripHook[registry.BlockOf(p.w.BlockAt(x, y, z))] {
			// The hook's own cell no longer holds one, always so on the removal
			// path; the early return leaves the string's flags and the sounds
			// to a later visit.
			p.tripHookOnRemoved(x, y, z, d, newAttached, power, changes, effects)
			return
		}
		p.tripEmitState(rx, y, rz, attached, power, wasAttached, wasPower, effects)
	}
	p.tripEmitState(x, y, z, attached, power, wasAttached, wasPower, effects)
	if !isBeingDestroyed {
		p.setShaped(x, y, z, tripHookState(blocklogic.FaceName(d), newAttached, power), changes)
		if canUpdate {
			p.tripHookNotifyNeighbors(x, y, z, d, changes)
		}
	}
	if wasAttached != attached {
		for i := 1; i < receiverPos; i++ {
			if wireStates[i] == 0 {
				continue
			}
			ws, ok := registry.WithBool(wireStates[i], registry.PropAttached, attached)
			if !ok {
				continue
			}
			cx, cz := x+dirDelta[d][0]*int32(i), z+dirDelta[d][2]*int32(i)
			if cur := registry.BlockOf(p.w.BlockAt(cx, y, cz)); !isTripwire[cur] && !isTripHook[cur] {
				continue
			}
			p.setShaped(cx, y, cz, ws, changes)
		}
	}
}

// tripHookNotifyNeighbors wakes the six neighbours of the cell and of the block
// behind it per facing, the block the hook's strong power reaches; the receiver
// write and the re-check both name their hook this way.
// vanilla: TripWireHookBlock.notifyNeighbors
func (p *Physics) tripHookNotifyNeighbors(x, y, z int32, facing int, changes *[]BlockChange) {
	p.wireWakeNeighbors(x, y, z, changes)
	dx, dy, dz := dirDelta[blocklogic.Opposite(facing)][0], dirDelta[blocklogic.Opposite(facing)][1], dirDelta[blocklogic.Opposite(facing)][2]
	p.wireWakeNeighbors(x+dx, y+dy, z+dz, changes)
}

// tripEmitState plays the click of the hook's power and attach transitions, at
// volume 0.4.
// vanilla: TripWireHookBlock.emitState
func (p *Physics) tripEmitState(x, y, z int32, attached, power, wasAttached, wasPower bool, effects *[]Effect) {
	var sound registry.Sound
	var event registry.GameEvent
	var pitch float32
	switch {
	case power && !wasPower:
		sound, pitch, event = registry.SoundBlockTripwireClickOn, 0.6, GameEventBlockActivate
	case !power && wasPower:
		sound, pitch, event = registry.SoundBlockTripwireClickOff, 0.5, GameEventBlockDeactivate
	case attached && !wasAttached:
		sound, pitch, event = registry.SoundBlockTripwireAttach, 0.7, GameEventBlockAttach
	case !attached && wasAttached:
		sound, event = registry.SoundBlockTripwireDetach, GameEventBlockDetach
		pitch = 1.2 / (p.rng.Float32()*0.2 + 0.9)
	default:
		return
	}
	*effects = append(*effects, Effect{SoundID: sound, X: x, Y: y, Z: z, Volume: 0.4, Pitch: pitch, GameEvent: event})
}

// tripHookOnRemoved is shared by the hook's removal wake and calculateState's
// replaced-hook guard: an attached or powered hook scans back as destroyed,
// detaching the line, and a powered one wakes its support.
// vanilla: TripWireHookBlock.affectNeighborsAfterRemoval, onRemoved
func (p *Physics) tripHookOnRemoved(x, y, z int32, f int, attached, power bool, changes *[]BlockChange, effects *[]Effect) {
	if attached || power {
		p.tripHookCalculate(x, y, z, f, attached, power, true, false, -1, 0, changes, effects)
	}
	if power {
		p.tripHookNotifyNeighbors(x, y, z, f, changes)
	}
}

// tripHookAttached reads a hook's "attached" property.
func tripHookAttached(state uint16) bool { return registry.StateBool(state, registry.PropAttached) }

// tripHookTick runs the re-check a string's change booked, with canUpdate so
// the re-read power reaches the block behind the hook.
// vanilla: TripWireHookBlock.tick
func (p *Physics) tripHookTick(x, y, z int32, state uint16, changes *[]BlockChange, effects *[]Effect) {
	p.tripHookCalculate(x, y, z, facingOfState(state), tripHookAttached(state), registry.StatePowered(state), false, true, -1, 0, changes, effects)
}

// tripwireOnPlace gives the hook behind the new wire's run a visit.
// vanilla: TripWireBlock.onPlace
func (p *Physics) tripwireOnPlace(x, y, z int32, state uint16, changes *[]BlockChange, effects *[]Effect) {
	p.tripwireUpdateSource(x, y, z, state, changes, effects)
}

// tripwireUpdateSource follows the strings south and west of the wire along a
// consecutive run to a hook facing back, whose calculateState re-reads the line
// with this wire as trigger; the far hook takes its state from the receiver
// write.
// vanilla: TripWireBlock.updateSource
func (p *Physics) tripwireUpdateSource(x, y, z int32, state uint16, changes *[]BlockChange, effects *[]Effect) {
	for _, d := range []int{dirSouth, dirWest} {
		for i := 1; i < tripwireSearchMax; i++ {
			nx, nz := x+dirDelta[d][0]*int32(i), z+dirDelta[d][2]*int32(i)
			nBlock := registry.BlockOf(p.w.BlockAt(nx, y, nz))
			if !isTripwire[nBlock] && !isTripHook[nBlock] {
				break
			}
			if isTripHook[nBlock] {
				nState := p.w.BlockAt(nx, y, nz)
				if facingOfState(nState) != blocklogic.Opposite(d) {
					break
				}
				p.tripHookCalculate(nx, y, nz, facingOfState(nState), tripHookAttached(nState), registry.StatePowered(nState), false, true, i, state, changes, effects)
				break
			}
		}
	}
}

// tripwireCheckPressed sets POWERED from whether anything is inside the band,
// writes a change and wakes the hooks; a press keeps a re-check booked, which
// is what unpowers the wire once bodies leave.
// vanilla: TripWireBlock.checkPressed
func (p *Physics) tripwireCheckPressed(x, y, z int32, state uint16, changes *[]BlockChange, effects *[]Effect) {
	wasPressed := registry.StatePowered(state)
	pressed := p.tripwirePressedIn(x, y, z, registry.StateBool(state, registry.PropAttached)) > 0
	if pressed != wasPressed {
		state, ok := registry.WithBool(p.w.BlockAt(x, y, z), registry.PropPowered, pressed)
		if !ok {
			return
		}
		p.setShaped(x, y, z, state, changes)
		p.tripwireUpdateSource(x, y, z, state, changes, effects)
	}
	if pressed {
		p.schedulePriority(x, y, z, p.now+tripwireRecheck, tickPriorityNormal)
	} else if wasPressed {
		p.schedule(x, y, z, p.now)
	}
}

// tripwirePressedIn counts the bodies in the wire's band: 1/16..2.5/16 high
// once attached, half a cell when slack.
// vanilla: TripWireBlock.getShape
func (p *Physics) tripwirePressedIn(x, y, z int32, attached bool) int {
	if p.toucher == nil {
		return 0
	}
	minY, maxY := 0.0, 8.0/16.0
	if attached {
		minY, maxY = 1.0/16.0, 2.5/16.0
	}
	return p.toucher.Pressing(Box{
		MinX: float64(x), MinY: float64(y) + minY, MinZ: float64(z),
		MaxX: float64(x + 1), MaxY: float64(y) + maxY, MaxZ: float64(z + 1),
	})
}

// tripwireTick runs the re-check a press booked, holding the wire down while
// bodies stand in it.
// vanilla: TripWireBlock.tick
func (p *Physics) tripwireTick(x, y, z int32, state uint16, changes *[]BlockChange, effects *[]Effect) {
	if !registry.StatePowered(state) {
		return
	}
	p.tripwireCheckPressed(x, y, z, state, changes, effects)
}
