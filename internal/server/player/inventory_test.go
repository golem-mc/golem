package player

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// StoredSlotOf converts a window slot to the number a player file gives it, and
// reports whether a file has a place for it at all. Crafting slots hold nothing
// between two sessions, and armour and the off hand are written elsewhere.
func StoredSlotOf(slot int) (int, bool) {
	switch {
	case slot >= HotbarSlot && slot < HotbarSlot+HotbarSlots:
		return storedHotbar + slot - HotbarSlot, true
	case slot >= MainSlot && slot < MainSlot+mainSlots:
		return slot, true
	default:
		return 0, false
	}
}

// TestSlotNumbering checks the two numberings a slot has against each other. A
// player file puts the hotbar first and the window puts it last, so getting
// this wrong would move everything nine slots every time a player logged in.
func TestSlotNumbering(t *testing.T) {
	// Taken from a vanilla server: giving a player three items put them in
	// window slots 36, 37 and 38, and the file it wrote called those 0, 1 and 2.
	for window, stored := range map[int]int{36: 0, 37: 1, 44: 8, 9: 9, 35: 35} {
		if got, ok := StoredSlotOf(window); !ok || got != stored {
			t.Errorf("StoredSlotOf(%d) = %d, %v; want %d, true", window, got, ok, stored)
		}
		if got, ok := WindowSlotOf(stored); !ok || got != window {
			t.Errorf("WindowSlotOf(%d) = %d, %v; want %d, true", stored, got, ok, window)
		}
	}

	for stored := range storedSlots {
		window, ok := WindowSlotOf(stored)
		if !ok {
			t.Errorf("WindowSlotOf(%d) has nowhere to go", stored)
			continue
		}
		if back, ok := StoredSlotOf(window); !ok || back != stored {
			t.Errorf("slot %d went to window slot %d and came back as %d", stored, window, back)
		}
	}

	// The slots a file has no place for: the crafting grid, which is emptied
	// when a window closes, and armour and the off hand, which have lived in a
	// tag of their own since 25w03a.
	for _, window := range []int{0, 1, 4, ArmorSlot, ArmorSlot + 3, OffHandSlot} {
		if _, ok := StoredSlotOf(window); ok {
			t.Errorf("window slot %d was given a place in a player file", window)
		}
	}
}

// TestInventoryRoundTrip checks that an inventory written to a player file and
// read back is the same inventory.
func TestInventoryRoundTrip(t *testing.T) {
	stone, _ := registry.ItemID("minecraft:stone")
	diamond, _ := registry.ItemID("minecraft:diamond")

	var inv Inventory
	inv.Set(HotbarSlot, protocol.ItemStack{ItemID: diamond, Count: 5})
	inv.Set(HotbarSlot+8, protocol.ItemStack{ItemID: stone, Count: 64})
	inv.Set(MainSlot+3, protocol.ItemStack{ItemID: stone, Count: 1})
	inv.Selected = 4

	root := nbt.Compound{}
	inv.StoreInventory(root)

	// The shape a vanilla server writes: one compound per occupied slot, naming
	// the item rather than numbering it.
	items, ok := nbt.Get[nbt.List](root, "Inventory")
	if !ok || len(items.Items) != 3 {
		t.Fatalf("Inventory = %v, %v; want the three slots that hold something", items, ok)
	}
	first, _ := nbt.CompoundView(items.Items[0])
	if slot, ok := nbt.Get[nbt.Byte](first, "Slot"); !ok || slot != 0 {
		t.Errorf("first Slot = %v, %v; want 0 (the first hotbar slot)", slot, ok)
	}
	if id, ok := nbt.Get[nbt.String](first, "id"); !ok || id != "minecraft:diamond" {
		t.Errorf("first id = %v, %v; want minecraft:diamond", id, ok)
	}
	if count, ok := nbt.Get[nbt.Int](first, "count"); !ok || count != 5 {
		t.Errorf("first count = %v, %v; want 5", count, ok)
	}
	if selected, ok := nbt.Get[nbt.Int](root, "SelectedItemSlot"); !ok || selected != 4 {
		t.Errorf("SelectedItemSlot = %v, %v; want 4", selected, ok)
	}

	back := LoadInventory(root)
	if back.Selected != inv.Selected {
		t.Errorf("Selected = %d, want %d", back.Selected, inv.Selected)
	}
	for slot := range inventorySlots {
		if !back.Slots[slot].Equal(inv.Slots[slot]) {
			t.Errorf("slot %d = %+v, want %+v", slot, back.Slots[slot], inv.Slots[slot])
		}
	}
}

