package containers

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The smithing table's tests: the window its block opens, the two recipe
// kinds its three inputs run - the netherite upgrades of smithing_transform,
// the armour trims of smithing_trim - and the take that spends one of each
// input. The recipes themselves are the generated tables' own answer (see
// registry/recipes_gen.go): the netherite upgrade takes the upgrade template,
// a diamond tool and a netherite ingot, and the sentry trim's pattern the
// trims test recounts from the table.

// The smithing window's own slots and the player's hotbar in its numbering:
// the template at 0, the base at 1, the addition at 2, the result at 3, the
// player's things from 4.
const (
	smithTemplateSlot = int16(0)
	smithBaseSlot     = int16(1)
	smithAdditionSlot = int16(2)
	smithResultSlot   = int16(3)
	smithHotbar0      = int16(4 + (player.HotbarSlot - player.MainSlot))
)

// smithingErrorProp reads where the error property stands and whether the
// window said so since the last look.
func smithingErrorProp(w *servertest.WindowedClient) (value int32, told bool) {
	w.T.Helper()
	props := servertest.CollectWindowProps(w)
	value, told = props[server.SmithingRecipeErrorProperty]
	return value, told
}

// TestTheSmithingTableUpgradesAnEnchantedPickaxe lays the netherite
// upgrade's three inputs - the upgrade template, an enchanted and worn
// diamond pickaxe, a netherite ingot - and takes the result: the netherite
// pickaxe wearing every component the base's patch had, its enchantments
// and its wear among them, and the take spends one of each input and
// sounds the smithing use event at the table.
func TestTheSmithingTableUpgradesAnEnchantedPickaxe(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

	if slots := w.OpenSmithing(srv); len(slots) != 40 {
		t.Fatalf("the smithing window has %d slots, want its 4 and the player's 36", len(slots))
	}
	// The window opens saying nothing is wrong, and nothing it does in
	// this test moves that: a recipe takes the inputs as soon as the
	// last of them lands.
	if got, told := smithingErrorProp(w); !told || got != 0 {
		t.Fatalf("the error property opened at %d (told %t), want a stated 0", got, told)
	}
	servertest.ForgetWindowProps(w)

	template := servertest.ItemStack(t, "minecraft:netherite_upgrade_smithing_template", 1)
	w.GiveAndLay(srv, template, smithTemplateSlot)
	pickaxe := servertest.ItemStack(t, "minecraft:diamond_pickaxe", 1)
	pickaxe.Damage = 1561
	pickaxe.Enchantments = map[int32]int32{servertest.SharpnessID(t): 5}
	w.GiveAndLay(srv, pickaxe, smithBaseSlot)
	// Two of the three laid, and the error property stays quiet: the
	// slots are not full, so there is nothing to explain.
	if _, told := smithingErrorProp(w); told {
		t.Error("an incomplete table was told of a recipe error")
	}

	ingot := servertest.ItemStack(t, "minecraft:netherite_ingot", 1)
	srv.GiveSlot("Testificate", player.HotbarSlot, ingot)
	w.Drain()
	w.Click(smithHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{smithHotbar0: {}}, ingot)
	// The ingot landing completes the transform: the result slot is
	// corrected with the upgraded pickaxe, the base's own components on
	// it, and the error property stays at the 0 it opened with.
	w.Click(smithAdditionSlot, 0, protocol.ClickModePickup)
	netherite := servertest.ItemStack(t, "minecraft:netherite_pickaxe", 1)
	netherite.Damage = pickaxe.Damage
	netherite.Enchantments = pickaxe.Enchantments
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		smithAdditionSlot: ingot,
		smithResultSlot:   netherite,
	}, protocol.ItemStack{})
	if _, told := smithingErrorProp(w); told {
		t.Error("a table whose recipe takes its inputs was told of an error")
	}

	// The take: the pickaxe onto the empty cursor, the inputs emptied
	// behind it, the result with nothing left to make, and the use
	// event at the table.
	w.Click(smithResultSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		smithTemplateSlot: {},
		smithBaseSlot:     {},
		smithAdditionSlot: {},
		smithResultSlot:   {},
	}, netherite)
	if !slices.Contains(w.Events, protocol.WorldEventSmithingUse) {
		t.Errorf("no smithing use event played; the events were %v", w.Events)
	}
}

