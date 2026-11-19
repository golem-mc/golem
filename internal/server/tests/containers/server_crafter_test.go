package containers

import (
	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"slices"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// crafterState returns the block state ID a crafter would be in with the given
// orientation and triggered flag, crafting at its default.
func crafterState(t *testing.T, orientation string, triggered bool) uint16 {
	t.Helper()
	s, ok := registry.FindState("minecraft:crafter", map[string]string{
		"orientation": orientation,
		"triggered":   strconv.FormatBool(triggered),
	})
	if !ok {
		t.Fatalf("FindState crafter orientation=%s triggered=%v: not found", orientation, triggered)
	}
	return s
}

// TestCrafterPlacementUsesTheLook checks CrafterBlock.getStateForPlacement:
// ORIENTATION pairs the front — the nearest looking direction reversed — with
// a top that is up for a horizontal front and follows the horizontal look
// when the front is vertical. A crafter placed looking steeply down while
// facing north is up_north; looking west across the ground is east_up.
func TestCrafterPlacementUsesTheLook(t *testing.T) {
	t.Parallel()
	srv, actor := servertest.AttachActor(t)
	actor.MoveToGround(4.5, 64, 4.5, true)
	actor.Settle()

	crafter, _ := registry.ItemID("minecraft:crafter")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: crafter, Count: 1})
	actor.Settle()

	// Facing north (yaw 180) and looking steeply down: the front is up and
	// the top is the horizontal look, north.
	actor.FacePlayer(4.5, 64, 4.5, 180, 60)
	actor.PlaceBlock(5, 63, 5, protocol.FaceTop)
	actor.Settle()
	if got := srv.BlockAt(5, 64, 5); got != crafterState(t, "up_north", false) {
		t.Errorf("crafter looking down facing north = %d, want %d (up_north)", got, crafterState(t, "up_north", false))
	}

	// Looking west horizontally: the front is east and the top is up.
	actor.FacePlayer(4.5, 64, 4.5, 90, 0)
	actor.PlaceBlock(6, 63, 5, protocol.FaceTop)
	actor.Settle()
	if got := srv.BlockAt(6, 64, 5); got != crafterState(t, "east_up", false) {
		t.Errorf("crafter looking west = %d, want %d (east_up)", got, crafterState(t, "east_up", false))
	}
}

// crafterFacingSouth stands a crafter at (4, 64, -1) facing south with the
// engine's main loop stopped, a chest at (4, 64, 0) when withChest is set.
func crafterFacingSouth(t *testing.T, withChest bool) (*server.Server, *servertest.WindowedClient) {
	t.Helper()
	return crafterFacingSouthIn(t, withChest, servertest.BlockTestConfig())
}

// crafterFacingSouthIn is crafterFacingSouth in a world built from cfg.
func crafterFacingSouthIn(t *testing.T, withChest bool, cfg *config.Config) (*server.Server, *servertest.WindowedClient) {
	t.Helper()
	srv, w := servertest.OpenWindowedOps(t, cfg)
	w.FacePlayer(4.5, 64, 1.5, 180, 0)
	if withChest {
		w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:chest", 1))
		w.PlaceBlock(4, 63, 0, protocol.FaceTop)
		w.ExpectBlockAck()
	}
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:crafter", 1))
	if withChest {
		w.Sneak(true)
		w.PlaceBlock(4, 64, 0, protocol.FaceNorth)
		w.Sneak(false)
	} else {
		w.PlaceBlock(4, 63, -1, protocol.FaceTop)
	}
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()
	if name, props := servertest.StatePropsOf(t, srv, 4, 64, -1); name != "minecraft:crafter" || props["orientation"] != "south_up" {
		t.Fatalf("the crafter stands as %s %v, want one facing south", name, props)
	}
	srv.StopTick()
	return srv, w
}

// powerCrafter puts a redstone block on the crafter's roof and runs the
// physics through the visit it books.
func powerCrafter(t *testing.T, srv *server.Server, w *servertest.WindowedClient) {
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:redstone_block", 1))
	w.Sneak(true)
	w.PlaceBlock(4, 64, -1, protocol.FaceTop)
	w.Sneak(false)
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()
	srv.TickPhysics(8)
}