// TestSwapHands checks that the F key moves the held item to the off hand and
// back, and that an item this server cannot represent - one carrying data
// components, kept in stored - moves with its slot rather than being emptied.
func TestSwapHands(t *testing.T) {
	diamond, _ := registry.ItemID("minecraft:diamond")
	stone, _ := registry.ItemID("minecraft:stone")

	var inv Inventory
	inv.Set(HotbarSlot+3, protocol.ItemStack{ItemID: diamond, Count: 5})
	inv.Set(OffHandSlot, protocol.ItemStack{ItemID: stone, Count: 1})
	inv.Selected = 3

	main, off := inv.SwapHands()
	if main != HotbarSlot+3 || off != OffHandSlot {
		t.Fatalf("SwapHands = %d, %d; want %d, %d", main, off, HotbarSlot+3, OffHandSlot)
	}
	if got := inv.Slots[HotbarSlot+3]; got.ItemID != stone || got.Count != 1 {
		t.Errorf("main hand = %+v, want the stone from the off hand", got)
	}
	if got := inv.Slots[OffHandSlot]; got.ItemID != diamond || got.Count != 5 {
		t.Errorf("off hand = %+v, want the diamond from the main hand", got)
	}

	inv.SwapHands()
	if got := inv.Slots[HotbarSlot+3]; got.ItemID != diamond || got.Count != 5 {
		t.Errorf("main hand = %+v after swapping back, want the diamond", got)
	}
	if got := inv.Slots[OffHandSlot]; got.ItemID != stone || got.Count != 1 {
		t.Errorf("off hand = %+v after swapping back, want the stone", got)
	}
}

// TestSwapHandsMovesStoredItem checks that an item with data components - which
// lives in stored rather than slots, and so is invisible to the client - is
// carried along when the hands are swapped, rather than being dropped.
func TestSwapHandsMovesStoredItem(t *testing.T) {
	diamond, _ := registry.ItemID("minecraft:diamond")
	enchanted := nbt.Compound{
		"id":    nbt.String("minecraft:shield"),
		"count": nbt.Int(1),
		"components": nbt.Compound{
			"minecraft:enchantments": nbt.Compound{"minecraft:unbreaking": nbt.Int(3)},
		},
	}

	var inv Inventory
	inv.Stored = map[int]nbt.Compound{}
	inv.Set(HotbarSlot, protocol.ItemStack{ItemID: diamond, Count: 1})
	inv.Stored[OffHandSlot] = enchanted
	inv.Selected = 0

	inv.SwapHands()

	if got := inv.Slots[OffHandSlot]; got.ItemID != diamond || got.Count != 1 {
		t.Errorf("off hand = %+v, want the diamond", got)
	}
	if got := inv.Slots[HotbarSlot]; !got.Empty() {
		t.Errorf("main hand = %+v, want empty (the stored shield is not sent)", got)
	}
	if entry, ok := inv.Stored[HotbarSlot]; !ok || !reflect.DeepEqual(entry, enchanted) {
		t.Errorf("main hand stored = %v, %v; want the enchanted shield", entry, ok)
	}
	if _, ok := inv.Stored[OffHandSlot]; ok {
		t.Errorf("off hand still has a stored entry, want it moved to the main hand")
	}
}

// TestPickBlockSwitchesToHotbarSlot checks that picking a block whose item
// is already in the hotbar moves the selection to it, not a new stack.
func TestPickBlockSwitchesToHotbarSlot(t *testing.T) {
	diamond, _ := registry.ItemID("minecraft:diamond")
	stone, _ := registry.ItemID("minecraft:stone")

	var inv Inventory
	inv.Set(HotbarSlot, protocol.ItemStack{ItemID: diamond, Count: 1})
	inv.Set(HotbarSlot+3, protocol.ItemStack{ItemID: stone, Count: 32})
	inv.Selected = 0 // holding the diamond

	a, b, ok := inv.Pick(stone, true)
	if a != -1 || b != -1 || ok {
		t.Fatalf("Pick = (%d, %d, %v), want no slot touched: switch to the stone's slot", a, b, ok)
	}
	if inv.Selected != 3 {
		t.Errorf("Selected = %d, want 3", inv.Selected)
	}
	if got := inv.Slots[HotbarSlot]; got.ItemID != diamond || got.Count != 1 {
		t.Errorf("slot 0 = %+v, want the diamond untouched", got)
	}
	if got := inv.Slots[HotbarSlot+3]; got.ItemID != stone || got.Count != 32 {
		t.Errorf("slot 3 = %+v, want the stone untouched", got)
	}
}