// TestTheSmithingWindowExplainsACombinationNoRecipeTakes lays three inputs
// whose kinds each slot takes but none of the smithing recipes wants - the
// trim of a tool is no recipe, the trim template does not upgrade - and
// checks the window say so on its error property, and stop saying it once an
// input leaves.
func TestTheSmithingWindowExplainsACombinationNoRecipeTakes(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

	w.OpenSmithing(srv)
	servertest.ForgetWindowProps(w)

	template := servertest.ItemStack(t, "minecraft:sentry_armor_trim_smithing_template", 1)
	w.GiveAndLay(srv, template, smithTemplateSlot)
	// A diamond pickaxe is a base the slots take, though only the
	// upgrades and the trims of trimmable armour are recipes. The last
	// input completing them turns the error on.
	pickaxe := servertest.ItemStack(t, "minecraft:diamond_pickaxe", 1)
	w.GiveAndLay(srv, pickaxe, smithBaseSlot)
	ingot := servertest.ItemStack(t, "minecraft:iron_ingot", 1)
	srv.GiveSlot("Testificate", player.HotbarSlot, ingot)
	w.Drain()
	w.Click(smithHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{smithHotbar0: {}}, ingot)
	w.Click(smithAdditionSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{smithAdditionSlot: ingot}, protocol.ItemStack{})
	if got, told := smithingErrorProp(w); !told || got != 1 {
		t.Fatalf("the error property = %d (told %t), want an explained 1", got, told)
	}
	servertest.ForgetWindowProps(w)

	// The pickaxe back off the base: not all three laid, the explained
	// error leaves with it, and the pair that remains is worth no result
	// the slot ever shows.
	w.Click(smithBaseSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{smithBaseSlot: {}}, pickaxe)
	if got, told := smithingErrorProp(w); !told || got != 0 {
		t.Fatalf("the error property = %d (told %t), want an evened 0", got, told)
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

// TestTheSmithingTableTrimsArmour lays the sentry trim's three inputs - its
// template, a diamond helmet, an iron ingot - and takes the helmet wearing
// the trim the recipe makes of them: the pattern the recipe names and the
// material the addition provides. Laying the same trim again answers
// nothing, and one of each input goes with the take.
func TestTheSmithingTableTrimsArmour(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

	w.OpenSmithing(srv)
	servertest.ForgetWindowProps(w)

	template := servertest.ItemStack(t, "minecraft:sentry_armor_trim_smithing_template", 1)
	w.GiveAndLay(srv, template, smithTemplateSlot)
	helmet := servertest.ItemStack(t, "minecraft:diamond_helmet", 1)
	w.GiveAndLay(srv, helmet, smithBaseSlot)
	ingot := servertest.ItemStack(t, "minecraft:iron_ingot", 1)
	srv.GiveSlot("Testificate", player.HotbarSlot, ingot)
	w.Drain()
	w.Click(smithHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{smithHotbar0: {}}, ingot)
	w.Click(smithAdditionSlot, 0, protocol.ClickModePickup)

	// The trimmed helmet: the base itself wearing the trim, its
	// material the addition provides and its pattern the sentry
	// recipe's own.
	material, ok := registry.TrimMaterials[servertest.ItemID(t, "minecraft:iron_ingot")]
	if !ok {
		t.Fatal("the generated trim materials have none for iron")
	}
	trimmed := helmet
	trimmed.SetTrim(material, trimPatternOf(t, "minecraft:sentry_armor_trim_smithing_template"))
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		smithAdditionSlot: ingot,
		smithResultSlot:   trimmed,
	}, protocol.ItemStack{})

	// The take spends one of each input, the result with nothing left
	// to make, and the trimmed helmet goes home on the cursor.
	w.Click(smithResultSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		smithTemplateSlot: {},
		smithBaseSlot:     {},
		smithAdditionSlot: {},
		smithResultSlot:   {},
	}, trimmed)
	// vanilla: ServerPlayer.triggerRecipeCrafted
	if !srv.AdvancementObtained("Testificate", "minecraft:adventure/trim_with_any_armor_pattern",
		"armor_trimmed_minecraft:sentry_armor_trim_smithing_template_smithing_trim") {
		t.Error("taking a sentry trim did not fire recipe_crafted for it")
	}
	w.Click(-999, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{}, protocol.ItemStack{})
	w.ExpectDrop(trimmed)

	// The same trim again: the inputs all laid, the result holds
	// nothing for them, and the window explains that.
	w.GiveAndLay(srv, template, smithTemplateSlot)
	w.GiveAndLay(srv, trimmed, smithBaseSlot)
	w.GiveAndLay(srv, ingot, smithAdditionSlot)
	if got, told := smithingErrorProp(w); !told || got != 1 {
		t.Fatalf("the error property = %d (told %t), want the same trim explained", got, told)
	}
	servertest.ForgetWindowProps(w)

	// Another material retrims the helmet the same recipe names the
	// pattern of: copper over iron, the trim replaced, and the addition
	// slot's iron traded back onto the cursor. The expectation is built
	// off the plain helmet again, so the stacks the server holds are
	// never the ones a later SetTrim would rewrite in place.
	copper := servertest.ItemStack(t, "minecraft:copper_ingot", 1)
	srv.GiveSlot("Testificate", player.HotbarSlot, copper)
	w.Drain()
	w.Click(smithHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{smithHotbar0: {}}, copper)
	copperMaterial, ok := registry.TrimMaterials[servertest.ItemID(t, "minecraft:copper_ingot")]
	if !ok {
		t.Fatal("the generated trim materials have none for copper")
	}
	retrimmed := helmet
	retrimmed.SetTrim(copperMaterial, trimPatternOf(t, "minecraft:sentry_armor_trim_smithing_template"))
	w.Click(smithAdditionSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		smithAdditionSlot: copper,
		smithResultSlot:   retrimmed,
	}, ingot)
}

