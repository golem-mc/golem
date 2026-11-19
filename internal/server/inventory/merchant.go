package inventory

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The merchant window's part of a click: Menu[0] and Menu[1] the payments,
// Menu[MerchantResultSlot] the stored result, recomputed by RecomputeMerchant
// after any payment change. Offers is shared with the merchant: a take bumps
// Uses in place.
// vanilla: MerchantMenu, MerchantContainer, MerchantResultSlot

// Cost is an item and count a trade asks for. The only component predicate
// modelled is Water.
// vanilla: ItemCost
type Cost struct {
	ItemID int32
	Count  int32
	// Water requires the stack's potion_contents to be exactly the water
	// potion.
	Water bool
}

var waterPotion = registry.MustPotion("minecraft:water")

// matches reports whether a stack is of the cost's item and components.
// vanilla: ItemCost.test
func (c Cost) matches(s protocol.ItemStack) bool {
	if s.Empty() || s.ItemID != c.ItemID {
		return false
	}
	if !c.Water {
		return true
	}
	contents, ok := s.PotionContentsOf()
	return ok && contents.Potion == waterPotion && contents.CustomColor == nil &&
		len(contents.CustomEffects) == 0 && contents.CustomName == nil
}

// PutPredicate writes the cost's component predicate for the Merchant Offers
// packet.
// vanilla: DataComponentExactPredicate.STREAM_CODEC
func (c Cost) PutPredicate(b *protocol.Buffer) {
	if !c.Water {
		b.PutVarInt(0)
		return
	}
	b.PutVarInt(1)
	b.PutVarInt(protocol.DataComponentPotionContents)
	b.PutPotionContents(protocol.PotionContents{Potion: waterPotion})
}

// Offer is one trade, vanilla's MerchantOffer. A zero CostB.Count means the
// trade has no second cost.
type Offer struct {
	CostA, CostB    Cost
	Result          protocol.ItemStack
	Uses, MaxUses   int32
	XP              int32
	SpecialPrice    int32
	Demand          int32
	PriceMultiplier float32
	// RewardExp is whether a trade drops an experience orb.
	RewardExp bool
}

// HasCostB reports whether the trade asks for a second item.
func (o Offer) HasCostB() bool { return o.CostB.Count > 0 }

// PricedCountA is the first cost's count after demand and the special
// price, clamped to one stack.
// vanilla: MerchantOffer.getModifiedCostCount
func (o Offer) PricedCountA() int32 {
	base := o.CostA.Count
	demand := max(0, int32(math.Floor(float64(float32(base*o.Demand)*o.PriceMultiplier))))
	return min(max(base+demand+o.SpecialPrice, 1), registry.MaxStackSize(o.CostA.ItemID))
}

// ReputationDiscount lowers each offer's price by the villager's reputation
// of the player scaled by the offer's price multiplier.
// vanilla: Villager.updateSpecialPrices
func ReputationDiscount(offers []Offer, reputation int) {
	for i := range offers {
		offers[i].SpecialPrice -= int32(math.Floor(float64(float32(reputation) * offers[i].PriceMultiplier)))
	}
}

// HeroDiscount lowers each offer's price for a player with Hero of the
// Village at the given amplifier, by at least one item.
// vanilla: Villager.updateSpecialPrices
func HeroDiscount(offers []Offer, amplifier int32) {
	modifier := 0.3 + 0.0625*float64(amplifier)
	for i := range offers {
		offers[i].SpecialPrice -= max(int32(math.Floor(modifier*float64(offers[i].CostA.Count))), 1)
	}
}

// OutOfStock reports whether the trade has no uses left.
// vanilla: MerchantOffer.isOutOfStock
func (o Offer) OutOfStock() bool { return o.Uses >= o.MaxUses }

// satisfiedBy reports whether the two payment stacks pay for the trade.
// vanilla: MerchantOffer.satisfiedBy
func (o Offer) satisfiedBy(a, b protocol.ItemStack) bool {
	if !o.CostA.matches(a) || a.Count < o.PricedCountA() {
		return false
	}
	if o.HasCostB() {
		return o.CostB.matches(b) && b.Count >= o.CostB.Count
	}
	return b.Empty()
}