// TestPickBlockSwapsAMainInventoryStackInCreative checks that creative
// picks a stack carried in the main inventory by swapping it into the held
// slot, and conjures only when nothing carried matches.
func TestPickBlockSwapsAMainInventoryStackInCreative(t *testing.T) {
	stone, _ := registry.ItemID("minecraft:stone")
	diamond, _ := registry.ItemID("minecraft:diamond")

	var inv Inventory
	inv.Set(HotbarSlot, protocol.ItemStack{ItemID: diamond, Count: 1})
	inv.Set(MainSlot, protocol.ItemStack{ItemID: stone, Count: 32})
	inv.Selected = 0

	a, b, ok := inv.Pick(stone, true)
	if a != HotbarSlot+1 || b != MainSlot || !ok {
		t.Fatalf("Pick = (%d, %d, %v), want (%d, %d, true): swap, not conjure", a, b, ok, HotbarSlot+1, MainSlot)
	}
	if got := inv.Slots[HotbarSlot+1]; got.ItemID != stone || got.Count != 32 {
		t.Errorf("held slot = %+v, want the stone swapped in", got)
	}
	if got := inv.Slots[MainSlot]; !got.Empty() {
		t.Errorf("main slot = %+v, want it emptied by the swap", got)
	}
	if inv.Selected != 1 {
		t.Errorf("Selected = %d, want 1 (the empty slot the pick chose)", inv.Selected)
	}
}

// TestPickSlotSwapsTheHeldSlotForTheInventoryItem checks that picking an
// item a survival player keeps in the main inventory swaps it with the
// slot the selection moved to: the stone lands in the hand, and the
// diamond the held slot gave up stays where it was.
func TestPickSlotSwapsTheHeldSlotForTheInventoryItem(t *testing.T) {
	stone, _ := registry.ItemID("minecraft:stone")
	diamond, _ := registry.ItemID("minecraft:diamond")

	var inv Inventory
	inv.Set(HotbarSlot, protocol.ItemStack{ItemID: diamond, Count: 1})
	inv.Set(MainSlot+2, protocol.ItemStack{ItemID: stone, Count: 32})
	inv.Selected = 0

	a, b, ok := inv.Pick(stone, false)
	if a != HotbarSlot+1 || b != MainSlot+2 || !ok {
		t.Fatalf("Pick = (%d, %d, %v), want (%d, %d, true): swap the selection's slot for the stone", a, b, ok, HotbarSlot+1, MainSlot+2)
	}
	if got := inv.Slots[HotbarSlot+1]; got.ItemID != stone || got.Count != 32 {
		t.Errorf("held slot = %+v, want the stone from the main inventory", got)
	}
	if got := inv.Slots[MainSlot+2]; !got.Empty() {
		t.Errorf("main slot = %+v, want it emptied by the swap", got)
	}
	if got := inv.Slots[HotbarSlot]; got.ItemID != diamond || got.Count != 1 {
		t.Errorf("slot 0 = %+v, want the diamond untouched", got)
	}
	if inv.Selected != 1 {
		t.Errorf("Selected = %d, want 1 (the empty slot the pick chose)", inv.Selected)
	}
}

// TestPickSlotSkipsAnEnchantedSlotWhenTheHotbarIsFull checks that with no
// empty hotbar slot, getSuitableHotbarSlot's second pass skips the
// enchanted one the player is holding and swaps the pick in past it.
func TestPickSlotSkipsAnEnchantedSlotWhenTheHotbarIsFull(t *testing.T) {
	stone, _ := registry.ItemID("minecraft:stone")
	diamond, _ := registry.ItemID("minecraft:diamond")
	enchanted := protocol.ItemStack{ItemID: diamond, Count: 1,
		Enchantments: map[int32]int32{1: 1}}

	var inv Inventory
	for i := range HotbarSlots {
		inv.Set(HotbarSlot+i, protocol.ItemStack{ItemID: diamond, Count: 1})
	}
	inv.Set(HotbarSlot+3, enchanted)
	inv.Set(MainSlot, protocol.ItemStack{ItemID: stone, Count: 32})
	inv.Selected = 3

	a, b, ok := inv.Pick(stone, false)
	if a != HotbarSlot+4 || b != MainSlot || !ok {
		t.Fatalf("Pick = (%d, %d, %v), want (%d, %d, true): swap past the enchanted slot", a, b, ok, HotbarSlot+4, MainSlot)
	}
	if inv.Selected != 4 {
		t.Errorf("Selected = %d, want 4 (the first slot past the enchanted one)", inv.Selected)
	}
	if got := inv.Slots[HotbarSlot+3]; got.ItemID != diamond || !got.Enchanted() {
		t.Errorf("slot 3 = %+v, want the enchanted diamond kept", got)
	}
}

