package world

import (
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// DiodePlaced books the power check belonging only to hand placement.
// vanilla: DiodeBlock.setPlacedBy, ComparatorBlock.shouldTurnOn
func (p *Physics) DiodePlaced(x, y, z int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.w.BlockAt(x, y, z)
	f := facingOfState(state)
	var should bool
	switch registry.BlockOf(state) {
	case registry.BlockRepeater:
		should = p.repeaterInput(x, y, z, f) > 0
	case registry.BlockComparator:
		should = p.comparatorShouldTurnOn(x, y, z, f, registry.StateValue(state, registry.PropMode) == "subtract")
	}
	if should {
		p.schedulePriority(x, y, z, p.now+1, tickPriorityNormal)
	}
}

// repeaterTick: a locked repeater does nothing, a powered one whose input fell
// turns off, and an unpowered one turns on, booking another visit if its input
// is already gone so the output holds at least one delay and a short pulse is
// not lost. The client-only write still runs its shape pass (an observer at the
// output sees it), then the on-place front wake.
// vanilla: DiodeBlock.tick
func (p *Physics) repeaterTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	f := facingOfState(state)
	if p.repeaterLocked(x, y, z, f) {
		return
	}
	should := p.repeaterInput(x, y, z, f) > 0
	powered := registry.StatePowered(state)
	// vanilla: DiodeBlock.tick — powered and still fed is the only quiet state;
	// an unfed visit turns a resting repeater on even after its input went.
	if powered && should {
		return
	}
	if !powered && !should {
		// VERY_HIGH, as for a repeater turning off.
		// vanilla: DiodeBlock.tick
		p.schedulePriority(x, y, z, p.now+int64(repeaterDelayOf(state)*2), tickPriorityVeryHigh)
	}
	next, _ := registry.WithBool(state, registry.PropPowered, !powered)
	p.setClientShaped(x, y, z, next, changes)
	p.repeaterWakeFront(x, y, z, f, changes)
}

// repeaterInput is the signal from the block behind, a wire's raw power
// standing in.
// vanilla: DiodeBlock.getInputSignal
func (p *Physics) repeaterInput(x, y, z int32, f int) int {
	// The input is at FACING; that block delivers through its opposite face.
	bx, by, bz := x+dirDelta[f][0], y+dirDelta[f][1], z+dirDelta[f][2]
	in := p.signalThrough(bx, by, bz, blocklogic.Opposite(f))
	if in >= 15 {
		return in
	}
	wstate := p.w.BlockAt(bx, by, bz)
	if isWire[registry.BlockOf(wstate)] {
		if power := registry.StatePower(wstate); power > in {
			in = power
		}
	}
	return in
}

// repeaterWakeFront wakes the block in front and all its neighbours but the
// repeater's side.
// vanilla: DiodeBlock.updateNeighborsInFront
func (p *Physics) repeaterWakeFront(x, y, z int32, f int, changes *[]BlockChange) {
	// The vanilla front is the side opposite FACING — the output side.
	fx, fy, fz := x+dirDelta[blocklogic.Opposite(f)][0], y+dirDelta[blocklogic.Opposite(f)][1], z+dirDelta[blocklogic.Opposite(f)][2]
	p.redstoneNeighborChanged(fx, fy, fz, p.w.BlockAt(fx, fy, fz), changes, false)
	for _, d := range updateNeighborOrder {
		if d == f {
			continue
		}
		nx, ny, nz := fx+dirDelta[d][0], fy+dirDelta[d][1], fz+dirDelta[d][2]
		p.redstoneNeighborChanged(nx, ny, nz, p.w.BlockAt(nx, ny, nz), changes, false)
	}
}

// repeaterCheckTickOnNeighbor schedules a tick when powered disagrees with the
// input and none is waiting, at the priority that lets a repeater feeding a
// diode react first. powered and delay are the repeater's properties.
// vanilla: checkTickOnNeighbor
func (p *Physics) repeaterCheckTickOnNeighbor(x, y, z int32, powered bool, delay, f int) {
	should := p.repeaterInput(x, y, z, f) > 0
	if powered == should {
		return
	}
	if p.repeaterWillTick(x, y, z, p.now) {
		return
	}
	delay *= 2
	priority := tickPriorityHigh
	if p.comparatorShouldPrioritize(x, y, z, f) {
		priority = tickPriorityExtremelyHigh
	} else if powered {
		priority = tickPriorityVeryHigh
	}
	p.schedulePriority(x, y, z, p.now+int64(delay), priority)
}

// repeaterDelayOf reads the repeater's delay property (1-4).
func repeaterDelayOf(state uint16) int {
	return max(stateIntOr(state, registry.PropDelay, 1), 1)
}

func (p *Physics) repeaterWillTick(x, y, z int32, tick int64) bool {
	for _, t := range p.queue {
		if t.pos.x == x && t.pos.y == y && t.pos.z == z && t.due == tick && !p.tickCancelled(t) {
			return true
		}
	}
	return false
}

// repeaterLocked reports a side diode strongly powering through its output.
// vanilla: DiodeBlock.isLocked
func (p *Physics) repeaterLocked(x, y, z int32, f int) bool {
	for _, d := range []int{dirNorth, dirSouth, dirEast, dirWest} {
		if d == f || d == blocklogic.Opposite(f) {
			continue
		}
		sx, sy, sz := x+dirDelta[d][0], y+dirDelta[d][1], z+dirDelta[d][2]
		sstate := p.w.BlockAt(sx, sy, sz)
		if !diodeBlocks[registry.BlockOf(sstate)] {
			continue
		}
		if facingOfState(sstate) != d {
			continue
		}
		if p.directAt(x, y, z, d) > 0 {
			return true
		}
	}
	return false
}

// RepeaterLocked is repeaterLocked for outside callers reading a placement's
// lock before its neighbour update.
func (p *Physics) RepeaterLocked(x, y, z int32, facing int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.repeaterLocked(x, y, z, facing)
}

// repeaterShapeUpdate recomputes LOCKED on a change off the facing axis,
// writing the bar clients draw.
// vanilla: RepeaterBlock.updateShape
func (p *Physics) repeaterShapeUpdate(x, y, z int32, d [3]int32, state uint16, changes *[]BlockChange) {
	// The axis test is symmetric, so the delta's direction needs no flip.
	rDir := directionOf(d)
	f := facingOfState(state)
	if rDir == f || rDir == blocklogic.Opposite(f) {
		return
	}
	locked := boolStr(p.repeaterLocked(x, y, z, f))
	if registry.StateValue(state, registry.PropLocked) == locked {
		return
	}
	next, _ := registry.WithProp(state, registry.PropLocked, locked)
	p.setClientShaped(x, y, z, next, changes)
	p.repeaterWakeFront(x, y, z, f, changes)
}

// cycleRepeater steps the delay 1-4 silently with a flag-3 write, then wakes the
// front as DiodeBlock.onPlace does.
// vanilla: RepeaterBlock.useWithoutItem
func (p *Physics) cycleRepeater(x, y, z int32, state uint16, changes *[]BlockChange) {
	d := repeaterDelayOf(state) + 1
	if d > repeaterDelayMax {
		d = 1
	}
	next, _ := registry.WithInt(state, registry.PropDelay, d)
	p.setShaped(x, y, z, next, changes)
	p.repeaterWakeFront(x, y, z, facingOfState(state), changes)
}
