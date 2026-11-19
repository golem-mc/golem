package containers

import (
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"maps"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The recipe book's recipes. A click in the book places its ingredients in
// the crafting grid, taking them from the inventory; when the player does not
// have them the client is answered with the ghost recipe instead. The book's
// open and filtering toggles, and the highlight a recipe loses once looked
// at, are the player's own, kept across a rejoin in their file.

// craftingTableRecipe is the display ID of minecraft:crafting_table - the
// shaped two-by-two of the planks tag - as the join's Recipe Book Add names
// it (internal/server/server_login_test.go reads the entry it belongs to).
var craftingTableRecipe = displayID("minecraft:crafting_table")

// oakPlanksRecipe is the display ID of minecraft:oak_planks, the shapeless
// craft of one log into four planks.
var oakPlanksRecipe = displayID("minecraft:oak_planks")

// oakBoatRecipe is the display ID of minecraft:oak_boat, the five-plank
// boat whose unlock criterion is water rather than an item.
var oakBoatRecipe = displayID("minecraft:oak_boat")

// displayID is the display ID the generated table numbers a recipe with.
func displayID(name string) int32 {
	display := registry.CraftingDisplayOfName(name)
	if display == nil {
		panic("the display table carries no " + name)
	}
	return display.ID
}

// TestTheRecipeBookFillsThePlayersOwnGrid places the one recipe a new
// player knows in their own 2x2 grid: the four planks it asks for move from
// the hotbar into the grid, one a square, and the result slot comes to hold
// the crafting table they make.
func TestTheRecipeBookFillsThePlayersOwnGrid(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

	planks := servertest.ItemStack(t, "minecraft:oak_planks", 5)
	srv.GiveSlot("Testificate", 36, planks)

	slots, ghost := w.PlaceRecipe(craftingTableRecipe, w.Window, false)
	if ghost != nil {
		t.Error("a placement the inventory could pay was answered with the ghost recipe")
	}
	want := map[int16]protocol.ItemStack{
		0: servertest.ItemStack(t, "minecraft:crafting_table", 1),
		1: servertest.ItemStack(t, "minecraft:oak_planks", 1),
		2: servertest.ItemStack(t, "minecraft:oak_planks", 1),
		3: servertest.ItemStack(t, "minecraft:oak_planks", 1),
		4: servertest.ItemStack(t, "minecraft:oak_planks", 1),
		// The hotbar slot the four planks came out of.
		36: servertest.ItemStack(t, "minecraft:oak_planks", 1),
	}
	for slot, stack := range want {
		if got, ok := slots[slot]; !ok {
			t.Errorf("slot %d was never corrected, want %+v", slot, stack)
		} else if !got.Equal(stack) {
			t.Errorf("slot %d = %+v, want %+v", slot, got, stack)
		}
	}
	if len(slots) != len(want) {
		t.Errorf("the placement corrected %d slots (%v), want the %d it touched", len(slots), slots, len(want))
	}
}

// TestTheRecipeBookFillsACraftingTablesGrid places the same shaped recipe in
// a crafting table's 3x3, where its two-by-two lands in the top left corner -
// four squares slot 1, 2, 4 and 5 - leaving the rest of the grid empty.
func TestTheRecipeBookFillsACraftingTablesGrid(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

	srv.GiveSlot("Testificate", 36, servertest.ItemStack(t, "minecraft:crafting_table", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()

	planks := servertest.ItemStack(t, "minecraft:oak_planks", 5)
	srv.GiveSlot("Testificate", 36, planks)
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuCrafting, false, 0, 0, 0)

	slots, ghost := w.PlaceRecipe(craftingTableRecipe, w.Window, false)
	if ghost != nil {
		t.Error("a placement the inventory could pay was answered with the ghost recipe")
	}
	want := map[int16]protocol.ItemStack{
		0: servertest.ItemStack(t, "minecraft:crafting_table", 1),
		1: servertest.ItemStack(t, "minecraft:oak_planks", 1),
		2: servertest.ItemStack(t, "minecraft:oak_planks", 1),
		4: servertest.ItemStack(t, "minecraft:oak_planks", 1),
		5: servertest.ItemStack(t, "minecraft:oak_planks", 1),
		// Window slot 37 is the hotbar's first in this window's numbering.
		37: servertest.ItemStack(t, "minecraft:oak_planks", 1),
	}
	for slot, stack := range want {
		if got, ok := slots[slot]; !ok {
			t.Errorf("slot %d was never corrected, want %+v", slot, stack)
		} else if !got.Equal(stack) {
			t.Errorf("slot %d = %+v, want %+v", slot, got, stack)
		}
	}
	for _, empty := range []int16{3, 6, 7, 8, 9} {
		if got, ok := slots[empty]; ok {
			t.Errorf("slot %d = %+v, want the placement to leave it alone", empty, got)
		}
	}
}

// knowsSmelting is a player file whose book holds the raw iron smelting
// recipe.
func knowsSmelting(t *testing.T) *config.Config {
	t.Helper()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Testificate"), nbt.Compound{
		"recipeBook": nbt.Compound{
			"recipes": nbt.List{Elem: nbt.TypeString,
				Items: []nbt.Tag{nbt.String("minecraft:iron_ingot_from_smelting_raw_iron")}},
		},
	})
	return cfg
}

