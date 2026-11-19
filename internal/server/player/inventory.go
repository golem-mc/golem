package player

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"slices"
)

// The player's own window, open whenever no other is, numbered as packets
// address it:
//
//	0      crafting output
//	1-4    the 2x2 crafting grid
//	5-8    armour: head, chest, legs, feet
//	9-35   the main inventory
//	36-44  the hotbar
//	45     the off hand
const (
	inventorySlots = 46

	// EnderSlots is how many slots an ender chest keeps for a player: the
	// same three rows a chest's window lays over its block entity.
	EnderSlots = 27

	craftingSlots  = 5 // the output and the 2x2 grid, slots 0-4
	ArmorSlot      = 5
	ArmorSlots     = 4
	MainSlot       = 9
	mainSlots      = 27
	HotbarSlot     = 36
	HotbarSlots    = 9
	OffHandSlot    = 45
	playerWindowID = 0

	// MainInvSlots is the main inventory plus hotbar, which every other
	// window appends after its own slots.
	MainInvSlots = mainSlots + HotbarSlots
)

// Inventory is what a player is carrying.
//
// A slot this server cannot model (an item with data components) keeps its
// loaded compound in Stored, so a vanilla world's shulker boxes survive a
// login here. The client sees it empty; putting something there replaces it.
type Inventory struct {
	// CanCraftRecipe gates ordinary crafting recipes; nil permits every recipe.
	CanCraftRecipe func(string) bool
	// CraftOverride is a plugin's result for the grid it was made for; it
	// stops applying once the grid's contents differ.
	CraftOverride *CraftOverride
	// MapScalable reports whether a map can be extended; ScaleMap saves the
	// next scale of a map under a fresh id. nil permits and keeps the id.
	MapScalable func(id int32) bool
	ScaleMap    func(id int32) (int32, bool)
	// MapState reads a map's scale and lock, ok false without saved data;
	// LockMap saves a locked copy under a fresh id. nil offers every recipe
	// and keeps the id.
	MapState func(id int32) (scale int8, locked, ok bool)
	LockMap  func(id int32) (int32, bool)

	Slots [inventorySlots]protocol.ItemStack

	// Stored holds the file's own version of a slot, for the slots that
	// have one this server cannot represent. Indexed the way Slots is.
	Stored map[int]nbt.Compound

	// Body and Saddle are equipment slots with no window slot; /item and the
	// player file reach them, stored under BodyStored and SaddleStored.
	Body, Saddle protocol.ItemStack

	// Ender is what the player keeps in any ender chest, theirs alone
	// whatever chest they stand at; EnderStored plays Stored's part.
	Ender       [EnderSlots]protocol.ItemStack
	EnderStored map[int]nbt.Compound

	// Selected is the hotbar slot the player is holding, 0 to 8.
	Selected int32

	// states are the per-window state IDs packets carry; a click echoes the
	// last one the client saw so the server can tell a stale click. The
	// player's own window keeps its count across containers opened over it.
	// It shares the inventory's lock because every window-packet builder runs
	// there. A window's first packet carries 1, the constant the format
	// shipped before state IDs meant anything.
	states map[int32]int32
}

// Keys of Stored for the equipment slots outside the window; no window slot
// is negative.
const (
	BodyStored   = -1
	SaddleStored = -2
)

// Set puts a stack in a slot, dropping whatever the file said was there:
// the player has replaced it.
func (inv *Inventory) Set(slot int, stack protocol.ItemStack) {
	inv.Slots[slot] = stack
	delete(inv.Stored, slot)
}

// ValidSlot reports whether a slot number from a client addresses a slot
// of the player's own window.
func ValidSlot(slot int32) bool {
	return slot >= 0 && slot < inventorySlots
}

