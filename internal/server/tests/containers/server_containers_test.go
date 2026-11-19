package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"io"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/console"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The container tests: the crafting of the player's own 2x2 and a table's
// 3x3, the click modes of the window protocol, the chests and furnaces the
// world keeps as block entities, and what breaking one spills.
//
// These drive a real connection the way the harness does, one crafted click
// at a time: the client here predicts nothing (it sends the click with no
// changed slots and an empty cursor), so every correction the server sends
// back is the server's own idea of what the click did, which is what the
// tests are after. A made-up client that tracked no state could not echo the
// state IDs, so the recorder keeps the last one each window said, the way a
// real client does, and answers every click with it.

// TestCraftPlanksFromALog is the shortest craft there is: one log in the
// player's own 2x2 grid, planks in the result slot, and taking them onto the
// cursor, leaving the grid empty and the result nothing again. The stack
// sizes go through the plain pick-up and place-one clicks on the way, which
// is also what the test is checking: the right-click that leaves one of a
// stack where it was clicked.
func TestCraftPlanksFromALog(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// Sixty-four oak logs land in the hotbar, the way a creative client
	// decides they do.
	logs := servertest.ItemStack(t, "minecraft:oak_log", 64)
	w.SetCreativeSlot(36, logs)

	// Left-click picks the whole stack up; right-click places one of it.
	// Slot 1 of the player's window is the first cell of the 2x2.
	w.Click(36, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{36: {}}, logs)

	w.Click(1, 1, protocol.ClickModePickup)
	carried := servertest.ItemStack(t, "minecraft:oak_log", 63)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1: servertest.ItemStack(t, "minecraft:oak_log", 1),
		// The result slot shows what the grid is worth: four planks.
		0: servertest.ItemStack(t, "minecraft:oak_planks", 4),
	}, carried)

	// The rest of the logs go home before the result is taken: a cursor
	// holding something the result is not never takes anything, the way
	// the vanilla server refuses the take. The grid and its result are
	// left untouched by that, so they are not corrected; the client that
	// predicted nothing was told them on the click before.
	w.Click(36, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		36: servertest.ItemStack(t, "minecraft:oak_log", 63),
	}, protocol.ItemStack{})

	// Taking the result with a plain left click puts the planks on the
	// cursor and consumes the log: the grid is empty and the result has
	// nothing to show for it.
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1: {},
		0: {},
	}, servertest.ItemStack(t, "minecraft:oak_planks", 4))
}

// TestThrowFromACraftingResult throws the result where a plain take would
// lift it: the Q drops the planks the grid is worth and spends the log, and
// the result slot falls empty with the grid.
func TestThrowFromACraftingResult(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// One log of a hotbar stack into the 2x2's first cell: the result
	// slot shows the four planks it is worth.
	logs := servertest.ItemStack(t, "minecraft:oak_log", 64)
	w.SetCreativeSlot(36, logs)
	w.Click(36, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{36: {}}, logs)
	w.Click(1, 1, protocol.ClickModePickup)
	carried := servertest.ItemStack(t, "minecraft:oak_log", 63)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1: servertest.ItemStack(t, "minecraft:oak_log", 1),
		0: servertest.ItemStack(t, "minecraft:oak_planks", 4),
	}, carried)

	// The rest of the logs go home to empty the cursor.
	w.Click(36, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		36: servertest.ItemStack(t, "minecraft:oak_log", 63),
	}, protocol.ItemStack{})

	// With the cursor empty the Q throws the result: the planks drop,
	// the log is consumed and the result slot falls empty with the grid.
	w.Click(0, 0, protocol.ClickModeThrow)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1: {},
		0: {},
	}, protocol.ItemStack{})
	w.ExpectDrop(servertest.ItemStack(t, "minecraft:oak_planks", 4))
}

// TestCraftingTableMakesSticksAndAPickaxe opens a crafting table on a
// right-click, fills its 3x3 the way the shaped recipes want - sticks from a
// column of two planks, then the pickaxe from three ingots over two sticks -
// and takes each result once, consuming what the grid held.
func TestCraftingTableMakesSticksAndAPickaxe(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// A table in hand, placed on the ground next to spawn.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:crafting_table", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()

	// Right-clicking the table opens it: a minecraft:crafting window with
	// an empty 3x3 and the player's things behind it.
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuCrafting, false, 0, 0, 0)

	// The hotbar's two stacks: planks in the selected slot, ingots in the
	// next. In the crafting window's numbering the hotbar is slots 37-45,
	// so these are 37 and 38.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:oak_planks", 64))
	w.SetCreativeSlot(37, servertest.ItemStack(t, "minecraft:iron_ingot", 64))

	// Sticks: one plank above another, in the left column of the grid -
	// window slots 1 and 4 - placed with the right-click that leaves one
	// of the held stack where it was clicked. Every click is drained in
	// before the next one, since a click echoing a state the server has
	// already moved past is answered with the whole window rather than the
	// slots it changed, and an echo is only fresh once the corrections are
	// read.
	w.Click(37, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		37: protocol.ItemStack{},
	}, servertest.ItemStack(t, "minecraft:oak_planks", 64))

	w.Click(1, 1, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1: servertest.ItemStack(t, "minecraft:oak_planks", 1),
		// One plank is already worth what the data pack says a plank
		// alone makes: an oak button.
		0: servertest.ItemStack(t, "minecraft:oak_button", 1),
	}, servertest.ItemStack(t, "minecraft:oak_planks", 63))

	w.Click(4, 1, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		4: servertest.ItemStack(t, "minecraft:oak_planks", 1),
		// What the grid was worth once the second plank landed: sticks,
		// which beat the button because the shaped recipes are tried
		// before the shapeless ones.
		0: servertest.ItemStack(t, "minecraft:stick", 4),
	}, servertest.ItemStack(t, "minecraft:oak_planks", 62))

	w.Click(37, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		37: servertest.ItemStack(t, "minecraft:oak_planks", 62),
	}, protocol.ItemStack{})

	// Taking the sticks leaves them on the cursor, the recipe's planks
	// consumed: the grid is empty and the result nothing again.
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{1: {}, 4: {}, 0: {}},
		servertest.ItemStack(t, "minecraft:stick", 4))

	// Two of the sticks go where the pickaxe wants them - a stick under
	// the middle of the top row and one under that, window slots 5 and 8 -
	// and the two the pattern does not need go into the main inventory
	// (window slot 20), out of the cursor's way. Two sticks in the grid
	// are worth nothing on their own, so the result falls back empty on
	// the way.
	w.Click(5, 1, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		5: servertest.ItemStack(t, "minecraft:stick", 1),
		0: protocol.ItemStack{},
	}, servertest.ItemStack(t, "minecraft:stick", 3))

	w.Click(8, 1, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		8: servertest.ItemStack(t, "minecraft:stick", 1),
		0: protocol.ItemStack{},
	}, servertest.ItemStack(t, "minecraft:stick", 2))

	w.Click(20, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		20: servertest.ItemStack(t, "minecraft:stick", 2),
	}, protocol.ItemStack{})

	// Three ingots along the top of the grid - window slots 1, 2 and 3 -
	// and the grid is worth a pickaxe; the rest of the ingots go back to
	// the hotbar, since the cursor has to be free to take what it made.
	w.Click(38, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		38: protocol.ItemStack{},
	}, servertest.ItemStack(t, "minecraft:iron_ingot", 64))

	w.Click(1, 1, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1: servertest.ItemStack(t, "minecraft:iron_ingot", 1),
		0: protocol.ItemStack{},
	}, servertest.ItemStack(t, "minecraft:iron_ingot", 63))

	w.Click(2, 1, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		2: servertest.ItemStack(t, "minecraft:iron_ingot", 1),
		// Two ingots over the two sticks is the hoe's pattern - a 2x3
		// box with the sticks under one column of it - which fits beside
		// the empty third column of the grid.
		0: servertest.ItemStack(t, "minecraft:iron_hoe", 1),
	}, servertest.ItemStack(t, "minecraft:iron_ingot", 62))

	w.Click(3, 1, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		3: servertest.ItemStack(t, "minecraft:iron_ingot", 1),
		// The full pattern: three ingots over the two sticks.
		0: servertest.ItemStack(t, "minecraft:iron_pickaxe", 1),
	}, servertest.ItemStack(t, "minecraft:iron_ingot", 61))

	w.Click(38, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		38: servertest.ItemStack(t, "minecraft:iron_ingot", 61),
	}, protocol.ItemStack{})

	// Taking it leaves the whole grid empty behind, the way the one shaped
	// recipe consumed exactly what it matched.
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1: {}, 2: {}, 3: {}, 5: {}, 8: {}, 0: {},
	}, servertest.ItemStack(t, "minecraft:iron_pickaxe", 1))

	// Closing the window hands the cursor and the grid back to the player,
	// so what was crafted is not lost with the window: the pickaxe lands
	// in an empty hotbar slot (the planks and ingots hold 36 and 37), the
	// main-inventory sticks stay where they were parked, and the window
	// the player is left looking at is their own.
	w.CloseWindow()
	w.ExpectWindowZero()
	want := map[int]protocol.ItemStack{
		36: servertest.ItemStack(t, "minecraft:oak_planks", 62),
		37: servertest.ItemStack(t, "minecraft:iron_ingot", 61),
		38: servertest.ItemStack(t, "minecraft:iron_pickaxe", 1),
		19: servertest.ItemStack(t, "minecraft:stick", 2),
	}
	for slot, stack := range want {
		if got := srv.InventorySlot("Testificate", slot); !got.Equal(stack) {
			w.T.Errorf("after close, slot %d = %+v, want %+v", slot, got, stack)
		}
	}
}

