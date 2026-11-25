package world

import (
	"github.com/google/uuid"

	"github.com/golem-mc/golem/registry"
)

var isTouchBlock = registry.BlocksOf(func(b registry.Block) bool {
	return isPressurePlate[b] || isButton[b] || isTripwire[b] || b == registry.BlockDetectorRail || b == registry.BlockBigDripleaf
})

// LoadedTouchBlocks counts the blocks in loaded chunks that the touch pass can react to.
func (w *World) LoadedTouchBlocks() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.touchBlocks
}

// TouchBlocksIn reports whether a loaded section overlapping the inclusive
// cell box contains a block the entity-inside touch pass reacts to.
// Near-cache reads follow BlockAt: an unloading chunk reads as it left.
func (w *World) TouchBlocksIn(x0, y0, z0, x1, y1, z1 int32) bool {
	return w.sectionCountIn(x0, y0, z0, x1, y1, z1, func(s *section) int32 { return s.touches.Load() })
}

// sectionCountIn reports whether count is positive for a loaded section
// overlapping the inclusive cell box.
func (w *World) sectionCountIn(x0, y0, z0, x1, y1, z1 int32, count func(*section) int32) bool {
	lo := max((y0-worldMinY)>>4, 0)
	hi := min((y1-worldMinY)>>4, worldHeight/sectionSize-1)
	if lo > hi {
		return false
	}
	for chx := x0 >> 4; chx <= x1>>4; chx++ {
		for chz := z0 >> 4; chz <= z1>>4; chz++ {
			pos := ChunkPos{X: chx, Z: chz}
			loaded := w.nearAt(pos)
			if loaded == nil {
				w.mu.RLock()
				loaded = w.chunks[pos]
				if loaded != nil {
					w.near[nearIndex(pos)].Store(loaded)
				}
				w.mu.RUnlock()
			}
			if loaded == nil {
				continue
			}
			for sec := lo; sec <= hi; sec++ {
				if count(&loaded.chunk.sections[sec]) > 0 {
					return true
				}
			}
		}
	}
	return false
}

// Toucher answers what stands on a plate, holds a button down or crosses a
// tripwire: the server owns the entities and implements it; tests can stub it.
type Toucher interface {
	// Touching reports how many living entities, and entities of any kind,
	// intersect the cell.
	Touching(x, y, z int32) (living, all int)
	// ArrowsIn reports how many arrows intersect box, the count a wooden button
	// reads.
	// vanilla: ButtonBlock.checkPressed
	ArrowsIn(box Box) int
	// Pressing reports how many entities of any kind intersect box, the count a
	// tripwire reads.
	// vanilla: TripWireBlock.checkPressed
	Pressing(box Box) int
	// Presser names the first entity intersecting the cell, the zero
	// EventSource when none is a player or mob.
	// vanilla: BasePressurePlateBlock.checkPressed
	Presser(x, y, z int32) EventSource
}

// EventSource is the entity behind a game event: a player or a mob, with
// whether it sneaks. The zero value is no source.
type EventSource struct {
	Player, Mob uuid.UUID
	Sneaking    bool
}

// SetToucher sets the engine's entity source; without one nothing stands on
// anything.
func (p *Physics) SetToucher(t Toucher) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.toucher = t
}

func (p *Physics) touchTouching(x, y, z int32) (living, all int) {
	if p.toucher == nil {
		return 0, 0
	}
	return p.toucher.Touching(x, y, z)
}

// EntityInside runs the cell block's entity-inside reaction, the
// BlockState.entityInside half of the movement sweep: plates, wooden buttons,
// tripwire and detector rails.
func (p *Physics) EntityInside(x, y, z int32, changes *[]BlockChange, effects *[]Effect) {
	// Most cells hold none of them.
	p.entityInside(x, y, z, p.w.BlockAt(x, y, z), changes, effects)
}

