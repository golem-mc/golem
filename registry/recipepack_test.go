package registry

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

func stackOf(t *testing.T, name string) protocol.ItemStack {
	t.Helper()
	id, ok := ItemID(name)
	if !ok {
		t.Fatalf("no item %s", name)
	}
	return protocol.ItemStack{ItemID: id, Count: 1}
}

// shapelessOf is a one-ingredient shapeless pack recipe.
func shapelessOf(t *testing.T, name, ingredient, result string, count int32) PackCrafting {
	t.Helper()
	in, out := stackOf(t, ingredient), stackOf(t, result)
	return PackCrafting{
		Name: name, Squares: []BookIngredient{{Items: []int32{in.ItemID}}},
		Items: []Ingredient{{in.ItemID}}, ResultID: out.ItemID, ResultCount: count, Notify: true}
}

// TestPropertySetsFollowTheRecipes checks that deriving the recipe book's
// item groups from the built-in recipes gives the generated table, which is
// what lets a pack's recipes change them.
func TestPropertySetsFollowTheRecipes(t *testing.T) {
	got := propertySetsOf(builtinRecipes)
	if len(got) != len(RecipePropertySets) {
		t.Fatalf("derived %d sets, want %d", len(got), len(RecipePropertySets))
	}
	for i, want := range RecipePropertySets {
		if got[i].Name != want.Name || !slices.Equal(got[i].Items, want.Items) {
			t.Errorf("set %s derived as %v, want %v", want.Name, got[i].Items, want.Items)
		}
	}
}

// TestPackRecipesReplaceBuiltInOnesByID checks that a pack recipe takes the
// place of the built-in one of its ID, whatever the kinds, keeps that
// recipe's display ID, and leaves the rest alone; and
// that an empty set restores the built-ins.
func TestPackRecipesReplaceBuiltInOnesByID(t *testing.T) {
	t.Cleanup(func() { ApplyPackRecipes(PackRecipes{}) })
	oak, planks := stackOf(t, "minecraft:oak_log"), stackOf(t, "minecraft:oak_planks")
	if got := CraftResult([]protocol.ItemStack{oak, {}, {}, {}}, 2); got.ItemID != planks.ItemID || got.Count != 4 {
		t.Fatalf("a log crafts %+v before the pack, want 4 planks", got)
	}
	old := CraftingDisplayOfName("minecraft:oak_planks")
	oldID := old.ID

	clay, stick := stackOf(t, "minecraft:clay_ball"), stackOf(t, "minecraft:stick")
	replacePlanks := shapelessOf(t, "minecraft:oak_planks", "minecraft:clay_ball", "minecraft:oak_planks", 7)
	ApplyPackRecipes(PackRecipes{
		Crafting: []PackCrafting{replacePlanks, shapelessOf(t, "pk:b", "minecraft:brick", "minecraft:stick", 1), shapelessOf(t, "pk:a", "minecraft:coal", "minecraft:stick", 1)},
		Cooking: [cookingKinds][]PackCooking{CookingSmelting: {{
			Name: "minecraft:iron_ingot_from_smelting_raw_iron", Ingredient: BookIngredient{Items: []int32{stick.ItemID}}, ResultID: planks.ItemID, ResultCount: 1, Duration: 20}}},
	})

	if got := CraftResult([]protocol.ItemStack{oak, {}, {}, {}}, 2); !got.Empty() {
		t.Errorf("a log crafts %+v after a pack replaced the planks recipe, want nothing", got)
	}
	if got := CraftResult([]protocol.ItemStack{clay, {}, {}, {}}, 2); got.ItemID != planks.ItemID || got.Count != 7 {
		t.Errorf("clay crafts %+v, want 7 planks", got)
	}
	if got := CraftResult([]protocol.ItemStack{stackOf(t, "minecraft:birch_log"), {}, {}, {}}, 2); got.Count != 4 {
		t.Errorf("a birch log crafts %+v, want the built-in 4 planks", got)
	}
	d := CraftingDisplayOfName("minecraft:oak_planks")
	if d.ID != oldID || d.ResultCount != 7 {
		t.Errorf("replaced display = id %d x%d, want id %d x7", d.ID, d.ResultCount, oldID)
	}
	a, b := CraftingDisplayOfName("pk:a"), CraftingDisplayOfName("pk:b")
	// One ID space spans every recipe kind's display.
	everyKind := int32(BuiltinRecipeDisplayCount)
	if a == nil || b == nil || a.ID != everyKind || b.ID != everyKind+1 {
		t.Fatalf("new displays = %+v and %+v, want IDs %d and %d, past every built-in display", a, b, everyKind, everyKind+1)
	}
	if CraftingDisplayOf(a.ID) != a || len(CraftingDisplaysNow()) != len(CraftingDisplays)+2 {
		t.Errorf("display lookup or count wrong: %d displays", len(CraftingDisplaysNow()))
	}
	if got := SmeltingMatch(stackOf(t, "minecraft:raw_iron")); got != nil {
		t.Errorf("raw iron smelts under %+v after a pack replaced its recipe, want nothing", got)
	}
	if got := SmeltingResult(stick); got.ItemID != planks.ItemID {
		t.Errorf("a stick smelts into %+v, want planks", got)
	}
	if !slices.Contains(itemsOfSet(RecipePropertySetsNow(), "minecraft:furnace_input"), stick.ItemID) ||
		slices.Contains(itemsOfSet(RecipePropertySetsNow(), "minecraft:furnace_input"), stackOf(t, "minecraft:raw_iron").ItemID) {
		t.Error("the furnace input set does not follow the pack's smelting recipe")
	}

	ApplyPackRecipes(PackRecipes{})
	if got := CraftResult([]protocol.ItemStack{oak, {}, {}, {}}, 2); got.Count != 4 {
		t.Errorf("a log crafts %+v after the pack is gone, want 4 planks", got)
	}
	if CraftingDisplayOfName("pk:a") != nil {
		t.Error("a pack display outlived its pack")
	}
}