// TestShiftClickCraftsAsManyAsTheIngredientsAllow puts a whole stack through
// the 2x2 at once and takes the result with the shift-click that keeps
// crafting until the ingredients or the room run out: sixty-four logs become
// one stack of planks, walking the inventory from its far end - the hotbar's
// rightmost slot - the way the client predicts the move and the vanilla
// menu answers it, every craft merging into the stack the last one made.
func TestShiftClickCraftsAsManyAsTheIngredientsAllow(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// The whole stack goes into the grid in one move: picked up with a
	// left click, placed with one.
	logs := servertest.ItemStack(t, "minecraft:oak_log", 64)
	planks := servertest.ItemStack(t, "minecraft:oak_planks", 64)
	w.SetCreativeSlot(36, logs)
	w.Click(36, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{36: {}}, logs)
	w.Click(1, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1: servertest.ItemStack(t, "minecraft:oak_log", 64),
		0: servertest.ItemStack(t, "minecraft:oak_planks", 4),
	}, protocol.ItemStack{})

	// One craft a log, sixty-four of them: the planks pile onto the
	// hotbar's rightmost slot, the first place the vanilla move tries,
	// until it holds a whole stack and the grid is left holding nothing.
	w.Click(0, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1:  protocol.ItemStack{},
		0:  protocol.ItemStack{},
		44: planks,
	}, protocol.ItemStack{})
}

// TestChestHoldsWhatItTookInAcrossASave checks the whole life of a chest's
// contents: opened by a right-click, filled from the hotbar, and closed -
// with the lid counting who is looking in - then the server shut down and
// started again on the same world, and the chest still holding what it was
// left with.
func TestChestHoldsWhatItTookInAcrossASave(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	srv, w := servertest.OpenWindowed(t, cfg)

	// A chest on the ground next to spawn.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:chest", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()

	// Right-clicking opens the 27 slots it keeps, every one of them empty:
	// the rest of the window is the player's own things, out of the
	// chest's part. The lid lifts for the one player looking in, which a
	// chest's open broadcasts.
	w.UseBlock(4, 64, 0)
	slots := w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	if len(slots) != 27+36 {
		w.T.Errorf("the chest window has %d slots, want its 27 and the player's 36", len(slots))
	}
	for i, stack := range slots[:27] {
		if !stack.Empty() {
			w.T.Errorf("chest slot %d = %+v, want nothing in it yet", i, stack)
		}
	}
	w.ExpectLid(4, 64, 0, 1)

	// The whole stack of diamonds goes into slot 0: the hotbar's first
	// window slot being 54 in a chest's numbering, a click lifts them and
	// another leaves them in the chest.
	diamonds := servertest.ItemStack(t, "minecraft:diamond", 64)
	w.SetCreativeSlot(36, diamonds)
	w.Click(54, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{54: {}}, diamonds)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: diamonds}, protocol.ItemStack{})

	// Closing hands nothing back - the diamonds are the chest's now - and
	// the lid falls with nobody looking in.
	w.CloseWindow()
	w.ExpectWindowZero()
	if got := srv.InventorySlot("Testificate", 36); !got.Empty() {
		w.T.Errorf("after the close, hotbar slot 0 = %+v, want it empty", got)
	}
	w.ExpectLid(4, 64, 0, 0)

	// What the chest holds is the block entity's, inside the world rather
	// than the player: read straight out of it before the save.
	if got := srv.InventoryAt(4, 64, 0); len(got) == 0 || !got[0].Equal(diamonds) {
		t.Errorf("the chest holds %+v, want the diamonds in slot 0", got)
	}

	// The save and the load: the player leaves, the server comes down, and
	// a new one standing on the same world directory reads the chest back
	// out of its region file.
	w.Disconnect()
	srv.Shutdown()

	_, back := servertest.OpenWindowed(t, cfg)
	back.UseBlock(4, 64, 0)
	slots = back.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	if got := slots[0]; !got.Equal(diamonds) {
		back.T.Errorf("after the load, chest slot 0 = %+v, want the diamonds", got)
	}
	back.ExpectLid(4, 64, 0, 1)
}

// TestAnEnderChestKeepsWhatItTookInAcrossASave checks the ender chest as
// the one chest a player owns: what goes into one goes into the player's
// own ender keeping, so a rejoin reads it back out of the player's data and
// a different ender chest in another place opens onto the very same slots.
// The ender chest keeps no block entity of contents of its own - its window
// opens without one arriving - and its lid is the ender chest's to tell.
func TestAnEnderChestKeepsWhatItTookInAcrossASave(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	srv, w := servertest.OpenWindowed(t, cfg)

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:ender_chest", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	slots := w.ExpectContainerOpen(protocol.MenuGeneric9x3, false, 4, 64, 0)
	if len(slots) != 27+36 {
		w.T.Errorf("the ender chest window has %d slots, want its 27 and the player's 36", len(slots))
	}

	diamonds := servertest.ItemStack(t, "minecraft:diamond", 64)
	w.SetCreativeSlot(36, diamonds)
	w.Click(54, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{54: {}}, diamonds)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: diamonds}, protocol.ItemStack{})
	w.CloseWindow()
	w.ExpectWindowZero()
	if got := srv.InventorySlot("Testificate", 36); !got.Empty() {
		w.T.Errorf("after the close, hotbar slot 0 = %+v, want it empty", got)
	}

	// The player leaves, the server comes down, and a new one standing on
	// the same world directory reads the ender keeping back out of the
	// player's data. A second ender chest, placed elsewhere, opens onto
	// what the first one took in: every ender chest is the same chest.
	w.Disconnect()
	srv.Shutdown()

	_, back := servertest.OpenWindowed(t, cfg)
	back.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:ender_chest", 1))
	back.MoveToGround(6.5, 64, 0.5, true)
	back.PlaceBlock(8, 63, 0, protocol.FaceTop)
	back.ExpectBlockAck()
	back.UseBlock(8, 64, 0)
	slots = back.ExpectContainerOpen(protocol.MenuGeneric9x3, false, 8, 64, 0)
	if got := slots[0]; !got.Equal(diamonds) {
		back.T.Errorf("the second ender chest's slot 0 = %+v, want the diamonds the first took in", got)
	}
	back.ExpectLidOf(8, 64, 0, 1, protocol.BlockEnderChest)
}

// TestTwoPlayersSeeEachOthersChestChanges checks the chest as the shared
// place it is: two windows on the same contents, each lid action counting
// whoever is looking in, and a click in one appearing in the other without
// either of them asking.
func TestTwoPlayersSeeEachOthersChestChanges(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	// One player alone puts the chest out and drains the placing away.
	alice := servertest.WindowedOf(t, "Alice", connect())
	alice.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:chest", 1))
	alice.PlaceBlock(4, 63, 0, protocol.FaceTop)
	alice.ExpectBlockAck()

	// The second player joins the same world, and both open the chest:
	// the first open lifts the lid for one, the second for both, and the
	// lid tells everyone either way - Bob hears Alice's open too, being
	// in the world by then.
	bob := servertest.WindowedOf(t, "Bob", connect())
	alice.UseBlock(4, 64, 0)
	alice.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	alice.ExpectLid(4, 64, 0, 1)
	bob.UseBlock(4, 64, 0)
	bob.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	alice.ExpectLid(4, 64, 0, 2)
	bob.ExpectLid(4, 64, 0, 1)
	bob.ExpectLid(4, 64, 0, 2)

	// Alice puts her stack in, and Bob's window shows it arriving: the
	// chest's slot corrected in his window's own numbering.
	diamonds := servertest.ItemStack(t, "minecraft:diamond", 13)
	alice.SetCreativeSlot(36, diamonds)
	alice.Click(54, 0, protocol.ClickModePickup)
	alice.ExpectClickResult(map[int16]protocol.ItemStack{54: {}}, diamonds)
	alice.Click(7, 0, protocol.ClickModePickup)
	alice.ExpectClickResult(map[int16]protocol.ItemStack{7: diamonds}, protocol.ItemStack{})
	bob.AwaitWindowSlot(7, diamonds)

	// What the chest holds now, both ends of it agree on.
	if got := srv.InventoryAt(4, 64, 0); len(got) == 0 || !got[7].Equal(diamonds) {
		t.Errorf("the shared chest holds %+v in slot 7, want the diamonds", got)
	}

	// The lids fall as the two of them look away, each hearing both.
	bob.CloseWindow()
	bob.ExpectWindowZero()
	alice.ExpectLid(4, 64, 0, 1)
	bob.ExpectLid(4, 64, 0, 1)
	alice.CloseWindow()
	alice.ExpectWindowZero()
	alice.ExpectLid(4, 64, 0, 0)
	bob.ExpectLid(4, 64, 0, 0)
}

