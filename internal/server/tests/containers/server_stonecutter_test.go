package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
)

// The stonecutter's tests: the window its block opens, the recipe its
// button picks out of the list the join sent, and the take that spends one
// input whatever the recipe's output is worth. The recipes themselves are
// the generated table's own answer (see registry/recipebook_gen.go):
// stone's list numbers its chiseled bricks first and its two-cobblestone-
// slab cut third, and the tests cut by those numbers.

// The stonecutter's own slots and the player's hotbar in its window: the
// input at 0, the result at 1, the player's things from 2.
const (
	stoneInputSlot  = int16(0)
	stoneResultSlot = int16(1)
	stoneHotbar0    = int16(2 + (player.HotbarSlot - player.MainSlot))
)

// openStonecutterCreative places the block at (4,64,0) from a creative
// client's hotbar, right-clicks it, and reads the window it opens.
func openStonecutterCreative(t *testing.T, w *servertest.WindowedClient) []protocol.ItemStack {
	t.Helper()
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:stonecutter", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	return w.ExpectContainerOpen(protocol.MenuStonecutter, false, 4, 64, 0)
}

// openStonecutterSurvival places the block at (4,64,0) out of a survival
// player's held stack, which the placement spends, and reads the window
// its right-click opens.
func openStonecutterSurvival(t *testing.T, srv *server.Server, w *servertest.WindowedClient) []protocol.ItemStack {
	t.Helper()
	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:stonecutter", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.SettleAndCollect(func(int32, []byte) {})
	w.UseBlock(4, 64, 0)
	return w.ExpectContainerOpen(protocol.MenuStonecutter, false, 4, 64, 0)
}

// expectSelectedStonecutterProps drains the queued packets into the
// property record and checks the selection the window last told the
// client about.
func expectSelectedStonecutterProps(w *servertest.WindowedClient, selected int32) {
	w.T.Helper()
	props := servertest.CollectWindowProps(w)
	if got, ok := props[server.StonecutterSelectedProperty]; !ok || got != selected {
		w.T.Errorf("the selection property = %d (present %t), want %d", got, ok, selected)
	}
}

// expectNothingCutsStone drains what the server queued after an exchange
// that should have changed nothing, and fails if a window packet - a slot
// correction, a property or the take's sound - says otherwise.
func expectNothingCutsStone(w *servertest.WindowedClient) {
	w.T.Helper()
	w.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlaySoundEffect:
			r := protocol.NewReader(data)
			sound := r.ReadVarInt()
			var name string
			if sound == 0 {
				name = r.ReadString(32767)
			} else {
				name, _ = servertest.SoundEventName(sound - 1)
			}
			if name == "minecraft:ui.stonecutter.take_result" {
				w.T.Error("a take the window had nothing for still played its sound")
			}
		case protocol.IDPlayContainerProperty, protocol.IDPlayContainerSlot:
			w.T.Errorf("a dead window exchange changed the window (packet 0x%02X)", id)
		}
	})
}