// take spends the trade's cost out of the two stacks, reporting whether
// they paid for it.
// vanilla: MerchantOffer.take
func (o Offer) take(a, b *protocol.ItemStack) bool {
	if !o.satisfiedBy(*a, *b) {
		return false
	}
	a.Count -= o.PricedCountA()
	if o.HasCostB() {
		b.Count -= o.CostB.Count
	}
	return true
}

// recipeFor is the index of the offer the payment buys, -1 for none. An
// in-range hint is the only offer checked; hint 0 is no hint.
// vanilla: MerchantOffers.getRecipeFor
func recipeFor(offers []Offer, a, b protocol.ItemStack, hint int32) int {
	if hint > 0 && int(hint) < len(offers) {
		if offers[hint].satisfiedBy(a, b) {
			return int(hint)
		}
		return -1
	}
	for i := range offers {
		if offers[i].satisfiedBy(a, b) {
			return i
		}
	}
	return -1
}

// activeOffer is the offer the payments buy, slots as costs A and B or
// swapped when the first does not pay; ok is false when none is in stock.
// vanilla: MerchantContainer.updateSellItem
func activeOffer(menu []protocol.ItemStack, offers []Offer, hint int32) (index int, ok bool) {
	a, b := menu[0], menu[1]
	if a.Empty() {
		a, b = menu[1], protocol.ItemStack{}
	}
	if a.Empty() {
		return -1, false
	}
	if i := recipeFor(offers, a, b, hint); i >= 0 && !offers[i].OutOfStock() {
		return i, true
	}
	if i := recipeFor(offers, b, a, hint); i >= 0 && !offers[i].OutOfStock() {
		return i, true
	}
	return -1, false
}

// RecomputeMerchant recomputes the result from the payments, reporting
// whether it changed; with no offers the slot is left alone.
// vanilla: MerchantContainer.updateSellItem
func RecomputeMerchant(menu []protocol.ItemStack, offers []Offer, hint int32) (stored bool) {
	var result protocol.ItemStack
	if menu[0].Empty() && menu[1].Empty() {
		// no payment, no result
	} else if len(offers) == 0 {
		return false
	} else if i, ok := activeOffer(menu, offers, hint); ok {
		result = offers[i].Result
	}
	if result.Equal(menu[MerchantResultSlot]) {
		return false
	}
	menu[MerchantResultSlot] = result
	return true
}

// merchantTake spends the cost, counts the use, reports its experience and
// recomputes.
// vanilla: MerchantResultSlot.onTake
func merchantTake(view View, inv *player.Inventory, outcome *Outcome) {
	i, ok := activeOffer(view.Menu, view.Offers, view.Hint)
	// vanilla: MerchantResultSlot.checkTakeAchievements, ItemStack.onCraftedBy
	if result := view.Menu[MerchantResultSlot]; !result.Empty() {
		outcome.Crafted = append(outcome.Crafted, result)
	}
	view.Menu[MerchantResultSlot] = protocol.ItemStack{}
	outcome.touched(MerchantResultSlot)
	if ok {
		offer := &view.Offers[i]
		a, b := view.Menu[0], view.Menu[1]
		if offer.take(&a, &b) || offer.take(&b, &a) {
			offer.Uses++
			outcome.MerchantTraded = append(outcome.MerchantTraded, *offer)
			setStack(view, inv, 0, nonEmpty(a), outcome)
			setStack(view, inv, 1, nonEmpty(b), outcome)
		}
	}
	outcome.MerchantUpdated = true
	RecomputeMerchant(view.Menu, view.Offers, view.Hint)
}

func nonEmpty(s protocol.ItemStack) protocol.ItemStack {
	if s.Empty() {
		return protocol.ItemStack{}
	}
	return s
}

// merchantFromResult is mode 0: the offer onto an empty cursor or the same
// item with room for all, either button.
// vanilla: AbstractContainerMenu.doClick PICKUP, Slot.tryRemove
func merchantFromResult(s *Session, view View, inv *player.Inventory, outcome *Outcome) {
	stored := view.Menu[MerchantResultSlot]
	if !takeResult(s, stored, stored) {
		return
	}
	merchantTake(view, inv, outcome)
}