// TestClickModesOnAnOpenChest runs the click modes the container protocol
// carries, on a chest: the shift-click that moves between the chest and the
// player's own things, the number-key swap, the paint drag that splits the
// held stack, the creative middle-click that conjures a full stack, and the
// drop key that throws out of a window's slot.
func TestClickModesOnAnOpenChest(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// The chest, opened.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:chest", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)

	// Slot 0 of the chest takes the stack of diamonds.
	diamonds := servertest.ItemStack(t, "minecraft:diamond", 13)
	w.SetCreativeSlot(36, diamonds)
	w.Click(54, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{54: {}}, diamonds)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: diamonds}, protocol.ItemStack{})

	// The shift-click out of the chest lands the stack in the player's
	// own things from the hotbar's far end: nothing to merge with, so the
	// first empty - counted from the back the way the vanilla chest menus
	// fill - is window slot 62, the last hotbar slot.
	w.Click(0, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		0:  protocol.ItemStack{},
		62: diamonds,
	}, protocol.ItemStack{})

	// Apples into the chest's slot 5, to swap with the hotbar's coal by
	// the number key that names it: everything in the way of the key
	// trades places, the chest's slot and the hotbar's.
	apples := servertest.ItemStack(t, "minecraft:apple", 10)
	coal := servertest.ItemStack(t, "minecraft:coal", 7)
	w.SetCreativeSlot(36, apples)
	w.Click(54, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{54: {}}, apples)
	w.Click(5, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{5: apples}, protocol.ItemStack{})
	w.SetCreativeSlot(37, coal)
	// The number-key swap arrives as mode 2 with the key as its button:
	// key 2, so button 1, which names the second hotbar slot.
	w.Click(5, 1, protocol.ClickModeSwap)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		5:  coal,
		55: apples,
	}, protocol.ItemStack{})

	// The paint drag: a stack of sticks held on the cursor, the drag
	// begun (slot -999, button 0 starting a left drag), two slots painted
	// (button 1 adding a slot to a left drag), and the drag ended (button
	// 2 ending a left drag), which is the moment the stack divides over
	// them - a whole stack over two empty slots leaving half in each.
	sticks64 := servertest.ItemStack(t, "minecraft:stick", 64)
	sticks32 := servertest.ItemStack(t, "minecraft:stick", 32)
	w.SetCreativeSlot(38, sticks64)
	w.Click(56, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{56: {}}, sticks64)
	w.Click(-999, 0, protocol.ClickModeDrag)
	w.Drain()
	w.Click(10, 1, protocol.ClickModeDrag)
	w.Drain()
	w.Click(11, 1, protocol.ClickModeDrag)
	w.Drain()
	w.Click(-999, 2, protocol.ClickModeDrag)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		10: sticks32,
		11: sticks32,
	}, protocol.ItemStack{})

	// The middle click a creative player conjures with: mode 3, and
	// whatever the slot holds, a full stack of it appears on the cursor,
	// slot untouched.
	w.Click(10, 2, protocol.ClickModeClone)
	w.ExpectClickResult(map[int16]protocol.ItemStack{}, sticks64)

	// The drop key with a stack still on the cursor throws nothing: the
	// vanilla THROW branch runs only with the cursor empty.
	w.Click(10, 0, protocol.ClickModeThrow)
	w.ExpectClickResult(nil, sticks64)

	// The conjured stack goes back into the chest's still-empty slot 12
	// and the cursor empties.
	w.Click(12, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{12: sticks64}, protocol.ItemStack{})

	// The drop key inside a window throws what the slot holds out of it:
	// one stick of the painted stack, on the ground as an item entity, the
	// slot keeping its thirty-one. The spawn takes the mailbox, so it is
	// awaited rather than drained.
	w.Click(10, 0, protocol.ClickModeThrow)
	w.AwaitWindowSlot(10, servertest.ItemStack(t, "minecraft:stick", 31))
	w.AwaitCursorItem(protocol.ItemStack{})
	w.ExpectDrop(servertest.ItemStack(t, "minecraft:stick", 1))

	// What the chest itself judges of all that clicking.
	contents := srv.InventoryAt(4, 64, 0)
	if got := contents[5]; !got.Equal(coal) {
		t.Errorf("chest slot 5 = %+v, want the coal", got)
	}
	if got := contents[10]; !got.Equal(servertest.ItemStack(t, "minecraft:stick", 31)) {
		t.Errorf("chest slot 10 = %+v, want the 31 sticks", got)
	}
	if got := contents[11]; !got.Equal(sticks32) {
		t.Errorf("chest slot 11 = %+v, want the 32 sticks", got)
	}
}

// TestClickOnAWindowBackgroundKeepsTheCarriedItem clicks the solid part of a
// window - the background between the slots, which the client reports as slot
// -1 - while holding an item, in every window there is: the player's own, the
// crafting table's, a chest's and a furnace's. Nothing moves: the click is
// not on a slot, so there is nothing for it to act on, and the stack stays on
// the cursor the way the vanilla server leaves it.
func TestClickOnAWindowBackgroundKeepsTheCarriedItem(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// The crafting table: opened, a stack of sticks lifted onto the cursor
	// from the hotbar, and the background clicked with each button. The
	// stack goes back where it came from before the next window, which
	// opens over the same hotbar.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:crafting_table", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuCrafting, false, 0, 0, 0)

	sticks := servertest.ItemStack(t, "minecraft:stick", 9)
	w.SetCreativeSlot(36, sticks)
	w.Click(37, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{37: {}}, sticks)

	w.Click(-1, 0, protocol.ClickModePickup)
	w.ExpectClickResult(nil, sticks)
	w.Click(-1, 1, protocol.ClickModePickup)
	w.ExpectClickResult(nil, sticks)

	w.Click(37, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{37: sticks}, protocol.ItemStack{})

	// The chest opens the same way, and its background leaves the cursor
	// alone the same way.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:chest", 1))
	w.PlaceBlock(5, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(5, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 5, 64, 0)

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:coal", 5))
	w.Click(54, 0, protocol.ClickModePickup)
	coal := servertest.ItemStack(t, "minecraft:coal", 5)
	w.ExpectClickResult(map[int16]protocol.ItemStack{54: {}}, coal)

	w.Click(-1, 0, protocol.ClickModePickup)
	w.ExpectClickResult(nil, coal)
	w.Click(-1, 1, protocol.ClickModePickup)
	w.ExpectClickResult(nil, coal)

	w.Click(54, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{54: coal}, protocol.ItemStack{})

	// The furnace, whose background does what the others' do.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:furnace", 1))
	w.PlaceBlock(6, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(6, 64, 0)
	w.ExpectContainerOpen(protocol.MenuFurnace, true, 6, 64, 0)

	w.SetCreativeSlot(36, sticks)
	w.Click(30, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{30: {}}, sticks)

	w.Click(-1, 0, protocol.ClickModePickup)
	w.ExpectClickResult(nil, sticks)
	w.Click(-1, 1, protocol.ClickModePickup)
	w.ExpectClickResult(nil, sticks)

	w.Click(30, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{30: sticks}, protocol.ItemStack{})

	// And the player's own window, which is there whenever nothing else is.
	w.CloseWindow()
	w.ExpectWindowZero()
	w.Click(36, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{36: {}}, sticks)

	w.Click(-1, 0, protocol.ClickModePickup)
	w.ExpectClickResult(nil, sticks)
	w.Click(-1, 1, protocol.ClickModePickup)
	w.ExpectClickResult(nil, sticks)
}

// TestClickOutsideAWindowDropsTheCarriedStack clicks the world beyond the
// window - slot -999 - the way a throw does: a left click drops the whole
// held stack, a right click one item of it, and the cursor ends holding what
// is left, which is what the vanilla server's own click outside runs.
func TestClickOutsideAWindowDropsTheCarriedStack(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:crafting_table", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuCrafting, false, 0, 0, 0)

	// A stack of five on the cursor: the left click outside throws all
	// five, and the cursor ends empty.
	sticks := servertest.ItemStack(t, "minecraft:stick", 5)
	w.SetCreativeSlot(36, sticks)
	w.Click(37, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{37: {}}, sticks)

	w.Click(-999, 0, protocol.ClickModePickup)
	w.ExpectClickResult(nil, protocol.ItemStack{})
	w.ExpectDrop(sticks)

	// A fresh stack on the cursor: the right click outside throws one, and
	// the cursor keeps the other four.
	w.SetCreativeSlot(36, sticks)
	w.Click(37, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{37: {}}, sticks)
	w.Click(-999, 1, protocol.ClickModePickup)
	w.ExpectClickResult(nil, servertest.ItemStack(t, "minecraft:stick", 4))
	w.ExpectDrop(servertest.ItemStack(t, "minecraft:stick", 1))
}

// TestBreakingAChestSpillsWhatItHeld makes a chest, fills it, and breaks the
// block: what it held lands on the ground as item entities, the block entity
// comes out of the world, and there is no window to be had from where it was.
func TestBreakingAChestSpillsWhatItHeld(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// The chest, filled with a stack of diamonds and closed.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:chest", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	diamonds := servertest.ItemStack(t, "minecraft:diamond", 17)
	w.SetCreativeSlot(36, diamonds)
	w.Click(54, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{54: {}}, diamonds)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: diamonds}, protocol.ItemStack{})
	w.CloseWindow()
	w.ExpectWindowZero()

	// The break settles into a spill and nothing else; creative drops no
	// chest block, so the one item to land is what the chest held. The
	// spill's dice are seeded, and the seed's first roll is a piece big
	// enough to carry the whole stack: seventeen diamonds leave as the
	// one item they went in as.
	srv.DropSeed(1)
	w.BreakBlock(4, 64, 0)
	w.ExpectDrop(diamonds)

	// The block entity is gone from the world.
	if got := srv.InventoryAt(4, 64, 0); got != nil {
		t.Errorf("the broken chest still holds %+v", got)
	}

	// And right-clicking where the chest was is right-clicking air: no
	// window opens.
	w.UseBlock(4, 64, 0)
	w.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlayOpenScreen {
			w.T.Error("a right-click where the chest was opened a window")
		}
	})
}