// HasBindingCurse reports whether an enchantment on the stack has
// PREVENT_ARMOR_CHANGE (Curse of Binding); it lives here because package
// rules imports this one.
// vanilla: EnchantmentHelper.has(stack, EnchantmentEffectComponents.PREVENT_ARMOR_CHANGE)
func HasBindingCurse(stack protocol.ItemStack) bool {
	for id, level := range stack.Enchantments {
		if def, ok := registry.EnchantmentEffectsByID(id); ok && level > 0 && def.Effects.PreventArmorChange {
			return true
		}
	}
	return false
}

// Equip wears the stack in slot from if using it wears it, returning the
// worn slot; what was worn before moves to from, or, when from holds more
// than one, is returned as stow for the caller to add to the inventory or
// drop while one of the stack is worn.
//
// It reports false if the item is not worn on use: the equippable
// component's swappable flag keeps a shield in hand and a carved pumpkin off
// the head. Shift-click equips by slot alone (inventory.armorSlotFor).
func (inv *Inventory) Equip(from int, creative bool) (worn int, stow protocol.ItemStack, ok bool) {
	if inv.Slots[from].Empty() {
		return 0, stow, false
	}
	item, ok := registry.StackEquippable(inv.Slots[from])
	if !ok || !item.Swappable || !item.AllowsEntity("minecraft:player") {
		return 0, stow, false
	}
	worn, ok = EquipmentSlotOf(item.Slot)
	if !ok || worn == from {
		return 0, stow, false
	}
	// vanilla: Equippable.swapWithEquipmentSlot
	if !creative && HasBindingCurse(inv.Slots[worn]) || inv.Slots[from].Same(inv.Slots[worn]) {
		return 0, stow, false
	}

	if held := inv.Slots[from]; held.Count > 1 {
		// vanilla: ItemStack.consumeAndReturn, which a creative hand
		// does not shrink
		stow = inv.Slots[worn]
		one := held
		one.Count = 1
		inv.Slots[worn] = one
		if !creative {
			inv.Slots[from].Count--
		}
	} else if creative {
		// The slot gets a copy; the hand keeps its stack when the slot was
		// empty, else takes what was worn.
		held := inv.Slots[from]
		if !inv.Slots[worn].Empty() {
			inv.Slots[from] = inv.Slots[worn]
		}
		inv.Slots[worn] = held
	} else {
		inv.Slots[from], inv.Slots[worn] = inv.Slots[worn], inv.Slots[from]
	}
	// Neither slot holds what the file said any more.
	delete(inv.Stored, from)
	delete(inv.Stored, worn)
	return worn, stow, true
}

// swapSlots exchanges two window slots, carrying Stored along so an
// unmodelled item survives being moved.
func (inv *Inventory) swapSlots(a, b int) {
	inv.Slots[a], inv.Slots[b] = inv.Slots[b], inv.Slots[a]
	aStored, bStored := inv.Stored[a], inv.Stored[b]
	delete(inv.Stored, a)
	delete(inv.Stored, b)
	if bStored != nil {
		inv.Stored[a] = bStored
	}
	if aStored != nil {
		inv.Stored[b] = aStored
	}
}

// SwapHands swaps the selected hotbar slot with the off hand (the F key),
// returning both window slots.
func (inv *Inventory) SwapHands() (main, off int) {
	main = HotbarSlot + int(inv.Selected)
	off = OffHandSlot
	inv.swapSlots(main, off)
	return main, off
}

// Pick is PickStack for a pick that names its item alone - count one, no
// components.
func (inv *Inventory) Pick(itemID int32, creative bool) (a, b int, ok bool) {
	return inv.PickStack(protocol.ItemStack{ItemID: itemID, Count: 1}, creative)
}

