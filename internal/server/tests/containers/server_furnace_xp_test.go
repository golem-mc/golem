package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/registry"
)

// The furnace's experience: the worth a finished smelt earns, kept against
// the recipe that earned it until a player takes the result or the furnace
// is broken, and paid out as the orbs vanilla's own award lays down. The
// tests drive a real furnace through its window the way the container tests
// do, tick its smelts by hand with the main loop stopped, and read the
// payout off the wire - the orb's own spawn packet carrying where it lies
// and the metadata carrying what it is worth.
//
// The totals are pinned on the recipe the data pack names
// iron_ingot_from_smelting_raw_iron, worth 0.7 a smelt: ten smelts land on
// seven points exactly in the float arithmetic the vanilla rule runs, so
// the payout is seven every run and the fraction's roll never comes up.

// furnaceXpRecipe is the recipe the tests smelt with, the one the data pack
// saves raw iron under: iron ingots, seven tenths of a point a smelt, the
// experience field of its own file.
const furnaceXpRecipe = "minecraft:iron_ingot_from_smelting_raw_iron"

// runTheSmelts turns the block entities the given number of times by hand,
// which is how a test runs a furnace's work in no time at all.
func runTheSmelts(t *testing.T, srv *server.Server, ticks int) {
	t.Helper()
	for range ticks {
		srv.TickBlockEntities()
	}
}

// smeltedFurnace is a furnace the tests have run ten raw iron through: the
// recipe counted ten times against the experience it owes, ten ingots in
// the output, and nothing left to smelt. Two coals see the whole run
// through, the first burning out partway with the second catching behind
// it, the way the furnace's own tick refuels.
func smeltedFurnace(t *testing.T, srv *server.Server, w *servertest.WindowedClient) {
	t.Helper()
	servertest.StopTheLoop(t, srv)
	servertest.LoadFurnace(t, srv, w, "minecraft:raw_iron", 10, 2)
	// Ten smelts at two hundred ticks each, the tick the last of them
	// lands on included, and two over to leave the furnace idle.
	runTheSmelts(t, srv, 10*200+2)

	used, ok := srv.FurnaceRecipesUsed(4, 64, 0)
	if !ok {
		t.Fatal("the furnace's block entity went missing")
	}
	if len(used) != 1 || used[furnaceXpRecipe] != 10 {
		t.Errorf("the furnace's smelting history = %v, want ten of the raw iron recipe", used)
	}
	ingots := servertest.ItemStack(t, "minecraft:iron_ingot", 10)
	if got := srv.InventoryAt(4, 64, 0); len(got) != 3 || !got[2].Equal(ingots) {
		t.Errorf("the furnace's output = %+v, want ten ingots in the result slot", got)
	}
}

// furnaceOrb is one experience orb the wire put in front of the test: the
// worth its metadata carries, the place its spawn packet named, and the
// entity ID the two travel under.
type furnaceOrb struct {
	id       int32
	value    int
	x, y, z  float64
	valueful bool
}

// furnaceOrbSighter is what a payout test reads the orbs with: the spawns
// of the minecraft:experience_orb type filed against the worth their
// metadata carries and where they were laid, stepping over everything else
// the window and the world put on the wire around them and answering
// keep-alives under it.
type furnaceOrbSighter struct {
	t    *testing.T
	c    *servertest.Client
	orbs []furnaceOrb
}

// readUntil files orb packets until stop says done or n ticks pass.
func (w *furnaceOrbSighter) readUntil(srv *server.Server, n int, stop func() bool) {
	w.t.Helper()
	done := func() bool { return stop != nil && stop() }
	if done() {
		return
	}
	w.c.ReadTicksSkipping(srv, n, nil, func(id int32, data []byte) bool {
		w.see(id, data)
		return done()
	})
}

// see files one packet against the watch.
func (w *furnaceOrbSighter) see(id int32, data []byte) {
	switch id {
	case protocol.IDPlaySpawnEntity:
		r := protocol.NewReader(data)
		entityID := r.ReadVarInt()
		r.ReadUUID()
		if r.ReadVarInt() != protocol.EntityTypeExperienceOrb {
			return
		}
		orb := furnaceOrb{id: entityID}
		orb.x, orb.y, orb.z = r.ReadF64(), r.ReadF64(), r.ReadF64()
		r.ReadLpVec3()
		r.ReadAngle()
		r.ReadAngle()
		r.ReadAngle()
		r.ReadVarInt()
		if err := r.Err(); err != nil {
			w.t.Fatalf("an orb's spawn packet did not read back: %v", err)
		}
		w.orbs = append(w.orbs, orb)
	case protocol.IDPlayEntityMetadata:
		w.readOrbMetadata(data)
	}
}