// TestABrokenChestIsGoneBeforeItsContentsSpill reads the wire order of a
// break: the block update that empties the chest's cell goes out before any
// of the item entities its contents become, so no item is born inside the
// chest's collision box and shoved out of it.
// vanilla: LevelChunk.setBlockState runs preRemoveSideEffects after the write
func TestABrokenChestIsGoneBeforeItsContentsSpill(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:chest", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	diamonds := servertest.ItemStack(t, "minecraft:diamond", 17)
	w.SetCreativeSlot(36, diamonds)
	w.Click(54, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{54: {}}, diamonds)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: diamonds}, protocol.ItemStack{})
	w.CloseWindow()
	w.ExpectWindowZero()

	w.BreakBlock(4, 64, 0)
	cleared, spawned := false, 0
	w.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayBlockUpdate:
			r := protocol.NewReader(data)
			x, y, z := r.ReadPosition()
			if x == 4 && y == 64 && z == 0 && r.ReadVarInt() == 0 {
				cleared = true
			}
		case protocol.IDPlaySpawnEntity:
			spawned++
			if !cleared {
				t.Error("an item spawned before the chest's cell was emptied")
			}
		}
	})
	if spawned == 0 {
		t.Error("the broken chest spilled nothing")
	}
}

// TestASurvivalChestDropsItselfAfterItsCellIsEmptied breaks an empty chest by
// hand: the item its loot table rolls spawns after the block update that
// empties the cell.
// vanilla: ServerPlayerGameMode.destroyBlock runs playerDestroy after the write
func TestASurvivalChestDropsItselfAfterItsCellIsEmptied(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Actor")
	c.MoveStep(0.5, 64, 0.5, true)
	if !srv.PlaceContainer(5, 64, 0, "minecraft:chest") {
		t.Fatal("could not stand the chest")
	}
	c.SettleAndCollect(func(int32, []byte) {})

	c.BreakBlock(5, 64, 0)
	// The hand takes a while to the chest, so the break lands behind the click.
	spawned := 0
	count := func(id int32, _ []byte) {
		if id == protocol.IDPlaySpawnEntity {
			spawned++
		}
	}
	for id, data := range c.Packets(servertest.ExpectTimeout) {
		if id == protocol.IDPlayBlockUpdate {
			r := protocol.NewReader(data)
			x, y, z := r.ReadPosition()
			if x == 5 && y == 64 && z == 0 && r.ReadVarInt() == 0 {
				break
			}
		}
		count(id, data)
	}
	if spawned != 0 {
		t.Error("the chest's drop spawned before its cell was emptied")
	}
	servertest.ExpectOneDroppedItem(t, srv, "minecraft:chest")
}

// TestASpilledChestLandsWhereVanillaPutsIt breaks a filled chest with the
// spill's dice seeded, and reads the item entities it laid down back off the
// wire: each stack spills at the one position the vanilla spill picks for it
// - floor of the block, then three quarters of a block of room, then the
// item's own width of offset - splitting into pieces of ten to thirty where
// the stack runs over, every piece thrown with the little arc whose upward
// hump stands over the sideways ones. The same seed, the same spill.
func TestASpilledChestLandsWhereVanillaPutsIt(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	const seed = 90210

	// The chest, filled with three stacks of its own and closed.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:chest", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	diamonds := servertest.ItemStack(t, "minecraft:diamond", 17)
	sticks := servertest.ItemStack(t, "minecraft:stick", 64)
	emeralds := servertest.ItemStack(t, "minecraft:emerald", 3)
	for i, stack := range []protocol.ItemStack{diamonds, sticks, emeralds} {
		w.SetCreativeSlot(36, stack)
		w.Click(54, 0, protocol.ClickModePickup)
		w.ExpectClickResult(map[int16]protocol.ItemStack{54: {}}, stack)
		w.Click(int16(i), 0, protocol.ClickModePickup)
		w.ExpectClickResult(map[int16]protocol.ItemStack{int16(i): stack}, protocol.ItemStack{})
	}
	w.CloseWindow()
	w.ExpectWindowZero()

	// The dice the spill rolls with, seeded for the break alone, and
	// the rolls it will make written down before the break: the same
	// seed makes the same spill, so the test knows how many items to
	// wait for.
	dice := rules.NewJavaRandom(seed)
	var want []servertest.SpillDrop
	for _, stack := range []protocol.ItemStack{diamonds, sticks, emeralds} {
		xo := 4.0 + dice.NextDouble()*0.75 + 0.125
		yo := 64.0 + dice.NextDouble()*0.75
		zo := 0.0 + dice.NextDouble()*0.75 + 0.125
		for !stack.Empty() {
			take := int(dice.NextIntBound(21)) + 10
			piece := stack
			if piece.Count > int32(take) {
				piece.Count = int32(take)
			}
			stack.Count -= piece.Count
			if stack.Count <= 0 {
				stack = protocol.ItemStack{}
			}
			want = append(want, servertest.SpillDrop{
				Stack: piece,
				X:     xo, Y: yo, Z: zo,
				Vx: dice.Triangle(0, 0.11485000171139836),
				Vy: dice.Triangle(0.2, 0.11485000171139836),
				Vz: dice.Triangle(0, 0.11485000171139836),
			})
		}
	}
	srv.DropSeed(seed)
	w.BreakBlock(4, 64, 0)
	for len(w.Spills) < len(want) || w.PendingSpill != nil {
		w.AwaitWindowPackets(func(int32, []byte) bool {
			return len(w.Spills) == len(want) && w.PendingSpill == nil
		})
	}

	// What the wire carried, piece for piece.
	if len(w.Spills) != len(want) {
		t.Fatalf("the spill laid down %d items, want the %d the dice say",
			len(w.Spills), len(want))
	}
	for i, got := range w.Spills {
		exp := want[i]
		if !got.Stack.Equal(exp.Stack) {
			t.Errorf("spill %d carries %+v, want %+v", i, got.Stack, exp.Stack)
		}
		if d := math.Abs(got.X-exp.X) + math.Abs(got.Y-exp.Y) + math.Abs(got.Z-exp.Z); d > 1e-9 {
			t.Errorf("spill %d stands at (%.6f, %.6f, %.6f), want (%.6f, %.6f, %.6f)",
				i, got.X, got.Y, got.Z, exp.X, exp.Y, exp.Z)
		}
		if d := math.Abs(got.Vx-exp.Vx) + math.Abs(got.Vy-exp.Vy) + math.Abs(got.Vz-exp.Vz); d > 0.01 {
			t.Errorf("spill %d was thrown at (%.6f, %.6f, %.6f), want (%.6f, %.6f, %.6f)",
				i, got.Vx, got.Vy, got.Vz, exp.Vx, exp.Vy, exp.Vz)
		}
	}
}