// PickStack is middle-click on a block: a hotbar match is selected, a main
// inventory match swaps into suitableHotbarSlot, and in creative an unheld
// item is conjured there, the held stack moving to the first free slot.
// Outside creative an unheld item changes nothing. The match compares the
// picked stack, components and all.
//
// It returns the held window slot and the other slot involved (the swap's
// source, or where the held stack went; -1 when only the held slot changed).
// ok is false when nothing was touched; Selected says where selection ended.
// vanilla: Inventory.pickSlot, Inventory.addAndPickItem
func (inv *Inventory) PickStack(pick protocol.ItemStack, creative bool) (a, b int, ok bool) {
	// Hotbar before main inventory, as findSlotMatchingItem walks them; the off
	// hand is not searched.
	for i := range HotbarSlots {
		if s := inv.Slots[HotbarSlot+i]; !s.Empty() && s.Same(pick) {
			inv.Selected = int32(i)
			return -1, -1, false
		}
	}
	for i := MainSlot; i < MainSlot+mainSlots; i++ {
		if s := inv.Slots[i]; !s.Empty() && s.Same(pick) {
			inv.Selected = int32(inv.suitableHotbarSlot())
			a = HotbarSlot + int(inv.Selected)
			inv.swapSlots(a, i)
			return a, i, true
		}
	}
	if !creative {
		return -1, -1, false
	}
	inv.Selected = int32(inv.suitableHotbarSlot())
	a = HotbarSlot + int(inv.Selected)
	b = -1
	// Move the held stack aside so the conjure does not destroy it; the free
	// slot must be in the main inventory, as the hotbar is full or the held
	// slot would have been empty.
	if !inv.slotFree(a) {
		forEachFreeSlot(func(slot int) {
			if b < 0 && inv.slotFree(slot) {
				b = slot
			}
		})
		if b >= 0 {
			inv.swapSlots(b, a)
		}
	}
	inv.Set(a, pick)
	return a, b, true
}

// slotFree reports whether nothing is in the slot: an item kept only in
// Stored, which Slots cannot represent, still occupies it.
func (inv *Inventory) slotFree(slot int) bool {
	_, stored := inv.Stored[slot]
	return inv.Slots[slot].Empty() && !stored
}

// slotEnchanted reports whether the slot's item holds enchantments, counting
// a Stored-only item whose components carry them.
func (inv *Inventory) slotEnchanted(slot int) bool {
	if inv.Slots[slot].Enchanted() {
		return true
	}
	components, _ := nbt.CompoundView(inv.Stored[slot]["components"])
	_, ok := components["minecraft:enchantments"]
	return ok
}

// suitableHotbarSlot returns a hotbar index (0-8) for a pick: the first empty
// slot from the selection, wrapping, then the first unenchanted, then the
// selected one.
// vanilla: Inventory.getSuitableHotbarSlot
func (inv *Inventory) suitableHotbarSlot() int {
	for j := range HotbarSlots {
		i := (int(inv.Selected) + j) % HotbarSlots
		if inv.slotFree(HotbarSlot + i) {
			return i
		}
	}
	for j := range HotbarSlots {
		i := (int(inv.Selected) + j) % HotbarSlots
		if !inv.slotEnchanted(HotbarSlot + i) {
			return i
		}
	}
	return int(inv.Selected)
}

// Add puts as much of stack as fits: onto same-item stacks first, then the
// first empty slot, hotbar first. The off hand is never an empty slot's
// answer, so overflow drops rather than parking there. A stack with
// components merges only with an identical one.
//
// It returns the count left over (kept by the item entity) and the changed
// slots, for the collector to be told one by one.
// vanilla: Inventory.add
func (inv *Inventory) Add(stack protocol.ItemStack) (left int32, slots []int) {
	if stack.Empty() {
		return 0, nil
	}
	max := registry.StackMaxStackSize(stack)
	left = stack.Count
	// Existing stacks of the same item first: merging a pickup in
	// spares an empty slot.
	inv.forEachRemainingSpaceSlot(func(slot int) {
		if left == 0 {
			return
		}
		into := &inv.Slots[slot]
		if into.Empty() || !into.Same(stack) ||
			into.Count >= registry.StackMaxStackSize(*into) {
			return
		}
		n := min(registry.StackMaxStackSize(*into)-into.Count, left)
		into.Count += n
		left -= n
		slots = append(slots, slot)
		if inv.Stored != nil {
			// The player has replaced whatever the file said this slot was.
			delete(inv.Stored, slot)
		}
	})
	forEachFreeSlot(func(slot int) {
		if left == 0 || !inv.Slots[slot].Empty() {
			return
		}
		n := min(max, left)
		landed := stack
		landed.Count = n
		inv.Slots[slot] = landed
		left -= n
		slots = append(slots, slot)
		if inv.Stored != nil {
			delete(inv.Stored, slot)
		}
	})
	return left, slots
}