// readOrbMetadata fills the worth in on the orb the metadata names, the
// packet being one of an orb's only when the spawn was seen first and the
// field it carries is the orb's own integer worth.
func (w *furnaceOrbSighter) readOrbMetadata(data []byte) {
	w.t.Helper()
	r := protocol.NewReader(data)
	entityID := r.ReadVarInt()
	for i := range w.orbs {
		if w.orbs[i].id != entityID {
			continue
		}
		for r.Err() == nil {
			index := r.ReadU8()
			if index == protocol.MetadataEnd {
				break
			}
			fieldType := r.ReadVarInt()
			if index == protocol.MetadataIndexOrbValue && fieldType == protocol.MetadataVarInt {
				w.orbs[i].value = int(r.ReadVarInt())
				w.orbs[i].valueful = true
				continue
			}
			w.t.Fatalf("orb metadata field %d of type %d, want the orb's integer worth", index, fieldType)
		}
		if err := r.Err(); err != nil {
			w.t.Fatalf("orb metadata: %v", err)
		}
		return
	}
}

// expectFurnaceOrbs fails unless the sighted orbs are exactly the payout
// the test names: one orb worth the total, at the position given - the
// taker's own place when the result was taken, the block's centre when the
// furnace was broken, which is where the vanilla award puts them.
func expectFurnaceOrbs(t *testing.T, orbs []furnaceOrb, wantValue int, x, y, z float64) {
	t.Helper()
	if len(orbs) != 1 {
		t.Fatalf("the payout left %d orbs (%v), want one worth %d", len(orbs), orbs, wantValue)
	}
	orb := orbs[0]
	if !orb.valueful {
		t.Fatal("the orb's worth never arrived")
	}
	if orb.value != wantValue {
		t.Errorf("the orb is worth %d, want %d", orb.value, wantValue)
	}
	const within = 0.01
	if math.Abs(orb.x-x) > within || math.Abs(orb.y-y) > within || math.Abs(orb.z-z) > within {
		t.Errorf("the orb was laid at (%g, %g, %g), want (%g, %g, %g)", orb.x, orb.y, orb.z, x, y, z)
	}
}

// sightPayout reads the wire for the one orb a payout owes, and fails unless
// it arrived worth what the smelts earned where the test says it should.
func sightPayout(srv *server.Server, t *testing.T, w *servertest.WindowedClient, wantValue int, x, y, z float64) {
	t.Helper()
	sighter := &furnaceOrbSighter{t: t, c: w.Client}
	sighter.readUntil(srv, 100, func() bool {
		return len(sighter.orbs) > 0 && sighter.orbs[len(sighter.orbs)-1].valueful
	})
	expectFurnaceOrbs(t, sighter.orbs, wantValue, x, y, z)
}

// TestFurnacePaysItsSmeltingExperienceWhenTheResultIsTaken runs ten raw
// iron through a furnace and takes the ten ingots out of the result slot:
// the payout is the seven points ten smelts of nought point seven earn, as
// one orb at the taker's own place - and taking from the empty result the
// payout emptied is one nothing arrives for.
func TestFurnacePaysItsSmeltingExperienceWhenTheResultIsTaken(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	smeltedFurnace(t, srv, w)

	// The window again: the ticks moved its slots while it was closed, and
	// the content the open sends is what puts the client's clicks back on
	// the state the server is at.
	w.UseBlock(4, 64, 0)
	slots := w.ExpectContainerOpen(protocol.MenuFurnace, true, 4, 64, 0)
	if got := slots[2]; !got.Equal(servertest.ItemStack(t, "minecraft:iron_ingot", 10)) {
		w.T.Errorf("the reopened window's result slot = %+v, want the ten ingots", got)
	}

	// The take. The orb the payout leaves rides the mailbox, so it is read
	// off the wire rather than awaited in the click's own corrections.
	w.Click(2, 0, protocol.ClickModePickup)
	sightPayout(srv, t, w, 7, 0.5, 64, 0.5)

	// The payout empties the smelting history, and the result slot with it.
	if used, _ := srv.FurnaceRecipesUsed(4, 64, 0); len(used) != 0 {
		t.Errorf("after the take, the furnace still owes %v", used)
	}
	if got := srv.InventoryAt(4, 64, 0); len(got) != 3 || !got[2].Empty() {
		t.Errorf("after the take, the furnace's result slot = %+v, want it empty", got)
	}

	// Taking from an empty result is nothing at all: no orb more arrives
	// for it, the first payout having been the whole of what was owed. The
	// take's own correction moved the state under the client, so the first
	// click is answered with the window and the second is the real one.
	w.Click(2, 0, protocol.ClickModePickup)
	w.ExpectWindowContent()
	w.Click(2, 0, protocol.ClickModePickup)
	sighter := &furnaceOrbSighter{t: t, c: w.Client}
	// The loop is stopped; the click alone sends any orb before the barrier.
	sighter.readUntil(srv, 0, nil)
	if len(sighter.orbs) != 0 {
		t.Errorf("the second take left %v, want nothing", sighter.orbs)
	}
}

