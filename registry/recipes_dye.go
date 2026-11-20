package registry

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
)

// The dyeing crafting recipes: a shulker box or bundle plus the dye of the
// colour wanted (crafting_transmute) and a piece of dyeable armour plus any
// dyes (crafting_dye). Their results carry the input's components, so they
// are matched here instead of read from recipes_gen.go.

// dyeIndex returns the DyeNames index of a dye item.
func dyeIndex(stack protocol.ItemStack) (int, bool) {
	if !Item(stack.ItemID).InTag(ItemTagIDDyes) {
		return 0, false
	}
	c, ok := StackDyeColour(stack)
	return int(c), ok
}

// transmuteDyed is a shulker box or bundle beside one dye: the box of that
// colour with the input's components; its own colour is no recipe.
// vanilla: TransmuteRecipe.matches, TransmuteRecipe.assemble
func transmuteDyed(cells []protocol.ItemStack, replaced map[string]bool) (protocol.ItemStack, bool) {
	i, j, ok := gridPair(cells)
	if !ok {
		return protocol.ItemStack{}, false
	}
	input, material := cells[i], cells[j]
	dye, ok := dyeIndex(material)
	if !ok {
		input, material = material, input
		if dye, ok = dyeIndex(material); !ok {
			return protocol.ItemStack{}, false
		}
	}
	var suffix string
	switch {
	case Item(input.ItemID).InTag(ItemTagIDShulkerBoxes):
		suffix = "_shulker_box"
	case Item(input.ItemID).InTag(ItemTagIDBundles):
		suffix = "_bundle"
	default:
		return protocol.ItemStack{}, false
	}
	name := "minecraft:" + DyeNames[dye] + suffix
	id, ok := ItemID(name)
	if !ok || id == input.ItemID || replaced[name] {
		return protocol.ItemStack{}, false
	}
	result := input
	result.ItemID = id
	result.Count = 1
	return result, true
}

// dyedArmour is one dyeable item and at least one dye: the item with the
// colour its current colour and the dyes mix to.
// vanilla: DyeRecipe.matches, DyeRecipe.assemble
func dyedArmour(cells []protocol.ItemStack) (protocol.ItemStack, bool) {
	var target protocol.ItemStack
	var dyes []int
	for _, cell := range cells {
		if cell.Empty() {
			continue
		}
		if slices.Contains(dyeableItems, cell.ItemID) {
			if !target.Empty() {
				return protocol.ItemStack{}, false
			}
			target = cell
			continue
		}
		dye, ok := dyeIndex(cell)
		if !ok {
			return protocol.ItemStack{}, false
		}
		dyes = append(dyes, dye)
	}
	if target.Empty() || len(dyes) == 0 {
		return protocol.ItemStack{}, false
	}
	return ApplyDyes(target, dyes), true
}

// ApplyDyes blends dye colour IDs with a stack's current colour, keeping one item.
// vanilla: DyedItemColor.applyDyes
func ApplyDyes(stack protocol.ItemStack, dyes []int) protocol.ItemStack {
	if len(dyes) == 0 {
		return stack
	}
	current, has := stack.DyedColorOf()
	stack.Count = 1
	stack.SetDyedColor(mixDyes(current, has, dyes))
	return stack
}

// mixDyes averages the item's current colour and the dyes, then scales the
// result back to the average brightness of the parts.
// vanilla: DyedItemColor.applyDyes
func mixDyes(current int32, has bool, dyes []int) int32 {
	var red, green, blue, intensity, count int32
	add := func(rgb int32) {
		r, g, b := rgb>>16&0xFF, rgb>>8&0xFF, rgb&0xFF
		intensity += max(r, g, b)
		red, green, blue = red+r, green+g, blue+b
		count++
	}
	if has {
		add(current)
	}
	for _, dye := range dyes {
		add(dyeTextureColours[dye])
	}
	red, green, blue = red/count, green/count, blue/count
	average := float32(intensity) / float32(count)
	peak := float32(max(red, green, blue))
	scale := func(c int32) int32 { return int32(float32(c) * average / peak) }
	return scale(red)<<16 | scale(green)<<8 | scale(blue)
}
