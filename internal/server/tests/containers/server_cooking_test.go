package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/registry"
)

// The blast furnace, the smoker, the brewing stand and the crafter: the four
// container kinds that had no block entity. The two cooking ones run their
// own recipe tables at their own speed, which is what separates them from
// the plain furnace; the brewing stand and the crafter open windows over
// their own slots.

// loadCooking places one of the cooking blocks at (4, 64, 0), opens the
// window its menu names, shift-clicks the input into the ingredient slot and
// the coal into the fuel slot, and closes the window again, so the ticks
// that follow have nobody to send their progress to.
func loadCooking(t *testing.T, srv *server.Server, w *servertest.WindowedClient, block string, menu, beType int32, input string, count, coal int32) {
	t.Helper()
	w.SetCreativeSlot(36, servertest.ItemStack(t, block, 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.SettleKeeping()
	srv.FlushBlockAcks()
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpenType(menu, beType, 4, 64, 0)

	w.SetCreativeSlot(36, servertest.ItemStack(t, input, count))
	w.Click(30, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		0:  servertest.ItemStack(t, input, count),
		30: protocol.ItemStack{},
	}, protocol.ItemStack{})
	w.SetCreativeSlot(37, servertest.ItemStack(t, "minecraft:coal", coal))
	w.Click(31, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1:  servertest.ItemStack(t, "minecraft:coal", coal),
		31: protocol.ItemStack{},
	}, protocol.ItemStack{})

	w.CloseWindow()
	w.ExpectWindowZero()
}

// TestABlastFurnaceRunsItsOwnRecipeAtDoubleSpeed cooks one raw iron in a
// blast furnace and reads the block entity back: the blasting recipe the
// data pack saves raw iron under, and its two hundred ticks cooked in a
// hundred - a fuel burns at twice the speed there - with the ingot only there
// once the hundredth tick has run.
func TestABlastFurnaceRunsItsOwnRecipeAtDoubleSpeed(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.StopTheLoop(t, srv)
	loadCooking(t, srv, w, "minecraft:blast_furnace", protocol.MenuBlastFurnace,
		protocol.BlockEntityBlastFurnace, "minecraft:raw_iron", 1, 1)

	// The ninety-nine ticks the plain furnace would still be cooking on:
	// nothing has come out of the blast furnace's result slot yet.
	runTheSmelts(t, srv, 99)
	if got := srv.InventoryAt(4, 64, 0); len(got) != 3 || !got[2].Empty() {
		t.Errorf("after 99 ticks the blast furnace output = %+v, want it still empty", got)
	}

	// The hundredth tick finishes the blasting recipe.
	runTheSmelts(t, srv, 1)
	used, ok := srv.FurnaceRecipesUsed(4, 64, 0)
	if !ok {
		t.Fatal("the blast furnace's block entity went missing")
	}
	const blasting = "minecraft:iron_ingot_from_blasting_raw_iron"
	if len(used) != 1 || used[blasting] != 1 {
		t.Errorf("the blast furnace's smelting history = %v, want one of %s", used, blasting)
	}
	ingot := servertest.ItemStack(t, "minecraft:iron_ingot", 1)
	if got := srv.InventoryAt(4, 64, 0); len(got) != 3 || !got[2].Equal(ingot) {
		t.Errorf("the blast furnace output = %+v, want one iron ingot", got)
	}
}

// TestASmokerRunsItsOwnRecipeAtDoubleSpeed cooks one raw beef in a smoker:
// the smoking recipe the data pack saves beef under, its two hundred ticks
// done in a hundred.
func TestASmokerRunsItsOwnRecipeAtDoubleSpeed(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.StopTheLoop(t, srv)
	loadCooking(t, srv, w, "minecraft:smoker", protocol.MenuSmoker,
		protocol.BlockEntitySmoker, "minecraft:beef", 1, 1)

	runTheSmelts(t, srv, 99)
	if got := srv.InventoryAt(4, 64, 0); len(got) != 3 || !got[2].Empty() {
		t.Errorf("after 99 ticks the smoker output = %+v, want it still empty", got)
	}

	runTheSmelts(t, srv, 1)
	used, ok := srv.FurnaceRecipesUsed(4, 64, 0)
	if !ok {
		t.Fatal("the smoker's block entity went missing")
	}
	const smoking = "minecraft:cooked_beef_from_smoking"
	if len(used) != 1 || used[smoking] != 1 {
		t.Errorf("the smoker's smelting history = %v, want one of %s", used, smoking)
	}
	steak := servertest.ItemStack(t, "minecraft:cooked_beef", 1)
	if got := srv.InventoryAt(4, 64, 0); len(got) != 3 || !got[2].Equal(steak) {
		t.Errorf("the smoker output = %+v, want one cooked beef", got)
	}
}