// pingNow sends a ping every hundred hand-driven ticks. With the loop
// stopped no keep-alive goes out, and a hand-driven run long enough on a slow
// machine would leave the connection silent past the server's read deadline.
func pingNow(c *servertest.Client, tick int32) {
	if tick%100 == 0 {
		c.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(int64(tick)) })
	}
}

// TestFurnaceSmeltsRawIronOnTheTick checks the whole of a smelt: the coal
// lights on the first tick, the properties the furnace's window draws itself
// with move as the ticks go by, the raw iron becomes an iron ingot on the
// tick the recipe's cooking time comes to, and the block's lit state falls
// back when the fuel has burned out.
func TestFurnaceSmeltsRawIronOnTheTick(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// The furnace, opened: three slots of its own and the player's things
	// behind them, and an idle furnace's four numbers all at zero.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:furnace", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	slots := w.ExpectContainerOpen(protocol.MenuFurnace, true, 4, 64, 0)
	if len(slots) != 3+36 {
		w.T.Errorf("the furnace window has %d slots, want its 3 and the player's 36", len(slots))
	}
	for _, property := range []int32{
		protocol.FurnacePropertyFuelLeft,
		protocol.FurnacePropertyFuelTotal,
		protocol.FurnacePropertyProgress,
		protocol.FurnacePropertyProgressTotal,
	} {
		w.AwaitWindowProperty(property, 0)
	}

	// The furnace is ticked by hand from here on, so that each tick's
	// numbers can be checked exactly; the main loop would tick it too. It
	// stops before the coal goes in, or a tick of its own could light the
	// furnace between the shift-click and the first tick by hand.
	srv.StopTick()

	// What smelts into the ingredient slot, what burns into the fuel slot:
	// the shift-click puts each where the furnace wants it.
	rawIron := servertest.ItemStack(t, "minecraft:raw_iron", 1)
	ingot := servertest.ItemStack(t, "minecraft:iron_ingot", 1)
	w.SetCreativeSlot(36, rawIron)
	w.SetCreativeSlot(37, servertest.ItemStack(t, "minecraft:coal", 1))
	w.Click(30, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		0:  rawIron,
		30: protocol.ItemStack{},
	}, protocol.ItemStack{})
	w.Click(31, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1:  servertest.ItemStack(t, "minecraft:coal", 1),
		31: protocol.ItemStack{},
	}, protocol.ItemStack{})

	// One tick, and the coal has caught: the fuel stands at the 1600 ticks
	// a coal is worth - it burns down from the next tick on - the smelt is
	// under way towards the 200 the recipe gives, the fuel slot is empty
	// of its coal, and the furnace block lights. The tick's telling of the
	// three slots arrives first, the four numbers after them.
	srv.TickBlockEntities()
	w.AwaitWindowSlot(0, rawIron)
	w.AwaitWindowSlot(1, protocol.ItemStack{})
	w.AwaitWindowSlot(2, protocol.ItemStack{})
	w.AwaitWindowProperty(protocol.FurnacePropertyFuelLeft, 1600)
	w.AwaitWindowProperty(protocol.FurnacePropertyFuelTotal, 1600)
	w.AwaitWindowProperty(protocol.FurnacePropertyProgress, 1)
	w.AwaitWindowProperty(protocol.FurnacePropertyProgressTotal, 200)
	if lit, ok := furnaceLit(srv, 4, 64, 0); !ok {
		t.Fatal("the furnace block went missing")
	} else if !lit {
		t.Error("the furnace block did not light")
	}

	// The smelt works its way there, a property at a time: each tick is
	// awaited at exactly the progress it should be at, which both pins the
	// pacing down and keeps the mailbox no deeper than the packets of one
	// tick, the awaiting itself being the reading.
	for tick := int32(2); tick <= 199; tick++ {
		srv.TickBlockEntities()
		pingNow(w.Client, tick)
		w.AwaitWindowProperty(protocol.FurnacePropertyProgress, tick)
	}

	// The two-hundredth tick finishes it: the raw iron is gone, the fuel
	// slot long since empty, an iron ingot in the output, and the progress
	// reset to nothing - 199 ticks of the coal spent getting there.
	srv.TickBlockEntities()
	w.AwaitWindowSlot(0, protocol.ItemStack{})
	w.AwaitWindowSlot(1, protocol.ItemStack{})
	w.AwaitWindowSlot(2, ingot)
	w.AwaitWindowProperty(protocol.FurnacePropertyProgress, 0)
	if spent, total, lit, litTotal, ok := srv.FurnaceAt(4, 64, 0); !ok {
		t.Fatal("the furnace's block entity went missing")
	} else if spent != 0 || total != 200 {
		t.Errorf("the smelt finished at (%d of %d)", spent, total)
	} else if lit != 1600-199 {
		t.Errorf("after the smelt, %d of the coal's %d ticks are left", lit, litTotal)
	}

	// The output is taken like anything else, and the ingot is a real
	// stack a player can hold.
	w.Click(2, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{2: protocol.ItemStack{}}, ingot)

	// The coal keeps the furnace lit with nothing to smelt, until it
	// burns out and the block falls back to unlit. The awaiting is of the
	// fuel itself now, counting down one tick at a time to the last of
	// the coal - after which the furnace says nothing more, having
	// nothing left to say.
	for tick := int32(201); tick <= 1601; tick++ {
		srv.TickBlockEntities()
		pingNow(w.Client, tick)
		w.AwaitWindowProperty(protocol.FurnacePropertyFuelLeft, 1600-(tick-1))
	}
	for range 10 {
		srv.TickBlockEntities()
	}
	if lit, ok := furnaceLit(srv, 4, 64, 0); !ok {
		t.Fatal("the furnace block went missing")
	} else if lit {
		t.Error("the furnace is still lit after its coal burned out")
	}
	if _, _, lit, _, ok := srv.FurnaceAt(4, 64, 0); !ok {
		t.Fatal("the furnace's block entity went missing")
	} else if lit != 0 {
		t.Errorf("%d ticks of fuel claim to remain", lit)
	}
}

// furnaceLit reports whether the furnace block at the position is in its
// lit state, which is the fire its clients draw with.
func furnaceLit(srv *server.Server, x, y, z int32) (lit bool, ok bool) {
	state := srv.BlockAt(x, y, z)
	name, props, known := registry.BlockStateName(state)
	if !known || name != "minecraft:furnace" {
		return false, false
	}
	return registry.ParseStateProps(props)["lit"] == "true", true
}

// TestAnAnvilMendsARenamedSwordAndChargesForIt opens an anvil, mends a worn
// sword with a diamond, names the mended sword and takes it, and checks the
// whole of what the window said on the way: the result the inputs make, the
// cost the property names for mending and rename alike, the take's level
// spend, and the anvil's own use event sounding where it stands. The wear
// roll the take carries cannot falsify any of this: a first wear on a
// pristine anvil only chips it, and the chipped and damaged anvils answer
// the same window, while the use event sounds whatever the roll decided.
func TestAnAnvilMendsARenamedSwordAndChargesForIt(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

	// The anvil, opened: no block entity behind it - the anvil keeps no
	// state of its own - and thirty-nine slots, its three and the
	// player's thirty-six.
	if slots := w.OpenAnvil(srv); len(slots) != 39 {
		t.Errorf("the anvil window has %d slots, want its 3 and the player's 36", len(slots))
	}

	// Levels to pay the take from, set the way /xp would.
	srv.SetExperienceLevels("Testificate", 10)

	// A worn sword onto the anvil's first slot.
	sword := servertest.ItemStack(t, "minecraft:diamond_sword", 1)
	sword.Damage = 780
	srv.GiveSlot("Testificate", player.HotbarSlot, sword)
	w.Drain()
	w.Click(30, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{30: {}}, sword)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: sword}, protocol.ItemStack{})

	// A diamond onto the second: the window mends the sword by a quarter
	// of its life, the result carrying the mending's own one-unit repair
	// cost, and the cost property names one level for it.
	diamond := servertest.ItemStack(t, "minecraft:diamond", 1)
	srv.GiveSlot("Testificate", player.HotbarSlot, diamond)
	w.Drain()
	w.Click(30, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{30: {}}, diamond)
	w.Click(1, 0, protocol.ClickModePickup)
	mended := sword
	mended.Damage = 390
	mended.RepairCost = 1
	w.ExpectClickResult(map[int16]protocol.ItemStack{1: diamond, 2: mended}, protocol.ItemStack{})
	if got := w.Props[server.AnvilCostProperty]; got != 1 {
		t.Errorf("the mending's cost = %d, want 1", got)
	}

	// The name field: this rename names the mended sword, which the
	// window answers with the renamed result and the rename's own level
	// on top of the mending's.
	renamed := mended
	name := protocol.Text{Text: "Sharp Sword"}
	renamed.CustomName = &name
	w.RenameInAnvil("Sharp Sword", renamed, 2)

	// The take: the renamed sword off the result, the inputs emptied
	// behind it, the cost back to nothing, two of the ten levels gone -
	// the mending's one and the rename's one - and the anvil's use
	// event sounding where it stands.
	w.Click(2, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		0: protocol.ItemStack{},
		1: protocol.ItemStack{},
		2: protocol.ItemStack{},
	}, renamed)
	if got := srv.ExperienceLevels("Testificate"); got != 8 {
		t.Errorf("levels after the take = %d, want 8", got)
	}
	if got := w.Props[server.AnvilCostProperty]; got != 0 {
		t.Errorf("the cost after the take = %d, want 0", got)
	}
	if !slices.Contains(w.Events, protocol.WorldEventAnvilUse) {
		t.Errorf("no anvil use event played; the events were %v", w.Events)
	}
}