// TestTakingASmeltedResultUnlocksItsRecipe takes the ingots ten raw iron
// smelted into: the take unlocks the smelting recipe, notified and
// highlighted. vanilla: AbstractFurnaceBlockEntity.awardUsedRecipesAndPopExperience
func TestTakingASmeltedResultUnlocksItsRecipe(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	smeltedFurnace(t, srv, w)
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuFurnace, true, 4, 64, 0)

	w.WatchBook = true
	w.Click(2, 0, protocol.ClickModePickup)
	want := registry.FurnaceDisplayOfName(furnaceXpRecipe).ID
	found := 0
	// The loop is stopped; the click itself sends the unlock before the barrier.
	w.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		if id != protocol.IDPlayRecipeBookAdd {
			return false
		}
		if add := readBookAdd(t, data); len(add.entries) == 1 && add.entries[0].id == want {
			found++
			if add.entries[0].flags != 0x03 {
				t.Errorf("the smelting recipe's flags = %#x, want notified and highlighted 0x03", add.entries[0].flags)
			}
		}
		return false
	})
	if found != 1 {
		t.Errorf("the take sent %d Recipe Book Adds of the smelting recipe, want 1", found)
	}
}

// TestFurnacePaysItsSmeltingExperienceWhenItIsBroken runs ten raw iron
// through a furnace and breaks the furnace with the ten ingots still in it:
// the payout is the same seven points, laid at the block's centre the way
// the vanilla server pays a removed furnace, with the ingots spilling out
// beside them.
func TestFurnacePaysItsSmeltingExperienceWhenItIsBroken(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	smeltedFurnace(t, srv, w)

	w.BreakBlock(4, 64, 0)
	sightPayout(srv, t, w, 7, 4.5, 64.5, 0.5)

	// The furnace is gone from the world, its contents spilled with it -
	// the ten ingots among them, the one item a creative break of a
	// furnace leaves, its own block having none in this game mode.
	if got := srv.InventoryAt(4, 64, 0); got != nil {
		t.Errorf("the broken furnace still holds %+v", got)
	}
	w.Settle()
	if got := srv.ItemCount(); got != 1 {
		t.Errorf("the break left %d items on the ground, want the furnace's ten ingots", got)
	}
}

// TestAFurnaceOwesNoOrbUntilItsCellIsEmptied breaks a furnace that owes
// experience and reads the wire: no entity spawns before the block update
// that empties the cell, so the orb is not pushed out of a solid block.
// vanilla: LevelChunk.setBlockState runs preRemoveSideEffects after the write
func TestAFurnaceOwesNoOrbUntilItsCellIsEmptied(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	smeltedFurnace(t, srv, w)

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
				t.Error("an entity spawned before the furnace's cell was emptied")
			}
		}
	})
	if spawned < 2 {
		t.Errorf("the break spawned %d entities, want the orb and the ingots", spawned)
	}
}

