package rules

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// BlastDrop is one merged stack a blast spawns, at the cell of the first block
// that dropped into it.
type BlastDrop struct {
	Pos   [3]int32
	Stack protocol.ItemStack
}

// BlastDrops merges the stacks a blast's blocks drop, so a crater leaves a few
// full stacks rather than one item entity per block.
// vanilla: ServerExplosion.addOrAppendStack, ServerExplosion.StackCollector
type BlastDrops struct {
	drops []BlastDrop
	// byItem lists, per item ID and in order, the indexes into drops that may
	// merge with that item, which spares scanning stacks of other items.
	byItem map[int32][]int
}

// blastStackCap is the cap StackCollector.tryMerge passes ItemEntity.merge,
// below the 64 of an item entity's own merge.
const blastStackCap = 16

// Add folds stack into the first collected stack it merges with, then keeps
// any remainder as a new stack at pos. A collected stack already over
// blastStackCap gets a negative delta, which grows the incoming stack back
// by what the cap trims off the collected one, so the total is kept.
func (d *BlastDrops) Add(stack protocol.ItemStack, pos [3]int32) {
	if stack.Empty() {
		return
	}
	for _, i := range d.byItem[stack.ItemID] {
		c := &d.drops[i].Stack
		if c.Count+stack.Count > registry.StackMaxStackSize(stack) || !c.Same(stack) {
			continue
		}
		delta := min(min(registry.StackMaxStackSize(*c), blastStackCap)-c.Count, stack.Count)
		c.Count += delta
		stack.Count -= delta
		if stack.Empty() {
			return
		}
	}
	if d.byItem == nil {
		d.byItem = map[int32][]int{}
	}
	d.byItem[stack.ItemID] = append(d.byItem[stack.ItemID], len(d.drops))
	d.drops = append(d.drops, BlastDrop{Pos: pos, Stack: stack})
}

// Drops returns the merged stacks in the order they were first collected.
func (d *BlastDrops) Drops() []BlastDrop { return d.drops }
