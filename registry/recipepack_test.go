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

func itemsOfSet(sets []RecipePropertySet, name string) []int32 {
	for _, s := range sets {
		if s.Name == name {
			return s.Items
		}
	}
	return nil
}

// TestCookingMatchAgreesWithLinearWalk checks the per-item index against a
// first-match walk of each table, built-in and after a pack recipe loads.
func TestCookingMatchAgreesWithLinearWalk(t *testing.T) {
	t.Cleanup(func() { ApplyPackRecipes(PackRecipes{}) })
	kinds := []string{"minecraft:furnace", "minecraft:blast_furnace", "minecraft:smoker", "minecraft:campfire"}
	check := func(label string) {
		for _, kind := range kinds {
			table := cookingTable(kind)
			for item := range int32(ItemCount) {
				var want *CookingRecipe
				for i := range table {
					if slices.Contains(table[i].Ingredient, item) {
						want = &table[i]
						break
					}
				}
				if got := CookingMatch(kind, protocol.ItemStack{ItemID: item, Count: 1}); got != want {
					t.Fatalf("%s: %s item %d matches %v, want %v", label, kind, item, got, want)
				}
			}
		}
	}
	check("built-in")
	stick, planks := stackOf(t, "minecraft:stick"), stackOf(t, "minecraft:oak_planks")
	ApplyPackRecipes(PackRecipes{Cooking: [cookingKinds][]PackCooking{CookingSmelting: {{
		Name: "pk:stick", Ingredient: BookIngredient{Items: []int32{stick.ItemID}}, ResultID: planks.ItemID, ResultCount: 1, Duration: 20}}}})
	check("pack")
	if CookingMatch("minecraft:furnace", stick) == nil {
		t.Error("pack recipe not matched")
	}
}
