package rules

import "github.com/golem-mc/golem/registry"

// bookshelfOffsets is EnchantingTableBlock.BOOKSHELF_OFFSETS: the ring edge
// two blocks out, at the table's height and one above.
var bookshelfOffsets = func() [][3]int32 {
	var offsets [][3]int32
	for dy := int32(0); dy <= 1; dy++ {
		for dx := int32(-2); dx <= 2; dx++ {
			for dz := int32(-2); dz <= 2; dz++ {
				if dx*dx != 4 && dz*dz != 4 {
					continue
				}
				offsets = append(offsets, [3]int32{dx, dy, dz})
			}
		}
	}
	return offsets
}()

// CountBookshelves counts power providers in the ring around the enchanting
// table at (x, y, z) whose halfway block transmits power (a shelf behind
// air, not stone). The count is raw; the cost maths caps it at 15.
// vanilla: EnchantingTableBlock.isValidBookShelf
func CountBookshelves(w BlockReader, x, y, z int32) int {
	shelves := 0
	for _, off := range bookshelfOffsets {
		state := w.BlockAt(x+off[0], y+off[1], z+off[2])
		if !registry.BlockOf(state).IsEnchantmentPowerProvider() {
			continue
		}
		state = w.BlockAt(x+off[0]/2, y+off[1], z+off[2]/2)
		if !registry.BlockOf(state).IsEnchantmentPowerTransmitter() {
			continue
		}
		shelves++
	}
	return shelves
}