func (p *Physics) entityInside(x, y, z int32, state uint16, changes *[]BlockChange, effects *[]Effect) {
	b := registry.BlockOf(state)
	if !isPressurePlate[b] && !isButton[b] && !isTripwire[b] && b != registry.BlockDetectorRail {
		return
	}
	switch {
	case b == registry.BlockDetectorRail:
		// The rail checks only while unpowered: the sweep repeats every tick a
		// body is inside, and the scheduled re-check holds the press.
		// vanilla: DetectorRailBlock.entityInside
		if registry.StateValue(state, registry.PropPowered) != "false" {
			return
		}
		p.detectorRailCheckPressed(x, y, z, changes)
	case isPressurePlate[b]:
		if !plateIsWeighted(b) && registry.StateValue(state, registry.PropPowered) != "false" {
			return
		}
		if plateIsWeighted(b) {
			if registry.StatePower(state) != 0 {
				return
			}
		}
		// The plate checks only at zero signal; the sweep repeats every tick,
		// so the first sweep presses and the scheduled tick holds it.
		p.plateCheckPressed(x, y, z, b, 0, changes, effects)
	case isButton[b]:
		// An arrow presses a wooden button as a use press does.
		if registry.StateValue(state, registry.PropPowered) == "false" && buttonOpensByArrow(b) {
			if p.buttonArrows(x, y, z, state) > 0 {
				p.pressButton(x, y, z, b, state, false, changes, effects)
			}
		}
	case isTripwire[b]:
		// Only a wire neither powered nor awaiting its re-check answers; the
		// press books one, which releases the wire ten ticks after the last
		// body leaves.
		// vanilla: TripWireBlock.entityInside
		if registry.StatePowered(state) || p.pending[blockKey{x, y, z}] {
			return
		}
		p.tripwireCheckPressed(x, y, z, state, changes, effects)
	}
}

// The touch pass the server drives.

// Box is one axis-aligned box, the shape the touch questions are asked in.
type Box struct {
	MinX, MinY, MinZ, MaxX, MaxY, MaxZ float64
}

// TouchBox is a pressure plate's listening area: a quarter block high, inset a
// sixteenth from the sides.
// vanilla: BasePressurePlateBlock.TOUCH_AABB
func TouchBox(x, y, z int32) Box {
	const e = 1.0 / 16.0
	return Box{
		MinX: float64(x) + e, MinY: float64(y), MinZ: float64(z) + e,
		MaxX: float64(x+1) - e, MaxY: float64(y) + 0.25, MaxZ: float64(z+1) - e,
	}
}

// EntityInsideCells runs the touch reactions in an inclusive cell box, with
// grounded dripleaf tilts after the redstone reactions. ey is the feet height.
func (p *Physics) EntityInsideCells(x0, y0, z0, x1, y1, z1 int32, ey float64, onGround bool, changes *[]BlockChange, effects *[]Effect, primes *[]TNTPrime) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.touchLeaves = p.touchLeaves[:0]
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				state := p.w.BlockAt(x, y, z)
				p.entityInside(x, y, z, state, changes, effects)
				// vanilla: BigDripleafBlock.canEntityTilt (feet above 11/16).
				if onGround && ey > float64(float32(y)+0.6875) && registry.BlockOf(state) == registry.BlockBigDripleaf {
					p.touchLeaves = append(p.touchLeaves, blockKey{x, y, z})
				}
			}
		}
	}
	// TNT a press primed is handed out under the lock that queued it, so a
	// concurrent wake cannot clear it first.
	wakeEffects, wakePrimes := p.takeWake()
	*effects = append(*effects, wakeEffects...)
	*primes = append(*primes, wakePrimes...)
	// Plates may power a leaf later in the box: test its final state and
	// neighbor signal only after every redstone reaction has run.
	for _, pos := range p.touchLeaves {
		p.dripleafStoodOn(pos.x, pos.y, pos.z, changes, effects)
	}
}

// ToucherMissing reports whether no entity source is set.
func (p *Physics) ToucherMissing() bool { return p.toucher == nil }

// FireSpreader answers whether a fire at the cell may tick, vanilla's
// canSpreadFireAround: some player within the fire_spread_radius_around_player
// game rule of the cell, or anywhere when the rule is -1.
type FireSpreader interface {
	// FireCanSpread reports whether the fire may run its whole visit: survival,
	// burn-out and spread.
	FireCanSpread(x, y, z int32) bool
}

// SetFireSpreader sets the engine's player source; without one fire ticks
// everywhere, as a rule of -1 reads.
func (p *Physics) SetFireSpreader(s FireSpreader) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.spreader = s
}

// SpreaderMissing reports whether no player source is set.
func (p *Physics) SpreaderMissing() bool { return p.spreader == nil }