// TestAnAnvilRefusesATakeThePlayerCannotPayFor puts a mending on the anvil
// with a player who has no levels, and clicks the result: nothing moves,
// the window answers with the cursor it already had, the cost stays named,
// and the anvil stands unworn by a take that never happened.
func TestAnAnvilRefusesATakeThePlayerCannotPayFor(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

	w.OpenAnvil(srv)

	// A worn sword and a diamond on the inputs: the mending is named and
	// its one level with it, though the player has nothing to pay from.
	sword := servertest.ItemStack(t, "minecraft:diamond_sword", 1)
	sword.Damage = 780
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
	mended.RepairCost = 1
	w.ExpectClickResult(map[int16]protocol.ItemStack{1: diamond, 2: mended}, protocol.ItemStack{})
	if got := w.Props[server.AnvilCostProperty]; got != 1 {
		t.Errorf("the mending's cost = %d, want 1", got)
	}

	// The click on the result the player cannot pay for: nothing moves,
	// the cursor is restated empty, and no slot is corrected.
	w.Click(2, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{}, protocol.ItemStack{})

	// No levels were spent, the cost is still the mending's one, and the
	// anvil stands as it was placed.
	if got := srv.ExperienceLevels("Testificate"); got != 0 {
		t.Errorf("levels after the refused take = %d, want 0", got)
	}
	if got := w.Props[server.AnvilCostProperty]; got != 1 {
		t.Errorf("the cost after the refused take = %d, want 1", got)
	}
	if got := srv.BlockNameAt(4, 64, 0); got != "minecraft:anvil" {
		t.Errorf("the anvil after the refused take = %q, want minecraft:anvil", got)
	}
}

// namedDirt is count dirt carrying the custom name name, the stack the
// anvil's rename makes of plain dirt.
func namedDirt(t *testing.T, name string, count int32) protocol.ItemStack {
	t.Helper()
	stack := servertest.ItemStack(t, "minecraft:dirt", count)
	text := protocol.Text{Text: name}
	stack.CustomName = &text
	return stack
}

// anvilWithNamedCursor opens an anvil with levels to pay from and builds the
// state the result-click tests start from: count of the renamed dirt on the
// cursor, the way the window's own take builds it, and one plain dirt
// swapped into the first input behind the cursor's back - the window worth
// one more of what the cursor already holds.
func anvilWithNamedCursor(t *testing.T, count int32) (*server.Server, *servertest.WindowedClient) {
	t.Helper()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())
	w.OpenAnvil(srv)
	// Levels to pay the takes from, set the way /xp would.
	srv.SetExperienceLevels("Testificate", 10)

	// The cursor's stack: count dirt into the first input, renamed, and
	// the whole result taken onto the empty cursor.
	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:dirt", count))
	w.Drain()
	w.Click(30, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{30: {}}, servertest.ItemStack(t, "minecraft:dirt", count))
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: servertest.ItemStack(t, "minecraft:dirt", count)}, protocol.ItemStack{})
	w.RenameInAnvil("Foo", namedDirt(t, "Foo", count), 1)
	w.Click(2, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: {}, 1: {}, 2: {}}, namedDirt(t, "Foo", count))
	if got := srv.ExperienceLevels("Testificate"); got != 9 {
		t.Errorf("levels after the build take = %d, want 9", got)
	}

	// One plain dirt into the first input without the cursor's help - the
	// number-key swap.
	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:dirt", 1))
	w.Drain()
	w.Click(0, 0, protocol.ClickModeSwap)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		30: {},
		0:  servertest.ItemStack(t, "minecraft:dirt", 1),
		2:  namedDirt(t, "Foo", 1),
	}, namedDirt(t, "Foo", count))
	return srv, w
}

// TestAnAnvilAddsTheResultsCountToAMatchingCursor takes the renamed dirt's
// result with the cursor already holding the same renamed dirt: the result's
// own one comes off onto the cursor, and no more - the vanilla click's
// tryRemove asks for the slot's count against the cursor's room, and a take
// that grew the cursor by its own count instead would mint items out of the
// air. Either button takes the whole result.
func TestAnAnvilAddsTheResultsCountToAMatchingCursor(t *testing.T) {
	t.Parallel()
	for _, click := range []struct {
		name   string
		button int8
	}{
		{"left", 0},
		{"right", 1},
	} {
		t.Run(click.name, func(t *testing.T) {
			t.Parallel()
			srv, w := anvilWithNamedCursor(t, 5)

			// The click on the result: the one renamed dirt comes off
			// onto the cursor, which ends at six, and the take that pays
			// for it empties the inputs and leaves the result nothing to
			// show.
			w.Click(2, click.button, protocol.ClickModePickup)
			w.ExpectClickResult(map[int16]protocol.ItemStack{
				0: {},
				1: {},
				2: {},
			}, namedDirt(t, "Foo", 6))
			if got := srv.ExperienceLevels("Testificate"); got != 8 {
				t.Errorf("levels after the take = %d, want 8", got)
			}
			if got := w.Props[server.AnvilCostProperty]; got != 0 {
				t.Errorf("the cost after the take = %d, want 0", got)
			}
		})
	}
}

// TestAnAnvilRefusesAResultTheCursorCannotTake clicks the result with a
// cursor the take cannot join: one with no room for the whole result, and
// one carrying the same item under another name, which isSameItemSameComponents
// puts behind every merge. Nothing moves, and the take that follows the drop
// proves it by still finding the result there to make.
func TestAnAnvilRefusesAResultTheCursorCannotTake(t *testing.T) {
	t.Parallel()
	for _, refusal := range []struct {
		name   string
		cursor int32  // the count the build puts on the cursor
		retype string // the name the window is retyped to, empty for none
		taken  string // the name of what the take after the drop ends with
	}{
		{"no room for the whole result", 64, "", "Foo"},
		{"differing components", 5, "Bar", "Bar"},
	} {
		t.Run(refusal.name, func(t *testing.T) {
			t.Parallel()
			srv, w := anvilWithNamedCursor(t, refusal.cursor)
			if refusal.retype != "" {
				// The cursor keeps its Foo dirt while the window makes
				// Bar dirt: the same item under another name. The cost
				// stays 1, so the window answers with the result slot
				// alone.
				w.RenameInAnvil(refusal.retype, namedDirt(t, refusal.retype, 1), 1)
			}

			// The click the cursor cannot join: nothing moves, and the
			// cursor is restated whole.
			w.Click(2, 0, protocol.ClickModePickup)
			w.ExpectClickResult(map[int16]protocol.ItemStack{}, namedDirt(t, "Foo", refusal.cursor))
			if got := srv.ExperienceLevels("Testificate"); got != 9 {
				t.Errorf("levels after the refused click = %d, want 9", got)
			}
			if got := w.Props[server.AnvilCostProperty]; got != 1 {
				t.Errorf("the cost after the refused click = %d, want 1", got)
			}

			// That nothing moved: the cursor dropped outside the window,
			// the plain click on the result still finds the take there to
			// make.
			w.Click(-999, 0, protocol.ClickModePickup)
			w.ExpectClickResult(map[int16]protocol.ItemStack{}, protocol.ItemStack{})
			w.ExpectDrop(namedDirt(t, "Foo", refusal.cursor))
			w.Click(2, 0, protocol.ClickModePickup)
			w.ExpectClickResult(map[int16]protocol.ItemStack{
				0: {},
				1: {},
				2: {},
			}, namedDirt(t, refusal.taken, 1))
			if got := srv.ExperienceLevels("Testificate"); got != 8 {
				t.Errorf("levels after the take = %d, want 8", got)
			}
		})
	}
}