// TestTheBrewingStandOpensItsFiveSlots places a brewing stand and opens it:
// the brewing_stand menu with its five block-entity slots and the player's
// things behind them.
func TestTheBrewingStandOpensItsFiveSlots(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:brewing_stand", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	slots := w.ExpectContainerOpenType(protocol.MenuBrewingStand, protocol.BlockEntityBrewingStand, 4, 64, 0)
	if len(slots) != 5+36 {
		w.T.Errorf("the brewing stand window has %d slots, want its 5 and the player's 36", len(slots))
	}
	for i, stack := range slots[:5] {
		if !stack.Empty() {
			w.T.Errorf("brewing stand slot %d = %+v, want nothing in it yet", i, stack)
		}
	}

	// The loop stops before the powder goes in: the tick would load it as
	// fuel power and empty the fuel slot behind the click's own correction.
	srv.StopTick()

	// Blaze powder laid in the fuel slot, slot 4.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:blaze_powder", 3))
	w.Click(5+27, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{5 + 27: {}}, servertest.ItemStack(t, "minecraft:blaze_powder", 3))
	w.Click(4, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{4: servertest.ItemStack(t, "minecraft:blaze_powder", 3)}, protocol.ItemStack{})
}

// smeltedBlastFurnace runs ten raw iron through a blast furnace at (4,64,0):
// the blasting recipe counted ten times, ten ingots in the result, and fuel
// still burning. A coal burns half its time there, eight smelts' worth, so
// the ten take a second one.
func smeltedBlastFurnace(t *testing.T, srv *server.Server, w *servertest.WindowedClient) {
	t.Helper()
	servertest.StopTheLoop(t, srv)
	loadCooking(t, srv, w, "minecraft:blast_furnace", protocol.MenuBlastFurnace,
		protocol.BlockEntityBlastFurnace, "minecraft:raw_iron", 10, 2)
	runTheSmelts(t, srv, 10*100+2)

	const blasting = "minecraft:iron_ingot_from_blasting_raw_iron"
	used, ok := srv.FurnaceRecipesUsed(4, 64, 0)
	if !ok {
		t.Fatal("the blast furnace's block entity went missing")
	}
	if len(used) != 1 || used[blasting] != 10 {
		t.Errorf("the blast furnace's smelting history = %v, want ten of the blasting recipe", used)
	}
	if got := srv.InventoryAt(4, 64, 0); len(got) != 3 || !got[2].Equal(servertest.ItemStack(t, "minecraft:iron_ingot", 10)) {
		t.Errorf("the blast furnace's output = %+v, want ten ingots", got)
	}
}

// openBrewingStand places a brewing stand at (4,64,0) and opens it.
func openBrewingStand(t *testing.T, srv *server.Server, w *servertest.WindowedClient) []protocol.ItemStack {
	t.Helper()
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:brewing_stand", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.SettleKeeping()
	srv.FlushBlockAcks()
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	return w.ExpectContainerOpenType(protocol.MenuBrewingStand, protocol.BlockEntityBrewingStand, 4, 64, 0)
}

// potionBottle is the bottle of a potion: one stack of the item with a
// potion_contents component naming the potion. The component's bytes are
// written by hand in the wire's own order, so what a brew produces is
// checked against the codec rather than against the server's own writer.
func PotionBottle(t *testing.T, item, potion string) protocol.ItemStack {
	t.Helper()
	var contents protocol.Buffer
	contents.PutBool(true)
	contents.PutVarInt(registry.MustPotion(potion))
	contents.PutBool(false) // no custom colour
	contents.PutVarInt(0)   // no custom effects
	contents.PutBool(false) // no custom name
	return protocol.ItemStack{
		ItemID: servertest.ItemID(t, item),
		Count:  1,
		Raw: []protocol.RawComponent{{
			Type: protocol.DataComponentPotionContents,
			Data: contents.Bytes(),
		}},
	}
}

