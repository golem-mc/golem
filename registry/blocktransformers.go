package registry

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// BlockTransformer identifies a supported built-in block conversion.
type BlockTransformer string

const (
	BlockTransformerAxe    BlockTransformer = "minecraft:axe"
	BlockTransformerHoe    BlockTransformer = "minecraft:hoe"
	BlockTransformerShovel BlockTransformer = "minecraft:shovel"
)

var blockTransformerRegistryIndex = slices.IndexFunc(Synchronized, func(r Registry) bool {
	return r.Name == "minecraft:block_transformer"
})

// StackBlockTransformer resolves the stack's patch, removal, then item default.
// vanilla: Item.useOn
func StackBlockTransformer(stack protocol.ItemStack) (BlockTransformer, bool) {
	if stack.Empty() {
		return "", false
	}
	if id, ok := stack.BlockTransformerOf(); ok {
		return BlockTransformer(Current()[blockTransformerRegistryIndex].Entries[id]), true
	}
	if stack.Removes(protocol.DataComponentBlockTransformer) {
		return "", false
	}
	transformer := propsOf(stack.ItemID).BlockTransformer
	return transformer, transformer != ""
}

func withBlockTransformerTag(components nbt.Compound, stack protocol.ItemStack) nbt.Compound {
	if id, ok := stack.BlockTransformerOf(); ok {
		if components == nil {
			components = nbt.Compound{}
		}
		components["minecraft:block_transformer"] = nbt.String(Current()[blockTransformerRegistryIndex].Entries[id])
	}
	return components
}

func readBlockTransformer(stack *protocol.ItemStack, value nbt.Tag) bool {
	name, ok := value.(nbt.String)
	if !ok {
		return false
	}
	switch transformer := BlockTransformer(Qualify(string(name))); transformer {
	case BlockTransformerAxe, BlockTransformerHoe, BlockTransformerShovel:
		stack.SetBlockTransformer(Current()[blockTransformerRegistryIndex].ID(string(transformer)))
		return true
	default:
		return false
	}
}
