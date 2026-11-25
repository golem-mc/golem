package world

import "github.com/google/uuid"

// HookKind names one physics-side event a BlockHooks listener may veto or edit.
type HookKind uint8

const (
	HookBurn HookKind = iota
	HookIgnite
	HookSpread
	HookForm
	HookFade
	HookGrow
	HookFromTo
	HookPhysics
	HookRedstone
	HookPiston
	HookLeaves
	HookSponge
	HookNote
	HookCauldron
	HookFertilize
	HookStructure
	HookMoisture
)

// Causes of HookIgnite.
const (
	HookCauseLava uint8 = iota
	HookCauseSpread
)

// HookDecision is one question to the listeners: the rule fills the inputs, the
// listeners set Cancel and the editable fields. X, Y, Z is the cell the rule
// is about to write; Old is the state there when asked.
type HookDecision struct {
	Kind    HookKind
	X, Y, Z int32
	Old     uint16
	// New is the state about to be written (Spread, Form, Fade, Grow), editable.
	New uint16
	// From is the cell the event comes from: the fire that spread, the fluid
	// that flowed, the neighbour that changed (Physics, as a position).
	From  [3]int32
	Cause uint8
	// Power is the signal before the change in OldPower and after it in Power,
	// editable (Redstone).
	OldPower, Power int
	// Dir is the push direction of a Piston, Extending whether it extends.
	Dir       int
	Extending bool
	// Blocks are the cells a Piston moves or a Sponge soaks up; a listener may
	// trim a Sponge's.
	Blocks [][3]int32
	// Instrument and Note are what a Note block plays, editable.
	Instrument string
	Note       int
	// Cells are the cells a Fertilize or Structure growth sets, with the state
	// each gets, editable; Actor is the player who bone-mealed it, uuid.Nil
	// for none.
	Cells  []BlockChange
	Actor  uuid.UUID
	Cancel bool
}

// BlockHooks lets the server's plugins veto and edit physics' writes.
type BlockHooks interface {
	// Listening reports whether any listener wants kind; it must be cheap, the
	// rules ask it before building a decision.
	Listening(kind HookKind) bool
	// Decide runs the listeners with Physics.mu released, on any goroutine.
	Decide(d *HookDecision)
}

// SetBlockHooks sets the plugin hooks; without them nothing is asked.
func (p *Physics) SetBlockHooks(h BlockHooks) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hooks = h
}

// listens is the gate every rule passes before it builds a decision.
func (p *Physics) listens(kind HookKind) bool {
	return p.hooks != nil && p.hooks.Listening(kind)
}

// ask runs the listeners with mu released, as they may call back into the
// world, and reports a veto. A cell that changed meanwhile counts as one: the
// rule decided about a state that is gone. mu is released only here, so the
// caller holds no state another goroutine could change across it.
func (p *Physics) ask(d *HookDecision) bool {
	if p.hooks == nil {
		return false
	}
	p.mu.Unlock()
	p.hooks.Decide(d)
	p.mu.Lock()
	return d.Cancel || p.w.BlockAt(d.X, d.Y, d.Z) != d.Old
}

// veto asks about replacing the block at x, y, z by state and returns the state
// to write, false when a listener vetoed it.
func (p *Physics) veto(kind HookKind, x, y, z int32, state uint16, from [3]int32) (uint16, bool) {
	if !p.listens(kind) {
		return state, true
	}
	d := HookDecision{Kind: kind, X: x, Y: y, Z: z, Old: p.w.BlockAt(x, y, z), New: state, From: from}
	if p.ask(&d) {
		return 0, false
	}
	return d.New, true
}

// redstonePower asks the listeners about a block's power going from old to
// power and returns the power to apply: a listener may change it, and a cell
// that changed meanwhile keeps old. No listener, no change.
func (p *Physics) redstonePower(x, y, z int32, old, power int) int {
	if old == power || !p.listens(HookRedstone) {
		return power
	}
	d := HookDecision{Kind: HookRedstone, X: x, Y: y, Z: z, Old: p.w.BlockAt(x, y, z), OldPower: old, Power: power}
	if p.ask(&d) {
		return old
	}
	return max(0, min(15, d.Power))
}

// setCauldron writes a cauldron's new fill level unless a listener vetoes the
// change, and reports whether it was written. A listener may change the state.
// vanilla: LayeredCauldronBlock.lowerFillLevel, handlePrecipitation
func (p *Physics) setCauldron(x, y, z int32, state uint16, changes *[]BlockChange) bool {
	state, ok := p.veto(HookCauldron, x, y, z, state, [3]int32{})
	if ok {
		p.setShaped(x, y, z, state, changes)
	}
	return ok
}

// physicsVetoed asks the BlockPhysicsEvent listeners about the neighbour at
// x, y, z of the written cell from, once per write, and reports a veto.
func (p *Physics) physicsVetoed(x, y, z int32, from [3]int32) bool {
	key := [6]int32{x, y, z, from[0], from[1], from[2]}
	if v, ok := p.physicsAsked[key]; ok {
		return v
	}
	hd := HookDecision{Kind: HookPhysics, X: x, Y: y, Z: z, Old: p.w.BlockAt(x, y, z), From: from}
	v := p.ask(&hd)
	if p.physicsAsked == nil {
		p.physicsAsked = map[[6]int32]bool{}
	}
	p.physicsAsked[key] = v
	return v
}
