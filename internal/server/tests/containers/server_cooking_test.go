package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
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

// TestABlastFurnaceOnlyTakesWhatBlasts checks the recipe table a blast
// furnace matches against: raw beef smelts and smokes but does not blast, so
// a blast furnace loaded with it lights its fuel and never finishes, while
// its shift-click routing keeps the beef out of the ingredient slot.
func TestABlastFurnaceOnlyTakesWhatBlasts(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.StopTheLoop(t, srv)

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:blast_furnace", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.SettleKeeping()
	srv.FlushBlockAcks()
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpenType(protocol.MenuBlastFurnace, protocol.BlockEntityBlastFurnace, 4, 64, 0)

	// A shift-clicked beef is not something a blast furnace cooks, so the
	// vanilla move swaps it from the hotbar to the main inventory rather
	// than laying it in the ingredient slot.
	beef := servertest.ItemStack(t, "minecraft:beef", 1)
	w.SetCreativeSlot(36, beef)
	w.Click(30, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		3:  beef,
		30: {},
	}, protocol.ItemStack{})
	if got := srv.InventoryAt(4, 64, 0); len(got) != 3 || !got[0].Empty() {
		t.Errorf("the blast furnace's ingredient slot = %+v, want the beef left out of it", got)
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

// TestTheCrafterOpensItsGridAndResult places a crafter and opens it: the
// crafter_3x3 menu whose own slots are the 3x3 grid, followed by the
// player's things and then the result the grid is worth.
func TestTheCrafterOpensItsGridAndResult(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:crafter", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	slots := w.ExpectContainerOpenType(protocol.MenuCrafter, protocol.BlockEntityCrafter, 4, 64, 0)
	// The crafter's 3x3, the player's 36, and the result after them.
	if len(slots) != 9+36+1 {
		w.T.Errorf("the crafter window has %d slots, want its 9, the player's 36 and the result", len(slots))
	}
	for i, stack := range slots[:9] {
		if !stack.Empty() {
			w.T.Errorf("crafter grid slot %d = %+v, want nothing in it yet", i, stack)
		}
	}

	// A plank laid in a grid cell updates the result the window shows: one
	// plank in the grid is worth a button.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:oak_planks", 1))
	w.Click(9+27, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{9 + 27: {}}, servertest.ItemStack(t, "minecraft:oak_planks", 1))
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		0:  servertest.ItemStack(t, "minecraft:oak_planks", 1),
		45: servertest.ItemStack(t, "minecraft:oak_button", 1),
	}, protocol.ItemStack{})

	// The result is the crafter's preview: clicking it gives nothing.
	w.Click(45, 0, protocol.ClickModePickup)
	w.ExpectClickResult(nil, protocol.ItemStack{})
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

// TestABlastFurnacesCookingIsSaved smelts ten raw iron in a blast furnace,
// saves the world with the ingots still in it, and restarts: the progress
// fields and the smelting history come back out of the region file rather
// than a fresh furnace's zeros.
func TestABlastFurnacesCookingIsSaved(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	srv, w := servertest.OpenWindowed(t, cfg)
	smeltedBlastFurnace(t, srv, w)
	spent, total, lit, litTotal, ok := srv.FurnaceAt(4, 64, 0)
	if !ok || total == 0 || litTotal == 0 {
		t.Fatalf("the blast furnace's cooking = (%d,%d,%d,%d), want its progress", spent, total, lit, litTotal)
	}
	w.Disconnect()
	srv.Shutdown()

	back, _ := servertest.OpenWindowed(t, cfg)
	spent2, total2, _, litTotal2, ok := back.FurnaceAt(4, 64, 0)
	if !ok {
		t.Fatal("the blast furnace's block entity did not come back with the chunk")
	}
	if spent2 != spent || total2 != total || litTotal2 != litTotal {
		t.Errorf("after the restart, the blast furnace's cooking = (%d,%d,%d), want (%d,%d,%d)",
			spent2, total2, litTotal2, spent, total, litTotal)
	}
	if _, _, lit2, _, _ := back.FurnaceAt(4, 64, 0); lit2 <= 0 {
		t.Errorf("after the restart, the blast furnace has no fuel left burning")
	}
	const blasting = "minecraft:iron_ingot_from_blasting_raw_iron"
	if used, ok := back.FurnaceRecipesUsed(4, 64, 0); !ok || len(used) != 1 || used[blasting] != 10 {
		t.Errorf("after the restart, the blast furnace's smelting history = %v, want ten of the blasting recipe", used)
	}
}

