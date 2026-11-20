package inventory

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// Session is one player's click state: the cursor stack and the drag in
// progress. Only the connection's goroutine clicks, opens and closes; anyone
// else (closing a window whose container broke) holds the inventory lock, so
// clicks handed a View under that lock need no lock of their own.
type Session struct {
	// Carried is the stack on the cursor, the server's alone to say.
	Carried protocol.ItemStack

	// DragEdit replaces the next drag's plan, set by a plugin that changed it.
	DragEdit *DragPlan

	// dragging is the paint drag in progress: its type and marks.
	dragging  bool
	dragType  int32
	dragSlots []int32

	// selectedBundleSlot and selectedBundleIndex are the scrolled-to bundle
	// entry, -1 each for none. Vanilla keeps it on the bundle component,
	// whose codec does not write it; here it is click state, so a copy of
	// the bundle never carries it.
	selectedBundleSlot  int32
	selectedBundleIndex int32

	// nextWindowID is the ID the last window opened was given.
	nextWindowID int32
}

// OpenWindow clears the bundle selection and returns the next window ID,
// 1 through 100 then back to 1.
// vanilla: ServerPlayer.nextContainerCounter
func (s *Session) OpenWindow() int32 {
	s.ClearBundleSelection()
	s.nextWindowID = s.nextWindowID%100 + 1
	return s.nextWindowID
}

// SelectBundle toggles the scrolled-to entry: repeating it clears, and an
// out-of-range index clears that slot's selection. Only the named slot is
// touched, as the packet reaches only its contents.
// vanilla: BundleContents.Mutable.toggleSelectedItem
func (s *Session) SelectBundle(slot, index, entries int32) {
	if s.selectedBundleSlot == slot && s.selectedBundleIndex == index {
		s.ClearBundleSelection()
		return
	}
	if index < 0 || index >= entries {
		s.clearBundleSelectionAt(slot)
		return
	}
	s.selectedBundleSlot = slot
	s.selectedBundleIndex = index
}

// ClearBundleSelection forgets the selection, which a window change does: the
// slot numbers it names belong to the window that is closing or opening.
func (s *Session) ClearBundleSelection() {
	s.selectedBundleSlot = -1
	s.selectedBundleIndex = -1
}

// clearBundleSelectionAt forgets the selection only when it names the bundle in
// slot; a click on another bundle leaves that bundle's selection alone.
func (s *Session) clearBundleSelectionAt(slot int32) {
	if s.selectedBundleSlot == slot {
		s.ClearBundleSelection()
	}
}

// bundleRemoveIndex is the entry a bundle click removes: the selected one,
// else the newest (first).
// vanilla: BundleItem.removeOne
func (s *Session) bundleRemoveIndex(slot int32, items []protocol.ItemStack) int {
	if s.selectedBundleSlot == slot &&
		s.selectedBundleIndex >= 0 && int(s.selectedBundleIndex) < len(items) {
		return int(s.selectedBundleIndex)
	}
	return 0
}

