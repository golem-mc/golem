package inventory

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// Clicks that move stacks without the cursor: number-key swap, double-click
// collect and shift-click.

// swapButtonSlot is the inventory slot a mode-2 button names: a hotbar slot
// for 0-8, the off hand for 40.
// vanilla: AbstractContainerMenu.doClick SWAP
func swapButtonSlot(button int32) (int, bool) {
	switch {
	case button >= 0 && button < player.HotbarSlots:
		return player.HotbarSlot + int(button), true
	case button == 40:
		return player.OffHandSlot, true
	}
	return 0, false
}

// hotbarSwap is mode 2: the window slot's stack trades with the keyed hotbar
// slot, or the off hand for F. A stack the slot refuses stays put.
func hotbarSwap(view View, inv *player.Inventory, button, slot int32, creative bool, xpLevel int32, outcome *Outcome) {
	if view.isAnvilResult(slot) {
		anvilSwap(view, inv, button, creative, xpLevel, outcome)
		return
	}
	if view.isStonecutterResult(slot) {
		stonecutterSwap(view, inv, button, outcome)
		return
	}
	if view.isSmithingResult(slot) {
		smithingSwap(view, inv, button, outcome)
		return
	}
	if view.isGrindstoneResult(slot) {
		grindstoneSwap(view, inv, button, outcome)
		return
	}
	if view.isCartographyResult(slot) {
		cartographyResultSlot.swap(view, inv, button, outcome)
		return
	}
	if view.isLoomResult(slot) {
		loomResultSlot.swap(view, inv, button, outcome)
		return
	}
	if view.isMerchantResult(slot) {
		merchantSwap(view, inv, button, outcome)
		return
	}
	if view.isResult(slot) {
		craftSwap(view, inv, button, outcome)
		return
	}
	invSlot, ok := swapButtonSlot(button)
	if !ok {
		return
	}
	// The off hand has no slot outside the player's own window: it changes
	// with no window slot to correct, the client having predicted the swap.
	windowSlotOfTarget, ok := view.windowSlotOfInv(invSlot)
	if ok && windowSlotOfTarget == slot {
		return
	}
	setTarget := func(stack protocol.ItemStack) {
		if ok {
			setStack(view, inv, windowSlotOfTarget, stack, outcome)
			return
		}
		inv.Set(invSlot, stack)
		outcome.Visible = true
	}
	swapped := view.Stack(inv, slot)
	hotbarStack := inv.Slots[invSlot]
	if !swapped.Empty() && boundArmour(view, inv, slot, creative) {
		return
	}
	if !view.placeable(slot, hotbarStack) {
		return
	}
	// The hotbar's stack goes in capped at the slot's limit (one for armour),
	// the rest staying on the hotbar. The furnace output swaps only into an
	// empty hotbar slot, and that is a take that pays the smelt's experience.
	limit := view.slotLimit(slot, hotbarStack)
	if swapped.Empty() {
		if hotbarStack.Count <= limit {
			setStack(view, inv, slot, hotbarStack, outcome)
			setTarget(protocol.ItemStack{})
			return
		}
		in := hotbarStack
		in.Count = limit
		setStack(view, inv, slot, in, outcome)
		rest := hotbarStack
		rest.Count = hotbarStack.Count - limit
		setTarget(rest)
		return
	}
	if hotbarStack.Count <= limit {
		setStack(view, inv, slot, hotbarStack, outcome)
		setTarget(swapped)
		outcome.tookFurnaceResult(view, slot)
		return
	}
	// The slot is occupied and the hotbar's stack is over its limit: the limit
	// goes in, the rest stays on the hotbar, and the slot's old stack goes to the
	// player's things, overflow dropped at the feet.
	in := hotbarStack
	in.Count = limit
	setStack(view, inv, slot, in, outcome)
	rest := hotbarStack
	rest.Count = hotbarStack.Count - limit
	setTarget(rest)
	outcome.tookFurnaceResult(view, slot)
	left, landed := inv.Add(swapped)
	for _, invSlot := range landed {
		outcome.touchedInv(view, invSlot)
		if player.VisibleSlot(inv, invSlot) {
			outcome.Visible = true
		}
	}
	if left > 0 {
		spill := swapped
		spill.Count = left
		outcome.throw(spill)
	}
}

// mayPickup is the slot's own mayPickup; only the anvil result and a worn
// piece with Curse of Binding refuse.
func mayPickup(view View, inv *player.Inventory, slot int32, creative bool, xpLevel int32) bool {
	if boundArmour(view, inv, slot, creative) {
		return false
	}
	if !view.isAnvilResult(slot) {
		return true
	}
	return anvilMayPickup(anvilAnswer(view, inv, creative), creative, xpLevel)
}