// TestTheStonecutterCutsWhatItsButtonsPick right-clicks the block, lays
// three stone in the input, and presses the buttons: the first recipe of
// stone's list cuts chiseled stone bricks, the third the two cobblestone
// slabs, and the result slot shows what the pressed one makes of one
// input. Taking the result spends one stone per take whatever it holds,
// and the input's last piece takes the result with it, the selection
// going empty with them.
func TestTheStonecutterCutsWhatItsButtonsPick(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	slots := openStonecutterCreative(t, w)
	if len(slots) != 2+36 {
		t.Fatalf("the stonecutter window has %d slots, want its 2 and the player's 36", len(slots))
	}
	// The window opens with the selection property the vanilla menu's
	// data slot starts at, telling the client nothing is picked.
	expectSelectedStonecutterProps(w, 0)
	servertest.ForgetWindowProps(w)

	// Three stone into the input slot out of the hotbar.
	stone := servertest.ItemStack(t, "minecraft:stone", 3)
	w.SetCreativeSlot(36, stone)
	w.Click(stoneHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{stoneHotbar0: {}}, stone)
	w.Click(stoneInputSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{stoneInputSlot: stone}, protocol.ItemStack{})
	// The input's item changed, so the selection - none yet - is the
	// client's to hear: the property goes to its -1, nothing picked.
	expectSelectedStonecutterProps(w, -1)
	servertest.ForgetWindowProps(w)

	// The first of stone's recipes: one chiseled stone brick a cut.
	w.ClickButton(0)
	expectCorrectedSlots(w, map[int16]protocol.ItemStack{
		stoneResultSlot: servertest.ItemStack(t, "minecraft:chiseled_stone_bricks", 1),
	})
	expectSelectedStonecutterProps(w, 0)
	servertest.ForgetWindowProps(w)

	// The take: the whole result onto the empty cursor, one stone of the
	// three spent, and the result slot still offering the same cut - no
	// correction for it, since nothing about it moved.
	w.Click(stoneResultSlot, 0, protocol.ClickModePickup)
	expectStonecutterTake(w, map[int16]protocol.ItemStack{
		stoneInputSlot: servertest.ItemStack(t, "minecraft:stone", 2),
	}, servertest.ItemStack(t, "minecraft:chiseled_stone_bricks", 1))
	// A take with input left behind it moves no selection, so the
	// property stays as the window last told it.
	if _, moved := servertest.CollectWindowProps(w)[server.StonecutterSelectedProperty]; moved {
		t.Error("the take moved a selection that still has its input")
	}
	servertest.ForgetWindowProps(w)

	// The third recipe's cut is worth two, and the result slot is
	// re-shown as two cobblestone slabs; the cursor holds the bricks, so
	// the take under it does nothing until they are out of the way.
	w.ClickButton(2)
	expectCorrectedSlots(w, map[int16]protocol.ItemStack{
		stoneResultSlot: servertest.ItemStack(t, "minecraft:cobblestone_slab", 2),
	})
	expectSelectedStonecutterProps(w, 2)
	servertest.ForgetWindowProps(w)
	w.Click(stoneResultSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{}, servertest.ItemStack(t, "minecraft:chiseled_stone_bricks", 1))
	w.Click(stoneHotbar0+1, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		stoneHotbar0 + 1: servertest.ItemStack(t, "minecraft:chiseled_stone_bricks", 1),
	}, protocol.ItemStack{})

	// The two-slab cut taken whole: one stone of the two left spent.
	w.Click(stoneResultSlot, 0, protocol.ClickModePickup)
	expectStonecutterTake(w, map[int16]protocol.ItemStack{
		stoneInputSlot: servertest.ItemStack(t, "minecraft:stone", 1),
	}, servertest.ItemStack(t, "minecraft:cobblestone_slab", 2))

	// The last stone goes with the take that spends it, the result slot
	// emptied and the selection reset while the slabs room beside their
	// own on the cursor.
	w.Click(stoneResultSlot, 0, protocol.ClickModePickup)
	expectStonecutterTake(w, map[int16]protocol.ItemStack{
		stoneInputSlot:  protocol.ItemStack{},
		stoneResultSlot: protocol.ItemStack{},
	}, servertest.ItemStack(t, "minecraft:cobblestone_slab", 4))
	expectSelectedStonecutterProps(w, -1)
	servertest.ForgetWindowProps(w)

	// The empty window gives no more takes and no recipe to pick.
	w.Click(stoneResultSlot, 0, protocol.ClickModePickup)
	expectNothingCutsStone(w)
	w.ClickButton(0)
	expectNothingCutsStone(w)
}

// TestARightClickOnTheStonecuttersResultCutsTheWholeOffer lays two stone
// in the input, picks the two-slab cut, and right-clicks the result: the
// result container's removeItem ignores the amount a right click asks for,
// so the whole offer comes onto the empty cursor, the same take a left
// click makes, one stone of the two spent.
// vanilla: AbstractContainerMenu.doClick PICKUP, ResultContainer.removeItem
func TestARightClickOnTheStonecuttersResultCutsTheWholeOffer(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	openStonecutterCreative(t, w)
	servertest.ForgetWindowProps(w)

	stone := servertest.ItemStack(t, "minecraft:stone", 2)
	w.SetCreativeSlot(36, stone)
	w.Click(stoneHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{stoneHotbar0: {}}, stone)
	w.Click(stoneInputSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{stoneInputSlot: stone}, protocol.ItemStack{})
	servertest.ForgetWindowProps(w)

	// The third of stone's recipes is the two-cobblestone-slab cut.
	w.ClickButton(2)
	expectCorrectedSlots(w, map[int16]protocol.ItemStack{
		stoneResultSlot: servertest.ItemStack(t, "minecraft:cobblestone_slab", 2),
	})
	servertest.ForgetWindowProps(w)

	// The right click hands the whole two-slab offer over, one stone of
	// the two spent, the cut still offered for the stone left.
	w.Click(stoneResultSlot, 1, protocol.ClickModePickup)
	expectStonecutterTake(w, map[int16]protocol.ItemStack{
		stoneInputSlot: servertest.ItemStack(t, "minecraft:stone", 1),
	}, servertest.ItemStack(t, "minecraft:cobblestone_slab", 2))
	if _, moved := servertest.CollectWindowProps(w)[server.StonecutterSelectedProperty]; moved {
		t.Error("the take moved a selection that still has its input")
	}
}

