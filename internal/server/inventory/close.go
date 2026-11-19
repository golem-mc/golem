package inventory

import (
	"iter"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// Handback is what closing a window gave back to the player.
type Handback struct {
	// Slots are the inventory slots that took items, in the player's own
	// window numbering.
	Slots []int

	// Dropped is what is thrown at the player's feet.
	Dropped []protocol.ItemStack

	// Visible marks that the held slot or the off hand took items.
	Visible bool

	drop bool
}

// Changed reports whether anything was handed back.
func (h *Handback) Changed() bool { return len(h.Slots) > 0 || len(h.Dropped) > 0 }

// Close hands back what the closing windows hold for the player. With own,
// the player's window closes too, its 2x2 first (vanilla runs
// InventoryMenu.removed before the container's). Each window gives its cursor,
// then its own input slots (grids, enchanting item and lapis, and the anvil,
// stonecutter, smithing, grindstone, cartography and loom inputs), never a
// result.
// vanilla: AbstractContainerMenu.removed, then clearContainer.
//
// A player still in the world gets stacks placed back, the rest dropped;
// with drop (disconnected, or the dead body a respawn removes) all drops.
// vanilla: dropOrPlaceInInventory
func (s *Session) Close(view View, inv *player.Inventory, own, drop bool) Handback {
	s.CancelDrag()
	h := Handback{drop: drop}
	if own {
		if view.Kind == KindPlayer {
			h.give(inv, s.takeCarried())
		}
		h.stacks(inv, inv.Slots[playerCraftingGrid:playerCraftingGrid+playerGridSlots])
	}
	if view.Kind == KindPlayer {
		return h
	}
	h.give(inv, s.takeCarried())
	switch view.Kind {
	case KindCrafting:
		h.stacks(inv, view.Grid)
	case KindEnchant:
		h.stacks(inv, view.Menu)
	case KindAnvil:
		h.stacks(inv, view.Menu[:AnvilResultSlot])
	case KindStonecutter:
		h.stacks(inv, view.Menu[:StonecutterResultSlot])
	case KindSmithing:
		h.stacks(inv, view.Menu[:SmithingResultSlot])
	case KindGrindstone:
		h.stacks(inv, view.Menu[:GrindstoneResultSlot])
	case KindCartography:
		h.stacks(inv, view.Menu[:CartographyResultSlot])
	case KindLoom:
		h.stacks(inv, view.Menu[:LoomResultSlot])
	case KindMerchant:
		// vanilla: MerchantMenu.removed
		h.stacks(inv, view.Menu[:MerchantResultSlot])
	case KindBeacon:
		// The payment is not put back: the whole stack drops where the
		// player stands. vanilla: BeaconMenu.removed
		if stack := view.Menu[0]; !stack.Empty() {
			view.Menu[0] = protocol.ItemStack{}
			h.Dropped = append(h.Dropped, stack)
		}
	}
	return h
}

func (s *Session) takeCarried() protocol.ItemStack {
	held := s.Carried
	s.Carried = protocol.ItemStack{}
	return held
}

func (h *Handback) stacks(inv *player.Inventory, stacks []protocol.ItemStack) {
	for i, stack := range stacks {
		stacks[i] = protocol.ItemStack{}
		h.give(inv, stack)
	}
}

func (h *Handback) give(inv *player.Inventory, stack protocol.ItemStack) {
	if stack.Empty() {
		return
	}
	if h.drop {
		h.Dropped = append(h.Dropped, stack)
		return
	}
	left, slots := placeBack(inv, stack)
	if !left.Empty() {
		h.Dropped = append(h.Dropped, left)
	}
	for _, slot := range slots {
		h.Slots = append(h.Slots, slot)
		h.Visible = h.Visible || player.VisibleSlot(inv, slot)
	}
}

// placeBack merges a stack onto partial same-item stacks (held slot, off
// hand, hotbar, main), then into the first empty hotbar or main slot, never
// an empty off hand. It returns the rest and the changed slots.
// vanilla: Inventory.placeItemBackInInventory
func placeBack(inv *player.Inventory, stack protocol.ItemStack) (protocol.ItemStack, []int) {
	var slots []int
	put := func(slot int) {
		into := inv.Slots[slot]
		var n int32
		switch {
		case into.Empty():
			n = min(stackMax(stack), stack.Count)
			into = stack
			into.Count = n
		case sameStack(stack, into) && into.Count < stackMax(into):
			n = min(stackMax(into)-into.Count, stack.Count)
			into.Count += n
		default:
			return
		}
		inv.Set(slot, into)
		stack.Count -= n
		slots = append(slots, slot)
	}

	partial := []int{player.HotbarSlot + int(inv.Selected), player.OffHandSlot}
	for slot := range inventoryOrder() {
		partial = append(partial, slot)
	}
	for _, slot := range partial {
		if stack.Empty() {
			break
		}
		if !inv.Slots[slot].Empty() {
			put(slot)
		}
	}
	for slot := range inventoryOrder() {
		if stack.Empty() {
			break
		}
		if inv.Slots[slot].Empty() {
			put(slot)
		}
	}
	return stack, slots
}

// inventoryOrder visits the hotbar and then the main rows, the order vanilla
// numbers its 36 inventory slots in.
func inventoryOrder() iter.Seq[int] {
	return func(yield func(int) bool) {
		for i := range player.HotbarSlots {
			if !yield(player.HotbarSlot + i) {
				return
			}
		}
		for i := range player.MainInvSlots - player.HotbarSlots {
			if !yield(player.MainSlot + i) {
				return
			}
		}
	}
}