// readBookSquareItems reads an item-list square's items in the order sent: a
// tag display whose HolderSet announces an inline list of n IDs as n+1.
func readBookSquareItems(t *testing.T, r *protocol.Reader) []int32 {
	t.Helper()
	if typ := r.ReadVarInt(); typ != 6 {
		t.Fatalf("square display type = %d, want 6 (tag)", typ)
	}
	n := r.ReadVarInt()
	if n == 0 {
		t.Fatalf("the square names tag %q, want an item list", r.ReadString(32767))
	}
	items := make([]int32, 0, n-1)
	for range n - 1 {
		items = append(items, r.ReadVarInt())
	}
	return items
}

// TestTheRecipeBookFillsAFurnacesInput places the smelting recipe in an open
// furnace: the raw iron moves into slot 0. With none in the inventory the
// client is answered with the ghost recipe.
// vanilla: AbstractFurnaceMenu.handlePlacement
func TestTheRecipeBookFillsAFurnacesInput(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, knowsSmelting(t))
	srv.GiveSlot("Testificate", 36, servertest.ItemStack(t, "minecraft:furnace", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpenType(protocol.MenuFurnace, protocol.BlockEntityFurnace, 4, 64, 0)
	recipe := registry.FurnaceDisplayOfName("minecraft:iron_ingot_from_smelting_raw_iron").ID

	if _, ghost := w.PlaceRecipe(recipe, w.Window, false); ghost == nil {
		t.Error("a placement with no raw iron was not answered with the ghost recipe")
	}
	srv.GiveSlot("Testificate", 36, servertest.ItemStack(t, "minecraft:raw_iron", 3))
	slots, ghost := w.PlaceRecipe(recipe, w.Window, false)
	if ghost != nil {
		t.Error("a placement the inventory could pay was answered with the ghost recipe")
	}
	if got, want := slots[0], servertest.ItemStack(t, "minecraft:raw_iron", 1); !got.Equal(want) {
		t.Errorf("furnace slot 0 = %+v, want %+v", got, want)
	}
}

// TestTheRecipeBookPlacesAShapelessRecipe places a shapeless recipe in both
// grids, through a player whose file names it: one oak log moves into the
// first cell and the result slot comes to hold the four planks it makes.
func TestTheRecipeBookPlacesAShapelessRecipe(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	// The fixture is the file a vanilla server wrote for the offline
	// player "Packrat", whose recipe book the data pack filled with every
	// recipe advancement hands out - the shapeless oak planks among them -
	// and whose inventory holds seventeen oak logs to place it with. The
	// player arrives where the file left them, and the flat world this
	// server generates has stone there, so the air it carved out comes
	// before the loading screen lifts.
	before := servertest.InstallPlayerFixture(t, cfg, "Packrat", "vanilla-inventory.dat")

	srv, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login("Packrat")
	c.Configure()
	c.ReadThroughJoin("Packrat")
	pos, _ := nbt.Get[nbt.List](before, "Pos")
	var at [3]float64
	for i, item := range pos.Items {
		if v, ok := item.(nbt.Double); ok {
			at[i] = float64(v)
		}
	}
	x, y, z := int32(at[0]), int32(at[1]), int32(at[2])
	air := servertest.BlockState(t, "minecraft:air")
	for dy := range int32(-2) + 5 {
		for _, dz := range []int32{0, 1} {
			if !srv.SetBlock(x, int32(y)+dy-2, z+dz, air) {
				t.Fatal("the fixture player's chunk was not loaded")
			}
		}
	}
	c.MarkLoaded()

	log := servertest.ItemID(t, "minecraft:oak_log")
	planks := servertest.ItemID(t, "minecraft:oak_planks")
	oneLog := protocol.ItemStack{ItemID: log, Count: 1}

	// First in the player's own 2x2: slot 37 carries the seventeen logs of
	// the file's first hotbar stack, one of which leaves it.
	w := &servertest.WindowedClient{Client: c, WantBlockEntityType: -1}
	w.Window = 0
	w.States = map[int32]int32{0: 1}
	slots, ghost := w.PlaceRecipe(oakPlanksRecipe, w.Window, false)
	if ghost != nil {
		t.Error("a placement the inventory could pay was answered with the ghost recipe")
	}
	if got := slots[1]; !got.Equal(oneLog) {
		t.Errorf("grid cell 1 = %+v, want one oak log", got)
	}
	if got, ok := slots[0]; !ok {
		t.Error("the result slot was never corrected, want the four planks one log makes")
	} else if got.ItemID != planks || got.Count != 4 {
		t.Errorf("result slot = %+v, want four oak planks", got)
	}
	if got := slots[37]; got.ItemID != log || got.Count != 16 {
		t.Errorf("slot 37 = %+v, want the sixteen logs left of seventeen", got)
	}

	// Then in a crafting table stood in the carved-out air beside them, the
	// one-ingredient recipe fills wherever it lands - the first cell - in
	// the table's 3x3 the same as in the 2x2.
	table := servertest.BlockState(t, "minecraft:crafting_table")
	if !srv.SetBlock(x, y, z+1, table) {
		t.Fatal("could not stand the crafting table")
	}
	w.UseBlock(x, y, z+1)
	w.ExpectContainerOpen(protocol.MenuCrafting, false, x, y, z)

	slots, ghost = w.PlaceRecipe(oakPlanksRecipe, w.Window, false)
	if ghost != nil {
		t.Error("a placement the inventory could pay was answered with the ghost recipe")
	}
	if got := slots[1]; !got.Equal(oneLog) {
		t.Errorf("grid cell 1 = %+v, want one oak log", got)
	}
	if got, ok := slots[0]; !ok {
		t.Error("the result slot was never corrected, want the four planks one log makes")
	} else if got.ItemID != planks || got.Count != 4 {
		t.Errorf("result slot = %+v, want four oak planks", got)
	}
	// Window slot 38 is the hotbar stack the logs ride in, in this
	// window's numbering: the fifteen left of sixteen.
	if got := slots[38]; got.ItemID != log || got.Count != 15 {
		t.Errorf("slot 38 = %+v, want fifteen oak logs", got)
	}
}

// TestARecipeThePlayerCannotCraftGetsTheGhostRecipe places the crafting
// table recipe with too few planks to make it: the answer is the ghost
// recipe and nothing else, and what the grid held is handed back to the
// inventory first.
func TestARecipeThePlayerCannotCraftGetsTheGhostRecipe(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

	// Three planks altogether, none of them spent yet.
	srv.GiveSlot("Testificate", 36, servertest.ItemStack(t, "minecraft:oak_planks", 3))

	slots, ghost := w.PlaceRecipe(craftingTableRecipe, w.Window, false)
	if ghost == nil {
		t.Fatal("a placement the inventory could not pay was not answered with the ghost recipe")
	}
	w.ExpectShapedPlanksGhost(ghost, w.Window)
	if len(slots) != 0 {
		t.Errorf("a ghosted placement also corrected slots %v, want the inventory left alone", slots)
	}

	// The next click moves all three onto the grid: the placement still
	// needs a fourth, the ghost comes again and the grid is handed back.
	w.Click(36, 0, protocol.ClickModePickup)
	w.SettleTrackingState()
	w.Click(1, 0, protocol.ClickModePickup)
	w.SettleTrackingState()

	slots, ghost = w.PlaceRecipe(craftingTableRecipe, w.Window, false)
	if ghost == nil {
		t.Fatal("a grid-held placement the inventory could not top up was not answered with the ghost recipe")
	}
	want := map[int16]protocol.ItemStack{
		0:  {}, // the result slot, refreshed over the emptied grid
		1:  {}, // the grid cell, emptied by the hand-back
		36: servertest.ItemStack(t, "minecraft:oak_planks", 3),
	}
	for slot, stack := range want {
		if got, ok := slots[slot]; !ok {
			t.Errorf("slot %d was never corrected, want %+v", slot, stack)
		} else if !got.Equal(stack) {
			t.Errorf("slot %d = %+v, want %+v", slot, got, stack)
		}
	}
}

// TestRecipeBookSettingsSurviveARejoin flips the book's open and filtering
// toggles through the Change Recipe Book Settings packet, and reads them
// back from the join of the connection that follows: the crafting book is
// open, the furnace is set to filter, and every other switch stays off.
func TestRecipeBookSettingsSurviveARejoin(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	_, connect := servertest.Start(t, cfg)

	c := connect().Join("Bookish")

	c.Send(protocol.IDPlayRecipeBookChangeSettings, func(b *protocol.Buffer) {
		b.PutVarInt(0) // crafting
		b.PutBool(true)
		b.PutBool(false)
	})
	c.Send(protocol.IDPlayRecipeBookChangeSettings, func(b *protocol.Buffer) {
		b.PutVarInt(1) // furnace
		b.PutBool(false)
		b.PutBool(true)
	})
	c.Disconnect()

	c = connect()
	c.Login("Bookish")
	c.Configure()
	var got [8]bool
	var read bool
	c.ReadJoin(func(packetID int32, data []byte) {
		if packetID != protocol.IDPlayRecipeBookSettings || read {
			return
		}
		read = true
		r := protocol.NewReader(data)
		for i := range got {
			got[i] = r.ReadBool()
		}
		if err := r.Err(); err != nil {
			t.Errorf("recipe book settings: %v", err)
		}
	})
	if !read {
		t.Fatal("the rejoin never carried the recipe book settings")
	}
	want := [8]bool{true, false, false, true, false, false, false, false}
	if got != want {
		t.Errorf("rejoined book settings = %v, want %v", got, want)
	}
	c.Disconnect()
}

// bookAdd is one Recipe Book Add as the tests read them: each entry's
// display ID and flags, and whether the packet replaces the book.
type bookAdd struct {
	entries []bookAddEntry
	replace bool
}

// bookAddEntry is one entry of a Recipe Book Add: the display ID the packet
// named and the entry's flags byte.
type bookAddEntry struct {
	id    int32
	flags uint8
}

// readBookAdd reads one Recipe Book Add whole. Walking a display past its
// squares is what makes an entry's flags reachable at all, so every slot
// display shape the server writes is stepped over on the way.
func readBookAdd(t *testing.T, data []byte) bookAdd {
	t.Helper()
	r := protocol.NewReader(data)
	add := bookAdd{}
	for range r.ReadVarInt() {
		var entry bookAddEntry
		entry.id = r.ReadVarInt()
		readBookDisplay(t, r)
		r.ReadVarInt() // the group
		r.ReadVarInt() // the category
		if r.ReadBool() {
			for range r.ReadVarInt() {
				readBookRequirements(t, r)
			}
		}
		entry.flags = r.ReadU8()
		add.entries = append(add.entries, entry)
	}
	add.replace = r.ReadBool()
	if err := r.Err(); err != nil {
		t.Fatalf("recipe book add: %v", err)
	}
	if got := r.Remaining(); got != 0 {
		t.Errorf("recipe book add has %d bytes left over", got)
	}
	return add
}

// readBookDisplay steps over one display: the shape, the squares, the
// result and the station.
func readBookDisplay(t *testing.T, r *protocol.Reader) {
	t.Helper()
	switch r.ReadVarInt() {
	case 3: // stonecutter: input, result, station
		for range 3 {
			readBookSlotDisplay(t, r)
		}
		return
	case 4: // smithing: template, base, addition, result, station
		for range 5 {
			readBookSlotDisplay(t, r)
		}
		return
	case 2: // furnace: ingredient, fuel, result, station, duration, experience
		for range 4 {
			readBookSlotDisplay(t, r)
		}
		r.ReadVarInt()
		r.ReadF32()
		return
	case 1: // shaped; shapeless carries no dimensions
		r.ReadVarInt() // the width
		r.ReadVarInt() // the height
	}
	for range r.ReadVarInt() {
		readBookSlotDisplay(t, r)
	}
	readBookSlotDisplay(t, r) // the result
	readBookSlotDisplay(t, r) // the station
}

// readBookSlotDisplay steps over one square of a display, whichever of the
// slot display shapes the server filled it with.
func readBookSlotDisplay(t *testing.T, r *protocol.Reader) {
	t.Helper()
	typ := r.ReadVarInt()
	switch typ {
	case 0: // a square the recipe leaves out
	case 1: // any fuel
	case 4: // one item
		r.ReadVarInt()
	case 5: // an item stack
		r.ReadVarInt()
		r.ReadVarInt()
		r.ReadVarInt() // no added components
		r.ReadVarInt() // none removed either
	case 8: // trim demo: base, material, pattern holder
		readBookSlotDisplay(t, r)
		readBookSlotDisplay(t, r)
		r.ReadVarInt()
	case 6: // a HolderSet: a tag or an item list
		if n := r.ReadVarInt(); n == 0 {
			r.ReadString(32767)
		} else {
			for range n - 1 {
				r.ReadVarInt()
			}
		}
	default:
		t.Fatalf("recipe book slot display type %d, none the server writes", typ)
	}
}

// readBookRequirements steps over one square's ID Set: the tag a type 0
// names, or the set of items an ad-hoc one lists, its count minus one.
func readBookRequirements(t *testing.T, r *protocol.Reader) {
	t.Helper()
	set := r.ReadVarInt()
	if set == 0 {
		r.ReadString(32767)
		return
	}
	for range set - 1 {
		r.ReadVarInt()
	}
}

// bookAddIDs is the set of display IDs the adds named.
func bookAddIDs(adds []bookAdd) map[int32]bool {
	ids := map[int32]bool{}
	for _, add := range adds {
		for _, entry := range add.entries {
			ids[entry.id] = true
		}
	}
	return ids
}

// bookAddNames names the display IDs the packet named, for a failure
// message.
func bookAddNames(t *testing.T, ids map[int32]bool) []string {
	t.Helper()
	names := make([]string, 0, len(ids))
	for id := range ids {
		if display := registry.CraftingDisplayOf(id); display != nil {
			names = append(names, display.Name)
		} else {
			names = append(names, fmt.Sprintf("display_%d", id))
		}
	}
	slices.Sort(names)
	return names
}

// bookAddOfNames looks the recipe names' display IDs up: the set a Recipe
// Book Add is expected to name exactly.
func bookAddOfNames(t *testing.T, names ...string) map[int32]bool {
	t.Helper()
	ids := make(map[int32]bool, len(names))
	for _, name := range names {
		display := registry.CraftingDisplayOfName(name)
		if display == nil {
			t.Fatalf("the display table carries no %s", name)
		}
		ids[display.ID] = true
	}
	return ids
}

// collectBookAdds pumps the next n ticks, collecting every Recipe Book Add
// they send, and reports whether any arrived. The unlock rides the mailbox
// on the tick clock, so a give's grant is there within n ticks or not at
// all.
func collectBookAdds(c *servertest.Client, srv *server.Server, n uint64) (adds []bookAdd, arrived bool) {
	c.T.Helper()
	c.PumpTicks(srv, n, func(id int32, data []byte) bool {
		if id == protocol.IDPlayRecipeBookAdd {
			adds = append(adds, readBookAdd(c.T, data))
		}
		return false
	})
	return adds, len(adds) > 0
}

// planksUnlockedRecipes are the recipes oak planks unlock, the ones whose
// recipe advancement's criteria name either them or the planks tag; the
// crafting table the player arrives knowing is not among them. vanilla: the
// recipe advancements of vanilla's data pack
var planksUnlockedRecipes = []string{
	"minecraft:barrel",
	"minecraft:oak_button",
	"minecraft:oak_door",
	"minecraft:oak_fence",
	"minecraft:oak_fence_gate",
	"minecraft:oak_pressure_plate",
	"minecraft:oak_sign",
	"minecraft:oak_slab",
	"minecraft:oak_stairs",
	"minecraft:oak_trapdoor",
	"minecraft:stick",
}

// boatRecipes are the recipes the boats' in_water criterion unlocks, every
// wood's boat among them. vanilla: the boat recipe advancements' in_water
var boatRecipes = []string{
	"minecraft:acacia_boat",
	"minecraft:bamboo_raft",
	"minecraft:birch_boat",
	"minecraft:cherry_boat",
	"minecraft:dark_oak_boat",
	"minecraft:jungle_boat",
	"minecraft:mangrove_boat",
	"minecraft:oak_boat",
	"minecraft:pale_oak_boat",
	"minecraft:poplar_boat",
	"minecraft:spruce_boat",
}

// TestIngredientsUnlockTheirRecipes gives a fresh player oak planks, which
// passes the unlock criteria of the recipes that ask for them or for the
// planks tag: each arrives in a Recipe Book Add of its own tick, notified
// and highlighted as new, without replacing the book. The recipes of the
// other woods' families stay locked: their criteria name their own planks.
func TestIngredientsUnlockTheirRecipes(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())

	c := connect()
	c.Login("Planker")
	c.WatchBook = true
	c.Configure()
	c.EnterWorld("Planker")

	srv.GiveSlot("Planker", 36, servertest.ItemStack(t, "minecraft:oak_planks", 4))

	adds, arrived := collectBookAdds(c, srv, 2)
	if !arrived {
		t.Fatal("the planks unlocked no recipe within two ticks")
	}
	for _, add := range adds {
		if add.replace {
			t.Error("a grant arriving during play replaced the book, want it to add to it")
		}
		for _, entry := range add.entries {
			if entry.flags != 0x03 {
				t.Errorf("recipe %s arrived with flags %#02x, want notified and highlighted as new",
					registry.CraftingDisplayOf(entry.id).Name, entry.flags)
			}
		}
	}
	got, want := bookAddIDs(adds), bookAddOfNames(t, planksUnlockedRecipes...)
	if !maps.Equal(got, want) {
		t.Errorf("the planks unlocked {%s}, want exactly {%s}",
			bookAddNames(t, got), bookAddNames(t, want))
	}
	c.Disconnect()
}

