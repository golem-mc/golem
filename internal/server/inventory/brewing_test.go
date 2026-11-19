package inventory

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// brewedBottle is the bottle of a potion, the component's bytes built by
// hand so the shape a mix is expected to make does not come from the
// server's own writer.
func brewedBottle(t *testing.T, item, potion string) protocol.ItemStack {
	t.Helper()
	var contents protocol.Buffer
	contents.PutBool(true)
	contents.PutVarInt(registry.MustPotion(potion))
	contents.PutBool(false) // no custom colour
	contents.PutVarInt(0)   // no custom effects
	contents.PutBool(false) // no custom name
	return protocol.ItemStack{
		ItemID: itemID(t, item),
		Count:  1,
		Raw:    []protocol.RawComponent{{Type: protocol.DataComponentPotionContents, Data: contents.Bytes()}},
	}
}

// TestEveryBrewingIngredientIsRecognised is the table of the items the data
// pack's brewing recipes name as a reagent: the ingredient slot takes each of
// them, and the check rejects every other registered item.
// vanilla: RecipePropertySet.BREWING_REAGENTS
func TestEveryBrewingIngredientIsRecognised(t *testing.T) {
	ingredients := []string{
		"minecraft:nether_wart",
		"minecraft:ghast_tear",
		"minecraft:redstone",
		"minecraft:glowstone_dust",
		"minecraft:fermented_spider_eye",
		"minecraft:gunpowder",
		"minecraft:dragon_breath",
		"minecraft:sugar",
		"minecraft:rabbit_foot",
		"minecraft:glistering_melon_slice",
		"minecraft:spider_eye",
		"minecraft:pufferfish",
		"minecraft:magma_cream",
		"minecraft:golden_carrot",
		"minecraft:blaze_powder",
		"minecraft:turtle_helmet",
		"minecraft:phantom_membrane",
		"minecraft:breeze_rod",
		"minecraft:cobweb",
		"minecraft:slime_block",
		"minecraft:stone",
	}
	for id, name := range registry.Items {
		if got, want := isBrewingIngredient(registry.Item(id)), slices.Contains(ingredients, name); got != want {
			t.Errorf("%s brewing ingredient = %v, want %v", name, got, want)
		}
	}
}

// TestBrewingFuelFallsThroughToTheIngredientSlot shift-clicks a blaze powder
// with the fuel slot already full: the vanilla menu passes it to the
// ingredient slot, which also takes it.
// vanilla: BrewingStandMenu.quickMoveStack
func TestBrewingFuelFallsThroughToTheIngredientSlot(t *testing.T) {
	blaze := protocol.ItemStack{ItemID: itemID(t, "minecraft:blaze_powder"), Count: 1}
	container := slots{
		{}, {}, {},
		{},
		{ItemID: blaze.ItemID, Count: 64},
	}
	inv := &player.Inventory{}
	inv.Set(player.MainSlot, blaze)
	var outcome Outcome
	session := &Session{}
	session.Click(View{Kind: KindBrewingStand, ID: 1, Entity: container}, inv,
		protocol.ClickModeQuickMove, 0, 5, false, 0, &outcome)
	if !container[3].Equal(blaze) {
		t.Errorf("ingredient slot = %+v, want the blaze powder from the full fuel slot", container[3])
	}
	if !inv.Slots[player.MainSlot].Empty() {
		t.Errorf("the player's slot = %+v, want it emptied by the move", inv.Slots[player.MainSlot])
	}
}