// TestTheSmithingInputsTakeOnlyTheirOwnKinds lays the cursor's stack on the
// input slots whose recipes never ask for it and the addition slot a base
// will not fit: nothing moves, the cursor keeps its stack, and the base slot
// the pickaxe does belong in takes it.
func TestTheSmithingInputsTakeOnlyTheirOwnKinds(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

	w.OpenSmithing(srv)

	dirt := servertest.ItemStack(t, "minecraft:dirt", 2)
	srv.GiveSlot("Testificate", player.HotbarSlot, dirt)
	w.Drain()
	w.Click(smithHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{smithHotbar0: {}}, dirt)
	for _, slot := range []int16{smithTemplateSlot, smithBaseSlot, smithAdditionSlot} {
		w.Click(slot, 0, protocol.ClickModePickup)
		w.ExpectClickResult(map[int16]protocol.ItemStack{}, dirt)
	}

	// A pickaxe belongs in the base slot alone.
	pickaxe := servertest.ItemStack(t, "minecraft:diamond_pickaxe", 1)
	w.Click(-999, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{}, protocol.ItemStack{})
	w.ExpectDrop(dirt)
	srv.GiveSlot("Testificate", player.HotbarSlot, pickaxe)
	w.Drain()
	w.Click(smithHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{smithHotbar0: {}}, pickaxe)
	w.Click(smithAdditionSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{}, pickaxe)
	w.Click(smithBaseSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{smithBaseSlot: pickaxe}, protocol.ItemStack{})
}