// boundArmour reports whether the slot is worn armour (the player's, or a
// mount's saddle or body) with Curse of Binding, which survival cannot take
// off.
// vanilla: ArmorSlot.mayPickup
func boundArmour(view View, inv *player.Inventory, slot int32, creative bool) bool {
	if creative {
		return false
	}
	if view.Kind == KindHorse && (slot == HorseSaddleSlot || slot == HorseArmorSlot) {
		return player.HasBindingCurse(view.Stack(inv, slot))
	}
	loc, index := view.resolve(slot)
	return loc == slotInv && index >= player.ArmorSlot && index < player.ArmorSlot+player.ArmorSlots &&
		player.HasBindingCurse(inv.Slots[index])
}

// collectStack is mode 6, double-click: the cursor stack fills up from same
// items across the window, full stacks last; the button only picks the
// client's order. Every draw is a take, so the furnace result pays its
// experience.
//
// With a cursor stack it gathers unless the clicked slot holds an item the
// player may pick up. A slot refusing pickup (an unaffordable anvil result)
// gathers like an empty one and keeps its item.
// vanilla: AbstractContainerMenu.doClick PICKUP_ALL
func collectStack(session *Session, view View, inv *player.Inventory, slot, button int32, creative bool, xpLevel int32, outcome *Outcome) {
	if session.Carried.Empty() {
		return
	}
	if clicked := view.Stack(inv, slot); !clicked.Empty() && mayPickup(view, inv, slot, creative, xpLevel) {
		return
	}
	order := make([]int32, 0, view.Slots())
	for s := range view.Slots() {
		order = append(order, int32(s))
	}
	if button == 1 {
		slices.Reverse(order)
	}
	// Pass one draws from partial stacks, pass two from full ones, so a full
	// stack is never opened while a partial one remains.
	for pass := range 2 {
		for _, s := range order {
			if view.Kind == KindMerchant {
				// vanilla: MerchantMenu.canTakeItemForPickAll
				return
			}
			held := session.Carried
			if held.Count >= stackMax(held) {
				return
			}
			// vanilla: StonecutterMenu.canTakeItemForPickAll,
			// SmithingMenu.canTakeItemForPickAll,
			// CartographyTableMenu.canTakeItemForPickAll, LoomMenu.canTakeItemForPickAll,
			// MerchantMenu.canTakeItemForPickAll
			if s == slot || view.isResult(s) || view.isStonecutterResult(s) ||
				view.isSmithingResult(s) || view.isCartographyResult(s) ||
				view.isLoomResult(s) || view.isMerchantResult(s) {
				continue
			}
			if view.isAnvilResult(s) {
				// The anvil result is a gather source; its take clears the inputs and
				// pays the cost.
				anvilCollect(session, view, inv, creative, xpLevel, outcome)
				continue
			}
			if view.isGrindstoneResult(s) {
				// The grindstone's result is one of the places a double
				// click gathers from too, its take emptying the inputs.
				grindstoneCollect(session, view, inv, outcome)
				continue
			}
			stack := view.Stack(inv, s)
			if stack.Empty() || !sameStack(stack, held) {
				continue
			}
			if pass == 0 && stack.Count >= stackMax(stack) {
				continue
			}
			// The cursor's stack size is the limit, so a worn carved pumpkin or
			// head can be gathered.
			// vanilla: AbstractContainerMenu.doClick PICKUP_ALL, Slot.mayPickup
			if boundArmour(view, inv, s, creative) {
				continue
			}
			take := min(stackMax(held)-held.Count, stack.Count)
			if take < stack.Count && !view.placeable(s, stack) {
				// vanilla: Slot.tryRemove, allowModification
				continue
			}
			session.Carried.Count += take
			left := stack
			left.Count = stack.Count - take
			if left.Count <= 0 {
				left = protocol.ItemStack{}
			}
			setStack(view, inv, s, left, outcome)
			outcome.tookFurnaceResult(view, s)
		}
	}
}