// TestAddLandsInTheFirstEmptyItemSlot checks the empty pass: the first
// empty hotbar slot, then the main inventory - and the off hand passed by
// even when it is the only slot free.
func TestAddLandsInTheFirstEmptyItemSlot(t *testing.T) {
	stone, _ := registry.ItemID("minecraft:stone")

	var inv Inventory
	left, slots := inv.Add(protocol.ItemStack{ItemID: stone, Count: 1})
	if left != 0 || len(slots) != 1 || slots[0] != HotbarSlot {
		t.Fatalf("Add into an empty inventory = %d left in %v, want it all in the first hotbar slot", left, slots)
	}

	// The off hand holding something is not what makes a slot free.
	inv = Inventory{}
	inv.Set(OffHandSlot, protocol.ItemStack{ItemID: stone, Count: 64})
	left, slots = inv.Add(protocol.ItemStack{ItemID: stone, Count: 1})
	if left != 0 || len(slots) != 1 || slots[0] != HotbarSlot {
		t.Fatalf("Add beside a full off hand = %d left in %v, want it in the first hotbar slot", left, slots)
	}
	if got := inv.Slots[OffHandSlot]; got.Count != 64 {
		t.Errorf("off hand = %+v, want the full stack untouched", got)
	}
}

// TestEquipmentRoundTrip checks that what a player is wearing survives
// being written to a file and read back: armour and the off hand are not
// in the Inventory list at all since 25w03a, but named in a tag.
func TestEquipmentRoundTrip(t *testing.T) {
	helmet, _ := registry.ItemID("minecraft:diamond_helmet")
	boots, _ := registry.ItemID("minecraft:golden_boots")
	shield, _ := registry.ItemID("minecraft:shield")

	var inv Inventory
	inv.Set(ArmorSlot, protocol.ItemStack{ItemID: helmet, Count: 1})
	inv.Set(ArmorSlot+3, protocol.ItemStack{ItemID: boots, Count: 1})
	inv.Set(OffHandSlot, protocol.ItemStack{ItemID: shield, Count: 1})

	root := nbt.Compound{}
	inv.StoreInventory(root)

	// Named rather than numbered.
	equipment, ok := nbt.Get[nbt.Compound](root, "equipment")
	if !ok {
		t.Fatal("nothing was written to the equipment tag")
	}
	for name, want := range map[string]string{
		"head":    "minecraft:diamond_helmet",
		"feet":    "minecraft:golden_boots",
		"offhand": "minecraft:shield",
	} {
		entry, ok := nbt.Get[nbt.Compound](equipment, name)
		if !ok {
			t.Errorf("equipment has no %s", name)
			continue
		}
		if id, _ := nbt.Get[nbt.String](entry, "id"); string(id) != want {
			t.Errorf("equipment %s id = %v, want %s", name, id, want)
		}
		if count, _ := nbt.Get[nbt.Int](entry, "count"); count != 1 {
			t.Errorf("equipment %s count = %v, want 1", name, count)
		}
		// The armour slots carry no Slot field: the name is what places them.
		if _, ok := entry["Slot"]; ok {
			t.Errorf("equipment %s was given a Slot field", name)
		}
	}
	for _, name := range []string{"chest", "legs"} {
		if _, ok := equipment[name]; ok {
			t.Errorf("equipment names %s, which holds nothing", name)
		}
	}
	items, _ := nbt.Get[nbt.List](root, "Inventory")
	if len(items.Items) != 0 {
		t.Errorf("Inventory = %v, want nothing: everything here is worn", items)
	}

	back := LoadInventory(root)
	for _, slot := range []int{ArmorSlot, ArmorSlot + 3, OffHandSlot} {
		if !back.Slots[slot].Equal(inv.Slots[slot]) {
			t.Errorf("slot %d = %+v, want %+v", slot, back.Slots[slot], inv.Slots[slot])
		}
	}
}

