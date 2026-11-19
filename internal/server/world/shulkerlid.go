package world

import "github.com/golem-mc/golem/registry"

// shulkerLidTicks is how many ticks the lid takes to open or close: its
// progress moves 0.1 a tick between 0 and 1.
// vanilla: ShulkerBoxBlockEntity.updateAnimation
const shulkerLidTicks = 10

type shulkerPhase uint8

const (
	shulkerOpening shulkerPhase = iota
	shulkerOpened
	shulkerClosing
)

// ShulkerShove is one tick of an opening lid: the box's cell, its facing and
// the opening before and after, 0 to 1, for the server to shove bodies on it.
type ShulkerShove struct {
	X, Y, Z  int32
	Facing   string
	From, To float32
}

// TakeShulkerShoves returns and clears the lids that opened this tick.
func (p *Physics) TakeShulkerShoves() []ShulkerShove {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.shulkerShoves
	p.shulkerShoves = nil
	return out
}

// shulkerLid is one box's lid animation; the CLOSED phase has no entry.
type shulkerLid struct {
	phase shulkerPhase
	// progress is the lid's opening in tenths, 0 to shulkerLidTicks.
	progress int
}

// ShulkerViewersChanged sets how many players look into the shulker box. The
// first viewer starts the lid opening and the last to leave starts it closing;
// its collision stops being a cube on the first animation tick. The engine's
// tick tells the neighbours of each phase change.
// vanilla: ShulkerBoxBlockEntity.triggerEvent
func (p *Physics) ShulkerViewersChanged(x, y, z int32, viewers int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pos := [3]int32{x, y, z}
	lid := p.shulkerLids[pos]
	if _, moving := p.w.shulkerLidsOpen.Load(pos); !moving {
		lid = nil // the box was replaced: a new block entity starts CLOSED
	}
	switch {
	case viewers > 0 && (lid == nil || lid.phase == shulkerClosing):
		if lid == nil {
			lid = &shulkerLid{}
			if p.shulkerLids == nil {
				p.shulkerLids = map[[3]int32]*shulkerLid{}
			}
			p.shulkerLids[pos] = lid
			p.w.shulkerLidsOpen.Store(pos, 0)
		}
		lid.phase = shulkerOpening
	case viewers == 0 && lid != nil && lid.phase != shulkerClosing:
		lid.phase = shulkerClosing
	}
}

// shulkerLidTicks steps every moving lid and runs vanilla's neighbour updates
// when a lid starts or finishes moving.
// vanilla: ShulkerBoxBlockEntity.updateAnimation
func (p *Physics) shulkerLidTicks(changes *[]BlockChange) {
	for pos, lid := range p.shulkerLids {
		x, y, z := pos[0], pos[1], pos[2]
		if _, moving := p.w.shulkerLidsOpen.Load(pos); !moving {
			delete(p.shulkerLids, pos) // SetBlock dropped it: the box was replaced
			continue
		}
		if !registry.BlockOf(p.w.BlockAt(x, y, z)).IsShulkerBox() {
			delete(p.shulkerLids, pos)
			p.w.shulkerLidsOpen.Delete(pos)
			continue
		}
		if !p.chunkTicksNow(x, z) {
			continue
		}
		was := lid.progress
		switch lid.phase {
		case shulkerOpening:
			lid.progress++
			p.shulkerShoves = append(p.shulkerShoves, ShulkerShove{X: x, Y: y, Z: z,
				Facing: registry.StateValue(p.w.BlockAt(x, y, z), registry.PropFacing),
				From:   float32(was) / shulkerLidTicks, To: float32(lid.progress) / shulkerLidTicks})
			if was == 0 {
				// Neighbour updates read this tick's collision shape.
				p.w.shulkerLidsOpen.Store(pos, lid.progress)
				p.shulkerNeighbourUpdates(x, y, z, changes)
			}
			if lid.progress >= shulkerLidTicks {
				lid.phase = shulkerOpened
				p.shulkerNeighbourUpdates(x, y, z, changes)
			}
		case shulkerClosing:
			lid.progress--
			if was == shulkerLidTicks {
				p.shulkerNeighbourUpdates(x, y, z, changes)
			}
			if lid.progress <= 0 {
				delete(p.shulkerLids, pos)
				p.w.shulkerLidsOpen.Delete(pos)
				p.shulkerNeighbourUpdates(x, y, z, changes)
			}
		}
		if _, moving := p.w.shulkerLidsOpen.Load(pos); moving && p.shulkerLids[pos] == lid {
			p.w.shulkerLidsOpen.Store(pos, lid.progress)
		}
	}
}

// shulkerNeighbourUpdates is doNeighborUpdates: the neighbours' shape
// update, then their neighbour update.
// vanilla: ShulkerBoxBlockEntity.doNeighborUpdates
func (p *Physics) shulkerNeighbourUpdates(x, y, z int32, changes *[]BlockChange) {
	state := p.w.BlockAt(x, y, z)
	p.shapeUpdateNeighbors(x, y, z, changes)
	p.wakeNeighbors(x, y, z, state, state, changes)
}
