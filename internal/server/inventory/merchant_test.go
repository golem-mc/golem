package inventory

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

func stackOf(t *testing.T, name string, count int32) protocol.ItemStack {
	t.Helper()
	return protocol.ItemStack{ItemID: itemID(t, name), Count: count}
}

// breadOffer sells 6 bread for 2 emeralds, 4 times.
func breadOffer(t *testing.T) Offer {
	t.Helper()
	return Offer{
		CostA:  Cost{ItemID: itemID(t, "minecraft:emerald"), Count: 2},
		Result: stackOf(t, "minecraft:bread", 6), MaxUses: 4, XP: 2,
	}
}

func merchantView(menu []protocol.ItemStack, offers []Offer, hint int32) View {
	return View{Kind: KindMerchant, Menu: menu, Offers: offers, Hint: hint}
}

// TestOfferPriceFollowsDemandAndSpecialPrice pins the first cost's count:
// demand scales it by the multiplier and floors, the special price adds,
// and it stays between 1 and a stack.
func TestOfferPriceFollowsDemandAndSpecialPrice(t *testing.T) {
	o := Offer{CostA: Cost{ItemID: itemID(t, "minecraft:emerald"), Count: 10}, PriceMultiplier: 0.2, Demand: 3}
	if got := o.PricedCountA(); got != 16 {
		t.Errorf("10 with demand 3 at 0.2 = %d, want 10+6", got)
	}
	o.SpecialPrice = -20
	if got := o.PricedCountA(); got != 1 {
		t.Errorf("discounted below one = %d, want 1", got)
	}
	o.SpecialPrice, o.Demand = 100, 0
	if got := o.PricedCountA(); got != 64 {
		t.Errorf("past a stack = %d, want 64", got)
	}
	o.SpecialPrice, o.Demand = 0, -5
	if got := o.PricedCountA(); got != 10 {
		t.Errorf("negative demand = %d, want the base 10", got)
	}
}

// TestRecomputeMerchantPaysOnlyForAnInStockOffer pins the result: the
// payment, in either slot, must cover the cost; a sold-out offer pays
// nothing; a hint inside the list names the only offer looked at.
func TestRecomputeMerchantPaysOnlyForAnInStockOffer(t *testing.T) {
	offer := breadOffer(t)
	emeralds := func(n int32) protocol.ItemStack { return stackOf(t, "minecraft:emerald", n) }
	for _, tc := range []struct {
		name   string
		a, b   protocol.ItemStack
		offers []Offer
		hint   int32
		want   protocol.ItemStack
	}{
		{"covered", emeralds(2), protocol.ItemStack{}, []Offer{offer}, 0, offer.Result},
		{"short", emeralds(1), protocol.ItemStack{}, []Offer{offer}, 0, protocol.ItemStack{}},
		{"second slot", protocol.ItemStack{}, emeralds(2), []Offer{offer}, 0, offer.Result},
		{"extra b refused", emeralds(2), stackOf(t, "minecraft:dirt", 1), []Offer{offer}, 0, protocol.ItemStack{}},
		{"sold out", emeralds(2), protocol.ItemStack{}, []Offer{{CostA: offer.CostA, Result: offer.Result, MaxUses: 4, Uses: 4}}, 0, protocol.ItemStack{}},
		{"hint names another", emeralds(2), protocol.ItemStack{}, []Offer{offer, offer}, 1, offer.Result},
		{"hint misses", emeralds(2), protocol.ItemStack{}, []Offer{offer, {CostA: Cost{ItemID: itemID(t, "minecraft:paper"), Count: 1}, Result: offer.Result, MaxUses: 1}}, 1, protocol.ItemStack{}},
	} {
		menu := []protocol.ItemStack{tc.a, tc.b, {}}
		RecomputeMerchant(menu, tc.offers, tc.hint)
		if !menu[MerchantResultSlot].Equal(tc.want) {
			t.Errorf("%s: result = %+v, want %+v", tc.name, menu[MerchantResultSlot], tc.want)
		}
	}
}

