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

// TestTheSmithingTakeRefusesTheCursorItCannotJoin checks the takes that
// move nothing: a cursor holding another item, and one holding the
// result's own without room for the whole offer.
func TestTheSmithingTakeRefusesTheCursorItCannotJoin(t *testing.T) {
	template := protocol.ItemStack{ItemID: itemID(t, "minecraft:netherite_upgrade_smithing_template"), Count: 1}
	pickaxe := protocol.ItemStack{ItemID: itemID(t, "minecraft:diamond_pickaxe"), Count: 1}
	ingot := protocol.ItemStack{ItemID: itemID(t, "minecraft:netherite_ingot"), Count: 1}
	netherite := protocol.ItemStack{ItemID: itemID(t, "minecraft:netherite_pickaxe"), Count: 1}
	dirt := protocol.ItemStack{ItemID: itemID(t, "minecraft:dirt"), Count: 1}

	for _, held := range []struct {
		name  string
		stack protocol.ItemStack
	}{
		{"another item", dirt},
		{"the result without room",
			protocol.ItemStack{ItemID: itemID(t, "minecraft:netherite_pickaxe"), Count: 64}},
	} {
		view := smithingView(template, pickaxe, ingot, netherite)
		session := &Session{Carried: held.stack}
		var outcome Outcome
		session.Click(view, &player.Inventory{}, protocol.ClickModePickup, 0, SmithingResultSlot,
			false, 0, &outcome)
		if got := session.Carried; !got.Equal(held.stack) {
			t.Errorf("carried = %+v, want the cursor untouched (%s)", got, held.name)
		}
		if got := view.Menu[0]; !got.Equal(template) {
			t.Errorf("the template = %+v, want it unspent (%s)", got, held.name)
		}
		if got := view.Menu[SmithingResultSlot]; !got.Equal(netherite) {
			t.Errorf("the result = %+v, want the offer still whole (%s)", got, held.name)
		}
		if outcome.SmithingTaken {
			t.Errorf("a refused take was marked taken (%s)", held.name)
		}
	}
}

// TestAPlainClickOnTheSmithingResultTakesItWhole checks the plain take's
// button: the result container's removeItem ignores the amount a right
// click asks for, so both buttons hand the whole result over, one of each
// input spent behind it.
// vanilla: AbstractContainerMenu.doClick PICKUP, ResultContainer.removeItem
func TestAPlainClickOnTheSmithingResultTakesItWhole(t *testing.T) {
	template := protocol.ItemStack{ItemID: itemID(t, "minecraft:netherite_upgrade_smithing_template"), Count: 1}
	pickaxe := protocol.ItemStack{ItemID: itemID(t, "minecraft:diamond_pickaxe"), Count: 1}
	ingot := protocol.ItemStack{ItemID: itemID(t, "minecraft:netherite_ingot"), Count: 1}
	// A two-count result is no recipe's make; the stored truth is set
	// straight so the click's own rule is what the take is read against.
	two := protocol.ItemStack{ItemID: itemID(t, "minecraft:netherite_pickaxe"), Count: 2}

	for _, button := range []int32{0, 1} {
		view := smithingView(template, pickaxe, ingot, two)
		session := &Session{}
		var outcome Outcome
		session.Click(view, &player.Inventory{}, protocol.ClickModePickup, button, SmithingResultSlot, false, 0, &outcome)
		if got := session.Carried; !got.Equal(two) {
			t.Errorf("button %d carried = %+v, want both pickaxes", button, got)
		}
		for slot := range int32(3) {
			if !view.Menu[slot].Empty() {
				t.Errorf("button %d input %d = %+v, want it spent", button, slot, view.Menu[slot])
			}
		}
		if !outcome.SmithingTaken {
			t.Errorf("button %d was not marked, want its use event paid for", button)
		}
	}
}