// TestABlastFurnacePaysItsExperienceWhenTheResultIsTaken takes ten ingots out
// of a blast furnace's result slot: taking from a cooking block's result pays
// the smelts it remembers, seven points here the way a furnace's ten raw iron
// pay.
func TestABlastFurnacePaysItsExperienceWhenTheResultIsTaken(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	smeltedBlastFurnace(t, srv, w)

	w.UseBlock(4, 64, 0)
	slots := w.ExpectContainerOpenType(protocol.MenuBlastFurnace, protocol.BlockEntityBlastFurnace, 4, 64, 0)
	if got := slots[2]; !got.Equal(servertest.ItemStack(t, "minecraft:iron_ingot", 10)) {
		w.T.Errorf("the reopened window's result slot = %+v, want the ten ingots", got)
	}
	w.Click(2, 0, protocol.ClickModePickup)
	sightPayout(srv, t, w, 7, 0.5, 64, 0.5)
	if used, _ := srv.FurnaceRecipesUsed(4, 64, 0); len(used) != 0 {
		t.Errorf("after the take, the blast furnace still owes %v", used)
	}
}

// TestABlastPaysABlastFurnacesExperience destroys a loaded blast furnace with
// a blast: the experience it owed is paid at the block's centre the way a
// broken furnace's is.
func TestABlastPaysABlastFurnacesExperience(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	smeltedBlastFurnace(t, srv, w)

	srv.RunExplosion(4.5, 64.5, 0.5, 4)
	sightPayout(srv, t, w, 7, 4.5, 64.5, 0.5)
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

// TestTheBrewingStandTakesOnlyWhatItBrews shift-clicks one of each of the
// four kinds of item the stand's slots are for: a potion into a bottle slot,
// an ingredient into slot 3 and the fuel into slot 4, while anything else - a
// dirt - moves between the player's things rather than into a bottle slot.
func TestTheBrewingStandTakesOnlyWhatItBrews(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.StopTheLoop(t, srv)
	openBrewingStand(t, srv, w)

	// The held hotbar slot is window slot 32 in the brewing stand's menu.
	dirt := servertest.ItemStack(t, "minecraft:dirt", 1)
	w.SetCreativeSlot(36, dirt)
	w.Click(32, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{5: dirt, 32: {}}, protocol.ItemStack{})

	potion := servertest.ItemStack(t, "minecraft:potion", 1)
	w.SetCreativeSlot(36, potion)
	w.Click(32, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: potion, 32: {}}, protocol.ItemStack{})

	wart := servertest.ItemStack(t, "minecraft:nether_wart", 1)
	w.SetCreativeSlot(36, wart)
	w.Click(32, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{3: wart, 32: {}}, protocol.ItemStack{})

	blaze := servertest.ItemStack(t, "minecraft:blaze_powder", 1)
	w.SetCreativeSlot(36, blaze)
	w.Click(32, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{4: blaze, 32: {}}, protocol.ItemStack{})
}