// TestShiftClickingTheStonecuttersResultCutsAllTheStone lays stone in the
// input of a survival window and shift-clicks the result: the cut moves
// into the player's things once per stone, hotbar-end first, for as long
// as the input has a piece to spend, so the whole stack is spent by the
// one click and the window is left holding nothing.
func TestShiftClickingTheStonecuttersResultCutsAllTheStone(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

	slots := openStonecutterSurvival(t, srv, w)
	if len(slots) != 2+36 {
		t.Fatalf("the stonecutter window has %d slots, want its 2 and the player's 36", len(slots))
	}

	// Two stone into the input out of the hotbar.
	stone := servertest.ItemStack(t, "minecraft:stone", 2)
	srv.GiveSlot("Testificate", player.HotbarSlot, stone)
	w.Drain()
	w.Click(stoneHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{stoneHotbar0: {}}, stone)
	w.Click(stoneInputSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{stoneInputSlot: stone}, protocol.ItemStack{})
	servertest.ForgetWindowProps(w)

	// The shift-click on the picked recipe: one cut once per stone, into
	// the hotbar's farthest slot - both craft's outputs merging into the
	// first stack the vanilla move fills - the input empty and the result
	// with it, all corrected, the selection reset, the sound once.
	w.ClickButton(0)
	expectCorrectedSlots(w, map[int16]protocol.ItemStack{
		stoneResultSlot: servertest.ItemStack(t, "minecraft:chiseled_stone_bricks", 1),
	})
	servertest.ForgetWindowProps(w)
	w.Click(stoneResultSlot, 0, protocol.ClickModeQuickMove)
	expectStonecutterTake(w, map[int16]protocol.ItemStack{
		stoneInputSlot:   protocol.ItemStack{},
		stoneResultSlot:  protocol.ItemStack{},
		stoneHotbar0 + 8: servertest.ItemStack(t, "minecraft:chiseled_stone_bricks", 2),
	}, protocol.ItemStack{})
	expectSelectedStonecutterProps(w, -1)
}

// TestCtrlQThrowsTheStonecuttersWholeOfferUntilTheInputRunsOut lays two
// stone in the input, picks the two-slab cut, and sends Ctrl+Q on the
// result: the whole offer leaves once per stone, four slabs in two drops,
// and the window is emptied and its selection reset.
func TestCtrlQThrowsTheStonecuttersWholeOfferUntilTheInputRunsOut(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	openStonecutterCreative(t, w)
	servertest.ForgetWindowProps(w)

	stone := servertest.ItemStack(t, "minecraft:stone", 2)
	w.SetCreativeSlot(36, stone)
	w.Click(stoneHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{stoneHotbar0: {}}, stone)
	w.Click(stoneInputSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{stoneInputSlot: stone}, protocol.ItemStack{})
	servertest.ForgetWindowProps(w)

	// The third of stone's recipes is the two-cobblestone-slab cut.
	w.ClickButton(2)
	expectCorrectedSlots(w, map[int16]protocol.ItemStack{
		stoneResultSlot: servertest.ItemStack(t, "minecraft:cobblestone_slab", 2),
	})
	servertest.ForgetWindowProps(w)

	w.Click(stoneResultSlot, 1, protocol.ClickModeThrow)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		stoneInputSlot:  protocol.ItemStack{},
		stoneResultSlot: protocol.ItemStack{},
	}, protocol.ItemStack{})
	slabs := servertest.ItemStack(t, "minecraft:cobblestone_slab", 2)
	w.ExpectDrop(slabs)
	w.ExpectDrop(slabs)
	expectSelectedStonecutterProps(w, -1)
}