// TestPackStonecuttingKeepsTheListSorted checks that the stonecutter list
// stays in alphabetical order of recipe ID with the pack's recipes in it, a
// replaced one gone.
func TestPackStonecuttingKeepsTheListSorted(t *testing.T) {
	t.Cleanup(func() { ApplyPackRecipes(PackRecipes{}) })
	stick := stackOf(t, "minecraft:stick")
	ApplyPackRecipes(PackRecipes{Stonecutting: []StonecutterRecipe{
		{ID: "pk:z", Input: stick.ItemID, ResultID: stick.ItemID, ResultCount: 2},
		{ID: "minecraft:andesite_slab_from_andesite_stonecutting", Input: stick.ItemID, ResultID: stick.ItemID, ResultCount: 3},
	}})
	cuts := StonecutterRecipesNow()
	if len(cuts) != len(StonecutterRecipes)+1 {
		t.Fatalf("%d stonecutter recipes, want %d", len(cuts), len(StonecutterRecipes)+1)
	}
	if !slices.IsSortedFunc(cuts, func(a, b StonecutterRecipe) int { return compareID(a.ID, b.ID) }) {
		t.Error("the stonecutter list is not in order of recipe ID")
	}
	if cuts[0].ResultCount != 3 || cuts[len(cuts)-1].ID != "pk:z" {
		t.Errorf("list runs from %+v to %+v, want the replaced recipe first and pk:z last", cuts[0], cuts[len(cuts)-1])
	}
}