// TestTheBrewingStandTellsItsWindowProperties opens a brewing stand and
// checks the four numbers its window draws itself with: the brew time and the
// fuel, each with its full mark, all zero before any brewing.
func TestTheBrewingStandTellsItsWindowProperties(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	openBrewingStand(t, srv, w)
	w.AwaitWindowProperty(protocol.BrewingPropertyBrewTime, 0)
	w.AwaitWindowProperty(protocol.BrewingPropertyFuel, 0)
	w.AwaitWindowProperty(protocol.BrewingPropertyBrewTotal, 0)
	w.AwaitWindowProperty(protocol.BrewingPropertyFuelTotal, 0)
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

// TestABrewingStandRestartsWhenItsIngredientChanges swaps the ingredient
// mid-brew: the countdown the nether wart began falls back to nothing, and
// the sugar starts its own four hundred ticks the tick after, spending
// another of the fuel power the first brew loaded.
func TestABrewingStandRestartsWhenItsIngredientChanges(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	placeBrewingStand(t, srv, w)
	loadBrewingStand(t, srv)
	servertest.TickBlockEntitiesByHand(srv, 5) // the wart's brew under way, at 396

	standSlot(t, srv, 3, servertest.ItemStack(t, "minecraft:sugar", 1))
	servertest.TickBlockEntitiesByHand(srv, 1)
	brewTime, fuel, ok := srv.BrewingAt(4, 64, 0)
	if !ok {
		t.Fatal("the brewing stand's block entity went missing")
	}
	if brewTime != 0 || fuel != 19 {
		t.Errorf("after the swap, the brew = (%d ticks left, %d fuel power), want the countdown reset with the fuel kept",
			brewTime, fuel)
	}

	// The sugar fits the water bottle too, so its own brew starts the
	// tick after the reset, and runs to the mundane potion.
	servertest.TickBlockEntitiesByHand(srv, 1)
	if brewTime, _, _ := srv.BrewingAt(4, 64, 0); brewTime != 400 {
		t.Errorf("the sugar's brew = %d ticks left, want it started at 400", brewTime)
	}
	servertest.TickBlockEntitiesByHand(srv, 400)
	mundane := servertest.PotionBottle(t, "minecraft:potion", "minecraft:mundane")
	if got := srv.InventoryAt(4, 64, 0); len(got) != 5 || !got[0].Equal(mundane) {
		t.Errorf("the stand holds %+v, want the mundane potion the sugar brewed", got)
	}
	if _, fuel, _ := srv.BrewingAt(4, 64, 0); fuel != 18 {
		t.Errorf("the fuel power after two brews = %d, want 18", fuel)
	}
}

// TestTheBrewingStandsWindowCountsTheBrewDown opens the stand's window and
// checks the two numbers it draws itself: the fuel power the first brew
// spends, the four-hundred countdown falling one a tick, and the reset to
// nothing the tick the ingredient is swapped, after which the countdown the
// sugar fits starts over.
func TestTheBrewingStandsWindowCountsTheBrewDown(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.StopTheLoop(t, srv)
	openBrewingStand(t, srv, w)
	w.AwaitWindowProperty(protocol.BrewingPropertyFuel, 0)
	if got := w.Props[protocol.BrewingPropertyBrewTime]; got != 0 {
		t.Errorf("brewing brew time = %d, want 0", got)
	}

	loadBrewingStand(t, srv)

	// The powder loads and the brew starts on the same tick: the powder's
	// slot told empty, nineteen fuel power of the powder's twenty, and a
	// four-hundred countdown of four hundred.
	srv.TickBlockEntities()
	w.AwaitWindowSlot(4, protocol.ItemStack{})
	w.AwaitWindowProperty(protocol.BrewingPropertyBrewTime, 400)
	w.AwaitWindowProperty(protocol.BrewingPropertyFuel, 19)
	w.AwaitWindowProperty(protocol.BrewingPropertyBrewTotal, 400)
	w.AwaitWindowProperty(protocol.BrewingPropertyFuelTotal, 20)

	srv.TickBlockEntities()
	w.AwaitWindowProperty(protocol.BrewingPropertyBrewTime, 399)
	srv.TickBlockEntities()
	w.AwaitWindowProperty(protocol.BrewingPropertyBrewTime, 398)

	// The ingredient swapped mid-brew: the countdown falls to nothing.
	standSlot(t, srv, 3, servertest.ItemStack(t, "minecraft:sugar", 1))
	srv.TickBlockEntities()
	w.AwaitWindowProperty(protocol.BrewingPropertyBrewTime, 0)

	// The sugar fits, so its own countdown starts the tick after,
	// spending another fuel power.
	srv.TickBlockEntities()
	w.AwaitWindowProperty(protocol.BrewingPropertyBrewTime, 400)
	w.AwaitWindowProperty(protocol.BrewingPropertyFuel, 18)
	srv.TickBlockEntities()
	w.AwaitWindowProperty(protocol.BrewingPropertyBrewTime, 399)
}

// TestABrewingStandStaysPutWithoutFuelOrABottle ticks a stand whose brew
// is missing a piece: nothing happens with no blaze powder to load, and
// with nothing for a mix to start from the fuel loads all the same - the
// loader runs whatever the slots hold - but no countdown starts until one
// of the bottles fits a mix.
func TestABrewingStandStaysPutWithoutFuelOrABottle(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	placeBrewingStand(t, srv, w)

	// A bottle and an ingredient with no fuel: three ticks of nothing.
	waterBottle := servertest.PotionBottle(t, "minecraft:potion", "minecraft:water")
	wart := servertest.ItemStack(t, "minecraft:nether_wart", 1)
	standSlot(t, srv, 0, waterBottle)
	standSlot(t, srv, 3, wart)
	servertest.TickBlockEntitiesByHand(srv, 3)
	got := srv.InventoryAt(4, 64, 0)
	if len(got) != 5 || !got[0].Equal(waterBottle) || !got[3].Equal(wart) {
		t.Errorf("after three ticks without fuel the stand holds %+v, want nothing spent", got)
	}
	if brewTime, fuel, ok := srv.BrewingAt(4, 64, 0); !ok {
		t.Fatal("the brewing stand's block entity went missing")
	} else if brewTime != 0 || fuel != 0 {
		t.Errorf("without fuel the brew = (%d ticks left, %d fuel power), want no countdown and no power",
			brewTime, fuel)
	}

	// The bottle out and the powder in: the fuel loads anyway, no brew
	// starts, and the block stops drawing the bottle.
	standSlot(t, srv, 0, protocol.ItemStack{})
	standSlot(t, srv, 4, servertest.ItemStack(t, "minecraft:blaze_powder", 2))
	servertest.TickBlockEntitiesByHand(srv, 1)
	if brewTime, fuel, _ := srv.BrewingAt(4, 64, 0); brewTime != 0 || fuel != 20 {
		t.Errorf("the loaded stand's brew = (%d ticks left, %d fuel power), want no countdown on twenty power",
			brewTime, fuel)
	}
	if got := srv.InventoryAt(4, 64, 0); len(got) != 5 ||
		!got[4].Equal(servertest.ItemStack(t, "minecraft:blaze_powder", 1)) {
		t.Errorf("the stand holds %+v, want one of the two powders loaded", got)
	}
	if _, props := servertest.StatePropsOf(t, srv, 4, 64, 0); props["has_bottle_0"] != "false" {
		t.Error("the block still draws the bottle that was taken out")
	}

	// The bottle back: the brew starts on the next tick, spending one of
	// the loaded power.
	standSlot(t, srv, 0, waterBottle)
	servertest.TickBlockEntitiesByHand(srv, 1)
	if brewTime, fuel, _ := srv.BrewingAt(4, 64, 0); brewTime != 400 || fuel != 19 {
		t.Errorf("once the bottle fits, the brew = (%d ticks left, %d fuel power), want (400, 19)",
			brewTime, fuel)
	}
}

// TestAnEmptyBrewingStandClearsStaleBottleBits ticks an empty stand whose
// block still carries has_bottle_0=true, as a save or a setblock can leave
// it: the first tick has no last count to compare with, so it writes the
// bits the empty slots hold and the stale property goes.
// vanilla: BrewingStandBlockEntity.serverTick
func TestAnEmptyBrewingStandClearsStaleBottleBits(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	placeBrewingStand(t, srv, w)

	name, props := servertest.StatePropsOf(t, srv, 4, 64, 0)
	props["has_bottle_0"] = "true"
	stale, ok := registry.FindState(name, props)
	if !ok {
		t.Fatal("could not build the stand's stale block state")
	}
	srv.SetBlock(4, 64, 0, stale)

	servertest.TickBlockEntitiesByHand(srv, 1)
	if _, props := servertest.StatePropsOf(t, srv, 4, 64, 0); props["has_bottle_0"] != "false" {
		t.Error("the empty stand still draws the bottle a stale bit left")
	}
}

// TestABrewingStandBrewsInTheNetherToo checks the brew reaches the other
// dimensions: a stand in the nether runs its four hundred ticks the same
// as the overworld's, its block entity being the nether's own.
func TestABrewingStandBrewsInTheNetherToo(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.CarryIntoNether(t, srv, w.Client, "Testificate", 8, 8)

	if !srv.PlaceContainerInDimension(world.NetherName, 8, 40, 8, "minecraft:brewing_stand") {
		t.Fatal("could not place the nether brewing stand")
	}
	srv.StopTick()
	fillNetherStand := func(slot int, stack protocol.ItemStack) {
		t.Helper()
		if !srv.SetInventoryInDimension(world.NetherName, 8, 40, 8, slot, stack) {
			t.Fatalf("could not fill the nether brewing stand's slot %d", slot)
		}
	}
	fillNetherStand(0, servertest.PotionBottle(t, "minecraft:potion", "minecraft:water"))
	fillNetherStand(3, servertest.ItemStack(t, "minecraft:nether_wart", 1))
	fillNetherStand(4, servertest.ItemStack(t, "minecraft:blaze_powder", 1))

	servertest.TickBlockEntitiesByHand(srv, 401)
	awkward := servertest.PotionBottle(t, "minecraft:potion", "minecraft:awkward")
	if got := srv.InventoryInDimension(world.NetherName, 8, 40, 8); len(got) != 5 ||
		!got[0].Equal(awkward) {
		t.Errorf("the nether stand holds %+v, want the awkward potion brewed there", got)
	}
}

// TestTheLastDragonBreathLeavesItsBottleInTheStand brews a splash potion
// into a lingering one with a single dragon breath: the breath's crafting
// remainder, the glass bottle it was in, takes the ingredient slot the
// spent breath emptied.
// vanilla: BrewingStandBlockEntity.doBrew
func TestTheLastDragonBreathLeavesItsBottleInTheStand(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	placeBrewingStand(t, srv, w)
	standSlot(t, srv, 0, servertest.PotionBottle(t, "minecraft:splash_potion", "minecraft:awkward"))
	standSlot(t, srv, 3, servertest.ItemStack(t, "minecraft:dragon_breath", 1))
	standSlot(t, srv, 4, servertest.ItemStack(t, "minecraft:blaze_powder", 1))

	servertest.TickBlockEntitiesByHand(srv, 401)

	lingering := servertest.PotionBottle(t, "minecraft:lingering_potion", "minecraft:awkward")
	got := srv.InventoryAt(4, 64, 0)
	if len(got) != 5 || !got[0].Equal(lingering) {
		t.Errorf("the stand holds %+v, want the lingering potion in slot 0", got)
	}
	if want := servertest.ItemStack(t, "minecraft:glass_bottle", 1); !got[3].Equal(want) {
		t.Errorf("slot 3 = %+v, want the spent dragon breath's glass bottle", got[3])
	}
	if items := srv.ItemsInDimension(world.DimensionName); len(items) != 0 {
		t.Errorf("the stand dropped %+v, want the bottle kept in the slot", items)
	}
}

// TestADragonBreathWithMoreLeftDropsTheBottle brews with two dragon
// breaths: the one it spends leaves the stack non-empty, so the glass
// bottle it was in is dropped at the stand instead, and the breath left
// stays in the ingredient slot.
// vanilla: BrewingStandBlockEntity.doBrew, Containers.dropItemStack
func TestADragonBreathWithMoreLeftDropsTheBottle(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	placeBrewingStand(t, srv, w)
	standSlot(t, srv, 0, servertest.PotionBottle(t, "minecraft:splash_potion", "minecraft:awkward"))
	standSlot(t, srv, 3, servertest.ItemStack(t, "minecraft:dragon_breath", 2))
	standSlot(t, srv, 4, servertest.ItemStack(t, "minecraft:blaze_powder", 1))

	servertest.TickBlockEntitiesByHand(srv, 401)

	lingering := servertest.PotionBottle(t, "minecraft:lingering_potion", "minecraft:awkward")
	got := srv.InventoryAt(4, 64, 0)
	if len(got) != 5 || !got[0].Equal(lingering) {
		t.Errorf("the stand holds %+v, want the lingering potion in slot 0", got)
	}
	if want := servertest.ItemStack(t, "minecraft:dragon_breath", 1); !got[3].Equal(want) {
		t.Errorf("slot 3 = %+v, want the one dragon breath left", got[3])
	}
	bottles := 0
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.Item == "minecraft:glass_bottle" {
			bottles++
		}
	}
	if bottles != 1 {
		t.Errorf("the stand dropped %d glass bottles, want the one the spent breath left",
			bottles)
	}
}