// TestUnlockedRecipesSurviveARejoin unlocks the oak plank recipes, leaves
// and comes back: the join's book carries the same set plus the crafting
// table the player arrived knowing, and no grant arrives again, which is
// the player file's recipeBook at work.
func TestUnlockedRecipesSurviveARejoin(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg)

	c := connect()
	c.Login("Planker")
	c.WatchBook = true
	c.Configure()
	c.EnterWorld("Planker")

	srv.GiveSlot("Planker", 36, servertest.ItemStack(t, "minecraft:oak_planks", 4))
	if _, arrived := collectBookAdds(c, srv, 2); !arrived {
		t.Fatal("the planks unlocked no recipe within two ticks")
	}
	c.Disconnect()

	c = connect()
	c.Login("Planker")
	c.WatchBook = true
	c.Configure()
	var joined []bookAdd
	c.ReadJoin(func(packetID int32, data []byte) {
		if packetID == protocol.IDPlayRecipeBookAdd {
			joined = append(joined, readBookAdd(c.T, data))
		}
	})
	if len(joined) != 1 {
		t.Fatalf("the join carried %d recipe book adds, want the one of the join", len(joined))
	}
	if !joined[0].replace {
		t.Error("the join's recipe book add did not replace the client's book")
	}
	kept := append(slices.Clone(planksUnlockedRecipes), "minecraft:crafting_table")
	got, want := bookAddIDs(joined), bookAddOfNames(t, kept...)
	if !maps.Equal(got, want) {
		t.Errorf("the rejoined book holds {%s}, want the same set as before plus the craft table", bookAddNames(t, got))
	}
	for _, entry := range joined[0].entries {
		if entry.flags != 0x02 {
			t.Errorf("the rejoined entry %s carries flags %#02x, want the never-looked-at highlight alone",
				registry.CraftingDisplayOf(entry.id).Name, entry.flags)
		}
	}
	c.MarkLoaded()

	// The set on disk is the one the player left with: nothing is granted
	// again on the ticks that follow.
	if adds, arrived := collectBookAdds(c, srv, 3); arrived {
		t.Errorf("the rejoined player was granted %s again, want no new grant", bookAddNames(t, bookAddIDs(adds)))
	}
	c.Disconnect()
}