// TestAPoweredCrafterCraftsIntoTheContainerInFront checks the rise's craft:
// the grid's recipe result goes into the chest the face opens onto, one
// item of every filled cell is spent, and the block shows crafting for six
// ticks. A craft that found room plays neither the sound nor the smoke.
func TestAPoweredCrafterCraftsIntoTheContainerInFront(t *testing.T) {
	t.Parallel()
	srv, w := crafterFacingSouth(t, true)
	log := servertest.ItemStack(t, "minecraft:oak_log", 2)
	if !srv.SetInventoryAt(4, 64, -1, 0, log) {
		t.Fatal("the crafter has no block entity to fill")
	}
	powerCrafter(t, srv, w)

	planks, _ := registry.ItemID("minecraft:oak_planks")
	if got := srv.InventoryAt(4, 64, 0); servertest.NothingIn(got) || got[0].ItemID != planks || got[0].Count != 4 {
		t.Errorf("the chest holds %+v, want four oak planks", got)
	}
	if got := srv.InventoryAt(4, 64, -1); got[0].Count != 1 {
		t.Errorf("the crafter's cell holds %+v, want one log spent", got[0])
	}
	if _, props := servertest.StatePropsOf(t, srv, 4, 64, -1); props["crafting"] != "true" {
		t.Errorf("the crafter shows crafting=%s right after a craft, want true", props["crafting"])
	}
	servertest.TickBlockEntitiesByHand(srv, 6)
	if _, props := servertest.StatePropsOf(t, srv, 4, 64, -1); props["crafting"] != "false" {
		t.Errorf("the crafter shows crafting=%s after six ticks, want false", props["crafting"])
	}
}

// TestAPoweredCrafterEjectsTheRemaindersAfterTheResult checks that the
// ingredients' crafting remainders leave the crafter too: four honey bottles
// make a honey block and leave four glass bottles.
func TestAPoweredCrafterEjectsTheRemaindersAfterTheResult(t *testing.T) {
	t.Parallel()
	srv, w := crafterFacingSouth(t, true)
	for _, slot := range []int{0, 1, 3, 4} {
		srv.SetInventoryAt(4, 64, -1, slot, servertest.ItemStack(t, "minecraft:honey_bottle", 1))
	}
	powerCrafter(t, srv, w)

	honey, _ := registry.ItemID("minecraft:honey_block")
	bottle, _ := registry.ItemID("minecraft:glass_bottle")
	var gotHoney, gotBottles int32
	for _, s := range srv.InventoryAt(4, 64, 0) {
		switch s.ItemID {
		case honey:
			gotHoney += s.Count
		case bottle:
			gotBottles += s.Count
		}
	}
	if gotHoney != 1 || gotBottles != 4 {
		t.Errorf("the chest holds %d honey blocks and %d glass bottles, want 1 and 4", gotHoney, gotBottles)
	}
	if got := srv.InventoryAt(4, 64, -1); !servertest.NothingIn(got) {
		t.Errorf("the crafter still holds %+v, want every honey bottle spent", got)
	}
}

// TestAPoweredCrafterWithoutARecipePlaysTheFailEvent checks the failed
// craft: nothing in the grid matches, so nothing is spent and the crafter
// clicks with its own event, 1050.
func TestAPoweredCrafterWithoutARecipePlaysTheFailEvent(t *testing.T) {
	t.Parallel()
	srv, w := crafterFacingSouth(t, true)
	srv.SetInventoryAt(4, 64, -1, 0, servertest.ItemStack(t, "minecraft:dirt", 1))
	w.DrainRecorded()
	w.Events = nil
	powerCrafter(t, srv, w)
	w.DrainRecorded()

	if !slices.Contains(w.Events, protocol.WorldEventCrafterFail) {
		t.Errorf("no fail event played; events were %v", w.Events)
	}
	if got := srv.InventoryAt(4, 64, -1); got[0].Count != 1 {
		t.Errorf("the crafter's cell holds %+v, want the dirt kept", got[0])
	}
	if got := srv.InventoryAt(4, 64, 0); !servertest.NothingIn(got) {
		t.Errorf("the chest holds %+v, want nothing", got)
	}
}