// TestAHopperRespectsTheBrewingStandsFaces puts a hopper beside a brewing
// stand and one over it: the side face reads the fuel slot, so a blaze
// powder goes to slot 4 rather than the ingredient slot 3; the top face reads
// only the ingredient slot, so a glass bottle never lands and a nether wart
// does.
func TestAHopperRespectsTheBrewingStandsFaces(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:brewing_stand", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()

	// The hopper beside the stand, its spout east into the stand's side.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:hopper", 1))
	w.Sneak(true)
	w.PlaceBlock(4, 64, 0, protocol.FaceWest)
	w.Sneak(false)
	w.ExpectBlockAck()
	srv.StopTick()

	// A blaze powder is both an ingredient and the fuel; the side face
	// reads the fuel slot, so it goes to slot 4.
	srv.SetInventoryAt(3, 64, 0, 0, servertest.ItemStack(t, "minecraft:blaze_powder", 1))
	servertest.TickBlockEntitiesByHand(srv, 1)
	got := srv.InventoryAt(4, 64, 0)
	if len(got) != 5 || got[4].ItemID != servertest.ItemStack(t, "minecraft:blaze_powder", 1).ItemID {
		t.Errorf("the brewing stand holds %+v, want the blaze powder in the fuel slot 4", got)
	}

	// The hopper above, its spout down into the stand's top face.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:hopper", 1))
	w.Sneak(true)
	w.PlaceBlock(4, 64, 0, protocol.FaceTop)
	w.Sneak(false)
	w.SettleKeeping()
	srv.FlushBlockAcks()
	w.ExpectBlockAck()

	// A glass bottle the top face will not take stays in the hopper.
	srv.SetInventoryAt(4, 65, 0, 0, servertest.ItemStack(t, "minecraft:glass_bottle", 1))
	servertest.TickBlockEntitiesByHand(srv, 1)
	got = srv.InventoryAt(4, 64, 0)
	if len(got) != 5 || !got[0].Empty() || !got[1].Empty() || !got[2].Empty() {
		t.Errorf("the brewing stand holds %+v, want the glass bottle left out of its bottle slots", got)
	}
	if got := srv.InventoryAt(4, 65, 0); servertest.NothingIn(got) {
		t.Errorf("the hopper is empty, want the glass bottle still in it")
	}

	// A nether wart it does take, into slot 3.
	srv.SetInventoryAt(4, 65, 0, 0, servertest.ItemStack(t, "minecraft:nether_wart", 1))
	servertest.TickBlockEntitiesByHand(srv, 1)
	got = srv.InventoryAt(4, 64, 0)
	if len(got) != 5 || got[3].ItemID != servertest.ItemStack(t, "minecraft:nether_wart", 1).ItemID {
		t.Errorf("the brewing stand holds %+v, want the nether wart in slot 3", got)
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

// TestTheCrafterTellsItsSlotStates opens a crafter and checks the ten numbers
// its window draws itself with - nine enabled slots and no trigger - then
// disables one slot and checks the window is told.
func TestTheCrafterTellsItsSlotStates(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	openCrafter(t, w)

	w.AwaitWindowProperty(protocol.CrafterPropertyTriggered, 0)
	for slot := range int32(protocol.CrafterPropertyTriggered) {
		if got := w.Props[slot]; got != 0 {
			t.Errorf("crafter slot property %d = %d, want 0 (enabled)", slot, got)
		}
	}
	if got := w.Props[protocol.CrafterPropertyTriggered]; got != 0 {
		t.Errorf("crafter triggered = %d, want 0", got)
	}

	toggleCrafterSlot(w, 0, false)
	w.AwaitWindowProperty(protocol.CrafterPropertySlots, 1)
}

// TestACrafterIgnoresASpectatorsSlotToggle opens a crafter, turns the player
// into a spectator, and sends the slot-state toggle: vanilla drops that
// packet for a spectator, so the grid slot stays enabled.
// vanilla: ServerGamePacketListenerImpl.handleContainerSlotStateChanged
func TestACrafterIgnoresASpectatorsSlotToggle(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Testificate"))
	w := servertest.WindowedOf(t, "Testificate", c)
	openCrafter(t, w)

	w.ChatCommand("gamemode spectator")
	servertest.WaitFor(t, srv, 1200, func() bool {
		return srv.PlayerGameMode("Testificate") == protocol.GameModeSpectator
	})
	w.Settle()

	toggleCrafterSlot(w, 0, false)
	w.Settle()

	disabled, ok := srv.CrafterDisabledSlots(4, 64, 0)
	if !ok {
		t.Fatal("the crafter's block entity went missing")
	}
	if disabled[0] {
		t.Error("a spectator disabled a crafter slot, want the toggle dropped")
	}
}

// TestTheCrafterRemembersDisabledSlotsAcrossARestart disables a crafter grid
// slot, saves the world and restarts: the disabled slot comes back with the
// block entity and its window opens told so.
func TestTheCrafterRemembersDisabledSlotsAcrossARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	srv, w := servertest.OpenWindowed(t, cfg)
	openCrafter(t, w)
	w.AwaitWindowProperty(protocol.CrafterPropertyTriggered, 0)
	toggleCrafterSlot(w, 0, false)
	w.AwaitWindowProperty(protocol.CrafterPropertySlots, 1)
	w.Disconnect()
	srv.Shutdown()

	_, w = servertest.OpenWindowed(t, cfg)
	openCrafterWindow(t, w)
	w.AwaitWindowProperty(protocol.CrafterPropertySlots, 1)
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

// TestAHopperSkipsADisabledCrafterSlot disables a crafter grid slot with the
// window's toggle, then has a hopper add one dirt: the disabled slot keeps
// what it held and the dirt goes to the next enabled one.
func TestAHopperSkipsADisabledCrafterSlot(t *testing.T) {
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

	openCrafterWindow(t, w)
	w.AwaitWindowProperty(protocol.CrafterPropertyTriggered, 0)
	toggleCrafterSlot(w, 0, false)
	w.AwaitWindowProperty(protocol.CrafterPropertySlots, 1)
	w.CloseWindow()
	w.ExpectWindowZero()
	srv.StopTick()

	srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1))
	srv.SetInventoryAt(4, 65, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1))
	servertest.TickBlockEntitiesByHand(srv, 1)
	got := srv.InventoryAt(4, 64, 0)
	if got[0].Count != 1 {
		t.Errorf("the disabled slot holds %d dirt, want the 1 it had", got[0].Count)
	}
	if got[1].Count != 1 {
		t.Errorf("crafter slot 1 holds %d dirt, want the hopper's one", got[1].Count)
	}
}