// TestAPlainQThrowsTheStonecuttersWholeOffer lays two stone in the input,
// picks the two-slab cut, and sends a plain Q on the result: the result
// container's removeItem ignores the amount a plain Q asks for, so the
// whole offer leaves in one drop, spending one stone, and the cut stays on
// offer for the stone left.
func TestAPlainQThrowsTheStonecuttersWholeOffer(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	openStonecutterCreative(t, w)
	servertest.ForgetWindowProps(w)

	stone := servertest.ItemStack(t, "minecraft:stone", 2)
	w.SetCreativeSlot(36, stone)
	w.Click(stoneHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{stoneHotbar0: {}}, stone)
	w.Click(stoneInputSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{stoneInputSlot: stone}, protocol.ItemStack{})
	servertest.ForgetWindowProps(w)

	// The third of stone's recipes is the two-cobblestone-slab cut.
	w.ClickButton(2)
	expectCorrectedSlots(w, map[int16]protocol.ItemStack{
		stoneResultSlot: servertest.ItemStack(t, "minecraft:cobblestone_slab", 2),
	})
	servertest.ForgetWindowProps(w)

	w.Click(stoneResultSlot, 0, protocol.ClickModeThrow)
	expectStonecutterTake(w, map[int16]protocol.ItemStack{
		stoneInputSlot: servertest.ItemStack(t, "minecraft:stone", 1),
	}, protocol.ItemStack{})
	w.ExpectDrop(servertest.ItemStack(t, "minecraft:cobblestone_slab", 2))
	// A take with input left behind it moves no selection.
	if _, moved := servertest.CollectWindowProps(w)[server.StonecutterSelectedProperty]; moved {
		t.Error("the plain Q moved a selection that still has its input")
	}
}

// TestStonecutterButtonPressesOutsideItsRecipesChangeNothing presses a
// number the input's list does not hold and a repeat of the pick already
// made, and checks the window answers both with nothing: no property, no
// inline slot, no sound, the connection still good behind them. Closing
// the window with stone still in the input hands the stone back.
func TestStonecutterButtonPressesOutsideItsRecipesChangeNothing(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalConfig())

	openStonecutterSurvival(t, srv, w)
	servertest.ForgetWindowProps(w)

	// Stone enough for a look at the buttons, laid in the input.
	stone := servertest.ItemStack(t, "minecraft:stone", 2)
	srv.GiveSlot("Testificate", player.HotbarSlot, stone)
	w.Drain()
	w.Click(stoneHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{stoneHotbar0: {}}, stone)
	w.Click(stoneInputSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{stoneInputSlot: stone}, protocol.ItemStack{})
	servertest.ForgetWindowProps(w)

	// Stone's list numbers eleven recipes; a number past them answers
	// nothing, negative nothing too.
	for _, button := range []int32{11, -1} {
		w.ClickButton(button)
		expectNothingCutsStone(w)
	}

	// The pick the window then makes - and its repeat, which answers
	// the same nothing: the selection is already what the press names.
	w.ClickButton(0)
	expectCorrectedSlots(w, map[int16]protocol.ItemStack{
		stoneResultSlot: servertest.ItemStack(t, "minecraft:chiseled_stone_bricks", 1),
	})
	expectSelectedStonecutterProps(w, 0)
	servertest.ForgetWindowProps(w)
	w.ClickButton(0)
	expectNothingCutsStone(w)

	// The window behind the presses that bought nothing still works.
	w.Chat("still here")
	w.ExpectChat("Testificate", "still here")

	// Closing the window with stone still laid in the input hands it
	// back, a stone never spent on a cut the window did not make.
	w.CloseWindow()
	w.ExpectWindowZero()
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Equal(stone) {
		t.Errorf("after close, the hotbar holds %+v, want the %v the input handed back", got, stone)
	}
}

func TestTakingAStonecutterResultUnlocksItsRecipe(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	openStonecutterCreative(t, w)
	servertest.HoldTicks(t, srv)
	stone := servertest.ItemStack(t, "minecraft:stone", 1)
	w.SetCreativeSlot(36, stone)
	w.Click(stoneHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{stoneHotbar0: {}}, stone)
	w.Click(stoneInputSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{stoneInputSlot: stone}, protocol.ItemStack{})
	w.ClickButton(0)
	expectCorrectedSlots(w, map[int16]protocol.ItemStack{
		stoneResultSlot: servertest.ItemStack(t, "minecraft:chiseled_stone_bricks", 1),
	})
	w.ChatCommand("recipe take @s minecraft:chiseled_stone_bricks_from_stone_stonecutting")
	w.SettleTrackingState()
	w.WatchBook = true
	w.Click(stoneResultSlot, 0, protocol.ClickModePickup)
	data := w.ExpectWithinTicks(srv, 5, protocol.IDPlayRecipeBookAdd)
	r := protocol.NewReader(data)
	if n := r.ReadVarInt(); n != 1 {
		t.Fatalf("take unlocked %d recipes, want 1", n)
	}
	r.ReadVarInt()
	if kind := r.ReadVarInt(); kind != 3 {
		t.Errorf("display type = %d, want stonecutter (3)", kind)
	}
	if flags := data[len(data)-2]; flags != 3 {
		t.Errorf("entry flags = %d, want notified and highlighted", flags)
	}
}