// quickMove is mode 1, shift-click: moves a stack between the window's own
// slots and the player's things without the cursor, as often as it fits.
// The direction depends on the window and the clicked slot.
// vanilla: each menu's quickMoveStack
func quickMove(view View, inv *player.Inventory, slot int32, creative bool, xpLevel int32, outcome *Outcome) {
	if boundArmour(view, inv, slot, creative) {
		return
	}
	if view.isAnvilResult(slot) {
		shiftAnvilFromResult(view, inv, creative, xpLevel, outcome)
		return
	}
	if view.isStonecutterResult(slot) {
		shiftStonecutterFromResult(view, inv, outcome)
		return
	}
	if view.isSmithingResult(slot) {
		shiftSmithingFromResult(view, inv, outcome)
		return
	}
	if view.isGrindstoneResult(slot) {
		shiftGrindstoneFromResult(view, inv, outcome)
		return
	}
	if view.isCartographyResult(slot) {
		cartographyResultSlot.shiftFromResult(view, inv, outcome)
		return
	}
	if view.isLoomResult(slot) {
		loomResultSlot.shiftFromResult(view, inv, outcome)
		return
	}
	if view.isMerchantResult(slot) {
		shiftMerchantFromResult(view, inv, outcome)
		return
	}

	if view.isResult(slot) {
		shiftCraftFromResult(view, inv, outcome)
		return
	}

	// Repeat while a pass moved something and the slot still holds the same
	// item: a pass that fills one target may leave the rest for another, and
	// the targets depend on what each pass left behind.
	// vanilla: AbstractContainerMenu.doClick (QUICK_MOVE)
	for {
		stack := view.Stack(inv, slot)
		if stack.Empty() {
			return
		}
		quickMoveStack(view, inv, slot, stack, outcome)
		left := view.Stack(inv, slot)
		if left.Empty() || left.Count >= stack.Count || left.ItemID != stack.ItemID {
			return
		}
	}
}