// TestBrewingFuelRestFallsThroughToTheIngredientSlot shift-clicks more blaze
// powder than the fuel slot has room for: the first pass fills the fuel
// slot, the repeat moves the rest to the ingredient slot.
// vanilla: AbstractContainerMenu.doClick (QUICK_MOVE), BrewingStandMenu.quickMoveStack
func TestBrewingFuelRestFallsThroughToTheIngredientSlot(t *testing.T) {
	blaze := itemID(t, "minecraft:blaze_powder")
	container := slots{
		{}, {}, {},
		{},
		{ItemID: blaze, Count: 60},
	}
	inv := &player.Inventory{}
	inv.Set(player.MainSlot, protocol.ItemStack{ItemID: blaze, Count: 10})
	var outcome Outcome
	(&Session{}).Click(View{Kind: KindBrewingStand, ID: 1, Entity: container}, inv,
		protocol.ClickModeQuickMove, 0, 5, false, 0, &outcome)
	if want := (protocol.ItemStack{ItemID: blaze, Count: 64}); !container[4].Equal(want) {
		t.Errorf("fuel slot = %+v, want 64 blaze powder", container[4])
	}
	if want := (protocol.ItemStack{ItemID: blaze, Count: 6}); !container[3].Equal(want) {
		t.Errorf("ingredient slot = %+v, want the 6 the fuel slot had no room for", container[3])
	}
	if !inv.Slots[player.MainSlot].Empty() {
		t.Errorf("the player's slot = %+v, want it emptied by the move", inv.Slots[player.MainSlot])
	}
}

// TestTheIngredientsAreTheMixesOwn checks the two tables agree: every
// reagent a brewing recipe names is one the ingredient slot takes, and
// nothing the slot takes is missing from the recipes.
// vanilla: RecipeManager's brewing_reagent property set
func TestTheIngredientsAreTheMixesOwn(t *testing.T) {
	named := map[string]bool{}
	for _, recipe := range registry.BrewingRecipes {
		for _, reagent := range recipe.Reagent {
			name, ok := registry.ItemName(reagent)
			if !ok {
				t.Fatal("a brewing recipe names an item the registry does not know")
			}
			named[name] = true
		}
	}
	for id, name := range registry.Items {
		if got, want := isBrewingIngredient(registry.Item(id)), named[name]; got != want {
			t.Errorf("%s ingredient slot = %v, mixes = %v", name, got, want)
		}
	}
}

// TestBrewingMixRunsVanillasMixes brews a handful of the mix lines: each
// named bottle and ingredient make the potion vanilla's own brewing makes,
// and the stand holding them is brewable.
func TestBrewingMixRunsVanillasMixes(t *testing.T) {
	for _, brew := range []struct{ item, potion, ingredient, to string }{
		{"minecraft:potion", "minecraft:water", "minecraft:nether_wart", "minecraft:awkward"},
		{"minecraft:potion", "minecraft:water", "minecraft:sugar", "minecraft:mundane"},
		{"minecraft:potion", "minecraft:water", "minecraft:glowstone_dust", "minecraft:thick"},
		{"minecraft:potion", "minecraft:awkward", "minecraft:sugar", "minecraft:swiftness"},
		{"minecraft:potion", "minecraft:awkward", "minecraft:breeze_rod", "minecraft:wind_charged"},
		{"minecraft:potion", "minecraft:night_vision", "minecraft:redstone", "minecraft:long_night_vision"},
		{"minecraft:potion", "minecraft:healing", "minecraft:fermented_spider_eye", "minecraft:harming"},
		{"minecraft:potion", "minecraft:awkward", "minecraft:phantom_membrane", "minecraft:slow_falling"},
		{"minecraft:potion", "minecraft:water", "minecraft:fermented_spider_eye", "minecraft:weakness"},
		{"minecraft:splash_potion", "minecraft:leaping", "minecraft:fermented_spider_eye", "minecraft:slowness"},
	} {
		bottle := brewedBottle(t, brew.item, brew.potion)
		ingredient := protocol.ItemStack{ItemID: itemID(t, brew.ingredient), Count: 1}
		want := brewedBottle(t, brew.item, brew.to)
		if got := BrewingMix(ingredient, bottle); !got.Equal(want) {
			t.Errorf("mixing %s into a %s of %s gave %+v, want the %s potion",
				brew.ingredient, brew.item, brew.potion, got, brew.to)
		}
		if !Brewable([]protocol.ItemStack{bottle, {}, {}, ingredient}) {
			t.Errorf("a %s bottle and %s are not brewable", brew.potion, brew.ingredient)
		}
	}

	// The two bottle conversions keep the potion inside and change the
	// bottle itself.
	for _, brew := range []struct{ item, ingredient, to, potion string }{
		{"minecraft:potion", "minecraft:gunpowder", "minecraft:splash_potion", "minecraft:water"},
		{"minecraft:splash_potion", "minecraft:dragon_breath", "minecraft:lingering_potion", "minecraft:healing"},
		{"minecraft:potion", "minecraft:gunpowder", "minecraft:splash_potion", "minecraft:strong_healing"},
	} {
		ingredient := protocol.ItemStack{ItemID: itemID(t, brew.ingredient), Count: 1}
		got := BrewingMix(ingredient, brewedBottle(t, brew.item, brew.potion))
		if want := brewedBottle(t, brew.to, brew.potion); !got.Equal(want) {
			t.Errorf("mixing %s into a %s of %s gave %+v, want the %s of %s",
				brew.ingredient, brew.item, brew.potion, got, brew.to, brew.potion)
		}
	}
}