// TestThrowFromAnAnvilResultDropsTheWholeStack is the anvil's plain Q: the
// result container's own removeItem ignores the amount a Q asks for, so the
// whole renamed stack drops and the take behind it spends the input and the
// level - a take that dropped one but spent the stack would destroy fifteen.
// vanilla: ResultContainer.removeItem, AbstractContainerMenu.doClick THROW
func TestThrowFromAnAnvilResultDropsTheWholeStack(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())
	w.OpenAnvil(srv)
	srv.SetExperienceLevels("Testificate", 10)

	// Sixteen dirt of the hotbar into the anvil's first input.
	dirt := servertest.ItemStack(t, "minecraft:dirt", 16)
	srv.GiveSlot("Testificate", player.HotbarSlot, dirt)
	w.Drain()
	w.Click(30, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{30: {}}, dirt)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: dirt}, protocol.ItemStack{})

	// The rename makes the whole input the result, worth one level.
	renamed := namedDirt(t, "Rocky", 16)
	w.RenameInAnvil("Rocky", renamed, 1)

	// The plain Q: the whole renamed stack leaves as one drop, the input
	// is spent with it.
	w.Click(2, 0, protocol.ClickModeThrow)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: {}, 2: {}}, protocol.ItemStack{})
	w.ExpectDrop(renamed)
	if got := srv.ExperienceLevels("Testificate"); got != 9 {
		t.Errorf("levels after the throw = %d, want 9", got)
	}
}

// TestAShiftClickFeedsAnAnvilsInputs shifts an enchanted book out of the
// player's things into the open anvil: into the first input when it is
// empty, into the second when the first is taken - and the recompute the
// moved input triggers answers the combine and its cost.
func TestAShiftClickFeedsAnAnvilsInputs(t *testing.T) {
	t.Parallel()

	// The book the shifts carry: one level of sharpness stored in it,
	// the magic a second input's book hands the anvil's maths.
	book := servertest.ItemStack(t, "minecraft:enchanted_book", 1)
	book.StoredEnchantments = map[int32]int32{servertest.SharpnessID(t): 1}

	// Both inputs empty: the book lays in the first, and the recompute
	// of a lone book leaves the result slot and the cost as they were.
	t.Run("the first input", func(t *testing.T) {
		t.Parallel()
		srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

		w.OpenAnvil(srv)
		srv.GiveSlot("Testificate", player.HotbarSlot, book)
		w.Drain()
		w.Click(30, 0, protocol.ClickModeQuickMove)
		w.ExpectClickResult(map[int16]protocol.ItemStack{
			30: {},
			0:  book,
		}, protocol.ItemStack{})
	})

	// The first input taken: the book lays in the second, and the
	// recompute the moved input triggers makes the sharpness join the
	// sword, the cost property naming the one level it charged.
	t.Run("the second", func(t *testing.T) {
		t.Parallel()
		srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

		w.OpenAnvil(srv)

		// A sword into the first input, the plain pickup way.
		sword := servertest.ItemStack(t, "minecraft:diamond_sword", 1)
		srv.GiveSlot("Testificate", player.HotbarSlot, sword)
		w.Drain()
		w.Click(30, 0, protocol.ClickModePickup)
		w.ExpectClickResult(map[int16]protocol.ItemStack{30: {}}, sword)
		w.Click(0, 0, protocol.ClickModePickup)
		w.ExpectClickResult(map[int16]protocol.ItemStack{0: sword}, protocol.ItemStack{})

		// The book shift-clicked out of the hotbar.
		srv.GiveSlot("Testificate", player.HotbarSlot, book)
		w.Drain()
		w.Click(30, 0, protocol.ClickModeQuickMove)
		enchanted := sword
		enchanted.Enchantments = map[int32]int32{servertest.SharpnessID(t): 1}
		enchanted.RepairCost = 1
		w.ExpectClickResult(map[int16]protocol.ItemStack{
			30: {},
			1:  book,
			2:  enchanted,
		}, protocol.ItemStack{})
		if got := w.Props[server.AnvilCostProperty]; got != 1 {
			t.Errorf("the cost after the shift = %d, want 1", got)
		}
	})
}

// TestAnAnvilConsumesTheSecondInputBesideARename combines two worn swords
// and names the result: a rename on top of a combine is not a rename alone,
// so the take consumes the second sword rather than leaving it in its slot -
// the duplicate the window would otherwise mint.
func TestAnAnvilConsumesTheSecondInputBesideARename(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

	w.OpenAnvil(srv)
	srv.SetExperienceLevels("Testificate", 10)

	// A worn sword onto the first input, another onto the second.
	sword := servertest.ItemStack(t, "minecraft:diamond_sword", 1)
	sword.Damage = 500
	srv.GiveSlot("Testificate", player.HotbarSlot, sword)
	w.Drain()
	w.Click(30, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{30: {}}, sword)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: sword}, protocol.ItemStack{})

	second := servertest.ItemStack(t, "minecraft:diamond_sword", 1)
	second.Damage = 500
	srv.GiveSlot("Testificate", player.HotbarSlot, second)
	w.Drain()
	w.Click(30, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{30: {}}, second)
	w.Click(1, 0, protocol.ClickModePickup)
	combined := sword
	combined.Damage = 0
	combined.RepairCost = 1
	w.ExpectClickResult(map[int16]protocol.ItemStack{1: second, 2: combined}, protocol.ItemStack{})

	// The name: the combine's result renamed, the pair now three levels -
	// the combine's two and the rename's one.
	renamed := combined
	name := protocol.Text{Text: "Excalibur"}
	renamed.CustomName = &name
	w.RenameInAnvil("Excalibur", renamed, 3)

	// The take: the renamed sword off the result, both inputs emptied -
	// the second sword consumed, not left behind - and three levels spent.
	w.Click(2, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		0: protocol.ItemStack{},
		1: protocol.ItemStack{},
		2: protocol.ItemStack{},
	}, renamed)
	if got := srv.ExperienceLevels("Testificate"); got != 7 {
		t.Errorf("levels after the take = %d, want 7", got)
	}
}

// TestAnAnvilNamesACombineTooDearToTake names two level-five, prior-worked
// swords: the combine's five levels and the rename's one come to forty-six,
// past what survival may take, so the result slot stays empty and the cost
// property names the dearer price rather than the rename's capped
// thirty-nine.
func TestAnAnvilNamesACombineTooDearToTake(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

	w.OpenAnvil(srv)
	srv.SetExperienceLevels("Testificate", 50)

	// Two swords carrying the same level five sharpness and twenty of
	// prior work each.
	enchantedSword := func() protocol.ItemStack {
		s := servertest.ItemStack(t, "minecraft:diamond_sword", 1)
		s.Enchantments = map[int32]int32{servertest.SharpnessID(t): 5}
		s.RepairCost = 20
		return s
	}
	first := enchantedSword()
	srv.GiveSlot("Testificate", player.HotbarSlot, first)
	w.Drain()
	w.Click(30, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{30: {}}, first)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: first}, protocol.ItemStack{})

	second := enchantedSword()
	srv.GiveSlot("Testificate", player.HotbarSlot, second)
	w.Drain()
	w.Click(30, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{30: {}}, second)
	w.Click(1, 0, protocol.ClickModePickup)
	// The combine alone is already dearer than forty - the prior work
	// forty and the enchantment's five - so its result runs empty.
	w.ExpectClickResult(map[int16]protocol.ItemStack{1: second}, protocol.ItemStack{})

	// The name takes the price to forty-six: still nothing survival may
	// take, so the result slot answers empty and the property names it.
	w.RenameInAnvil("Excalibur", protocol.ItemStack{}, 46)
}