// TestPackRecipesTryInPathThenNamespaceOrder checks that where several
// recipes take the same grid the first by path, then namespace, wins, pack or
// built-in alike.
// vanilla: Identifier.compareTo
func TestPackRecipesTryInPathThenNamespaceOrder(t *testing.T) {
	t.Cleanup(func() { ApplyPackRecipes(PackRecipes{}) })
	grid := func(item string) []protocol.ItemStack { return []protocol.ItemStack{stackOf(t, item), {}, {}, {}} }
	ApplyPackRecipes(PackRecipes{Crafting: []PackCrafting{
		// Path sorts after the built-in oak_planks.
		shapelessOf(t, "pk:zzz_logs", "minecraft:oak_log", "minecraft:diamond", 1),
		shapelessOf(t, "minecraft:zzz_dirt", "minecraft:dirt", "minecraft:emerald", 1),
		shapelessOf(t, "pk:sortfirst", "minecraft:dirt", "minecraft:diamond", 1),
		// The same path in two namespaces goes by namespace.
		shapelessOf(t, "pk:tie", "minecraft:gravel", "minecraft:emerald", 1),
		shapelessOf(t, "aa:tie", "minecraft:gravel", "minecraft:diamond", 1),
	}})
	diamond := stackOf(t, "minecraft:diamond").ItemID
	if got := CraftResult(grid("minecraft:oak_log"), 2); got.Count != 4 {
		t.Errorf("an oak log crafts %+v, want the built-in 4 planks to come first", got)
	}
	for _, item := range []string{"minecraft:dirt", "minecraft:gravel"} {
		if got := CraftResult(grid(item), 2); got.ItemID != diamond {
			t.Errorf("%s crafts %+v, want the diamond recipe that sorts first", item, got)
		}
	}
}

// TestDroppedPackFilesRemoveTheBuiltInRecipe checks that a pack file that
// does not decode takes the built-in recipe of its ID away.
// vanilla: RecipeManager.prepare
func TestDroppedPackFilesRemoveTheBuiltInRecipe(t *testing.T) {
	t.Cleanup(func() { ApplyPackRecipes(PackRecipes{}) })
	ApplyPackRecipes(PackRecipes{Dropped: []string{"minecraft:oak_planks"}})
	oak, birch := stackOf(t, "minecraft:oak_log"), stackOf(t, "minecraft:birch_log")
	if got := CraftResult([]protocol.ItemStack{oak, {}, {}, {}}, 2); !got.Empty() {
		t.Errorf("an oak log crafts %+v after its recipe file broke, want nothing", got)
	}
	if got := CraftResult([]protocol.ItemStack{birch, {}, {}, {}}, 2); got.Count != 4 {
		t.Errorf("a birch log crafts %+v, want the built-in 4 planks", got)
	}
	if CraftingDisplayOfName("minecraft:oak_planks") != nil {
		t.Error("the dropped recipe still has a book display")
	}
}

func itemsOfSet(sets []RecipePropertySet, name string) []int32 {
	for _, s := range sets {
		if s.Name == name {
			return s.Items
		}
	}
	return nil
}

func TestPackWorkstationDisplaysShareIDsAndMergeInputs(t *testing.T) {
	t.Cleanup(func() { ApplyPackRecipes(PackRecipes{}) })
	cut := "minecraft:andesite_slab_from_andesite_stonecutting"
	trim := "minecraft:sentry_armor_trim_smithing_template_smithing_trim"
	cutID, trimID := StonecutterDisplayOfName(cut).ID, SmithingDisplayOfName(trim).ID
	ApplyPackRecipes(PackRecipes{
		Stonecutting: []StonecutterRecipe{
			{ID: cut, Input: int32(ItemStone), ResultID: int32(ItemStick), ResultCount: 2},
			{ID: cut, Input: int32(ItemAndesite), ResultID: int32(ItemStick), ResultCount: 2},
		},
		SmithingTransform: []SmithingTransformRecipe{{ID: "pk:b", Base: Ingredient{int32(ItemStone)}, ResultID: int32(ItemDiamond), ResultCount: 1}},
		SmithingTrim:      []SmithingTrimRecipe{{ID: "pk:a", Template: Ingredient{int32(ItemSentryArmorTrimSmithingTemplate)}, Base: Ingredient{int32(ItemDiamondHelmet)}, Addition: Ingredient{int32(ItemIronIngot)}, Pattern: 8}},
		Crafting:          []PackCrafting{shapelessOf(t, trim, "minecraft:stone", "minecraft:stick", 1)},
	})
	if d := StonecutterDisplayOfName(cut); d == nil || d.ID != cutID || d.ResultCount != 2 ||
		!slices.Equal(d.Ingredient.Items, []int32{int32(ItemStone), int32(ItemAndesite)}) {
		t.Errorf("merged stonecutting display = %+v", d)
	}
	if len(StonecutterDisplaysNow()) != len(StonecutterDisplays) {
		t.Error("expanded ingredient created duplicate displays")
	}
	if SmithingDisplayOfName(trim) != nil || CraftingDisplayOfName(trim).ID != trimID {
		t.Error("cross-kind replacement lost its ID or kept the old display")
	}
	for i, name := range []string{"pk:a", "pk:b"} {
		d := SmithingDisplayOfName(name)
		if d == nil || d.ID != int32(BuiltinRecipeDisplayCount+i) || d.Trim != (i == 0) {
			t.Fatalf("new smithing display = %+v", d)
		}
		if id, ok := RecipeDisplayIDOfName(name); !ok || id != d.ID || SmithingDisplayOf(id) != d {
			t.Error("smithing display lookup lost its ID")
		}
	}
	if StonecutterDisplayOf(cutID).Name != cut {
		t.Error("stonecutter display lookup lost its name")
	}
	ApplyPackRecipes(PackRecipes{Dropped: []string{cut, trim}})
	if StonecutterDisplayOfName(cut) != nil || SmithingDisplayOfName(trim) != nil {
		t.Error("dropped recipe retained its book display")
	}
}

