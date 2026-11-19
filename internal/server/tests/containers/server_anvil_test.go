package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The anvil result's shift-click and double click, the two paths that must
// not spend the anvil when the result goes nowhere or the cursor is not its
// stack. vanilla: ItemCombinerMenu.quickMoveStack, AbstractContainerMenu
// doClick PICKUP_ALL.

// TestAShiftClickOutOfAFullAnvilSpendsNothing renames a sword and shift-clicks
// the result with every slot of the player's things full: the quick-move
// answers nothing, so the take behind it must not run and the inputs, the
// result and the levels must all stay.
func TestAShiftClickOutOfAFullAnvilSpendsNothing(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

	w.OpenAnvil(srv)
	srv.SetExperienceLevels("Testificate", 10)

	// A sword onto the first input, renamed: one level of work.
	sword := servertest.ItemStack(t, "minecraft:diamond_sword", 1)
	srv.GiveSlot("Testificate", player.HotbarSlot, sword)
	w.Drain()
	w.Click(30, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{30: {}}, sword)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: sword}, protocol.ItemStack{})
	renamed := sword
	name := protocol.Text{Text: "Sharp Sword"}
	renamed.CustomName = &name
	w.RenameInAnvil("Sharp Sword", renamed, 1)

	// Every slot of the player's things filled, so the result has nowhere
	// to go.
	stone := servertest.ItemStack(t, "minecraft:stone", 64)
	for slot := player.MainSlot; slot < player.MainSlot+player.MainInvSlots; slot++ {
		srv.GiveSlot("Testificate", slot, stone)
	}
	w.Drain()

	// The shift-click that moves nothing: the cursor is restated whole and
	// the levels and the cost are left as they were.
	w.Click(2, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{}, protocol.ItemStack{})
	if got := srv.ExperienceLevels("Testificate"); got != 10 {
		t.Errorf("levels after the full shift = %d, want 10", got)
	}
	if got := w.Props[server.AnvilCostProperty]; got != 1 {
		t.Errorf("the cost after the full shift = %d, want 1", got)
	}

	// The input and the result are still there: the plain click on the
	// result still finds the renamed sword to take.
	w.Click(2, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: {}, 1: {}, 2: {}}, renamed)
	if got := srv.ExperienceLevels("Testificate"); got != 9 {
		t.Errorf("levels after the take = %d, want 9", got)
	}
}

// TestADoubleClickOnAnEmptyAnvilSlotGathersTheResultIntoAMatchingCursor
// double-clicks an empty player slot with the cursor already holding the
// result's own renamed dirt: the sweep reaches the result, the whole answer
// comes off onto the cursor and the take behind it clears the inputs and
// pays.
func TestADoubleClickOnAnEmptyAnvilSlotGathersTheResultIntoAMatchingCursor(t *testing.T) {
	t.Parallel()
	srv, w := anvilWithNamedCursor(t, 5)

	w.Click(3, 0, protocol.ClickModeCollect)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: {}, 1: {}, 2: {}}, namedDirt(t, "Foo", 6))
	if got := srv.ExperienceLevels("Testificate"); got != 8 {
		t.Errorf("levels after the gather = %d, want 8", got)
	}
	if got := w.Props[server.AnvilCostProperty]; got != 0 {
		t.Errorf("the cost after the gather = %d, want 0", got)
	}
}

// TestADoubleClickOnAnEmptyAnvilSlotLeavesAnUnrelatedCursorAlone gathers
// with a cursor of another item than the result: canItemQuickReplace needs
// the same item and components, so the result must stay and no take may be
// paid for.
func TestADoubleClickOnAnEmptyAnvilSlotLeavesAnUnrelatedCursorAlone(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

	w.OpenAnvil(srv)
	srv.SetExperienceLevels("Testificate", 10)

	// One dirt onto the first input, renamed, so the window offers the
	// named dirt as its result.
	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:dirt", 1))
	w.Drain()
	w.Click(30, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{30: {}}, servertest.ItemStack(t, "minecraft:dirt", 1))
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: servertest.ItemStack(t, "minecraft:dirt", 1)}, protocol.ItemStack{})
	w.RenameInAnvil("Foo", namedDirt(t, "Foo", 1), 1)

	// A stone onto the cursor: an item of its own, unrelated to the
	// result.
	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:stone", 1))
	w.Drain()
	w.Click(30, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{30: {}}, servertest.ItemStack(t, "minecraft:stone", 1))

	// A double click on an empty player slot: the cursor's stone is no
	// match for the result, so nothing moves and the level is not spent.
	w.Click(3, 0, protocol.ClickModeCollect)
	w.ExpectClickResult(map[int16]protocol.ItemStack{}, servertest.ItemStack(t, "minecraft:stone", 1))
	if got := srv.ExperienceLevels("Testificate"); got != 10 {
		t.Errorf("levels after the gather = %d, want 10", got)
	}
	if got := w.Props[server.AnvilCostProperty]; got != 1 {
		t.Errorf("the cost after the gather = %d, want 1", got)
	}
}