// TestTheMerchantTakeSpendsTheCostAndCountsTheUse covers the plain click on
// the result: the cost leaves the payment, the use is counted in the shared
// list and the experience reported, and the result stays on offer while
// the payment still covers it.
func TestTheMerchantTakeSpendsTheCostAndCountsTheUse(t *testing.T) {
	offers := []Offer{breadOffer(t)}
	menu := []protocol.ItemStack{stackOf(t, "minecraft:emerald", 5), {}, offers[0].Result}
	view := merchantView(menu, offers, 0)
	session := &Session{}
	var outcome Outcome
	session.Click(view, &player.Inventory{}, protocol.ClickModePickup, 0, MerchantResultSlot, false, 0, &outcome)
	if !session.Carried.Equal(offers[0].Result) {
		t.Errorf("carried = %+v, want the bread", session.Carried)
	}
	if got := menu[0].Count; got != 3 {
		t.Errorf("payment = %d, want 3 emeralds left", got)
	}
	if offers[0].Uses != 1 || len(outcome.MerchantTraded) != 1 || outcome.MerchantTraded[0].XP != 2 {
		t.Errorf("uses = %d, traded = %+v, want 1 use and one trade of 2 xp", offers[0].Uses, outcome.MerchantTraded)
	}
	if !menu[MerchantResultSlot].Equal(offers[0].Result) {
		t.Errorf("result = %+v, want the offer still on", menu[MerchantResultSlot])
	}
	// A cursor holding something else refuses the take: no use is spent.
	session.Carried = stackOf(t, "minecraft:dirt", 1)
	outcome = Outcome{}
	session.Click(view, &player.Inventory{}, protocol.ClickModePickup, 0, MerchantResultSlot, false, 0, &outcome)
	if offers[0].Uses != 1 {
		t.Errorf("a refused take spent a use: %d", offers[0].Uses)
	}
}

// TestShiftClickOnTheMerchantResultTradesUntilItRunsOut pins the repeating
// quick move: every pass moves the offer home and spends a use, until the
// payment no longer covers it or the offer is sold out.
func TestShiftClickOnTheMerchantResultTradesUntilItRunsOut(t *testing.T) {
	offers := []Offer{breadOffer(t)}
	menu := []protocol.ItemStack{stackOf(t, "minecraft:emerald", 7), {}, offers[0].Result}
	view := merchantView(menu, offers, 0)
	inv := &player.Inventory{}
	session := &Session{}
	var outcome Outcome
	session.Click(view, inv, protocol.ClickModeQuickMove, 0, MerchantResultSlot, false, 0, &outcome)
	if offers[0].Uses != 3 {
		t.Errorf("uses = %d, want 3 trades out of 7 emeralds at 2", offers[0].Uses)
	}
	if got := menu[0]; got.Count != 1 {
		t.Errorf("payment = %+v, want 1 emerald left", got)
	}
	if got := inv.Slots[player.HotbarSlot+player.HotbarSlots-1]; got.ItemID != offers[0].Result.ItemID || got.Count != 18 {
		t.Errorf("hotbar end = %+v, want 18 bread", got)
	}
	if !menu[MerchantResultSlot].Empty() {
		t.Errorf("result = %+v, want none for the lone emerald", menu[MerchantResultSlot])
	}
}

// twoOffers pays 10 emeralds for bread and 5 for apples.
func twoOffers(t *testing.T) []Offer {
	t.Helper()
	emerald := itemID(t, "minecraft:emerald")
	return []Offer{
		{CostA: Cost{ItemID: emerald, Count: 10}, Result: stackOf(t, "minecraft:bread", 1), MaxUses: 4},
		{CostA: Cost{ItemID: emerald, Count: 5}, Result: stackOf(t, "minecraft:apple", 1), MaxUses: 4},
	}
}

// TestShiftClickStopsWhenTheResultChangesItem pins the repeat condition: the
// bread trade leaves 5 emeralds, which pay for apples, and the shift-click
// must not take them.
func TestShiftClickStopsWhenTheResultChangesItem(t *testing.T) {
	offers := twoOffers(t)
	menu := []protocol.ItemStack{stackOf(t, "minecraft:emerald", 15), {}, offers[0].Result}
	view := merchantView(menu, offers, -1)
	inv := &player.Inventory{}
	session := &Session{}
	var outcome Outcome
	session.Click(view, inv, protocol.ClickModeQuickMove, 0, MerchantResultSlot, false, 0, &outcome)
	if offers[0].Uses != 1 || offers[1].Uses != 0 {
		t.Errorf("uses = %d and %d, want the bread trade once and no apple trade", offers[0].Uses, offers[1].Uses)
	}
}

// TestCtrlQStopsWhenTheResultChangesItem is the same for the throw loop.
func TestCtrlQStopsWhenTheResultChangesItem(t *testing.T) {
	offers := twoOffers(t)
	menu := []protocol.ItemStack{stackOf(t, "minecraft:emerald", 15), {}, offers[0].Result}
	view := merchantView(menu, offers, -1)
	inv := &player.Inventory{}
	session := &Session{}
	var outcome Outcome
	session.Click(view, inv, protocol.ClickModeThrow, 1, MerchantResultSlot, false, 0, &outcome)
	if offers[0].Uses != 1 || offers[1].Uses != 0 || len(outcome.Dropped) != 1 {
		t.Errorf("uses = %d and %d, dropped %d, want one bread trade", offers[0].Uses, offers[1].Uses, len(outcome.Dropped))
	}
}

