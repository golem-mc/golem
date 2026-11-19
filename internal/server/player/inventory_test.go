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

// TestInventoryRoundTripKeepsABundlesContents checks the player file keeps a
// bundle's contents: the component's value is wire bytes a file cannot name,
// so the codec writes the stacks it holds as the stacks a file names.
func TestInventoryRoundTripKeepsABundlesContents(t *testing.T) {
	bundle, ok := registry.ItemID("minecraft:bundle")
	if !ok {
		t.Fatal("the item registry has no minecraft:bundle")
	}
	stone, _ := registry.ItemID("minecraft:stone")

	stack := protocol.ItemStack{ItemID: bundle, Count: 1}
	stack.SetBundleContents([]protocol.ItemStack{{ItemID: stone, Count: 64}})

	var inv Inventory
	inv.Set(HotbarSlot, stack)

	root := nbt.Compound{}
	inv.StoreInventory(root)

	back := LoadInventory(root)
	if got := back.Slots[HotbarSlot]; !got.Equal(stack) {
		t.Errorf("the bundle came back as %+v, want %+v", got, stack)
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

// TestSwapHandsEmpty checks that swapping with an empty off hand merely moves
// the held item across, and that nothing is invented in its place.
func TestSwapHandsEmpty(t *testing.T) {
	diamond, _ := registry.ItemID("minecraft:diamond")

	var inv Inventory
	inv.Set(HotbarSlot+1, protocol.ItemStack{ItemID: diamond, Count: 3})
	inv.Selected = 1

	inv.SwapHands()

	if got := inv.Slots[HotbarSlot+1]; !got.Empty() {
		t.Errorf("main hand = %+v, want empty", got)
	}
	if got := inv.Slots[OffHandSlot]; got.ItemID != diamond || got.Count != 3 {
		t.Errorf("off hand = %+v, want the diamond", got)
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

// TestPickBlockFillsSelectedSlotWhenEmpty checks that picking a block when
// the selected slot is empty puts the fresh stack there:
// getSuitableHotbarSlot's first empty slot is the selected one.
func TestPickBlockFillsSelectedSlotWhenEmpty(t *testing.T) {
	stone, _ := registry.ItemID("minecraft:stone")

	var inv Inventory
	inv.Selected = 2

	a, b, ok := inv.Pick(stone, true)
	if a != HotbarSlot+2 || b != -1 || !ok {
		t.Fatalf("Pick = (%d, %d, %v), want (%d, -1, true): fill the empty selected slot", a, b, ok, HotbarSlot+2)
	}
	if got := inv.Slots[HotbarSlot+2]; got.ItemID != stone || got.Count != 1 {
		t.Errorf("slot 2 = %+v, want a fresh stack of stone", got)
	}
	if inv.Selected != 2 {
		t.Errorf("Selected = %d, want 2 (unchanged)", inv.Selected)
	}
}

// TestPickBlockUsesNextEmptyHotbarSlot checks that picking a block whose
// item is not carried, with the selected slot occupied but a later one
// free, puts the fresh stack in the next empty slot and switches to it
// rather than overwriting the held item.
func TestPickBlockUsesNextEmptyHotbarSlot(t *testing.T) {
	diamond, _ := registry.ItemID("minecraft:diamond")
	stone, _ := registry.ItemID("minecraft:stone")

	var inv Inventory
	inv.Set(HotbarSlot, protocol.ItemStack{ItemID: diamond, Count: 1})
	inv.Selected = 0

	a, b, ok := inv.Pick(stone, true)
	if a != HotbarSlot+1 || b != -1 || !ok {
		t.Fatalf("Pick = (%d, %d, %v), want (%d, -1, true): the next empty slot", a, b, ok, HotbarSlot+1)
	}
	if got := inv.Slots[HotbarSlot+1]; got.ItemID != stone || got.Count != 1 {
		t.Errorf("slot 1 = %+v, want a fresh stack of stone", got)
	}
	if got := inv.Slots[HotbarSlot]; got.ItemID != diamond || got.Count != 1 {
		t.Errorf("slot 0 = %+v, want the diamond untouched", got)
	}
	if inv.Selected != 1 {
		t.Errorf("Selected = %d, want 1 (the new slot)", inv.Selected)
	}
}

// TestPickBlockWrapsAroundToFirstSlot checks that the search for an
// empty hotbar slot wraps from the end of the hotbar back to the
// beginning, the way the vanilla server's getSuitableHotbarSlot does.
func TestPickBlockWrapsAroundToFirstSlot(t *testing.T) {
	diamond, _ := registry.ItemID("minecraft:diamond")
	stone, _ := registry.ItemID("minecraft:stone")

	var inv Inventory
	for i := 5; i < HotbarSlots; i++ {
		inv.Set(HotbarSlot+i, protocol.ItemStack{ItemID: diamond, Count: 1})
	}
	inv.Selected = 7

	a, b, ok := inv.Pick(stone, true)
	if a != HotbarSlot || b != -1 || !ok {
		t.Fatalf("Pick = (%d, %d, %v), want (%d, -1, true): wrap to the first slot", a, b, ok, HotbarSlot)
	}
	if got := inv.Slots[HotbarSlot]; got.ItemID != stone || got.Count != 1 {
		t.Errorf("slot 0 = %+v, want a fresh stack of stone", got)
	}
	if inv.Selected != 0 {
		t.Errorf("Selected = %d, want 0", inv.Selected)
	}
}

// TestPickBlockMovesTheHeldStackAsideWhenConjuring checks that a creative
// conjure with every hotbar slot occupied moves the held stack to the
// first free slot - the first main-inventory slot here - instead of
// destroying it: addAndPickItem shifts it out of the way first.
func TestPickBlockMovesTheHeldStackAsideWhenConjuring(t *testing.T) {
	diamond, _ := registry.ItemID("minecraft:diamond")
	stone, _ := registry.ItemID("minecraft:stone")

	var inv Inventory
	for i := range HotbarSlots {
		inv.Set(HotbarSlot+i, protocol.ItemStack{ItemID: diamond, Count: 1})
	}
	inv.Selected = 3

	a, b, ok := inv.Pick(stone, true)
	if a != HotbarSlot+3 || b != MainSlot || !ok {
		t.Fatalf("Pick = (%d, %d, %v), want (%d, %d, true): the held stack moved aside", a, b, ok, HotbarSlot+3, MainSlot)
	}
	if got := inv.Slots[HotbarSlot+3]; got.ItemID != stone || got.Count != 1 {
		t.Errorf("held slot = %+v, want the fresh stone", got)
	}
	if got := inv.Slots[MainSlot]; got.ItemID != diamond || got.Count != 1 {
		t.Errorf("first free slot = %+v, want the held diamond moved there", got)
	}
	if inv.Selected != 3 {
		t.Errorf("Selected = %d, want 3 (unchanged)", inv.Selected)
	}
}

// TestPickBlockConjureOverwritesTheHeldStackWhenNowhereFree checks that a
// creative conjure with all 36 slots full rewrites the held slot in place:
// there is no free slot to move the held stack to, and addAndPickItem
// still picks.
func TestPickBlockConjureOverwritesTheHeldStackWhenNowhereFree(t *testing.T) {
	diamond, _ := registry.ItemID("minecraft:diamond")
	stone, _ := registry.ItemID("minecraft:stone")

	var inv Inventory
	for i := range HotbarSlots {
		inv.Set(HotbarSlot+i, protocol.ItemStack{ItemID: diamond, Count: 1})
	}
	for i := MainSlot; i < MainSlot+mainSlots; i++ {
		inv.Set(i, protocol.ItemStack{ItemID: diamond, Count: 1})
	}
	inv.Selected = 3

	a, b, ok := inv.Pick(stone, true)
	if a != HotbarSlot+3 || b != -1 || !ok {
		t.Fatalf("Pick = (%d, %d, %v), want (%d, -1, true): the held slot rewritten", a, b, ok, HotbarSlot+3)
	}
	if got := inv.Slots[HotbarSlot+3]; got.ItemID != stone || got.Count != 1 {
		t.Errorf("held slot = %+v, want the fresh stone where the diamond was", got)
	}
	for i := range HotbarSlots {
		if i == 3 {
			continue
		}
		if got := inv.Slots[HotbarSlot+i]; got.ItemID != diamond || got.Count != 1 {
			t.Errorf("hotbar slot %d = %+v, want the diamond untouched", i, got)
		}
	}
	for i := MainSlot; i < MainSlot+mainSlots; i++ {
		if got := inv.Slots[i]; got.ItemID != diamond || got.Count != 1 {
			t.Errorf("main slot %d = %+v, want the diamond untouched", i, got)
		}
	}
	if inv.Selected != 3 {
		t.Errorf("Selected = %d, want 3 (unchanged)", inv.Selected)
	}
}

// TestPickBlockNoopWhenAlreadyHolding checks that picking a block whose
// item is already in the selected slot changes nothing.
func TestPickBlockNoopWhenAlreadyHolding(t *testing.T) {
	stone, _ := registry.ItemID("minecraft:stone")

	var inv Inventory
	inv.Set(HotbarSlot+2, protocol.ItemStack{ItemID: stone, Count: 5})
	inv.Selected = 2

	a, b, ok := inv.Pick(stone, true)
	if a != -1 || b != -1 || ok {
		t.Fatalf("Pick = (%d, %d, %v), want (-1, -1, false): nothing to do", a, b, ok)
	}
	if got := inv.Slots[HotbarSlot+2]; got.ItemID != stone || got.Count != 5 {
		t.Errorf("slot 2 = %+v, want the original stack untouched", got)
	}
	if inv.Selected != 2 {
		t.Errorf("Selected = %d, want 2 (unchanged)", inv.Selected)
	}
}

// TestPickBlockFavorsEarlierHotbarSlot checks that when more than one hotbar
// slot holds the item, the selection moves to the first one, matching the
// vanilla server's findSlotMatchingItem.
func TestPickBlockFavorsEarlierHotbarSlot(t *testing.T) {
	stone, _ := registry.ItemID("minecraft:stone")
	diamond, _ := registry.ItemID("minecraft:diamond")

	var inv Inventory
	inv.Set(HotbarSlot+1, protocol.ItemStack{ItemID: diamond, Count: 1})
	inv.Set(HotbarSlot+5, protocol.ItemStack{ItemID: stone, Count: 1})
	inv.Set(HotbarSlot+7, protocol.ItemStack{ItemID: stone, Count: 1})
	inv.Selected = 1

	a, b, ok := inv.Pick(stone, true)
	if a != -1 || b != -1 || ok {
		t.Fatalf("Pick = (%d, %d, %v), want no slot touched", a, b, ok)
	}
	if inv.Selected != 5 {
		t.Errorf("Selected = %d, want 5 (the first hotbar slot holding stone)", inv.Selected)
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

// TestPickSlotSkipsAnEnchantedStoredOnlySlot checks that the second pass of
// getSuitableHotbarSlot also skips a Stored-only item carrying enchantments.
func TestPickSlotSkipsAnEnchantedStoredOnlySlot(t *testing.T) {
	stone, _ := registry.ItemID("minecraft:stone")
	dirt, _ := registry.ItemID("minecraft:dirt")
	shield := nbt.Compound{
		"id":    nbt.String("minecraft:shield"),
		"count": nbt.Int(1),
		"components": nbt.Compound{
			"minecraft:enchantments": nbt.Compound{"minecraft:unbreaking": nbt.Int(3)},
		},
	}

	var inv Inventory
	inv.Stored = map[int]nbt.Compound{HotbarSlot + 3: shield}
	for i := range HotbarSlots {
		if i != 3 {
			inv.Set(HotbarSlot+i, protocol.ItemStack{ItemID: dirt, Count: 1})
		}
	}
	inv.Set(MainSlot, protocol.ItemStack{ItemID: stone, Count: 32})
	inv.Selected = 3

	a, b, ok := inv.Pick(stone, false)
	if a != HotbarSlot+4 || b != MainSlot || !ok {
		t.Fatalf("Pick = (%d, %d, %v), want (%d, %d, true)", a, b, ok, HotbarSlot+4, MainSlot)
	}
	if _, ok := inv.Stored[HotbarSlot+3]; !ok {
		t.Errorf("the enchanted stored item left slot 3")
	}
}

// TestPickSlotSelectsTheHotbarSlot checks that picking an item already on
// the hotbar moves the selection to it, the same pick a creative player
// makes - and that already holding the item leaves the slots alone.
func TestPickSlotSelectsTheHotbarSlot(t *testing.T) {
	stone, _ := registry.ItemID("minecraft:stone")

	var inv Inventory
	inv.Set(HotbarSlot+4, protocol.ItemStack{ItemID: stone, Count: 12})
	inv.Selected = 0

	a, b, ok := inv.Pick(stone, false)
	if a != -1 || b != -1 || ok {
		t.Fatalf("Pick = (%d, %d, %v), want no slot touched: select the stone's slot", a, b, ok)
	}
	if inv.Selected != 4 {
		t.Errorf("Selected = %d, want 4", inv.Selected)
	}

	a, b, ok = inv.Pick(stone, false)
	if a != -1 || b != -1 || ok {
		t.Fatalf("Pick while holding the item = (%d, %d, %v), want nothing to change", a, b, ok)
	}
	if inv.Selected != 4 {
		t.Errorf("Selected = %d, want 4 (still)", inv.Selected)
	}
}

// TestPickSlotDoesNothingForAnItemThePlayerLacks checks that picking an item
// the player does not carry leaves the slots and the selection alone -
// survival never creates a stack, which is what sets it apart from creative,
// and only the swap and the conjure take a suitable hotbar slot.
func TestPickSlotDoesNothingForAnItemThePlayerLacks(t *testing.T) {
	diamond, _ := registry.ItemID("minecraft:diamond")
	stone, _ := registry.ItemID("minecraft:stone")

	var inv Inventory
	inv.Set(HotbarSlot, protocol.ItemStack{ItemID: diamond, Count: 1})
	inv.Selected = 0

	a, b, ok := inv.Pick(stone, false)
	if a != -1 || b != -1 || ok {
		t.Fatalf("Pick = (%d, %d, %v), want nothing to change", a, b, ok)
	}
	if got := inv.Slots[HotbarSlot]; got.ItemID != diamond || got.Count != 1 {
		t.Errorf("held slot = %+v, want the diamond untouched", got)
	}
	for slot := MainSlot; slot < MainSlot+mainSlots; slot++ {
		if !inv.Slots[slot].Empty() {
			t.Errorf("main slot %d holds %+v, want the inventory unchanged", slot, inv.Slots[slot])
		}
	}
	if inv.Selected != 0 {
		t.Errorf("Selected = %d, want 0 (unchanged)", inv.Selected)
	}
}

// TestPickSlotLeavesARenamedStackAlone checks that matching goes by the
// whole stack, not the item alone: vanilla's findSlotMatchingItem compares
// components, so a renamed stone is not the stone a pick asks for.
func TestPickSlotLeavesARenamedStackAlone(t *testing.T) {
	stone, _ := registry.ItemID("minecraft:stone")
	diamond, _ := registry.ItemID("minecraft:diamond")
	name := protocol.Text{Text: "Rock"}
	renamed := protocol.ItemStack{ItemID: stone, Count: 32, CustomName: &name}

	var inv Inventory
	inv.Set(HotbarSlot, protocol.ItemStack{ItemID: diamond, Count: 1})
	inv.Set(MainSlot, renamed)
	inv.Selected = 0

	a, b, ok := inv.Pick(stone, false)
	if a != -1 || b != -1 || ok {
		t.Fatalf("Pick = (%d, %d, %v), want nothing to change", a, b, ok)
	}
	if got := inv.Slots[MainSlot]; !got.Same(renamed) {
		t.Errorf("main slot = %+v, want the renamed stone untouched", got)
	}
	if inv.Selected != 0 {
		t.Errorf("Selected = %d, want 0 (unchanged)", inv.Selected)
	}
}

// TestPickSlotMovesStoredItem checks that an item with data components -
// which lives in stored rather than slots, and so is invisible to the
// client - is carried along when a pick swaps the held slot, rather than
// being dropped.
func TestPickSlotMovesStoredItem(t *testing.T) {
	stone, _ := registry.ItemID("minecraft:stone")
	enchanted := nbt.Compound{
		"id":    nbt.String("minecraft:shield"),
		"count": nbt.Int(1),
		"components": nbt.Compound{
			"minecraft:custom_name": nbt.String("Guard"),
		},
	}

	var inv Inventory
	inv.Stored = map[int]nbt.Compound{}
	inv.Stored[HotbarSlot] = enchanted
	inv.Set(MainSlot, protocol.ItemStack{ItemID: stone, Count: 1})
	dirt, _ := registry.ItemID("minecraft:dirt")
	// Every other hotbar slot is occupied, so the held slot is the only choice.
	for slot := HotbarSlot + 1; slot < HotbarSlot+HotbarSlots; slot++ {
		inv.Set(slot, protocol.ItemStack{ItemID: dirt, Count: 1})
	}
	inv.Selected = 0

	a, b, ok := inv.Pick(stone, false)
	if a != HotbarSlot || b != MainSlot || !ok {
		t.Fatalf("Pick = (%d, %d, %v), want a swap of the held slot", a, b, ok)
	}
	if got := inv.Slots[HotbarSlot]; got.ItemID != stone || got.Count != 1 {
		t.Errorf("held slot = %+v, want the stone", got)
	}
	if entry, ok := inv.Stored[MainSlot]; !ok || !reflect.DeepEqual(entry, enchanted) {
		t.Errorf("main slot stored = %v, %v; want the enchanted shield", entry, ok)
	}
	if _, ok := inv.Stored[HotbarSlot]; ok {
		t.Errorf("the held slot still has a stored entry, want it moved to the main slot")
	}
}

// TestAddSkipsTheEmptyOffHand checks where a pickup lands when the 36 item
// slots are full and the off hand is empty: nowhere. vanilla's getFreeSlot
// scans only the item slots, so what does not fit comes back to be dropped
// rather than parking in the off hand.
func TestAddSkipsTheEmptyOffHand(t *testing.T) {
	stone, _ := registry.ItemID("minecraft:stone")
	dirt, _ := registry.ItemID("minecraft:dirt")

	var inv Inventory
	for slot := HotbarSlot; slot < HotbarSlot+HotbarSlots; slot++ {
		inv.Set(slot, protocol.ItemStack{ItemID: stone, Count: 64})
	}
	for slot := MainSlot; slot < MainSlot+mainSlots; slot++ {
		inv.Set(slot, protocol.ItemStack{ItemID: stone, Count: 64})
	}

	left, slots := inv.Add(protocol.ItemStack{ItemID: dirt, Count: 1})
	if left != 1 || len(slots) != 0 {
		t.Fatalf("Add = %d left over in %v, want the whole stack back and no slot touched", left, slots)
	}
	if got := inv.Slots[OffHandSlot]; !got.Empty() {
		t.Errorf("off hand = %+v, want it still empty", got)
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

// TestAddMergesIntoTheOffHand checks the merge pass, which is the one way
// an empty off hand is ever filled: getSlotWithRemainingSpace looks at the
// selected slot first, then the off hand, then the 36 item slots.
func TestAddMergesIntoTheOffHand(t *testing.T) {
	stone, _ := registry.ItemID("minecraft:stone")
	dirt, _ := registry.ItemID("minecraft:dirt")

	var inv Inventory
	inv.Set(HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 64})
	inv.Set(HotbarSlot+2, protocol.ItemStack{ItemID: dirt, Count: 30})
	inv.Set(OffHandSlot, protocol.ItemStack{ItemID: dirt, Count: 10})
	inv.Selected = 0 // holding the full stone

	left, slots := inv.Add(protocol.ItemStack{ItemID: dirt, Count: 5})
	if left != 0 {
		t.Fatalf("Add = %d left over, want it all merged", left)
	}
	if len(slots) != 1 || slots[0] != OffHandSlot {
		t.Fatalf("Add changed %v, want the off hand alone", slots)
	}
	if got := inv.Slots[OffHandSlot]; got.Count != 15 {
		t.Errorf("off hand = %+v, want fifteen dirt", got)
	}
	if got := inv.Slots[HotbarSlot+2]; got.Count != 30 {
		t.Errorf("hotbar slot = %+v, want the thirty dirt untouched (the off hand merges first)", got)
	}

	// The selected slot is the first look: a partial stack in the hand
	// takes the pickup before the off hand does.
	inv = Inventory{}
	inv.Set(HotbarSlot+2, protocol.ItemStack{ItemID: dirt, Count: 59})
	inv.Set(OffHandSlot, protocol.ItemStack{ItemID: dirt, Count: 10})
	inv.Selected = 2

	left, slots = inv.Add(protocol.ItemStack{ItemID: dirt, Count: 5})
	if left != 0 || len(slots) != 1 || slots[0] != HotbarSlot+2 {
		t.Fatalf("Add = %d left over in %v, want it merged into the selected slot", left, slots)
	}
	if got := inv.Slots[HotbarSlot+2]; got.Count != 64 {
		t.Errorf("selected slot = %+v, want it filled to the stack size", got)
	}
	if got := inv.Slots[OffHandSlot]; got.Count != 10 {
		t.Errorf("off hand = %+v, want it untouched", got)
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

// TestEquipmentRemovedWhenTakenOff checks that a player who takes everything
// off is saved without it. The tag is carried over from the file they were
// loaded with, so leaving it alone would put the armour back on next time.
func TestEquipmentRemovedWhenTakenOff(t *testing.T) {
	root := nbt.Compound{"equipment": nbt.Compound{
		"head": nbt.Compound{"id": nbt.String("minecraft:diamond_helmet"), "count": nbt.Int(1)},
	}}

	inv := LoadInventory(root)
	helmet, _ := registry.ItemID("minecraft:diamond_helmet")
	if got := inv.Slots[ArmorSlot]; got.ItemID != helmet || got.Count != 1 {
		t.Fatalf("head slot = %+v, want the helmet it was loaded with", got)
	}

	inv.Set(ArmorSlot, protocol.ItemStack{})
	inv.StoreInventory(root)

	if equipment, ok := root["equipment"]; ok {
		t.Errorf("equipment = %v, want the tag gone entirely", equipment)
	}
	if again := LoadInventory(root); !again.Slots[ArmorSlot].Empty() {
		t.Errorf("head slot came back as %+v after being emptied", again.Slots[ArmorSlot])
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

// TestInventoryReadsAMissingCountAsOne checks a stored stack without a count
// is one item.
// vanilla: ItemStack.MAP_CODEC
func TestInventoryReadsAMissingCountAsOne(t *testing.T) {
	inv := LoadInventory(nbt.Compound{"Inventory": nbt.List{
		Elem:  nbt.TypeCompound,
		Items: []nbt.Tag{nbt.Compound{"Slot": nbt.Byte(0), "id": nbt.String("minecraft:stone")}},
	}})
	slot, _ := WindowSlotOf(0)
	if got := inv.Slots[slot]; got.Empty() || got.Count != 1 {
		t.Errorf("slot 0 holds %+v, want one stone", got)
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

// TestEquipWearsOneOfAStack checks that using a stack of more than one
// wears one of it and hands back what was worn for the inventory, the hand
// keeping the rest; creative keeps the whole stack.
// vanilla: Equippable.swapWithEquipmentSlot
func TestEquipWearsOneOfAStack(t *testing.T) {
	pumpkin, _ := registry.ItemID("minecraft:carved_pumpkin")
	sticks, ok := registry.StoredStack(nbt.Compound{"id": nbt.String("minecraft:stick"), "count": nbt.Int(5),
		"components": nbt.Compound{"minecraft:equippable": nbt.Compound{"slot": nbt.String("head")}}})
	if !ok {
		t.Fatal("the head equippable stick did not read")
	}
	for _, creative := range []bool{false, true} {
		var inv Inventory
		inv.Set(HotbarSlot, sticks)
		inv.Set(ArmorSlot, protocol.ItemStack{ItemID: pumpkin, Count: 1})
		worn, stow, ok := inv.Equip(HotbarSlot, creative)
		if !ok || worn != ArmorSlot {
			t.Fatalf("creative=%v: Equip = %d, %v; want %d, true", creative, worn, ok, ArmorSlot)
		}
		if got := inv.Slots[ArmorSlot]; got.ItemID != sticks.ItemID || got.Count != 1 {
			t.Errorf("creative=%v: head = %+v, want one stick", creative, got)
		}
		left := int32(4)
		if creative {
			left = 5
		}
		if got := inv.Slots[HotbarSlot]; got.ItemID != sticks.ItemID || got.Count != left {
			t.Errorf("creative=%v: hand = %+v, want %d sticks", creative, got, left)
		}
		if stow.ItemID != pumpkin {
			t.Errorf("creative=%v: stow = %+v, want the pumpkin", creative, stow)
		}
	}
}

// TestEquipSkipsBoundArmour checks that using a helmet does not swap out a
// worn piece with Curse of Binding, except in creative.
// vanilla: Equippable.swapWithEquipmentSlot
func TestEquipSkipsBoundArmour(t *testing.T) {
	helmet, _ := registry.ItemID("minecraft:iron_helmet")
	curse, _ := registry.EnchantmentID("minecraft:binding_curse")
	for _, creative := range []bool{false, true} {
		var inv Inventory
		inv.Set(HotbarSlot, protocol.ItemStack{ItemID: helmet, Count: 1})
		inv.Set(ArmorSlot, protocol.ItemStack{ItemID: helmet, Count: 1, Enchantments: map[int32]int32{curse: 1}})
		_, _, ok := inv.Equip(HotbarSlot, creative)
		if ok != creative {
			t.Errorf("Equip over a cursed helmet, creative=%v: equipped = %v", creative, ok)
		}
	}
}

// TestPickBlockKeepsAStoredOnlyItem checks that a slot holding an item only
// Stored can represent counts as taken, so a creative pick neither
// overwrites it nor parks the held stack on it.
func TestPickBlockKeepsAStoredOnlyItem(t *testing.T) {
	stone, _ := registry.ItemID("minecraft:stone")
	unmodelled := nbt.Compound{"id": nbt.String("minecraft:not_an_item")}

	t.Run("selected slot", func(t *testing.T) {
		var inv Inventory
		inv.Stored = map[int]nbt.Compound{HotbarSlot + 2: unmodelled}
		inv.Selected = 2
		a, _, ok := inv.Pick(stone, true)
		if !ok || a == HotbarSlot+2 || inv.Selected == 2 {
			t.Fatalf("Pick = slot %d, %v, Selected %d; want the first empty slot instead of the stored one", a, ok, inv.Selected)
		}
		if !reflect.DeepEqual(inv.Stored[HotbarSlot+2], unmodelled) {
			t.Errorf("Stored = %v, want the item kept", inv.Stored)
		}
	})
	t.Run("full hotbar", func(t *testing.T) {
		var inv Inventory
		for i := range HotbarSlots {
			inv.Set(HotbarSlot+i, protocol.ItemStack{ItemID: stone + 1, Count: 1})
		}
		inv.Stored = map[int]nbt.Compound{MainSlot: unmodelled}
		inv.Selected = 0
		a, b, ok := inv.Pick(stone, true)
		if !ok || b != MainSlot+1 {
			t.Fatalf("Pick = (%d, %d, %v); want the held stack moved to the first free slot %d", a, b, ok, MainSlot+1)
		}
		if !reflect.DeepEqual(inv.Stored[MainSlot], unmodelled) {
			t.Errorf("Stored = %v, want the item at the first main slot kept", inv.Stored)
		}
	})
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

// TestEquipCreativeCopyAndSameStack checks the creative copy rule and that a
// piece identical to the worn one is not swapped.
// vanilla: Equippable.swapWithEquipmentSlot
func TestEquipCreativeCopyAndSameStack(t *testing.T) {
	helmet, _ := registry.ItemID("minecraft:iron_helmet")
	gold, _ := registry.ItemID("minecraft:golden_helmet")

	var inv Inventory
	inv.Set(OffHandSlot, protocol.ItemStack{ItemID: helmet, Count: 1})
	inv.Set(ArmorSlot, protocol.ItemStack{ItemID: helmet, Count: 1})
	if _, _, ok := inv.Equip(OffHandSlot, false); ok {
		t.Error("Equip of a piece identical to the worn one reported success")
	}

	inv = Inventory{}
	inv.Set(OffHandSlot, protocol.ItemStack{ItemID: helmet, Count: 1})
	if worn, _, ok := inv.Equip(OffHandSlot, true); !ok || worn != ArmorSlot {
		t.Fatalf("creative Equip = %d, %v", worn, ok)
	}
	if inv.Slots[OffHandSlot].ItemID != helmet || inv.Slots[ArmorSlot].ItemID != helmet {
		t.Errorf("hand %+v, head %+v; want both the helmet", inv.Slots[OffHandSlot], inv.Slots[ArmorSlot])
	}

	inv.Set(OffHandSlot, protocol.ItemStack{ItemID: gold, Count: 1})
	inv.Equip(OffHandSlot, true)
	if inv.Slots[OffHandSlot].ItemID != helmet || inv.Slots[ArmorSlot].ItemID != gold {
		t.Errorf("hand %+v, head %+v; want the swap", inv.Slots[OffHandSlot], inv.Slots[ArmorSlot])
	}
}
