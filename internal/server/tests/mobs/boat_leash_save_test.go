package mobs

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// TestABoatTiedToAFenceIsStillTiedAfterItsChunkReloads ties a boat to a
// fence knot, lets its chunk unload with the player's leaving, and finds the
// knot's cell saved as the boat's lead and the reloaded boat tied to the
// knot on that fence again.
// vanilla: AbstractBoat.addAdditionalSaveData, Leashable.restoreLeashFromSave
func TestABoatTiedToAFenceIsStillTiedAfterItsChunkReloads(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Herder"))
	c := connect().Join("Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	boatOnAKnot(t, srv, c)

	c.Disconnect()
	servertest.WaitFor(t, srv, 200, func() bool { return srv.BoatCount() == 0 })
	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:oak_boat", func(compound nbt.Compound) {
		if got, _ := nbt.Get[nbt.IntArray](compound, "leash"); !slices.Equal(got, nbt.IntArray{6, 64, 5}) {
			t.Errorf("the saved boat's leash = %v, want the fence's cell [6 64 5]", compound["leash"])
		}
	})

	connect().Join("Herder")
	servertest.WaitFor(t, srv, 200, func() bool {
		ids := srv.BoatIDs()
		if len(ids) != 1 {
			return false
		}
		holder, _ := srv.BoatLeashHolder(ids[0])
		knotID, ok := srv.KnotAt(6, 64, 5)
		return ok && holder == knotID
	})
}

// boatTiedToThePlayer stands a boat beside the joined player and ties it to
// them with the one lead given, leaving the hand empty.
func boatTiedToThePlayer(t *testing.T, srv *server.Server, c *servertest.Client) int32 {
	t.Helper()
	boatID := srv.SpawnBoat(5.5, 64, 7.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	servertest.AwaitBoatSpawn(c, protocol.EntityTypeOakBoat)
	giveLead(t, srv, "Herder", 1)
	c.UseOn(boatID)
	if attached, holding := c.ExpectLinkPacket(); attached != boatID || holding != c.PlayerEntityID {
		t.Fatalf("the tie links (%d, %d), want the boat %d on the player", attached, holding, boatID)
	}
	return boatID
}

// boatOnAKnot ties a boat to the player and moves it onto a knot on a fence
// at (6, 64, 5), leaving the hand empty.
func boatOnAKnot(t *testing.T, srv *server.Server, c *servertest.Client) (int32, servertest.SpawnedKnot) {
	t.Helper()
	c.ChatCommand("setblock 6 64 5 minecraft:oak_fence")
	c.Settle()
	boatID := boatTiedToThePlayer(t, srv, c)
	// The use on the fence moves the boat onto a knot there.
	// vanilla: LeadItem.bindPlayerMobs
	c.UseBlock(6, 64, 5)
	knot := c.ExpectKnotSpawn()
	if attached, holding := c.ExpectLinkPacket(); attached != boatID || holding != knot.EntityID {
		t.Fatalf("the fence's link holds (%d, %d), want the boat %d on the knot %d",
			attached, holding, boatID, knot.EntityID)
	}
	return boatID, knot
}

// TestShearsOnAKnotCutABoatsLead uses shears on a knot holding a boat: the
// lead is cut and dropped, and the emptied knot comes down.
// vanilla: LeashFenceKnotEntity.interact, Entity.shearOffAllLeashConnections
func TestShearsOnAKnotCutABoatsLead(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	boatID, knot := boatOnAKnot(t, srv, c)

	srv.StopTick()
	srv.GiveSlot("Herder", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:shears"), Count: 1})
	c.UseOn(knot.EntityID)
	if drop := c.ExpectLeashDrop(); drop.Stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:lead") || drop.Stack.Count != 1 {
		t.Errorf("the shears dropped %+v, want one lead", drop.Stack)
	}
	if !c.AwaitKnotRemoval(knot.EntityID) {
		t.Error("the knot stood after the boat's lead was cut")
	}
	if holder, ok := srv.BoatLeashHolder(boatID); !ok || holder != 0 {
		t.Errorf("the boat's holder after the shears = (%d, %v), want none", holder, ok)
	}
}