// TestStandingInWaterUnlocksTheBoats puts water under a fresh player's
// feet, which passes the boats' criteria: all eleven name the water a body
// stands in, and none of them takes an ingredient.
// vanilla: the boat recipe advancements' in_water
func TestStandingInWaterUnlocksTheBoats(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())

	c := connect()
	c.Login("Wader")
	c.WatchBook = true
	c.Configure()
	c.EnterWorld("Wader")

	pos := srv.PlayerPosition("Wader")
	if !srv.SetBlock(int32(pos.X), int32(pos.Y), int32(pos.Z), servertest.BlockState(t, "minecraft:water")) {
		t.Fatal("the player's own cell could not be made water")
	}

	adds, arrived := collectBookAdds(c, srv, 2)
	if !arrived {
		t.Fatal("standing in water unlocked no recipe within two ticks")
	}
	got, want := bookAddIDs(adds), bookAddOfNames(t, boatRecipes...)
	if !maps.Equal(got, want) {
		t.Errorf("standing in water unlocked {%s}, want the eleven boats", bookAddNames(t, got))
	}
	c.Disconnect()
}

func TestLimitedCraftingRequiresUnlockedRecipes(t *testing.T) {
	for _, table := range []bool{false, true} {
		t.Run(fmt.Sprintf("table=%t", table), func(t *testing.T) {
			t.Parallel()
			srv, w := servertest.OpenWindowedOps(t, servertest.SurvivalTestConfig())
			if table {
				srv.GiveSlot("Testificate", 36, servertest.ItemStack(t, "minecraft:crafting_table", 1))
				w.PlaceBlock(4, 63, 0, protocol.FaceTop)
				w.ExpectBlockAck()
				w.UseBlock(4, 64, 0)
				w.ExpectContainerOpen(protocol.MenuCrafting, false, 0, 0, 0)
			}
			// Keep ingredient unlocks from changing the book during the assertions.
			servertest.HoldTicks(t, srv)
			log := servertest.ItemStack(t, "minecraft:oak_log", 1)
			planks := servertest.ItemStack(t, "minecraft:oak_planks", 4)
			w.ChatCommand("gamerule limited_crafting true")
			expectLimitedCraftingResult(w, protocol.ItemStack{})
			srv.GiveSlot("Testificate", 9, log)
			w.SettleTrackingState()
			from := int16(9)
			if table {
				from = 10
			}
			w.Click(from, 0, protocol.ClickModePickup)
			w.ExpectClickResult(nil, log)
			w.Click(1, 0, protocol.ClickModePickup)
			w.ExpectClickResult(map[int16]protocol.ItemStack{0: {}, 1: log}, protocol.ItemStack{})

			// Each way of taking a result must refuse the locked recipe.
			for _, mode := range []int32{protocol.ClickModePickup, protocol.ClickModeQuickMove, protocol.ClickModeSwap, protocol.ClickModeThrow} {
				w.Click(0, 0, mode)
				w.ExpectClickResult(nil, protocol.ItemStack{})
			}
			if !table {
				w.ChatCommand("item replace entity @s player.crafting.0 with minecraft:oak_log")
				expectLimitedCraftingResult(w, protocol.ItemStack{})
			}

			// A stale state forces a full sync, which must also hide the result.
			w.ClickWithState(0, 0, protocol.ClickModePickup, -1)
			response := w.CollectClickResponse()
			if len(response.Contents) != 1 || !response.Contents[0].Slots[0].Empty() || !response.Contents[0].Slots[1].Equal(log) {
				t.Fatalf("locked grid resync = %+v", response)
			}

			w.ChatCommand("gamerule limited_crafting false")
			expectLimitedCraftingResult(w, planks)
			w.ChatCommand("gamerule limited_crafting true")
			expectLimitedCraftingResult(w, protocol.ItemStack{})
			w.ChatCommand("recipe give @s minecraft:oak_planks")
			expectLimitedCraftingResult(w, planks)
			w.ChatCommand("recipe take @s minecraft:oak_planks")
			expectLimitedCraftingResult(w, protocol.ItemStack{})
			w.ChatCommand("recipe give @s minecraft:oak_planks")
			expectLimitedCraftingResult(w, planks)
			w.Click(0, 0, protocol.ClickModePickup)
			w.ExpectClickResult(map[int16]protocol.ItemStack{0: {}, 1: {}}, planks)
		})
	}
}

// expectLimitedCraftingResult tracks the permission change's new window state.
func expectLimitedCraftingResult(w *servertest.WindowedClient, want protocol.ItemStack) {
	w.T.Helper()
	for _, correction := range w.CollectContainerSlots() {
		if correction.Window == w.Window && correction.Slot == 0 {
			if !correction.Stack.Equal(want) {
				w.T.Errorf("result = %+v, want %+v", correction.Stack, want)
			}
			return
		}
	}
	w.T.Fatal("crafting result was not refreshed")
}