// forEachRemainingSpaceSlot visits merge targets: the selected hotbar slot,
// the off hand, then the 36 item slots.
// vanilla: Inventory.getSlotWithRemainingSpace
func (inv *Inventory) forEachRemainingSpaceSlot(visit func(int)) {
	visit(HotbarSlot + int(inv.Selected))
	visit(OffHandSlot)
	forEachFreeSlot(visit)
}

// forEachFreeSlot visits the 36 item slots a pickup can land in, hotbar
// first; never the crafting grid, the armour or the off hand.
// vanilla: Inventory.getFreeSlot
func forEachFreeSlot(visit func(int)) {
	for i := HotbarSlot; i < HotbarSlot+HotbarSlots; i++ {
		visit(i)
	}
	for i := MainSlot; i < MainSlot+mainSlots; i++ {
		visit(i)
	}
}

// VisibleSlot reports whether other players see the slot and so need an
// equipment broadcast: armour, off hand and the held hotbar slot.
func VisibleSlot(inv *Inventory, slot int) bool {
	return slot >= ArmorSlot && slot < ArmorSlot+ArmorSlots ||
		slot == OffHandSlot ||
		slot == HotbarSlot+int(inv.Selected)
}

// Clear empties every slot, which is what a death does to the inventory once
// its contents have been scattered as item entities.
func (inv *Inventory) Clear() {
	for i := range inv.Slots {
		inv.Slots[i] = protocol.ItemStack{}
	}
	inv.Body, inv.Saddle = protocol.ItemStack{}, protocol.ItemStack{}
	inv.Stored = nil
}

// StateID is the state ID the last window packet built from this
// inventory carried for the window named.
func (inv *Inventory) StateID(windowID int32) int32 { return inv.states[windowID] }

// ForgetState drops a closed window's state count: the window ID is
// never reused, so a window opened later counts from one again.
func (inv *Inventory) ForgetState(windowID int32) { delete(inv.states, windowID) }

// ContainerContent builds Set Container Content for the player's own window
// at join: every slot, the 2x2 grid's result (the file stores none), and an
// empty cursor.
func (inv *Inventory) ContainerContent() []byte {
	return inv.ContainerContentWith(protocol.ItemStack{})
}

// ContainerContentWith is ContainerContent with the given stack on the
// cursor, for a caller that touched it.
func (inv *Inventory) ContainerContentWith(carried protocol.ItemStack) []byte {
	// A copy, not the inventory itself: only the packet carries the grid's result.
	var slots [inventorySlots]protocol.ItemStack
	copy(slots[:], inv.Slots[:])
	_, slots[0] = inv.CraftMatch(inv.Slots[playerCraftingGrid:playerCraftingGrid+playerGridSlots], playerGridWidth)
	return inv.WindowContent(playerWindowID, slots[:], carried)
}

// CraftOverride is the result a plugin chose for one grid's contents.
type CraftOverride struct {
	Cells  []protocol.ItemStack
	Width  int32
	Result protocol.ItemStack
}

func (o *CraftOverride) matches(cells []protocol.ItemStack, width int32) bool {
	return o.Width == width && slices.EqualFunc(o.Cells, cells, protocol.ItemStack.Equal)
}