// TestInventoryKeepsWhatItCannotSend checks that a stack this server cannot put
// in front of a player survives being loaded and saved anyway. A player who
// logs into a world played on a vanilla server and out again would otherwise
// lose every filled shulker box or whatever else they owned whose components
// this server has no model for.
func TestInventoryKeepsWhatItCannotSend(t *testing.T) {
	compass := nbt.Compound{
		"Slot":  nbt.Byte(1),
		"id":    nbt.String("minecraft:compass"),
		"count": nbt.Int(1),
		"components": nbt.Compound{
			"minecraft:lodestone_tracker": nbt.String("not a tracker"),
		},
	}
	future := nbt.Compound{
		"Slot":  nbt.Byte(2),
		"id":    nbt.String("minecraft:something_from_a_later_version"),
		"count": nbt.Int(1),
	}
	root := nbt.Compound{"Inventory": nbt.List{
		Elem:  nbt.TypeCompound,
		Items: []nbt.Tag{compass, future},
	}}

	inv := LoadInventory(root)

	for _, slot := range []int{HotbarSlot + 1, HotbarSlot + 2} {
		if !inv.Slots[slot].Empty() {
			t.Errorf("slot %d was handed to the client as %+v", slot, inv.Slots[slot])
		}
	}

	saved := nbt.Compound{}
	inv.StoreInventory(saved)
	items, ok := nbt.Get[nbt.List](saved, "Inventory")
	if !ok || len(items.Items) != 2 {
		t.Fatalf("Inventory = %v, %v; want both stacks written back", items, ok)
	}
	for i, want := range []nbt.Compound{compass, future} {
		got, ok := items.Items[i].(nbt.Compound)
		if !ok {
			t.Fatalf("item %d is a %s, want a compound", i, items.Items[i].Type())
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("item %d = %v, want %v", i, got, want)
		}
	}

	stone, _ := registry.ItemID("minecraft:stone")
	inv.Set(HotbarSlot+1, protocol.ItemStack{ItemID: stone, Count: 1})

	replaced := nbt.Compound{}
	inv.StoreInventory(replaced)
	items, _ = nbt.Get[nbt.List](replaced, "Inventory")
	first, _ := nbt.CompoundView(items.Items[0])
	if id, _ := nbt.Get[nbt.String](first, "id"); id != "minecraft:stone" {
		t.Errorf("slot 1 holds %v, want the stone that replaced the sword", id)
	}
	if _, ok := first["components"]; ok {
		t.Error("the replaced stack kept the components of the one it replaced")
	}
}

// TestInventoryIgnoresNonsense checks that a file which is not what this server
// expects leaves the player with an empty inventory rather than a broken one.
func TestInventoryIgnoresNonsense(t *testing.T) {
	for _, tc := range []struct {
		name string
		root nbt.Compound
	}{
		{"no inventory", nbt.Compound{}},
		{"not a list", nbt.Compound{"Inventory": nbt.String("stuff")}},
		{"items are not compounds", nbt.Compound{"Inventory": nbt.List{
			Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("stone")},
		}}},
		{"slot out of range", nbt.Compound{"Inventory": nbt.List{
			Elem: nbt.TypeCompound,
			Items: []nbt.Tag{nbt.Compound{
				"Slot": nbt.Byte(-50), "id": nbt.String("minecraft:stone"), "count": nbt.Int(1),
			}},
		}}},
		{"legacy Count key", nbt.Compound{"Inventory": nbt.List{
			Elem: nbt.TypeCompound,
			Items: []nbt.Tag{nbt.Compound{
				"Slot": nbt.Byte(0), "id": nbt.String("minecraft:stone"), "Count": nbt.Byte(3),
			}},
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inv := LoadInventory(tc.root)
			for slot := range inventorySlots {
				if !inv.Slots[slot].Empty() {
					t.Errorf("slot %d holds %+v, want nothing", slot, inv.Slots[slot])
				}
			}
		})
	}

	// A selected slot outside the hotbar is not one the client could point at.
	for _, selected := range []nbt.Int{-1, 9, 1000} {
		inv := LoadInventory(nbt.Compound{"SelectedItemSlot": selected})
		if inv.Selected != 0 {
			t.Errorf("SelectedItemSlot %d gave %d, want 0", selected, inv.Selected)
		}
	}
}

