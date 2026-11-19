package world

// CrafterAnimationTicks is how long a craft keeps the block's crafting flag.
// vanilla: CrafterBlock.dispenseFrom, setCraftingTicksRemaining
const CrafterAnimationTicks = 6

// CrafterStartAnimation starts the crafting countdown.
func (b *BlockEntity) CrafterStartAnimation() {
	b.Wake()
	b.CraftingTicks = CrafterAnimationTicks
}

// CrafterTick counts the crafting animation down one tick and reports
// whether it ran out on this one, when the flag is cleared.
// vanilla: CrafterBlockEntity.serverTick
func (b *BlockEntity) CrafterTick() (done bool) {
	if b.CraftingTicks <= 0 {
		return false
	}
	b.CraftingTicks--
	return b.CraftingTicks == 0
}