// awaitWindowClosed waits, within ticks ticks, for the window the server is
// closing on its player: the Close Container naming the open window, and the
// player's own window told hotbar slot 1 holds handedBack (an empty
// handedBack waits for the close alone). A test with its tick loop stopped
// passes 0 ticks, once whatever closes the window has run. The teleport's
// position sync, the block changes of whatever took the container away, and
// the tick end's acknowledgement of an earlier interaction of this client's
// own are read past on the way. The slot correction is waited for itself,
// not settled for: the tick writes it after the close, and the connection
// can answer a ping between the two.
func awaitWindowClosed(w *servertest.WindowedClient, srv *server.Server, ticks int, handedBack protocol.ItemStack) {
	w.T.Helper()
	closed := false
	var hotbar1 *protocol.ItemStack
	done := w.ReadTicks(srv, ticks, func(id int32, data []byte) bool {
		if w.Record(id, data) {
			return false
		}
		switch id {
		case protocol.IDPlayUpdateTime, protocol.IDPlayBlockUpdate,
			protocol.IDPlaySectionBlocks, protocol.IDPlayUpdateLight,
			protocol.IDPlaySyncPosition, protocol.IDPlayAckBlockChange,
			protocol.IDPlaySoundEffect, protocol.IDPlayCenterChunk,
			protocol.IDPlayChunkBatchStart, protocol.IDPlayChunkData,
			protocol.IDPlayChunkBatchFinished, protocol.IDPlayUnloadChunk:
			// The tick's own time sync, the teleport's news of where the
			// body now stands and the chunks around it, the block changes
			// of the world, the ack and echoes a right-click leaves, and
			// the sound a chest's lid falls with.
		case protocol.IDPlayContainerSlot:
			r := protocol.NewReader(data)
			if window := r.ReadVarInt(); window != 0 {
				return false
			}
			w.TrackState(0, r.ReadVarInt())
			if slot := r.ReadI16(); slot == 37 {
				stack, _ := r.ReadSlot()
				hotbar1 = &stack
			}
		case protocol.IDPlayCloseContainer:
			if got := protocol.NewReader(data).ReadVarInt(); got != w.Window {
				w.T.Errorf("close container for window %d, want %d", got, w.Window)
			}
			closed = true
		default:
			w.T.Fatalf("unexpected packet 0x%02X while waiting for the window to close", id)
		}
		return closed && (handedBack.Empty() || hotbar1 != nil)
	})
	switch {
	case !closed:
		w.T.Fatalf("the window was not closed within %d ticks", ticks)
	case !done:
		w.T.Errorf("no window slot took the handed-back %+v", handedBack)
	case hotbar1 != nil && !hotbar1.Equal(handedBack):
		w.T.Errorf("after the close, hotbar slot 1 = %+v, want what the cursor handed back", *hotbar1)
	}
}

// TestATeleportedPlayerHasTheirWindowClosed checks the validity the vanilla
// server runs on the open menu every tick: a player teleported out of reach
// of the container they were looking into has the window closed on them, the
// Close Container naming it and the cursor's stack handed back into their
// inventory.
// vanilla: AbstractContainerMenu.stillValid
func TestATeleportedPlayerHasTheirWindowClosed(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.OpenHopper()

	// A stack lifted onto the cursor, which the close has to hand back.
	sticks := servertest.ItemStack(t, "minecraft:stick", 7)
	w.SetCreativeSlot(37, sticks)
	w.Click(33, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{33: {}}, sticks)

	// The console teleports the player thirty-two blocks out, past the
	// reach the vanilla menu stays valid within.
	console.New(strings.NewReader("tp Testificate 32 64 0\n"), io.Discard, io.Discard, srv).Serve()

	awaitWindowClosed(w, srv, 20, sticks)
}

// TestAContainerGoneFromUnderAWindowClosesIt checks the other half of the
// open menu's validity: the container has to still be there. A hopper
// /setblock'd away from the console closes the window on its player, with
// the cursor handed back.
// vanilla: Container.stillValidBlockEntity
func TestAContainerGoneFromUnderAWindowClosesIt(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.OpenHopper()

	sticks := servertest.ItemStack(t, "minecraft:stick", 3)
	w.SetCreativeSlot(37, sticks)
	w.Click(33, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{33: {}}, sticks)

	// The console replaces the hopper with stone: its block entity goes
	// with the block, and the window over nothing closes with the next
	// tick's validity check.
	console.New(strings.NewReader("setblock 4 64 0 minecraft:stone\n"), io.Discard, io.Discard, srv).Serve()

	awaitWindowClosed(w, srv, 20, sticks)
}

// TestSaveAllLeavesAnOnlinePlayersWindowAlone runs /save-all while a player
// holds a stack on the cursor and one in the crafting grid: the save writes
// the player's file but must not close their window, so the cursor and the
// grid stay where they are and nothing lands on the ground. vanilla:
// PlayerList.saveAll
func TestSaveAllLeavesAnOnlinePlayersWindowAlone(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// One log in the grid, the rest left on the cursor.
	srv.GiveSlot("Testificate", 36, servertest.ItemStack(t, "minecraft:oak_log", 5))
	w.PutInGrid(36, 1)

	srv.RunLine("save-all")

	if n := srv.ItemCount(); n != 0 {
		t.Errorf("/save-all dropped %d item entities, want none", n)
	}
	if got := srv.InventorySlot("Testificate", 1); !got.Equal(servertest.ItemStack(t, "minecraft:oak_log", 1)) {
		t.Errorf("the grid after /save-all = %+v, want the one log", got)
	}
}

// TestAClickOnAContainerABlastRemovedIsRefused opens a chest holding a stack
// and, with the tick stopped so only the click can close the window, removes
// the chest's block entity between the click's lookup of it and its lock -
// the window a blast's own removal leaves - then clicks the stack. The click
// must find the container gone: the stack stays in the chest and the window
// is closed on the player, rather than the click lifting a stack the blast is
// about to spill as well.
func TestAClickOnAContainerABlastRemovedIsRefused(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.StopTick()

	// The chest, holding one dirt in slot 0, opened.
	if !srv.PlaceContainer(4, 64, 0, "minecraft:chest") {
		t.Fatal("the chest would not stand up")
	}
	dirt := servertest.ItemStack(t, "minecraft:dirt", 1)
	srv.SetInventoryAt(4, 64, 0, 0, dirt)
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	w.ExpectLid(4, 64, 0, 1)

	// The click looks the chest up; the seam then takes it out of the world
	// the way a blast does, keeping the pointer so the test can read what
	// the click did to its slots.
	var removed *world.BlockEntity
	srv.ClickBetween(func(b *world.BlockEntity) {
		removed = b
		srv.RemoveBlockEntity(4, 64, 0)
	})

	// The click that would take the dirt must not: the chest is gone, and
	// the window closes on the player with nothing handed back.
	w.Click(0, 0, protocol.ClickModePickup)
	awaitWindowClosed(w, srv, 0, protocol.ItemStack{})

	var kept protocol.ItemStack
	removed.Use(func(b *world.BlockEntity) { kept = b.Slots[0] })
	if !kept.Equal(dirt) {
		t.Errorf("the click took %+v out of a removed chest, want the dirt left", kept)
	}
}

// TestAWindowClosesPastTheTicksBlockAck checks that the wait for a close
// reads past the acknowledgement the tick end flushes for an interaction of
// the same client's own: the ack rides behind the updates at whatever tick
// ran, so a use whose ack a test never reads can land it inside the wait.
func TestAWindowClosesPastTheTicksBlockAck(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.StopTick()

	// A hopper placed and opened with the loop stopped: the placement's ack
	// is flushed by hand, the use's is left pending while the open is read,
	// then put on the wire where a running tick would have landed it.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:hopper", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.SettleKeeping()
	srv.FlushBlockAcks()
	w.ExpectBlockAck()

	w.UseBlock(4, 64, 0)
	if slots := w.ExpectContainerOpenType(protocol.MenuHopper, protocol.BlockEntityHopper, 4, 64, 0); len(slots) != 5+36 {
		t.Errorf("the hopper window has %d slots, want its 5 and the player's 36", len(slots))
	}
	// The open is read before the ack is flushed, so the ack cannot be
	// swallowed by the open's own reader; the settle first makes sure the
	// use handler has recorded it.
	w.SettleKeeping()
	srv.FlushBlockAcks()

	// The container gone from under the window: the next survival pass
	// closes it, and the close crosses behind the ack already on the wire.
	console.New(strings.NewReader("setblock 4 64 0 minecraft:stone\n"), io.Discard, io.Discard, srv).Serve()
	srv.TickSurvival()
	awaitWindowClosed(w, srv, 0, protocol.ItemStack{})
}

// TestAWindowIDCountsToOneHundredThenWraps opens the same hopper 101 times
// and checks each window's ID: the IDs run 1 through 100, and the 101st
// wraps to 1.
// vanilla: ServerPlayer.nextContainerCounter
func TestAWindowIDCountsToOneHundredThenWraps(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:hopper", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	for n := 1; n <= 101; n++ {
		w.UseBlock(4, 64, 0)
		w.ExpectContainerOpenType(protocol.MenuHopper, protocol.BlockEntityHopper, 4, 64, 0)
		want := int32((n-1)%100 + 1)
		if w.Window != want {
			t.Fatalf("window %d opened with ID %d, want %d", n, w.Window, want)
		}
		w.CloseWindow()
		w.ExpectWindowZero()
	}
}
