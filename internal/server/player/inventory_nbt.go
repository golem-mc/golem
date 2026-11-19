package player

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Slots as a player file numbers them, which is not how the window does: the
// hotbar comes first on disk and last in the window.
const (
	storedHotbar = 0  // through 8, the window's 36-44
	storedMain   = 9  // through 35, the same slots as the window
	storedSlots  = 36 // what Inventory holds: the hotbar and the main inventory
)

// equipmentSlots pairs the worn window slots with their equipment tag names
// (since 25w03a) and with the numbered Inventory slots of pre-1.21.5 files,
// which PlayerEquipmentFix rebuilds the tag from.
//
// Body and saddle have no window or legacy slot: their window is the negative
// Stored key (BodyStored, SaddleStored), and the legacy readers skip them.
var equipmentSlots = []struct {
	window int
	name   string
	stored int
}{
	{ArmorSlot, "head", 103},
	{ArmorSlot + 1, "chest", 102},
	{ArmorSlot + 2, "legs", 101},
	{ArmorSlot + 3, "feet", 100},
	{OffHandSlot, "offhand", -106},
	{BodyStored, "body", legacyNone},
	{SaddleStored, "saddle", legacyNone},
}

// legacyNone is the stored slot of an equipment slot older files lack.
const legacyNone = 1 << 20

// equipmentStack is the stack an equipment slot's key addresses.
func (inv *Inventory) equipmentStack(window int) *protocol.ItemStack {
	switch window {
	case BodyStored:
		return &inv.Body
	case SaddleStored:
		return &inv.Saddle
	}
	return &inv.Slots[window]
}

// WindowSlotOf is the way back, for a slot number read out of a player file.
func WindowSlotOf(stored int) (int, bool) {
	switch {
	case stored >= storedHotbar && stored < storedHotbar+HotbarSlots:
		return HotbarSlot + stored - storedHotbar, true
	case stored >= storedMain && stored < storedSlots:
		return stored, true
	default:
		return 0, false
	}
}

// legacyEquipmentWindow maps a pre-1.21.5 worn slot (100-103, -106) to its
// window slot.
// vanilla: PlayerEquipmentFix.SLOT_TRANSLATIONS
func legacyEquipmentWindow(stored int) (int, bool) {
	for _, slot := range equipmentSlots {
		if slot.stored == stored && slot.window >= 0 {
			return slot.window, true
		}
	}
	return 0, false
}

// LoadInventory reads a player file's inventory. A stack whose item this
// version lacks, or with components the registry's stack codec does not
// read, is kept as its compound and written back untouched on save.
func LoadInventory(root nbt.Compound) Inventory {
	inv := Inventory{Stored: map[int]nbt.Compound{}}

	if selected, ok := nbt.Get[nbt.Int](root, "SelectedItemSlot"); ok &&
		selected >= 0 && selected < HotbarSlots {
		inv.Selected = int32(selected)
	}

	LoadEquipment(root, &inv)

	items, ok := nbt.Get[nbt.List](root, "Inventory")
	if !ok {
		return inv
	}
	for _, item := range items.Items {
		entry, ok := nbt.CompoundView(item)
		if !ok {
			continue
		}
		stored, ok := nbt.Get[nbt.Byte](entry, "Slot")
		if !ok {
			continue
		}
		slot, ok := WindowSlotOf(int(stored))
		if !ok {
			// Before 1.21.5 a file kept the worn items in the Inventory
			// list; PlayerEquipmentFix moves them to the equipment tag.
			if window, ok := legacyEquipmentWindow(int(stored)); ok {
				if stack, ok := registry.StoredStack(entry); ok {
					inv.Slots[window] = stack
				} else {
					inv.Stored[window] = entry
				}
			}
			// A slot a file can hold but this server puts nowhere is left behind.
			continue
		}
		if stack, ok := registry.StoredStack(entry); ok {
			inv.Slots[slot] = stack
		} else {
			inv.Stored[slot] = entry
		}
	}
	ender, ok := nbt.Get[nbt.List](root, "EnderItems")
	if !ok {
		return inv
	}
	for _, item := range ender.Items {
		entry, ok := nbt.CompoundView(item)
		if !ok {
			continue
		}
		stored, ok := nbt.Get[nbt.Byte](entry, "Slot")
		if !ok || int(stored) < 0 || int(stored) >= EnderSlots {
			continue
		}
		slot := int(stored)
		if stack, ok := registry.StoredStack(entry); ok {
			inv.Ender[slot] = stack
		} else {
			if inv.EnderStored == nil {
				inv.EnderStored = map[int]nbt.Compound{}
			}
			inv.EnderStored[slot] = entry
		}
	}
	return inv
}

// LoadEquipment reads what a player was wearing, which a file keeps apart from
// the rest of what they were carrying.
func LoadEquipment(root nbt.Compound, inv *Inventory) {
	equipment, ok := nbt.Get[nbt.Compound](root, "equipment")
	if !ok {
		return
	}
	for _, slot := range equipmentSlots {
		entry, ok := nbt.Get[nbt.CompoundTag](equipment, slot.name)
		if !ok {
			continue
		}
		if stack, ok := registry.StoredStack(entry); ok {
			*inv.equipmentStack(slot.window) = stack
		} else {
			inv.Stored[slot.window] = entry.Entries()
		}
	}
}