// placeBrewingStand places a brewing stand at (4,64,0) with the tick loop
// stopped, so the ticks that follow are the ones the test runs by hand.
func placeBrewingStand(t *testing.T, srv *server.Server, w *servertest.WindowedClient) {
	t.Helper()
	servertest.StopTheLoop(t, srv)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:brewing_stand", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.SettleKeeping()
	srv.FlushBlockAcks()
	w.ExpectBlockAck()
}

// standSlot puts a stack into one of the brewing stand at (4,64,0)'s
// slots, the way a click into it would leave it.
func standSlot(t *testing.T, srv *server.Server, slot int, stack protocol.ItemStack) {
	t.Helper()
	if !srv.SetInventoryAt(4, 64, 0, slot, stack) {
		t.Fatalf("could not fill the brewing stand's slot %d", slot)
	}
}

// loadBrewingStand fills a placed brewing stand with a water bottle, a
// nether wart and one blaze powder, the awkward potion's brew.
func loadBrewingStand(t *testing.T, srv *server.Server) {
	t.Helper()
	standSlot(t, srv, 0, servertest.PotionBottle(t, "minecraft:potion", "minecraft:water"))
	standSlot(t, srv, 3, servertest.ItemStack(t, "minecraft:nether_wart", 1))
	standSlot(t, srv, 4, servertest.ItemStack(t, "minecraft:blaze_powder", 1))
}

