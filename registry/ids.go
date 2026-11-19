package registry

// blockOfState is BlockOf's table, filled from blockInfo's runs.
var blockOfState = func() *[StateCount]Block {
	t := new([StateCount]Block)
	b := Block(0)
	for s := range uint16(StateCount) {
		for b+1 < BlockCount && blockInfo[b+1].first <= s {
			b++
		}
		t[s] = b
	}
	return t
}()

// blockByName is BlockByName's index.
var blockByName = func() map[string]Block {
	m := make(map[string]Block, BlockCount)
	for b := range Block(BlockCount) {
		m[blockInfo[b].name] = b
	}
	return m
}()

// BlockOf returns the block a state belongs to. An ID past the last state
// reads as BlockAir, the same fallback BlockStateName's callers take.
func BlockOf(state uint16) Block {
	if int(state) >= StateCount {
		return BlockAir
	}
	return blockOfState[state]
}

// BlockByName returns the block a registry name such as "minecraft:stone"
// names, and false for a name that is no block.
func BlockByName(name string) (Block, bool) {
	b, ok := blockByName[name]
	return b, ok
}

// Name returns the block's registry name, such as "minecraft:stone".
func (b Block) Name() string { return blockInfo[b].name }

// DefaultState returns the state ID a placed block starts in.
func (b Block) DefaultState() uint16 { return blockInfo[b].deflt }

// ItemByName returns the item a registry name such as "minecraft:stone"
// names, and false for a name that is no item.
func ItemByName(name string) (Item, bool) {
	id, ok := itemIDs[name]
	return Item(id), ok
}

// Name returns the item's registry name, such as "minecraft:water_bucket".
func (i Item) Name() string { return Items[i] }

// BlocksOf is BlocksWhere for a rule written on the block, such as Block.IsLeaves.
func BlocksOf(match func(Block) bool) *[BlockCount]bool {
	t := new([BlockCount]bool)
	for b := range Block(BlockCount) {
		t[b] = match(b)
	}
	return t
}

// BlocksWhere returns a table indexed by Block marking the blocks whose
// registry name match accepts, for a name rule read once at init rather than
// per lookup.
func BlocksWhere(match func(name string) bool) *[BlockCount]bool {
	t := new([BlockCount]bool)
	for b := range Block(BlockCount) {
		t[b] = match(blockInfo[b].name)
	}
	return t
}