// CraftMatch matches a player crafting grid, respecting recipe permissions.
// vanilla: RecipeCraftingHolder.setRecipeUsed
func (inv *Inventory) CraftMatch(cells []protocol.ItemStack, width int32) (string, protocol.ItemStack) {
	id, result := registry.CraftMatch(cells, width)
	if o := inv.CraftOverride; o != nil && o.matches(cells, width) {
		return id, o.Result
	}
	// CraftMatch retains an ID for decorated pots even though they are special.
	// vanilla: DecoratedPotRecipe extends CustomRecipe
	if id != "" && id != "minecraft:decorated_pot" && inv.CanCraftRecipe != nil && !inv.CanCraftRecipe(id) {
		return "", protocol.ItemStack{}
	}
	if inv.MapScalable != nil && !MapCraftable(result, inv.MapScalable) {
		return "", protocol.ItemStack{}
	}
	return id, result
}

// TakeCraft is a craft result as it leaves the result slot: a map marked for
// scaling or locking loses the mark and takes the id of its scaled or locked
// copy.
// vanilla: MapItem.onCraftedPostProcess
func (inv *Inventory) TakeCraft(result protocol.ItemStack) protocol.ItemStack {
	return PostProcessMap(result, inv.ScaleMap, inv.LockMap)
}

// MapCraftable reports whether a craft result can be taken: a map marked for
// scaling needs a map that is not fully zoomed.
// vanilla: MapExtendingRecipe.matches
func MapCraftable(result protocol.ItemStack, scalable func(int32) bool) bool {
	if op, marked := result.MapPostProcessingOf(); !marked || op != protocol.MapPostProcessingScale {
		return true
	}
	mapID, ok := result.MapIDOf()
	return ok && scalable(mapID)
}

// PostProcessMap drops a result's map post-processing mark and gives it the
// id of its scaled or locked copy; nil functions leave the id as is.
// vanilla: MapItem.onCraftedPostProcess
func PostProcessMap(result protocol.ItemStack, scale, lock func(int32) (int32, bool)) protocol.ItemStack {
	op, marked := result.MapPostProcessingOf()
	if !marked {
		return result
	}
	result.RemoveMapPostProcessing()
	post := scale
	if op == protocol.MapPostProcessingLock {
		post = lock
	}
	if mapID, ok := result.MapIDOf(); ok && post != nil {
		if copied, ok := post(mapID); ok {
			result.SetMapID(copied)
		}
	}
	return result
}

const (
	playerCraftingGrid = 1
	playerGridSlots    = 4
	playerGridWidth    = 2
)

// WindowContent builds Set Container Content for a window: its ID, a fresh
// state ID, its slots and the cursor. It runs under the inventory's lock, so
// two sends cannot interleave state IDs.
func (inv *Inventory) WindowContent(windowID int32, slots []protocol.ItemStack, carried protocol.ItemStack) []byte {
	state := inv.states[windowID] + 1
	if inv.states == nil {
		inv.states = map[int32]int32{}
	}
	inv.states[windowID] = state
	var b protocol.Buffer
	b.PutVarInt(windowID)
	b.PutVarInt(state)
	b.PutVarInt(int32(len(slots)))
	for _, stack := range slots {
		b.PutSlot(stack)
	}
	b.PutSlot(carried)
	return b.Bytes()
}

// WindowSlot builds Set Container Slot for one window slot with a fresh state
// ID: how a misprediction is corrected and an opened window filled in.
func (inv *Inventory) WindowSlot(windowID int32, slot int32, stack protocol.ItemStack) []byte {
	state := inv.states[windowID] + 1
	if inv.states == nil {
		inv.states = map[int32]int32{}
	}
	inv.states[windowID] = state
	var b protocol.Buffer
	b.PutVarInt(windowID)
	b.PutVarInt(state)
	b.PutI16(int16(slot))
	b.PutSlot(stack)
	return b.Bytes()
}

// HeldItem builds the packet that moves the player's hotbar selection.
func HeldItem(selected int32) []byte {
	var b protocol.Buffer
	b.PutVarInt(selected)
	return b.Bytes()
}