// TestABrewingStandBrewsAnAwkwardPotion runs a brewing stand's own tick on
// a stand loaded with a water bottle, a nether wart and one blaze powder:
// the powder loads as twenty fuel power, the brew the wart fits starts at
// four hundred ticks, and its last tick pours the awkward potion into the
// bottle, spends the wart, and leaves the bottle drawn on the block.
func TestABrewingStandBrewsAnAwkwardPotion(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	placeBrewingStand(t, srv, w)
	loadBrewingStand(t, srv)

	// The first tick loads the powder and starts the brew in the same
	// breath: twenty fuel power, one spent starting, the countdown at
	// four hundred, and the powder's slot spent.
	servertest.TickBlockEntitiesByHand(srv, 1)
	brewTime, fuel, ok := srv.BrewingAt(4, 64, 0)
	if !ok {
		t.Fatal("the brewing stand's block entity went missing")
	}
	if brewTime != 400 || fuel != 19 {
		t.Errorf("after the first tick, the brew = (%d ticks left, %d fuel power), want (400, 19)",
			brewTime, fuel)
	}
	if got := srv.InventoryAt(4, 64, 0); len(got) != 5 || !got[4].Empty() {
		t.Errorf("the stand holds %+v, want the blaze powder loaded as fuel", got)
	}
	// The bottle went in before any brew: the first tick draws it on the
	// block as has_bottle_0.
	if _, props := servertest.StatePropsOf(t, srv, 4, 64, 0); props["has_bottle_0"] != "true" {
		t.Error("the brewing stand's block does not draw its bottle")
	}

	// One tick short of the end: the wart is not spent yet, and the
	// countdown has fallen a tick a turn.
	servertest.TickBlockEntitiesByHand(srv, 399)
	brewTime, fuel, _ = srv.BrewingAt(4, 64, 0)
	if brewTime != 1 || fuel != 19 {
		t.Errorf("after 400 ticks, the brew = (%d ticks left, %d fuel power), want (1, 19)",
			brewTime, fuel)
	}

	// The four hundred and first tick pours the potion: the bottle now
	// brews the awkward potion, the wart spent, the fuel power kept, the
	// potion's brew event played at the stand, and no brew behind it.
	servertest.TickBlockEntitiesByHand(srv, 1)
	w.AwaitWindowPackets(func(id int32, data []byte) bool {
		if id != protocol.IDPlayWorldEvent {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadI32() != protocol.WorldEventBrewing {
			return false
		}
		x, y, z := r.ReadPosition()
		if x != 4 || y != 64 || z != 0 {
			w.T.Errorf("the brew event played at (%d,%d,%d), want the stand", x, y, z)
		}
		return true
	})
	awkward := servertest.PotionBottle(t, "minecraft:potion", "minecraft:awkward")
	got := srv.InventoryAt(4, 64, 0)
	if len(got) != 5 || !got[0].Equal(awkward) {
		t.Errorf("the brewed stand holds %+v, want the awkward potion in slot 0", got)
	}
	for slot, stack := range got[1:] {
		if !stack.Empty() {
			t.Errorf("slot %d = %+v, want the ingredient and the fuel spent and no other bottle", slot+1, stack)
		}
	}
	if brewTime, fuel, _ := srv.BrewingAt(4, 64, 0); brewTime != 0 || fuel != 19 {
		t.Errorf("after the brew, the stand = (%d ticks left, %d fuel power), want (0, 19)",
			brewTime, fuel)
	}
	if _, props := servertest.StatePropsOf(t, srv, 4, 64, 0); props["has_bottle_0"] != "true" {
		t.Error("the brewed bottle is not drawn on the block")
	}

	// Nothing restarts on its own: no ingredient is left.
	servertest.TickBlockEntitiesByHand(srv, 5)
	if brewTime, fuel, _ := srv.BrewingAt(4, 64, 0); brewTime != 0 || fuel != 19 {
		t.Errorf("after the brew, five more ticks moved it to (%d, %d), want no second brew",
			brewTime, fuel)
	}
}

// openCrafter places a crafter at (4,64,0) and opens it.
func openCrafter(t *testing.T, w *servertest.WindowedClient) []protocol.ItemStack {
	t.Helper()
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:crafter", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	return openCrafterWindow(t, w)
}

// openCrafterWindow right-clicks a crafter already standing at (4,64,0) and
// opens it, which is how a test reopens one a restart brought back.
func openCrafterWindow(t *testing.T, w *servertest.WindowedClient) []protocol.ItemStack {
	t.Helper()
	w.UseBlock(4, 64, 0)
	return w.ExpectContainerOpenType(protocol.MenuCrafter, protocol.BlockEntityCrafter, 4, 64, 0)
}

// toggleCrafterSlot sends the slot-state-changed packet disabling one grid
// slot, the way a client clicking an empty cell does.
func toggleCrafterSlot(w *servertest.WindowedClient, slot int32, enabled bool) {
	w.Send(protocol.IDPlayContainerSlotStateChanged, func(b *protocol.Buffer) {
		b.PutVarInt(slot)
		b.PutVarInt(w.Window)
		b.PutBool(enabled)
	})
}

// TestAHopperSpreadsIntoACrafter fills a crafter's grid with dirt of unequal
// counts and cobblestone elsewhere, then has a hopper add one dirt: the even
// spread puts it on the smallest dirt stack rather than filling slot by slot.
func TestAHopperSpreadsIntoACrafter(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:crafter", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:hopper", 1))
	w.Sneak(true)
	w.PlaceBlock(4, 64, 0, protocol.FaceTop)
	w.Sneak(false)
	w.ExpectBlockAck()
	srv.StopTick()

	dirt := servertest.ItemStack(t, "minecraft:dirt", 1)
	cobble := servertest.ItemStack(t, "minecraft:cobblestone", 1)
	srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 3))
	srv.SetInventoryAt(4, 64, 0, 1, dirt)
	for slot := 2; slot < 9; slot++ {
		srv.SetInventoryAt(4, 64, 0, slot, cobble)
	}
	srv.SetInventoryAt(4, 65, 0, 0, dirt)

	servertest.TickBlockEntitiesByHand(srv, 1)
	got := srv.InventoryAt(4, 64, 0)
	if len(got) != 9 || got[1].Count != 2 {
		t.Errorf("the crafter grid = %+v, want the dirt evened onto the count-one slot 1", got)
	}
	if got[0].Count != 3 {
		t.Errorf("crafter slot 0 = %d dirt, want the 3 it held", got[0].Count)
	}
}