// TestShiftClickOnAMerchantPaymentGoesHome pins the quick move out of the
// payment slot and between the player's rows.
func TestShiftClickOnAMerchantPaymentGoesHome(t *testing.T) {
	menu := []protocol.ItemStack{stackOf(t, "minecraft:emerald", 3), {}, {}}
	view := merchantView(menu, []Offer{breadOffer(t)}, 0)
	inv := &player.Inventory{}
	session := &Session{}
	var outcome Outcome
	session.Click(view, inv, protocol.ClickModeQuickMove, 0, 0, false, 0, &outcome)
	if !menu[0].Empty() || inv.Slots[player.MainSlot].Count != 3 {
		t.Errorf("payment %+v, first main %+v, want the emeralds home in the main rows", menu[0], inv.Slots[player.MainSlot])
	}
	// From the main rows to the hotbar.
	session.Click(view, inv, protocol.ClickModeQuickMove, 0, 3, false, 0, &outcome)
	if inv.Slots[player.HotbarSlot].Count != 3 {
		t.Errorf("hotbar = %+v, want the emeralds in its first slot", inv.Slots[player.HotbarSlot])
	}
}

// TestNothingIsPlacedInTheMerchantResult pins the result slot against a
// cursor and a double click.
func TestNothingIsPlacedInTheMerchantResult(t *testing.T) {
	offers := []Offer{breadOffer(t)}
	menu := []protocol.ItemStack{stackOf(t, "minecraft:emerald", 2), {}, offers[0].Result}
	view := merchantView(menu, offers, 0)
	inv := &player.Inventory{}
	inv.Slots[player.MainSlot] = stackOf(t, "minecraft:bread", 3)
	session := &Session{Carried: stackOf(t, "minecraft:bread", 1)}
	var outcome Outcome
	session.Click(view, inv, protocol.ClickModeCollect, 0, 4, false, 0, &outcome)
	if session.Carried.Count != 1 {
		t.Errorf("the double click left %d bread held, want the 1 held: the window gathers nothing", session.Carried.Count)
	}
	if got := inv.Slots[player.MainSlot]; got.Count != 3 {
		t.Errorf("the double click took from the things: %+v", got)
	}
	if offers[0].Uses != 0 || !menu[0].Equal(stackOf(t, "minecraft:emerald", 2)) {
		t.Errorf("a double click took the result: uses %d, payment %+v", offers[0].Uses, menu[0])
	}
	if view.placeable(MerchantResultSlot, stackOf(t, "minecraft:bread", 1)) {
		t.Error("the result takes a placement")
	}
}

// TestSelectTradeMovesTheCostsFromTheThings pins tryMoveItems: the old
// payments go home first, the new costs come out of the things as far as a
// stack, and a second cost fills the second slot.
func TestSelectTradeMovesTheCostsFromTheThings(t *testing.T) {
	offer := Offer{
		CostA:  Cost{ItemID: itemID(t, "minecraft:emerald"), Count: 1},
		CostB:  Cost{ItemID: itemID(t, "minecraft:paper"), Count: 1},
		Result: stackOf(t, "minecraft:bread", 1), MaxUses: 4,
	}
	menu := []protocol.ItemStack{stackOf(t, "minecraft:dirt", 2), {}, {}}
	view := merchantView(menu, []Offer{offer, breadOffer(t)}, 0)
	inv := &player.Inventory{}
	inv.Slots[player.MainSlot] = stackOf(t, "minecraft:emerald", 40)
	inv.Slots[player.MainSlot+1] = stackOf(t, "minecraft:emerald", 40)
	inv.Slots[player.MainSlot+2] = stackOf(t, "minecraft:paper", 5)
	var outcome Outcome
	SelectTrade(view, inv, 0, &outcome)
	if got := menu[0]; got.ItemID != offer.CostA.ItemID || got.Count != 64 {
		t.Errorf("payment 0 = %+v, want a full stack of emeralds", got)
	}
	if got := menu[1]; got.ItemID != offer.CostB.ItemID || got.Count != 5 {
		t.Errorf("payment 1 = %+v, want the 5 paper", got)
	}
	if got := inv.Slots[player.MainSlot+1]; got.Count != 16 {
		t.Errorf("second emerald stack = %+v, want the 16 left over", got)
	}
	var dirt int32
	for _, s := range inv.Slots[player.MainSlot : player.MainSlot+player.MainInvSlots] {
		if s.ItemID == itemID(t, "minecraft:dirt") {
			dirt += s.Count
		}
	}
	if dirt != 2 {
		t.Errorf("the old payment is %d dirt in the things, want 2", dirt)
	}

	// An index past the list moves nothing.
	before := menu[0]
	SelectTrade(view, inv, 5, &outcome)
	if !menu[0].Equal(before) {
		t.Error("Select Trade past the offers moved items")
	}
}