// TestABrokenAnvilWakesTheSandOnIt takes a rename out of a damaged anvil on a
// roll that breaks it: the sand resting on the anvil must start to fall.
// vanilla: AnvilMenu.onTake, Level.removeBlock
func TestABrokenAnvilWakesTheSandOnIt(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

	w.OpenAnvil(srv)
	srv.SetExperienceLevels("Testificate", 10)
	damaged, ok := registry.FindState("minecraft:damaged_anvil", nil)
	if !ok {
		t.Fatal("the registry has no damaged anvil")
	}
	sand, ok := registry.FindState("minecraft:sand", nil)
	if !ok {
		t.Fatal("the registry has no sand")
	}
	servertest.WaitFor(t, srv, 1200, func() bool { return srv.SetBlock(4, 64, 0, damaged) })
	servertest.WaitFor(t, srv, 1200, func() bool { return srv.SetBlock(4, 65, 0, sand) })
	w.Drain()

	sword := servertest.ItemStack(t, "minecraft:diamond_sword", 1)
	srv.GiveSlot("Testificate", player.HotbarSlot, sword)
	w.Drain()
	w.Click(30, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{30: {}}, sword)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: sword}, protocol.ItemStack{})
	renamed := sword
	name := protocol.Text{Text: "Sharp Sword"}
	renamed.CustomName = &name
	w.RenameInAnvil("Sharp Sword", renamed, 1)

	// A seed whose first roll wears the anvil (below 12%).
	seed := int64(0)
	for rand.New(rand.NewSource(seed)).Float64() >= 0.12 {
		seed++
	}
	srv.SetLevelSeed(seed)
	w.Click(2, 0, protocol.ClickModePickup)
	w.AwaitFallingSpawn()
}

// TestAnAnvilTreatsANonBreakingSpaceNameAsBlank names an unnamed sword with
// a lone no-break space, which StringUtil.isBlank counts as blank: nothing
// to do, so no result and no cost. A lone U+0085 is neither Java whitespace
// nor a space separator, so it names the sword for a level.
// vanilla: AnvilMenu.createResult, StringUtil.isWhitespace
func TestAnAnvilTreatsANonBreakingSpaceNameAsBlank(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

	w.OpenAnvil(srv)
	srv.SetExperienceLevels("Testificate", 10)

	sword := servertest.ItemStack(t, "minecraft:diamond_sword", 1)
	srv.GiveSlot("Testificate", player.HotbarSlot, sword)
	w.Drain()
	w.Click(30, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{30: {}}, sword)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: sword}, protocol.ItemStack{})

	w.RenameInAnvil(" ", protocol.ItemStack{}, 0)

	named := sword
	name := protocol.Text{Text: "\u0085"}
	named.CustomName = &name
	w.RenameInAnvil("\u0085", named, 1)
}

// TestAnAnvilRepairCostSaturates mends a sword whose prior work is already
// the int32 maximum: the result's repair cost stays at the maximum rather
// than wrapping to -1. Creative, so the price is no bar to the result.
// vanilla: AnvilMenu.calculateIncreasedRepairCost
func TestAnAnvilRepairCostSaturates(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.GameMode = "creative"
	srv, w := servertest.OpenWindowed(t, cfg)

	w.OpenAnvil(srv)

	sword := servertest.ItemStack(t, "minecraft:diamond_sword", 1)
	sword.Damage = 780
	sword.RepairCost = math.MaxInt32
	srv.GiveSlot("Testificate", player.HotbarSlot, sword)
	w.Drain()
	w.Click(30, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{30: {}}, sword)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: sword}, protocol.ItemStack{})

	diamond := servertest.ItemStack(t, "minecraft:diamond", 1)
	srv.GiveSlot("Testificate", player.HotbarSlot, diamond)
	w.Drain()
	w.Click(30, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{30: {}}, diamond)
	w.Click(1, 0, protocol.ClickModePickup)
	mended := sword
	mended.Damage = 390
	w.ExpectClickResult(map[int16]protocol.ItemStack{1: diamond, 2: mended}, protocol.ItemStack{})
}

// TestAnAnvilMendsWithAnEnchantedMaterialAndLeavesItsMagic puts a worn
// diamond sword and a diamond carrying Sharpness V in the anvil: the
// material mends the sword and its magic stays off the result.
// vanilla: AnvilMenu.createResult
func TestAnAnvilMendsWithAnEnchantedMaterialAndLeavesItsMagic(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

	w.OpenAnvil(srv)

	sword := servertest.ItemStack(t, "minecraft:diamond_sword", 1)
	sword.Damage = 780
	srv.GiveSlot("Testificate", player.HotbarSlot, sword)
	w.Drain()
	w.Click(30, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{30: {}}, sword)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: sword}, protocol.ItemStack{})

	sharpness, ok := registry.EnchantmentID("minecraft:sharpness")
	if !ok {
		t.Fatal("no sharpness enchantment")
	}
	diamond := servertest.ItemStack(t, "minecraft:diamond", 1)
	diamond.Enchantments = map[int32]int32{sharpness: 5}
	srv.GiveSlot("Testificate", player.HotbarSlot, diamond)
	w.Drain()
	w.Click(30, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{30: {}}, diamond)
	w.Click(1, 0, protocol.ClickModePickup)
	mended := sword
	mended.Damage = 390
	mended.RepairCost = 1
	w.ExpectClickResult(map[int16]protocol.ItemStack{1: diamond, 2: mended}, protocol.ItemStack{})
}
