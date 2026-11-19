package inventory

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The smithing window over the given template, base, addition and result
// slot's truth, laid out the way a click leaves them.
func smithingView(template, base, addition, result protocol.ItemStack) View {
	return View{Kind: KindSmithing,
		Menu: []protocol.ItemStack{template, base, addition, result}}
}

// smithingHotbarSlot is where the hotbar sits in the smithing window's
// numbering: the template, base, addition and result ahead of the player's
// things.
const smithingHotbarSlot = SmithingResultSlot + 1 + player.HotbarSlot - player.MainSlot

// TestTheSmithingTakeSpendsOneOfEachInput checks the take: one template,
// one base and one addition go, and the result turns to what the remaining
// inputs are worth - here nothing, an emptied base.
func TestTheSmithingTakeSpendsOneOfEachInput(t *testing.T) {
	template := protocol.ItemStack{ItemID: itemID(t, "minecraft:netherite_upgrade_smithing_template"), Count: 2}
	pickaxe := protocol.ItemStack{ItemID: itemID(t, "minecraft:diamond_pickaxe"), Count: 1}
	ingot := protocol.ItemStack{ItemID: itemID(t, "minecraft:netherite_ingot"), Count: 2}
	netherite := protocol.ItemStack{ItemID: itemID(t, "minecraft:netherite_pickaxe"), Count: 1}

	// The take: the netherite pickaxe onto the empty cursor, one of each
	// input spent, and the result with no base worth anything.
	view := smithingView(template, pickaxe, ingot, netherite)
	session := &Session{}
	var outcome Outcome
	session.Click(view, &player.Inventory{}, protocol.ClickModePickup, 0, SmithingResultSlot,
		false, 0, &outcome)
	if got := session.Carried; !got.Equal(netherite) {
		t.Errorf("carried = %+v, want the result's stack", got)
	}
	templateOne := template
	templateOne.Count = 1
	ingotOne := ingot
	ingotOne.Count = 1
	if got := view.Menu[0]; !got.Equal(templateOne) {
		t.Errorf("the template = %+v, want one template of two", got)
	}
	if got := view.Menu[1]; !got.Empty() {
		t.Errorf("the base = %+v, want it spent", got)
	}
	if got := view.Menu[2]; !got.Equal(ingotOne) {
		t.Errorf("the addition = %+v, want one ingot of two", got)
	}
	if got := view.Menu[SmithingResultSlot]; !got.Empty() {
		t.Errorf("the result = %+v, want nothing left to make", got)
	}
	if !outcome.SmithingTaken {
		t.Error("the take was not marked, want its use event paid for")
	}
	if !containsAll(outcome.Announce, 0, 1, 2, SmithingResultSlot) {
		t.Errorf("announced %v, want the three inputs and the emptied result", outcome.Announce)
	}
}

// TestRecomputeSmithingRetrimsWithoutTouchingTheInputs lays an already
// trimmed base and the other trim's addition, and checks the recompute
// write its answer into the result slot alone: the ingredient stays byte
// for byte what the player laid, however much of it the result was built
// from.
func TestRecomputeSmithingRetrimsWithoutTouchingTheInputs(t *testing.T) {
	base := protocol.ItemStack{ItemID: itemID(t, "minecraft:diamond_helmet"), Count: 1}
	ironMaterial, ok := registry.TrimMaterials[itemID(t, "minecraft:iron_ingot")]
	if !ok {
		t.Fatal("the generated trim materials have none for iron")
	}
	copperMaterial, ok := registry.TrimMaterials[itemID(t, "minecraft:copper_ingot")]
	if !ok {
		t.Fatal("the generated trim materials have none for copper")
	}
	base.SetTrim(ironMaterial, trimPatternOf(t, "minecraft:sentry_armor_trim_smithing_template"))

	menu := []protocol.ItemStack{
		{ItemID: itemID(t, "minecraft:sentry_armor_trim_smithing_template"), Count: 1},
		base,
		{ItemID: itemID(t, "minecraft:copper_ingot"), Count: 1},
		{},
	}
	if !RecomputeSmithing(menu) {
		t.Fatal("the retrim stored nothing, want the helmet with the copper trim")
	}
	if got := menu[1]; !got.Equal(base) {
		t.Errorf("the base = %+v, want the iron trim it was laid with", got)
	}
	if got := menu[3]; got.ItemID != base.ItemID || got.Count != 1 {
		t.Errorf("the retrim = %+v, want the helmet with the copper trim", got)
	}
	if m, p, ok := menu[3].TrimOf(); !ok || m != copperMaterial ||
		p != trimPatternOf(t, "minecraft:sentry_armor_trim_smithing_template") {
		t.Errorf("the retrim wears %d/%d (ok %t), want copper and the sentry pattern",
			m, p, ok)
	}
	// The base's own trim is still what the window laid.
	if m, _, ok := menu[1].TrimOf(); !ok || m != ironMaterial {
		t.Errorf("the base's trim became %d (ok %t), want the iron it stays", m, ok)
	}
}

// trimPatternOf looks the pattern the named armour trim's recipe gives its
// base, by the template item the recipe asks for.
func trimPatternOf(t *testing.T, template string) int32 {
	t.Helper()
	for _, recipe := range registry.SmithingTrimRecipes {
		if name, ok := registry.ItemName(recipe.Template[0]); ok && name == template {
			return recipe.Pattern
		}
	}
	t.Fatalf("the generated trim recipes have none for %s", template)
	return -1
}

// netheriteStack is a fresh netherite pickaxe, for the result slot the
// placeable check must refuse.
func netheriteStack(t *testing.T) protocol.ItemStack {
	t.Helper()
	return protocol.ItemStack{ItemID: itemID(t, "minecraft:netherite_pickaxe"), Count: 1}
}

// containsAll reports whether the list holds every one of the slots.
func containsAll(list []int32, slots ...int32) bool {
	for _, slot := range slots {
		var found bool
		if slices.Contains(list, slot) {
			found = true
		}
		if !found {
			return false
		}
	}
	return true
}