// TestRecomputeSmithingStoresOnlyWhatMoved checks the recompute: a
// completed transform stores once, the same result again stays quiet, and
// an emptied input empties the result.
func TestRecomputeSmithingStoresOnlyWhatMoved(t *testing.T) {
	template := protocol.ItemStack{ItemID: itemID(t, "minecraft:netherite_upgrade_smithing_template"), Count: 1}
	pickaxe := protocol.ItemStack{ItemID: itemID(t, "minecraft:diamond_pickaxe"), Count: 1}
	ingot := protocol.ItemStack{ItemID: itemID(t, "minecraft:netherite_ingot"), Count: 1}
	netherite := protocol.ItemStack{ItemID: itemID(t, "minecraft:netherite_pickaxe"), Count: 1}

	menu := []protocol.ItemStack{template, pickaxe, ingot, protocol.ItemStack{}}
	if !RecomputeSmithing(menu) {
		t.Error("the completed inputs stored nothing, want the result")
	}
	if got := menu[SmithingResultSlot]; !got.Equal(netherite) {
		t.Errorf("the result = %+v, want the upgrade", got)
	}
	if RecomputeSmithing(menu) {
		t.Error("the same inputs stored again, want the quiet answer")
	}
	menu[1] = protocol.ItemStack{}
	if !RecomputeSmithing(menu) {
		t.Error("the emptied base stored nothing, want the result gone")
	}
	if got := menu[SmithingResultSlot]; !got.Empty() {
		t.Errorf("the result = %+v, want nothing without a base", got)
	}
}

// TestTheSmithingInputsTakeOnlyTheirKinds checks the property sets the three
// input slots take by: the template slot a trim template and nothing
// dirtier, the base the smithing bases, the addition the smithing
// additions, and the player's things behind them anything.
func TestTheSmithingInputsTakeOnlyTheirKinds(t *testing.T) {
	view := View{Kind: KindSmithing}
	dirt := protocol.ItemStack{ItemID: itemID(t, "minecraft:dirt"), Count: 1}
	template := protocol.ItemStack{ItemID: itemID(t, "minecraft:netherite_upgrade_smithing_template"), Count: 1}
	pickaxe := protocol.ItemStack{ItemID: itemID(t, "minecraft:diamond_pickaxe"), Count: 1}
	ingot := protocol.ItemStack{ItemID: itemID(t, "minecraft:netherite_ingot"), Count: 1}

	for _, check := range []struct {
		slot     int32
		stack    protocol.ItemStack
		accepted bool
	}{
		{0, template, true},
		{0, dirt, false},
		{0, pickaxe, false},
		{1, pickaxe, true},
		{1, template, false},
		{2, ingot, true},
		{2, pickaxe, false},
		{3, netheriteStack(t), false}, // the result takes nothing
		{smithingHotbarSlot, dirt, true},
	} {
		if got := view.placeable(check.slot, check.stack); got != check.accepted {
			t.Errorf("placeable(%d, %d) = %t, want %t",
				check.slot, check.stack.ItemID, got, check.accepted)
		}
	}
}

// TestSmithingAcceptsRoutesTheShiftClick checks the shift-click's routing
// question: a stack whose kind an empty input takes moves in, one whose
// slots are all taken - or whose kind no input wants - moves among the
// player's own things instead.
func TestSmithingAcceptsRoutesTheShiftClick(t *testing.T) {
	template := protocol.ItemStack{ItemID: itemID(t, "minecraft:netherite_upgrade_smithing_template"), Count: 1}
	pickaxe := protocol.ItemStack{ItemID: itemID(t, "minecraft:diamond_pickaxe"), Count: 1}
	ingot := protocol.ItemStack{ItemID: itemID(t, "minecraft:netherite_ingot"), Count: 1}
	dirt := protocol.ItemStack{ItemID: itemID(t, "minecraft:dirt"), Count: 1}

	empty := smithingView(protocol.ItemStack{}, protocol.ItemStack{}, protocol.ItemStack{},
		protocol.ItemStack{})
	filled := smithingView(template, pickaxe, ingot, protocol.ItemStack{})

	if !smithingAccepts(empty, &player.Inventory{}, pickaxe) {
		t.Error("a pickaxe the empty base slot has room for was refused")
	}
	for _, check := range []struct {
		view  View
		stack protocol.ItemStack
		want  bool
	}{
		{empty, template, true},
		{empty, dirt, false},
		{filled, template, false},
		{filled, ingot, false},
		{filled, pickaxe, false},
	} {
		if got := smithingAccepts(check.view, &player.Inventory{}, check.stack); got != check.want {
			t.Errorf("smithingAccepts = %t, want %t (%d into the laid window)",
				got, check.want, check.stack.ItemID)
		}
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
