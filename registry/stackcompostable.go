package registry

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// StackCompostable returns the stack's effective composting layer provider.
func StackCompostable(stack protocol.ItemStack) (LootNumber, bool) {
	if stack.Empty() || stack.Removes(protocol.DataComponentCompostable) {
		return nil, false
	}
	if len(stack.Raw) != 0 {
		if c, ok := stack.CompostableOf(); ok {
			if c.Provider != "" {
				return LootProviderRef{Name: Qualify(c.Provider)}, true
			}
			return LootConstant{Value: float64(c.Layers)}, true
		}
	}
	if stack.ItemID < 0 || stack.ItemID >= int32(ItemCount) {
		return nil, false
	}
	n := compostLayers[stack.ItemID]
	return n, n != nil
}

// vanilla: Compostable.CODEC, ResolvableInt.CODEC
func readCompostableComponent(stack *protocol.ItemStack, value nbt.Tag) bool {
	entry, ok := nbt.CompoundView(value)
	if !ok {
		return false
	}
	var c protocol.CompostableComponent
	if n, ok := nbt.IntValue(entry["layers"]); ok {
		c.Layers = n
	} else if name, ok := entry["layers"].(nbt.String); ok && protocol.ValidIdentifier(string(name)) {
		c.Provider = Qualify(string(name))
	} else {
		return false
	}
	stack.SetCompostable(c)
	return true
}

func withCompostableTag(components nbt.Compound, stack protocol.ItemStack) nbt.Compound {
	if c, ok := stack.CompostableOf(); ok {
		if components == nil {
			components = nbt.Compound{}
		}
		var layers nbt.Tag = nbt.Int(c.Layers)
		if c.Provider != "" {
			layers = nbt.String(c.Provider)
		}
		tag := nbt.NewOrderedCompound(1)
		tag.Set("layers", layers)
		components["minecraft:compostable"] = tag
	}
	return components
}