// TestTheSmithingResultMovesByTheOtherTakes lays the upgrade's inputs and
// takes the result the three other ways: the shift-click into the player's
// things, the Q onto the ground, and the number-key swap into a hotbar slot,
// each spending one of each input.
func TestTheSmithingResultMovesByTheOtherTakes(t *testing.T) {
	newWindow := func(t *testing.T) (*server.Server, *servertest.WindowedClient, protocol.ItemStack) {
		t.Helper()
		srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())
		w.OpenSmithing(srv)
		w.GiveAndLay(srv, servertest.ItemStack(t, "minecraft:netherite_upgrade_smithing_template", 1), smithTemplateSlot)
		pickaxe := servertest.ItemStack(t, "minecraft:diamond_pickaxe", 1)
		w.GiveAndLay(srv, pickaxe, smithBaseSlot)
		w.GiveAndLay(srv, servertest.ItemStack(t, "minecraft:netherite_ingot", 1), smithAdditionSlot)
		return srv, w, servertest.ItemStack(t, "minecraft:netherite_pickaxe", 1)
	}

	t.Run("the shift-click", func(t *testing.T) {
		t.Parallel()
		_, w, netherite := newWindow(t)

		w.Click(smithResultSlot, 0, protocol.ClickModeQuickMove)
		w.ExpectClickResult(map[int16]protocol.ItemStack{
			smithTemplateSlot: {},
			smithBaseSlot:     {},
			smithAdditionSlot: {},
			smithResultSlot:   {},
			smithHotbar0 + 8:  netherite,
		}, protocol.ItemStack{})
	})

	t.Run("the Q", func(t *testing.T) {
		t.Parallel()
		_, w, netherite := newWindow(t)

		w.Click(smithResultSlot, 0, protocol.ClickModeThrow)
		w.ExpectClickResult(map[int16]protocol.ItemStack{
			smithTemplateSlot: {},
			smithBaseSlot:     {},
			smithAdditionSlot: {},
			smithResultSlot:   {},
		}, protocol.ItemStack{})
		w.ExpectDrop(netherite)
	})

	t.Run("the number-key swap", func(t *testing.T) {
		t.Parallel()
		_, w, netherite := newWindow(t)

		w.Click(smithResultSlot, 5, protocol.ClickModeSwap)
		w.ExpectClickResult(map[int16]protocol.ItemStack{
			smithTemplateSlot: {},
			smithBaseSlot:     {},
			smithAdditionSlot: {},
			smithResultSlot:   {},
			smithHotbar0 + 5:  netherite,
		}, protocol.ItemStack{})
	})
}

// TestClosingTheSmithingWindowHandsItsInputsBack closes the window with
// the template still laid, and checks the stack go back to the player's
// things, the take never taken.
func TestClosingTheSmithingWindowHandsItsInputsBack(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

	w.OpenSmithing(srv)
	template := servertest.ItemStack(t, "minecraft:netherite_upgrade_smithing_template", 2)
	w.GiveAndLay(srv, template, smithTemplateSlot)

	w.CloseWindow()
	w.ExpectWindowZero()
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Equal(template) {
		t.Errorf("after close, the hotbar holds %+v, want the %v the inputs handed back", got, template)
	}
}

// TestTheSmithingTableTrimsWithAnIngotsOwnProvidedMaterial trims with a gold
// ingot whose own provides_trim_material names diamond.
// vanilla: SmithingTrimRecipe.applyTrim
func TestTheSmithingTableTrimsWithAnIngotsOwnProvidedMaterial(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())
	w.OpenSmithing(srv)
	servertest.ForgetWindowProps(w)

	template := servertest.ItemStack(t, "minecraft:coast_armor_trim_smithing_template", 1)
	w.GiveAndLay(srv, template, smithTemplateSlot)
	chestplate := servertest.ItemStack(t, "minecraft:iron_chestplate", 1)
	w.GiveAndLay(srv, chestplate, smithBaseSlot)
	ingot := storedStack(t, "minecraft:gold_ingot",
		nbt.Compound{"minecraft:provides_trim_material": nbt.String("minecraft:diamond")})
	srv.GiveSlot("Testificate", player.HotbarSlot, ingot)
	w.Drain()
	w.Click(smithHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{smithHotbar0: {}}, ingot)
	w.Click(smithAdditionSlot, 0, protocol.ClickModePickup)

	diamond, ok := registry.Get("minecraft:trim_material")
	if !ok {
		t.Fatal("no trim_material registry")
	}
	trimmed := chestplate
	trimmed.SetTrim(diamond.ID("minecraft:diamond"), trimPatternOf(t, "minecraft:coast_armor_trim_smithing_template"))
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		smithAdditionSlot: ingot,
		smithResultSlot:   trimmed,
	}, protocol.ItemStack{})
}