// Outcome collects a click's changes made under the inventory and block
// entity locks, so the packets go out after release.
type Outcome struct {
	// BundleSounds are played to nearby players except the clicker, whose client plays them locally.
	BundleSounds []registry.Sound

	// Announce lists the window slots whose final stacks the client is
	// told whether it predicted them or not.
	Announce []int32

	// EntitySlots are the changed container slots, which other viewers
	// hear in their own windows' numbering.
	EntitySlots []int32

	// Carried is the stack the cursor ended with; it is always sent.
	Carried protocol.ItemStack

	// Dropped is what the click threw out of the window, to land as item
	// entities once no lock is held.
	Dropped []protocol.ItemStack

	// Thrown indexes the part of Dropped thrown from window or cursor, which
	// the drop statistics count and which records the player as thrower;
	// spilled shift-click leftovers are not.
	// vanilla: AbstractContainerMenu.doClick player.drop(stack, true)
	Thrown []int

	// Crafted is each result a craft took out of a crafting result slot,
	// what the player's crafted counters are raised by.
	Crafted []protocol.ItemStack

	// Recipes is each table or smithing recipe a take crafted, which
	// recipe_crafted fires for.
	Recipes []RecipeCraft

	// Visible marks that a slot the other players watch - armour, the
	// held hotbar slot, the off hand - changed.
	Visible bool

	// ResultTaken marks a furnace output take this click; the caller pays
	// the stored smelting experience on it.
	ResultTaken bool

	// AnvilTaken marks an anvil result take, which clears the inputs,
	// spends the level cost and damages the anvil.
	AnvilTaken bool

	// AnvilCost is the level cost that went with AnvilTaken, for the
	// caller to spend from the snapshot it took before the click.
	AnvilCost int32

	// StonecutterTaken marks a stonecutter take (one input a cut); one
	// sound however many takes.
	StonecutterTaken bool

	// SmithingTaken marks a smithing take (one of each input); one use
	// event however many takes.
	SmithingTaken bool

	// GrindstoneTaken marks that the grindstone's result gave items up
	// this click, the take that empties both its inputs.
	GrindstoneTaken bool

	// GrindstoneWorth is the inputs' non-curse enchantment experience, for
	// the caller to drop as orbs at the block.
	// vanilla: GrindstoneMenu.getExperienceAmount's sum
	GrindstoneWorth int

	// CartographyTaken marks a cartography take (one of each input); one
	// sound however many takes.
	CartographyTaken bool

	// LoomTaken marks a loom take (a banner and a dye); one sound however
	// many takes.
	LoomTaken bool

	// MerchantTraded is the offer of each trade the merchant window's result
	// paid out this click, in order.
	MerchantTraded []Offer
	// MerchantUpdated is set when the result was recomputed from the payments,
	// changed or not.
	MerchantUpdated bool
	// BrewedPotions is the potion ID of each potion a brewing stand's bottle
	// slots gave up this click, protocol.NoPotion for one without. vanilla: BrewingStandMenu.PotionSlot.onTake
	BrewedPotions []int32
	// replaced is the stack the last write to a brewing stand's bottle slot
	// displaced.
	replaced protocol.ItemStack
}

// touched adds a window slot to the ones the client will be corrected on.
func (o *Outcome) touched(slots ...int32) {
	o.Announce = append(o.Announce, slots...)
}

// touchedInv adds a player inventory slot, in the window's numbering.
func (o *Outcome) touchedInv(v View, invSlot int) {
	if slot, ok := v.windowSlotOfInv(invSlot); ok {
		o.Announce = append(o.Announce, slot)
	}
}

// tookFurnaceResult records a furnace output take, on which the stored
// smelting experience is paid; only the furnace window has one.
func (o *Outcome) tookFurnaceResult(view View, slot int32) {
	if view.Kind == KindBrewingStand && slot < brewingBottleSlots {
		if contents, ok := o.replaced.PotionContentsOf(); ok {
			o.BrewedPotions = append(o.BrewedPotions, contents.Potion)
		}
		o.replaced = protocol.ItemStack{}
	}
	if view.Kind.Cooking() && slot == furnaceResultSlot {
		o.ResultTaken = true
	}
}

// throw drops a stack the player threw out of the window or cursor.
func (o *Outcome) throw(stack protocol.ItemStack) {
	o.Thrown = append(o.Thrown, len(o.Dropped))
	o.Dropped = append(o.Dropped, stack)
}

// RecipeCraft is a recipe a take made and the item IDs of the non-empty
// ingredient cells it spent.
type RecipeCraft struct {
	ID          string
	Ingredients []int32
}

// newRecipeCraft is the RecipeCraft of recipe id made from cells.
func newRecipeCraft(id string, cells []protocol.ItemStack) RecipeCraft {
	craft := RecipeCraft{ID: id}
	for _, cell := range cells {
		if !cell.Empty() {
			craft.Ingredients = append(craft.Ingredients, cell.ItemID)
		}
	}
	return craft
}