// TestClosingTheMerchantWindowHandsBackThePayments pins the close.
func TestClosingTheMerchantWindowHandsBackThePayments(t *testing.T) {
	menu := []protocol.ItemStack{stackOf(t, "minecraft:emerald", 3), stackOf(t, "minecraft:paper", 1), stackOf(t, "minecraft:bread", 6)}
	view := merchantView(menu, nil, 0)
	inv := &player.Inventory{}
	session := &Session{}
	session.Close(view, inv, false, false)
	if inv.Slots[player.MainSlot].Count+inv.Slots[player.HotbarSlot].Count == 0 {
		t.Error("the payments were not handed back")
	}
	if !menu[0].Empty() || !menu[1].Empty() {
		t.Error("the payment slots still hold their stacks")
	}
}

// vanilla: ItemCost.test
func TestAWaterCostAcceptsOnlyAPlainWaterBottle(t *testing.T) {
	potion := itemID(t, "minecraft:potion")
	water := protocol.ItemStack{ItemID: potion, Count: 1}
	water.SetPotion(waterPotion)
	awkward := protocol.ItemStack{ItemID: potion, Count: 1}
	awkward.SetPotion(registry.MustPotion("minecraft:awkward"))
	offer := Offer{CostA: Cost{ItemID: potion, Count: 1, Water: true}, Result: stackOf(t, "minecraft:emerald", 1), MaxUses: 2}
	for _, c := range []struct {
		name  string
		stack protocol.ItemStack
		want  bool
	}{{"water", water, true}, {"awkward", awkward, false}, {"bare", protocol.ItemStack{ItemID: potion, Count: 1}, false}} {
		if got := offer.satisfiedBy(c.stack, protocol.ItemStack{}); got != c.want {
			t.Errorf("%s bottle pays = %v, want %v", c.name, got, c.want)
		}
	}
}

// vanilla: MerchantMenu.moveFromInventoryToPaymentSlot
func TestSelectTradeSkipsStacksTheCostRejects(t *testing.T) {
	potion := itemID(t, "minecraft:potion")
	healing := protocol.ItemStack{ItemID: potion, Count: 1}
	healing.SetPotion(registry.MustPotion("minecraft:healing"))
	water := protocol.ItemStack{ItemID: potion, Count: 1}
	water.SetPotion(waterPotion)
	offer := Offer{CostA: Cost{ItemID: potion, Count: 1, Water: true}, Result: stackOf(t, "minecraft:emerald", 1), MaxUses: 2}
	menu := []protocol.ItemStack{{}, {}, {}}
	view := merchantView(menu, []Offer{offer}, 0)
	inv := &player.Inventory{}
	inv.Slots[player.MainSlot] = healing
	inv.Slots[player.MainSlot+1] = water
	var outcome Outcome
	SelectTrade(view, inv, 0, &outcome)
	if !menu[0].Equal(water) {
		t.Errorf("payment 0 = %+v, want the water bottle", menu[0])
	}
	if !inv.Slots[player.MainSlot].Equal(healing) {
		t.Errorf("first slot = %+v, want the healing potion left in place", inv.Slots[player.MainSlot])
	}
}

// vanilla: Villager.updateSpecialPrices
func TestHeroDiscountTakesAtLeastOneItem(t *testing.T) {
	offers := []Offer{{CostA: Cost{ItemID: itemID(t, "minecraft:emerald"), Count: 2}}, {CostA: Cost{ItemID: itemID(t, "minecraft:emerald"), Count: 20}}}
	HeroDiscount(offers, 1)
	if offers[0].SpecialPrice != -1 || offers[1].SpecialPrice != -7 {
		t.Errorf("special prices = %d, %d, want -1 and -7", offers[0].SpecialPrice, offers[1].SpecialPrice)
	}
}

// vanilla: Villager.updateSpecialPrices
func TestReputationDiscountScalesByThePriceMultiplier(t *testing.T) {
	offers := []Offer{{CostA: Cost{ItemID: itemID(t, "minecraft:emerald"), Count: 5}, PriceMultiplier: 0.05}}
	ReputationDiscount(offers, 60)
	if offers[0].SpecialPrice != -3 {
		t.Errorf("special price = %d, want -3", offers[0].SpecialPrice)
	}
	ReputationDiscount(offers, -30)
	if offers[0].SpecialPrice != -3+2 {
		t.Errorf("special price after a bad reputation = %d, want -1", offers[0].SpecialPrice)
	}
}