// quickMoveStack is one pass of a plain slot's shift-click.
// vanilla: each menu's quickMoveStack
func quickMoveStack(view View, inv *player.Inventory, slot int32, stack protocol.ItemStack, outcome *Outcome) {
	invBase := view.InvBase()

	// Target slots by window and source; reversed walks from the hotbar end,
	// as the chest menus fill the player's things.
	var targets []int32
	// playersEnd is where main inventory and hotbar end; the player's own
	// window has the off hand after, outside every move but grid and armour
	// returns.
	playersEnd := invBase + player.MainInvSlots
	if slot < invBase {
		if reversed, ok := containerReturnOrder(view.Kind, slot); ok {
			finishQuickMove(view, inv, slot, stack, playerSectionSlots(invBase, playersEnd), reversed, outcome)
			return
		}
	}
	switch view.Kind {
	// Player's own window: grid, armour and off hand into the player's
	// things, main inventory to hotbar and back.
	case KindPlayer:
		switch {
		case slot < player.MainSlot || slot == player.OffHandSlot:
			targets = playerSectionSlots(player.MainSlot, playersEnd)
		case slot < player.HotbarSlot:
			targets = playerSectionSlots(player.HotbarSlot, playersEnd)
		default:
			targets = playerSectionSlots(player.MainSlot, player.HotbarSlot)
		}
		// Armour tries its own slot first and falls in among the things only
		// when it is taken.
		// vanilla: InventoryMenu.quickMoveStack
		if slot >= player.MainSlot {
			if worn, ok := armorSlotFor(stack); ok {
				targets = append([]int32{int32(worn)}, targets...)
			}
		}

	case KindCrafting:
		// The grid first; the main/hotbar swap only when nothing fit.
		// vanilla: CraftingMenu.quickMoveStack
		var grid []int32
		for s := craftingTableGrid; s < craftingTableGrid+CraftingGridCells; s++ {
			grid = append(grid, int32(s))
		}
		left := moveIntoStacks(view, inv, stack, grid, false, outcome)
		if left.Count < stack.Count {
			setStack(view, inv, slot, left, outcome)
			return
		}
		targets = playerInventorySwap(invBase, slot)

	case KindCrafter:
		// The crafter's grid is its own slots 0-8, the result one the
		// player cannot shift into.
		for s := range CraftingGridCells {
			targets = append(targets, int32(s))
		}

	case KindBrewingStand:
		// Fuel, then ingredient, then a potion into a bottle slot; anything
		// else swaps main inventory and hotbar.
		// vanilla: BrewingStandMenu.quickMoveStack
		switch {
		case IsBrewingFuel(stack):
			// Fuel slot first; blaze powder it cannot take falls through to
			// the ingredient slot, which brews with it too.
			// vanilla: BrewingStandMenu.quickMoveStack
			filled := view.Stack(inv, 4)
			if (filled.Empty() || sameStack(filled, stack)) &&
				filled.Count < stackMax(stack) {
				targets = []int32{4}
			} else if isBrewingIngredient(registry.Item(stack.ItemID)) {
				targets = []int32{3}
			}
		case isBrewingIngredient(registry.Item(stack.ItemID)):
			targets = []int32{3}
		case isPotionBottle(stack):
			targets = []int32{0, 1, 2}
		default:
			targets = playerInventorySwap(invBase, slot)
		}

	// Chest-shaped windows route like the chest: barrel, shulker box and
	// ender chest share its menu, dispenser and dropper are its nine-slot
	// form, the hopper its five-slot one.
	case KindChest, KindBarrel, KindShulker, KindEnderChest,
		KindDispenser, KindDropper, KindHopper, KindDoubleChest, KindMenu:
		targets = playerSectionSlots(0, invBase)

	case KindFurnace, KindBlastFurnace, KindSmoker:
		// Into the slot the item belongs in (cookable to ingredient, burnable
		// to fuel), otherwise the main/hotbar swap.
		// vanilla: AbstractFurnaceMenu.quickMoveStack
		if registry.CookingMatch(recipeKind(view.Kind), stack) != nil {
			targets = []int32{0}
		} else if IsCookingFuel(stack) {
			targets = []int32{1}
		} else {
			targets = playerInventorySwap(invBase, slot)
		}

	case KindEnchant:
		// Lapis to the lapis slot, anything else one item into the item
		// slot; the rest of a stack stays, and an occupied slot moves
		// nothing.
		if isLapis(stack) {
			targets = []int32{1}
		} else {
			if view.Stack(inv, 0).Empty() && view.placeable(0, stack) {
				one := stack
				one.Count = 1
				setStack(view, inv, 0, one, outcome)
				stack.Count--
			}
			if stack.Count <= 0 {
				stack = protocol.ItemStack{}
			}
			setStack(view, inv, slot, stack, outcome)
			return
		}

	case KindHorse:

		// Armour slot, then saddle slot (each only when empty and accepting),
		// then the chest; what the chest takes none of moves among the
		// player's things.
		// vanilla: AbstractMountInventoryMenu.quickMoveStack
		switch {
		case view.Stack(inv, HorseArmorSlot).Empty() && view.placeable(HorseArmorSlot, stack):
			targets = []int32{HorseArmorSlot}
		case view.Stack(inv, HorseSaddleSlot).Empty() && view.placeable(HorseSaddleSlot, stack):
			targets = []int32{HorseSaddleSlot}
		default:
			if view.Columns > 0 {
				left := moveIntoStacks(view, inv, stack, playerSectionSlots(HorseBaseSlots, invBase), false, outcome)
				if left.Count < stack.Count {
					setStack(view, inv, slot, left, outcome)
					return
				}
			}
			targets = playerInventorySwap(invBase, slot)
		}

	case KindAnvil:
		// The two inputs, the first that takes it first.
		// vanilla: ItemCombinerMenu.quickMoveStack
		targets = []int32{0, 1}

	case KindStonecutter:
		// Cuttable items to the input, the rest among the player's things;
		// the input home.
		// vanilla: StonecutterMenu.quickMoveStack
		if stonecutterAccepts(stack) {
			targets = []int32{0}
		} else {
			targets = playerInventorySwap(invBase, slot)
		}

	case KindSmithing:
		// From the player's things into the first empty input whose property
		// set takes the stack, the rest among themselves; inputs go home.
		// vanilla: ItemCombinerMenu.quickMoveStack
		if smithingAccepts(view, inv, stack) {
			targets = []int32{0, 1, 2}
		} else {
			targets = playerInventorySwap(invBase, slot)
		}

	case KindGrindstone:
		// With an input free, grindable items go to the inputs only, leftovers
		// staying put; with both taken the move runs among the player's rows.
		// Inputs go home.
		// vanilla: GrindstoneMenu.quickMoveStack
		if view.Stack(inv, 0).Empty() || view.Stack(inv, 1).Empty() {
			targets = []int32{0, 1}
		} else {
			targets = playerInventorySwap(invBase, slot)
		}

	case KindCartography:
		// From the player's things: map to the map slot, paper, empty map or
		// glass pane to the addition, the rest among their rows; inputs go
		// home.
		// vanilla: CartographyTableMenu.quickMoveStack
		switch {
		case isCartographyMap(stack):
			targets = []int32{0}
		case isCartographyAddition(stack):
			targets = []int32{1}
		default:
			targets = playerInventorySwap(invBase, slot)
		}

	case KindLoom:
		// From the player's things: banner, dye or pattern to its slot, the
		// rest among their rows; inputs go home.
		// vanilla: LoomMenu.quickMoveStack
		switch {
		case isLoomBanner(stack):
			targets = []int32{0}
		case isLoomDye(stack):
			targets = []int32{1}
		case isLoomPatternItem(stack):
			targets = []int32{2}
		default:
			targets = playerInventorySwap(invBase, slot)
		}

	case KindMerchant:
		// Payments go home forward; main inventory to hotbar and back.
		// vanilla: MerchantMenu.quickMoveStack
		targets = playerInventorySwap(invBase, slot)

	case KindBeacon:
		// One payment item, only into an empty slot; the rest among the
		// player's things.
		// vanilla: BeaconMenu.quickMoveStack
		if view.Stack(inv, 0).Empty() && view.placeable(0, stack) && stack.Count == 1 {
			targets = []int32{0}
		} else {
			targets = playerInventorySwap(invBase, slot)
		}
	}

	finishQuickMove(view, inv, slot, stack, targets, false, outcome)
}

