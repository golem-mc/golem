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

// TestTheSmithingTableKeepsTheBasesRemovedDefaults upgrades a diamond
// pickaxe whose patch removes max_damage: the netherite pickaxe in the result
// slot and the one taken carry the removal on the wire and in what a save
// writes.
func TestTheSmithingTableKeepsTheBasesRemovedDefaults(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	srv, w := servertest.OpenWindowed(t, cfg)
	w.OpenSmithing(srv)
	servertest.ForgetWindowProps(w)

	template := servertest.ItemStack(t, "minecraft:netherite_upgrade_smithing_template", 1)
	w.GiveAndLay(srv, template, smithTemplateSlot)
	pickaxe := servertest.ItemStack(t, "minecraft:diamond_pickaxe", 1)
	if !pickaxe.Remove(protocol.DataComponentMaxDamage) {
		t.Fatal("max_damage is not a removable component")
	}
	w.GiveAndLay(srv, pickaxe, smithBaseSlot)
	ingot := servertest.ItemStack(t, "minecraft:netherite_ingot", 1)
	srv.GiveSlot("Testificate", player.HotbarSlot, ingot)
	w.Drain()
	w.Click(smithHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{smithHotbar0: {}}, ingot)
	w.Click(smithAdditionSlot, 0, protocol.ClickModePickup)
	netherite := servertest.ItemStack(t, "minecraft:netherite_pickaxe", 1)
	netherite.Remove(protocol.DataComponentMaxDamage)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		smithAdditionSlot: ingot,
		smithResultSlot:   netherite,
	}, protocol.ItemStack{})

	// Take the upgrade into the inventory and save: the file keeps the removal.
	w.Click(smithResultSlot, 0, protocol.ClickModePickup)
	w.Drain()
	w.Click(smithHotbar0, 0, protocol.ClickModePickup)
	w.Drain()
	w.Disconnect()
	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Testificate"))
	items, _ := nbt.Get[nbt.List](root, "Inventory")
	found := false
	for _, item := range items.Items {
		entry := item.(nbt.Compound)
		if id, _ := nbt.Get[nbt.String](entry, "id"); id != "minecraft:netherite_pickaxe" {
			continue
		}
		found = true
		components, _ := nbt.Get[nbt.Compound](entry, "components")
		if _, ok := components["!minecraft:max_damage"]; !ok {
			t.Errorf("saved netherite pickaxe components = %v, want !minecraft:max_damage", components)
		}
	}
	if !found {
		t.Errorf("no netherite pickaxe in the saved inventory %v", items)
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