// vanilla: RecipeManager.prepare, Identifier.compareTo
func TestPackBrewingRecipesKeepVanillaOrder(t *testing.T) {
	t.Cleanup(func() { ApplyPackRecipes(PackRecipes{}) })
	water, sugar := stackOf(t, "minecraft:potion"), stackOf(t, "minecraft:sugar")
	water.SetPotion(MustPotion("minecraft:water"))
	added := BrewingRecipe{ID: "pk:brewing/sugar_water", Input: Ingredient{water.ItemID}, Potion: MustPotion("minecraft:water"),
		Reagent: Ingredient{sugar.ItemID}, ResultID: water.ItemID, ResultPotion: MustPotion("minecraft:swiftness")}
	ApplyPackRecipes(PackRecipes{Brewing: []BrewingRecipe{added}})
	if r := BrewingMatch(water, sugar); r == nil || r.ID != "minecraft:brewing/potion_water_sugar" {
		t.Fatalf("competing recipe = %+v, want the earlier vanilla recipe", r)
	}
	brick, stick := stackOf(t, "minecraft:brick"), stackOf(t, "minecraft:stick")
	replacement := BrewingRecipe{ID: "minecraft:brewing/potion_water_sugar", Input: Ingredient{brick.ItemID}, Potion: -1,
		Reagent: Ingredient{stick.ItemID}, ResultID: int32(ItemGlassBottle), ResultPotion: -1}
	ApplyPackRecipes(PackRecipes{Brewing: []BrewingRecipe{added, replacement}})
	if r := BrewingMatch(water, sugar); r == nil || r.ID != added.ID {
		t.Errorf("new mix = %+v, want %s after replacing its competitor", r, added.ID)
	}
	for _, potion := range []int32{-1, MustPotion("minecraft:water"), MustPotion("minecraft:awkward")} {
		input := brick
		if potion >= 0 {
			input.SetPotion(potion)
		}
		if r := BrewingMatch(input, stick); r == nil || r.ID != replacement.ID {
			t.Errorf("input potion %d matches %+v, want wildcard replacement", potion, r)
		}
	}
	if !ItemInPropertySet("minecraft:brewing_input", brick.ItemID) || !ItemInPropertySet("minecraft:brewing_reagent", stick.ItemID) {
		t.Error("pack input and reagent missing from property sets")
	}
	if BrewingMatch(protocol.ItemStack{}, stick) != nil || BrewingMatch(brick, protocol.ItemStack{}) != nil || BrewingMatch(brick, sugar) != nil {
		t.Error("an empty or wrong ingredient matches a pack recipe")
	}
	ApplyPackRecipes(PackRecipes{})
	if BrewingMatch(brick, stick) != nil || ItemInPropertySet("minecraft:brewing_input", brick.ItemID) || ItemInPropertySet("minecraft:brewing_reagent", stick.ItemID) {
		t.Error("pack recipe or property sets survived its removal")
	}
}
