package player

import (
	"slices"

	"github.com/golem-mc/golem/registry"
)

type mapHoldings struct {
	valid          bool
	tagsGeneration uint64
	ids            []int32
	hidden         bool
}

// MapHoldings runs fn with the distinct carried map IDs and whether equipment hides
// the holder's marker. The IDs must not be modified or retained beyond fn.
// vanilla: MapItemSavedData.tickCarriedBy, hasMapInvisibilityItemEquipped
func (p *Player) MapHoldings(fn func(ids []int32, hidden bool)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	gen := registry.TagsGeneration()
	h := &p.mapHoldings
	if !h.valid || h.tagsGeneration != gen {
		h.ids = h.ids[:0]
		h.hidden = false
		for _, slot := range equipmentSlots {
			if slot.window != OffHandSlot && registry.Item(p.inventory.equipmentStack(slot.window).ItemID).InTag(registry.ItemTagIDMapInvisibilityEquipment) {
				h.hidden = true
				break
			}
		}
		for slot := ArmorSlot; slot <= OffHandSlot; slot++ {
			stack := &p.inventory.Slots[slot]
			item := registry.Item(stack.ItemID)
			if item.IsMap() {
				if id, ok := stack.MapIDOf(); ok && !slices.Contains(h.ids, id) {
					h.ids = append(h.ids, id)
				}
			}
		}
		h.tagsGeneration = gen
		h.valid = true
	}
	fn(h.ids, h.hidden)
}