// TestBrewingMixLeavesWhatNoMixFits checks the mixes' gates: a bottle no
// mix starts from comes back untouched and brews nothing.
func TestBrewingMixLeavesWhatNoMixFits(t *testing.T) {
	// The turtle helmet's mixes start from an awkward base: water is none.
	waterBottle := brewedBottle(t, "minecraft:potion", "minecraft:water")
	turtle := protocol.ItemStack{ItemID: itemID(t, "minecraft:turtle_helmet"), Count: 1}
	if got := BrewingMix(turtle, waterBottle); !got.Equal(waterBottle) {
		t.Errorf("mixing a turtle helmet into water gave %+v, want the bottle untouched", got)
	}
	if Brewable([]protocol.ItemStack{waterBottle, {}, {}, turtle}) {
		t.Error("a water bottle and a turtle helmet claim a brew")
	}

	// The glass bottle the slots take is not a container: no mix starts
	// from it, whatever the ingredient.
	glass := protocol.ItemStack{ItemID: itemID(t, "minecraft:glass_bottle"), Count: 1}
	wart := protocol.ItemStack{ItemID: itemID(t, "minecraft:nether_wart"), Count: 1}
	if got := BrewingMix(wart, glass); !got.Equal(glass) {
		t.Errorf("mixing a nether wart into a glass bottle gave %+v, want the bottle untouched", got)
	}
	if Brewable([]protocol.ItemStack{glass, {}, {}, wart}) {
		t.Error("a glass bottle and a nether wart claim a brew")
	}

	// Every recipe names the potion its bottle holds, the bottle conversions
	// too: a potion with no potion named starts none, and neither does the
	// luck potion, which the data pack converts into no other bottle.
	gunpowder := protocol.ItemStack{ItemID: itemID(t, "minecraft:gunpowder"), Count: 1}
	for _, bottle := range []protocol.ItemStack{
		{ItemID: itemID(t, "minecraft:potion"), Count: 1},
		brewedBottle(t, "minecraft:potion", "minecraft:luck"),
	} {
		if Brewable([]protocol.ItemStack{bottle, {}, {}, gunpowder}) {
			t.Errorf("%+v and gunpowder claim a brew", bottle)
		}
		if got := BrewingMix(gunpowder, bottle); !got.Equal(bottle) {
			t.Errorf("mixing gunpowder into %+v gave %+v, want the potion untouched", bottle, got)
		}
	}

	// An empty bottle slot pours nothing, and no ingredient means no brew.
	if got := BrewingMix(wart, protocol.ItemStack{}); !got.Empty() {
		t.Errorf("mixing into an empty slot gave %+v, want it empty", got)
	}
	if Brewable([]protocol.ItemStack{waterBottle, {}, {}}) {
		t.Error("a stand with no ingredient claims a brew")
	}
}