// shiftMerchantFromResult is mode 1: the offer moves into the player's
// things, hotbar end first, repeating while the result is the same item
// (components aside) and some moved. A remainder is lost to the recompute.
// vanilla: MerchantMenu.quickMoveStack, AbstractContainerMenu.doClick QUICK_MOVE
func shiftMerchantFromResult(view View, inv *player.Inventory, outcome *Outcome) {
	base := view.Kind.Layout().InvBase
	targets := playerSectionSlots(base, base+player.MainInvSlots)
	for {
		result := view.Menu[MerchantResultSlot]
		if result.Empty() {
			return
		}
		left := moveIntoStacks(view, inv, result, targets, true, outcome)
		if left.Count == result.Count {
			return
		}
		merchantTake(view, inv, outcome)
		if view.Menu[MerchantResultSlot].ItemID != result.ItemID {
			return
		}
	}
}

// merchantSwap is mode 2: the offer into an empty hotbar slot, then the
// take.
// vanilla: AbstractContainerMenu.doClick SWAP
func merchantSwap(view View, inv *player.Inventory, button int32, outcome *Outcome) {
	invSlot, ok := swapButtonSlot(button)
	if !ok {
		return
	}
	stored := view.Menu[MerchantResultSlot]
	if stored.Empty() || !inv.Slots[invSlot].Empty() {
		return
	}
	inv.Set(invSlot, stored)
	outcome.touchedInv(view, invSlot)
	if player.VisibleSlot(inv, invSlot) {
		outcome.Visible = true
	}
	merchantTake(view, inv, outcome)
}

// merchantThrow is mode 4: the whole offer drops whatever Q asks, then the
// take; Ctrl+Q repeats while the result is the same item.
// vanilla: MerchantContainer.removeItem, AbstractContainerMenu.doClick THROW,
// ItemStack.isSameItem
func merchantThrow(view View, inv *player.Inventory, button int32, outcome *Outcome) {
	for {
		stored := view.Menu[MerchantResultSlot]
		if stored.Empty() {
			return
		}
		merchantTake(view, inv, outcome)
		outcome.throw(stored)
		if button == 0 || view.Menu[MerchantResultSlot].ItemID != stored.ItemID {
			return
		}
	}
}

// SelectTrade handles Select Trade: sets the hint, empties the payments into
// the player's things and, if both clear, fills them with the offer's costs.
// vanilla: MerchantMenu.setSelectionHint, MerchantMenu.tryMoveItems
func SelectTrade(view View, inv *player.Inventory, index int32, outcome *Outcome) {
	if index < 0 || int(index) >= len(view.Offers) {
		return
	}
	base := view.Kind.Layout().InvBase
	targets := playerSectionSlots(base, base+player.MainInvSlots)
	for slot := range int32(2) {
		old := view.Stack(inv, slot)
		if old.Empty() {
			continue
		}
		left := moveIntoStacks(view, inv, old, targets, true, outcome)
		if left.Count == old.Count {
			return
		}
		setStack(view, inv, slot, nonEmpty(left), outcome)
	}
	if !view.Stack(inv, 0).Empty() || !view.Stack(inv, 1).Empty() {
		return
	}
	offer := view.Offers[index]
	moveToPayment(view, inv, 0, offer.CostA, outcome)
	if offer.HasCostB() {
		moveToPayment(view, inv, 1, offer.CostB, outcome)
	}
}

// moveToPayment fills a payment slot with the cost's item from the player's
// things, up to a stack.
// vanilla: MerchantMenu.moveFromInventoryToPaymentSlot
func moveToPayment(view View, inv *player.Inventory, payment int32, cost Cost, outcome *Outcome) {
	base := view.Kind.Layout().InvBase
	for slot := base; slot < base+player.MainInvSlots; slot++ {
		have := view.Stack(inv, slot)
		if !cost.matches(have) {
			continue
		}
		current := view.Stack(inv, payment)
		if !current.Empty() && !sameStack(have, current) {
			continue
		}
		limit := stackMax(have)
		moved := min(limit-current.Count, have.Count)
		paid := have
		paid.Count = current.Count + moved
		have.Count -= moved
		setStack(view, inv, slot, nonEmpty(have), outcome)
		setStack(view, inv, payment, paid, outcome)
		if paid.Count >= limit {
			break
		}
	}
}