// TestFurnacePaysItsSmeltingExperienceOnAShiftClick runs one raw gold
// through a furnace and shift-clicks the result into the player's things:
// the quick-move is one of the takes vanilla pays the furnace's stored
// experience on, and the one smelt's worth - a whole point, a recipe whose
// experience is one and whose fraction therefore never rolls - arrives as
// one orb of one at the taker's own place.
func TestFurnacePaysItsSmeltingExperienceOnAShiftClick(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.StopTheLoop(t, srv)
	servertest.LoadFurnace(t, srv, w, "minecraft:raw_gold", 1, 1)
	runTheSmelts(t, srv, 200+2)

	const goldRecipe = "minecraft:gold_ingot_from_smelting_raw_gold"
	used, ok := srv.FurnaceRecipesUsed(4, 64, 0)
	if !ok {
		t.Fatal("the furnace's block entity went missing")
	}
	if len(used) != 1 || used[goldRecipe] != 1 {
		t.Errorf("the furnace's smelting history = %v, want one of the raw gold recipe", used)
	}

	// The window again, the ingot in the result slot, and the shift-click
	// taking it into the player's things - the orb read off the wire, the
	// mailbox it rides being the click's corrections' own channel.
	w.UseBlock(4, 64, 0)
	slots := w.ExpectContainerOpen(protocol.MenuFurnace, true, 4, 64, 0)
	if got := slots[2]; !got.Equal(servertest.ItemStack(t, "minecraft:gold_ingot", 1)) {
		w.T.Errorf("the reopened window's result slot = %+v, want the gold ingot", got)
	}
	w.Click(2, 0, protocol.ClickModeQuickMove)
	sightPayout(srv, t, w, 1, 0.5, 64, 0.5)

	if got := srv.InventoryAt(4, 64, 0); len(got) != 3 || !got[2].Empty() {
		t.Errorf("after the shift-click, the furnace's result slot = %+v, want it empty", got)
	}
	if used, _ := srv.FurnaceRecipesUsed(4, 64, 0); len(used) != 0 {
		t.Errorf("after the shift-click, the furnace still owes %v", used)
	}
}

// TestFurnaceRemembersWhatItOwesAcrossARestart runs ten raw iron through a
// furnace, saves the world with the ingots still in it, and brings a new
// server up on the same directory: the smelting history comes back out of
// the region file with the rest of the block entity, and taking the result
// then pays the seven points the old furnace earned.
func TestFurnaceRemembersWhatItOwesAcrossARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	srv, w := servertest.OpenWindowed(t, cfg)
	smeltedFurnace(t, srv, w)
	w.Disconnect()
	srv.Shutdown()

	back, w := servertest.OpenWindowed(t, cfg)
	used, ok := back.FurnaceRecipesUsed(4, 64, 0)
	if !ok {
		t.Fatal("the furnace's block entity did not come back with the chunk")
	}
	if len(used) != 1 || used[furnaceXpRecipe] != 10 {
		t.Errorf("after the restart, the furnace's smelting history = %v, want the ten it owed", used)
	}

	// The window, the result slot still holding what the old furnace
	// smelted, and the take paying for the smelts it remembers.
	back.StopTick()
	w.UseBlock(4, 64, 0)
	slots := w.ExpectContainerOpen(protocol.MenuFurnace, true, 4, 64, 0)
	if got := slots[2]; !got.Equal(servertest.ItemStack(t, "minecraft:iron_ingot", 10)) {
		w.T.Errorf("after the restart, the window's result slot = %+v, want the ten ingots", got)
	}
	w.Click(2, 0, protocol.ClickModePickup)
	sightPayout(back, t, w, 7, 0.5, 64, 0.5)
	if used, _ := back.FurnaceRecipesUsed(4, 64, 0); len(used) != 0 {
		t.Errorf("after the paid take, the restarted furnace still owes %v", used)
	}
}

// TestPayingAFurnaceMarksItsChunkForTheNextSave pays a furnace's stored
// experience after a save has made the chunk clean, the order a result take
// leaves when an autosave lands between the slot click and the payout: the
// emptied smelting history must reach the next save, or a reload pays the
// same experience again.
func TestPayingAFurnaceMarksItsChunkForTheNextSave(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	smeltedFurnace(t, srv, w)

	// The furnace going out queued a relight on the pool; landing after the
	// save, its light install marks the chunk dirty again.
	srv.WaitRelights()
	if !srv.SaveAll(false) {
		t.Fatal("the save failed")
	}
	srv.FlushSaves()
	if srv.ChunkDirtyAt(4, 0) {
		t.Fatal("the chunk is still dirty after a save, so the payout's mark cannot be told apart")
	}

	srv.PayFurnaceSmelts(4, 64, 0)
	if used, _ := srv.FurnaceRecipesUsed(4, 64, 0); len(used) != 0 {
		t.Fatalf("the payout left the furnace owing %v", used)
	}
	if !srv.ChunkDirtyAt(4, 0) {
		t.Error("the payout emptied the smelting history without marking the chunk for the next save")
	}
}
