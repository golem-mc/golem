package registry

import "github.com/golem-mc/golem/nbt"

// PaintingSize returns a painting variant's size in blocks by its ID in the
// minecraft:painting_variant registry: a data pack's entry from its width
// and height, a built-in one from PaintingVariants. ok is false for an ID
// the registry lacks or a pack entry whose size is missing or outside 1-16.
// vanilla: PaintingVariant.DIRECT_CODEC
func PaintingSize(id int32) (width, height int32, ok bool) {
	r, _ := Get("minecraft:painting_variant")
	if id < 0 || int(id) >= len(r.Entries) {
		return 0, 0, false
	}
	if data, packed := nbt.CompoundView(r.Data[r.Entries[id]]); packed {
		width, wok := nbt.IntValue(data["width"])
		height, hok := nbt.IntValue(data["height"])
		if !wok || !hok || width < 1 || width > 16 || height < 1 || height > 16 {
			return 0, 0, false
		}
		return width, height, true
	}
	if int(id) >= len(PaintingVariants) {
		return 0, 0, false
	}
	dims := PaintingVariants[id]
	return dims.Width, dims.Height, true
}