// storeItem writes one item the way a file holds it. The caller has
// checked the item is one this version names.
func storeItem(stored int, stack protocol.ItemStack, setSlot bool) *nbt.OrderedCompound {
	if setSlot {
		return registry.SlottedStackTag(stored, stack)
	}
	return registry.StackTag(stack)
}

// StoreInventory writes an inventory back in the shape a player file
// holds it: a list of the occupied slots, each naming its item.
func (inv *Inventory) StoreInventory(root nbt.Compound) {
	inv.StoreSlots(root)
	root["SelectedItemSlot"] = nbt.Int(inv.Selected)
	inv.StoreEquipment(root)
	inv.StoreEnder(root)
}

// StoreSlots writes only the occupied inventory slots, for Player's save phase.
func (inv *Inventory) StoreSlots(root nbt.CompoundTag) {
	items := make([]nbt.Tag, 0, storedSlots)
	for stored := range storedSlots {
		slot, ok := WindowSlotOf(stored)
		if !ok {
			continue
		}
		// What the file said, for a slot that was never ours to change.
		if entry, ok := inv.Stored[slot]; ok {
			items = append(items, entry)
			continue
		}
		stack := inv.Slots[slot]
		if stack.Empty() {
			continue
		}
		if _, ok := registry.ItemName(stack.ItemID); !ok {
			continue
		}
		items = append(items, storeItem(stored, stack, true))
	}

	root.Set("Inventory", nbt.List{Elem: nbt.TypeCompound, Items: items})
}

// StoreEnder writes the ender chest under vanilla's name, even when empty.
func (inv *Inventory) StoreEnder(root nbt.CompoundTag) {
	items := make([]nbt.Tag, 0, EnderSlots)
	for slot := range EnderSlots {
		if entry, ok := inv.EnderStored[slot]; ok {
			items = append(items, entry)
			continue
		}
		stack := inv.Ender[slot]
		if stack.Empty() {
			continue
		}
		if _, ok := registry.ItemName(stack.ItemID); !ok {
			continue
		}
		items = append(items, storeItem(slot, stack, true))
	}
	root.Set("EnderItems", nbt.List{Elem: nbt.TypeCompound, Items: items})
}

// StoreEquipment writes what the player is wearing into the tag a file
// keeps it in: a compound naming each worn slot, not a numbered list.
func (inv *Inventory) StoreEquipment(root nbt.CompoundTag) {
	equipment := &nbt.OrderedCompound{}
	// vanilla: EntityEquipment.CODEC iterates EquipmentSlot enum order.
	for _, index := range [...]int{4, 3, 2, 1, 0, 5, 6} {
		slot := equipmentSlots[index]
		// What the file said, for a slot that was never ours to change.
		if entry, ok := inv.Stored[slot.window]; ok {
			equipment.Set(slot.name, entry)
			continue
		}
		stack := *inv.equipmentStack(slot.window)
		if stack.Empty() {
			continue
		}
		if _, ok := registry.ItemName(stack.ItemID); !ok {
			continue
		}
		equipment.Set(slot.name, storeItem(0, stack, false))
	}

	// PlayerEquipment.isEmpty includes the main hand, which its codec omits
	// because Inventory already stores it.
	if len(equipment.Entries()) == 0 && inv.Slots[HotbarSlot+int(inv.Selected)].Empty() {
		root.Delete("equipment")
		return
	}
	root.Set("equipment", equipment)
}

// StoreLegacyEquipment moves StoreEquipment's entries into the pre-1.21.5
// numbered slots (100-103, -106), leaving out the equipment tag the fixer
// rebuilds from them.
// vanilla: PlayerEquipmentFix.SLOT_TRANSLATIONS
func (inv *Inventory) StoreLegacyEquipment(root nbt.CompoundTag) {
	equipment, ok := nbt.Get[nbt.Compound](root.Entries(), "equipment")
	if ok {
		items, _ := nbt.Get[nbt.List](root.Entries(), "Inventory")
		for _, slot := range equipmentSlots {
			entry, ok := nbt.Get[nbt.CompoundTag](equipment, slot.name)
			if !ok || slot.stored == legacyNone {
				continue
			}
			moved := nbt.Copy(entry).(nbt.CompoundTag)
			moved.Set("Slot", nbt.Byte(slot.stored))
			items.Items = append(items.Items, moved)
		}
		root.Set("Inventory", items)
	}
	root.Delete("equipment")
}

// EquipmentSlotOf is the window slot a file's equipment name refers to.
func EquipmentSlotOf(name string) (int, bool) {
	for _, slot := range equipmentSlots {
		if slot.name == name && slot.window >= 0 {
			return slot.window, true
		}
	}
	return 0, false
}