// containerReturnOrder keeps result-specific extraction ahead of ordinary returns.
func containerReturnOrder(kind Kind, slot int32) (reversed, ok bool) {
	switch kind {
	case KindChest, KindBarrel, KindShulker, KindEnderChest, KindDispenser, KindDropper,
		KindHopper, KindDoubleChest, KindMenu, KindCrafter, KindBrewingStand, KindEnchant, KindHorse, KindBeacon:
		return true, true
	case KindFurnace, KindBlastFurnace, KindSmoker:
		return slot == furnaceResultSlot, true
	case KindCrafting, KindAnvil, KindStonecutter, KindSmithing, KindGrindstone,
		KindCartography, KindLoom, KindMerchant:
		return false, true
	}
	return false, false
}

func finishQuickMove(view View, inv *player.Inventory, slot int32, stack protocol.ItemStack, targets []int32, reversed bool, outcome *Outcome) {
	left := moveIntoStacks(view, inv, stack, targets, reversed, outcome)
	setStack(view, inv, slot, left, outcome)
	if left.Count < stack.Count {
		// A shift-click out of the furnace output is the take that pays its
		// stored experience (vanilla: onQuickCraft).
		outcome.tookFurnaceResult(view, slot)
	}
}

// playerSectionSlots lists the window slot numbers of the player's things.
func playerSectionSlots(from, to int32) []int32 {
	slots := make([]int32, 0, to-from)
	for s := from; s < to; s++ {
		slots = append(slots, s)
	}
	return slots
}

// playerInventorySwap is the shift-click within the player's things: main
// inventory to hotbar and back. Several container menus fall through to it.
func playerInventorySwap(invBase, slot int32) []int32 {
	mainEnd := invBase + player.MainInvSlots - player.HotbarSlots
	if slot < mainEnd {
		return playerSectionSlots(mainEnd, invBase+player.MainInvSlots)
	}
	return playerSectionSlots(invBase, mainEnd)
}

// moveIntoStacks moves what it can of stack into targets, onto same-item
// stacks first then empty slots, backwards when filling the player's things;
// each slot stops at slotLimit. It returns what found no place.
func moveIntoStacks(view View, inv *player.Inventory, stack protocol.ItemStack, targets []int32, reversed bool, outcome *Outcome) protocol.ItemStack {
	order := targets
	if reversed {
		order = slices.Clone(targets)
		slices.Reverse(order)
	}
	for _, slot := range order {
		if stack.Count <= 0 {
			break
		}
		inSlot := view.Stack(inv, slot)
		limit := view.slotLimit(slot, stack)
		if inSlot.Empty() || !sameStack(inSlot, stack) ||
			inSlot.Count >= limit || !view.placeable(slot, stack) {
			continue
		}
		n := min(limit-inSlot.Count, stack.Count)
		inSlot.Count += n
		setStack(view, inv, slot, inSlot, outcome)
		stack.Count -= n
	}
	// Empty slots, same direction: vanilla runs both passes one way, so a
	// shift-click out of a chest into an empty inventory lands at the
	// hotbar's far end.
	emptyOrder := order
	for _, slot := range emptyOrder {
		if stack.Count <= 0 {
			break
		}
		if !view.Stack(inv, slot).Empty() || !view.placeable(slot, stack) {
			continue
		}
		in := stack
		in.Count = min(view.slotLimit(slot, stack), stack.Count)
		setStack(view, inv, slot, in, outcome)
		stack.Count -= in.Count
	}
	return stack
}