// TestContainerContentCoversEverySlot checks the packet a returning player is
// filled in with: one entry per slot of their window, in slot order, and the
// cursor left empty.
func TestContainerContentCoversEverySlot(t *testing.T) {
	diamond, _ := registry.ItemID("minecraft:diamond")

	var inv Inventory
	inv.Set(HotbarSlot, protocol.ItemStack{ItemID: diamond, Count: 5})

	r := protocol.NewReader(inv.ContainerContent())
	if got := r.ReadVarInt(); got != playerWindowID {
		t.Errorf("window ID = %d, want %d", got, playerWindowID)
	}
	r.ReadVarInt() // state ID
	if got := r.ReadVarInt(); got != inventorySlots {
		t.Fatalf("slots = %d, want %d", got, inventorySlots)
	}
	for slot := range inventorySlots {
		stack, ok := r.ReadSlot()
		if !ok {
			t.Fatalf("slot %d did not read back", slot)
		}
		if !stack.Equal(inv.Slots[slot]) {
			t.Errorf("slot %d = %+v, want %+v", slot, stack, inv.Slots[slot])
		}
	}
	if cursor, ok := r.ReadSlot(); !ok || !cursor.Empty() {
		t.Errorf("carried item = %+v, %v; want nothing", cursor, ok)
	}
	if r.Remaining() != 0 {
		t.Errorf("%d bytes left over", r.Remaining())
	}
}

// TestEquipGoesBySwappable checks the right-click half of what an item's
// equippable component says: a shield names the off hand but is not
// swappable, so right-clicking raises it where it is instead of stowing
// it, and a carved pumpkin stays in the hand. The shift-click is the
// other way an item goes on, and that one goes by the slot alone - see
// the inventory package's off-hand test.
func TestEquipGoesBySwappable(t *testing.T) {
	shield, _ := registry.ItemID("minecraft:shield")
	pumpkin, _ := registry.ItemID("minecraft:carved_pumpkin")
	helmet, _ := registry.ItemID("minecraft:iron_helmet")

	var inv Inventory
	inv.Set(HotbarSlot, protocol.ItemStack{ItemID: shield, Count: 1})
	worn, _, ok := inv.Equip(HotbarSlot, false)
	if ok {
		t.Errorf("Equip of a shield reported slot %d, want not equipped", worn)
	}
	if inv.Slots[HotbarSlot].ItemID != shield {
		t.Errorf("main hand = %+v, want the shield still held", inv.Slots[HotbarSlot])
	}
	if !inv.Slots[OffHandSlot].Empty() {
		t.Errorf("off hand = %+v, want it still empty", inv.Slots[OffHandSlot])
	}

	inv = Inventory{}
	inv.Set(HotbarSlot, protocol.ItemStack{ItemID: pumpkin, Count: 1})
	if _, _, ok := inv.Equip(HotbarSlot, false); ok {
		t.Error("Equip of a carved pumpkin reported it put on, want not equipped")
	}
	if inv.Slots[ArmorSlot].ItemID == pumpkin {
		t.Error("the head slot holds the pumpkin, want it still empty")
	}

	// A helmet goes on, and what was worn comes back to the hand.
	inv = Inventory{}
	inv.Set(HotbarSlot, protocol.ItemStack{ItemID: helmet, Count: 1})
	inv.Set(ArmorSlot, protocol.ItemStack{ItemID: pumpkin, Count: 1})
	worn, _, ok = inv.Equip(HotbarSlot, false)
	if !ok || worn != ArmorSlot {
		t.Fatalf("Equip of a helmet = %d, %v; want %d, true", worn, ok, ArmorSlot)
	}
	if got := inv.Slots[ArmorSlot]; got.ItemID != helmet {
		t.Errorf("head = %+v, want the helmet", got)
	}
	if got := inv.Slots[HotbarSlot]; got.ItemID != pumpkin {
		t.Errorf("main hand = %+v, want the pumpkin it replaced", got)
	}
}

// TestBodyAndSaddleRoundTrip checks that the body and saddle slots outside
// the window are written to the equipment tag and read back from it.
func TestBodyAndSaddleRoundTrip(t *testing.T) {
	armor, _ := registry.ItemID("minecraft:leather_horse_armor")
	saddle, _ := registry.ItemID("minecraft:saddle")
	inv := Inventory{
		Body:   protocol.ItemStack{ItemID: armor, Count: 1},
		Saddle: protocol.ItemStack{ItemID: saddle, Count: 1},
	}
	root := nbt.Compound{}
	inv.StoreEquipment(root)

	loaded := Inventory{Stored: map[int]nbt.Compound{}}
	LoadEquipment(root, &loaded)
	if loaded.Body.ItemID != armor || loaded.Saddle.ItemID != saddle {
		t.Errorf("loaded body %+v and saddle %+v, want the armor and the saddle", loaded.Body, loaded.Saddle)
	}
}
