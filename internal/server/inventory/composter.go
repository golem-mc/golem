package inventory

import "github.com/golem-mc/golem/internal/protocol"

// ComposterMoves records what the moves through a composter's views did, for
// the caller to apply to the level once its locks are down.
type ComposterMoves struct {
	// Received is the one compostable a move laid in through the top - a
	// hopper moves one item a turn.
	Received *protocol.ItemStack
	// Emptied is whether a take lifted the bone meal out.
	Emptied bool
}

// Input is the view of a composter below the full level.
func (m *ComposterMoves) Input() Block { return composterInput{m} }

// Output is the view of a level-8 composter holding the given bone meal.
func (m *ComposterMoves) Output(meal protocol.ItemStack) Block {
	return composterOutput{m: m, meal: meal}
}

// composterInput is the input container: one top-face slot taking one
// compostable. It always reads empty (nothing rests in a composter); a put
// is the single-item hand-off before the roll.
// vanilla: ComposterBlock.InputContainer
type composterInput struct{ m *ComposterMoves }

// Slot is what the slot holds, which is nothing: the item a move posts
// through the top goes into the level, not into a store.
func (b composterInput) Slot(int) protocol.ItemStack { return protocol.ItemStack{} }

// Put takes one of what a move posted: vanilla's container's stack size
// being one, the rest of a bigger stack never reaches it.
func (b composterInput) Put(_ int, stack protocol.ItemStack) {
	one := stack
	one.Count = min(one.Count, 1)
	b.m.Received = &one
}

// Stored is whether the slot already took its one item, after which the
// container refuses.
func (b composterInput) Stored(int) bool { return b.m.Received != nil }

// Stacks is the block's slots for reading; a view over the block state
// keeps none.
func (b composterInput) Stacks() []protocol.ItemStack { return nil }

// Len is one: the level's one slot.
func (b composterInput) Len() int { return 1 }

// Kind is the input view's move rules.
func (b composterInput) Kind() Kind { return KindComposterInput }

// composterOutput is the output container: its bone meal, taken only from
// below, the take emptying the level.
// vanilla: ComposterBlock.OutputContainer
type composterOutput struct {
	m    *ComposterMoves
	meal protocol.ItemStack
}

// Slot is the bone meal the full composter holds, one of it.
func (b composterOutput) Slot(int) protocol.ItemStack { return b.meal }

// Put records the take that emptied the slot, the drain that levels the
// composter down.
func (b composterOutput) Put(_ int, stack protocol.ItemStack) {
	if stack.Empty() {
		b.m.Emptied = true
	}
}

// Stacks is the block's slots for reading; a view over the block state
// keeps none.
func (b composterOutput) Stacks() []protocol.ItemStack { return nil }

// Len is one: the bone meal's one slot.
func (b composterOutput) Len() int { return 1 }

// Kind is the output view's move rules.
func (b composterOutput) Kind() Kind { return KindComposterOutput }

// ComposterFull is a level-7 composter awaiting its ready tick: no face
// opens a slot.
// vanilla: ComposterBlock.EmptyContainer
type ComposterFull struct{}

// Slot is what a slot holds, which is nothing.
func (b ComposterFull) Slot(int) protocol.ItemStack { return protocol.ItemStack{} }

// Put drops what no move sends: nothing can reach a slot to put.
func (b ComposterFull) Put(int, protocol.ItemStack) {}

// Stacks is the block's slots for reading; the full composter holds none.
func (b ComposterFull) Stacks() []protocol.ItemStack { return nil }

// Len is none.
func (b ComposterFull) Len() int { return 0 }

// Kind is the full composter's move rules: nothing moves either way.
func (b ComposterFull) Kind() Kind { return KindComposterFull }

// Peek is the input slot's item and count.
func (b composterInput) Peek(slot int) (itemID, count int32) {
	st := b.Slot(slot)
	return st.ItemID, st.Count
}

// Peek is the output slot's item and count.
func (b composterOutput) Peek(slot int) (itemID, count int32) {
	st := b.Slot(slot)
	return st.ItemID, st.Count
}

// Peek is the full composter's empty slot view.
func (b ComposterFull) Peek(slot int) (itemID, count int32) {
	st := b.Slot(slot)
	return st.ItemID, st.Count
}

// Filled is none: the input view never holds a stack.
func (b composterInput) Filled() uint64 { return 0 }

// Filled is the bone meal slot's bit.
func (b composterOutput) Filled() uint64 {
	if b.Slot(0).Count > 0 {
		return 1
	}
	return 0
}

// Filled is none: the full composter has no slots.
func (b ComposterFull) Filled() uint64 { return 0 }
