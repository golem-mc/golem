package registry

import "github.com/golem-mc/golem/internal/protocol"

// The data-driven crafting recipes that rewrite one input stack: crafting_dye
// below, crafting_transmute and crafting_imbue in recipes_transmute.go.

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
