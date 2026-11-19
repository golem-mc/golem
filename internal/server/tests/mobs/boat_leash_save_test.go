package mobs

import (
	"encoding/binary"
	"fmt"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
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

// TestASavedBoatLeadFindsItsPlayerOrDropsAfterTheGrace summons two boats
// whose saved lead names a holder by UUID: the one naming the player in its
// world is tied to them on its first tick, and the one naming nobody waits
// a hundred ticks and then lays its lead.
// vanilla: Leashable.restoreLeashFromSave
func TestASavedBoatLeadFindsItsPlayerOrDropsAfterTheGrace(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)

	w := servertest.OfflineUUIDWords("Herder")
	c.ChatCommand(fmt.Sprintf("summon minecraft:oak_boat 5.5 64 5.5 {leash:{UUID:[I;%d,%d,%d,%d]}}",
		int32(w[0]), int32(w[1]), int32(w[2]), int32(w[3])))
	c.Settle()
	ids := srv.BoatIDs()
	if len(ids) != 1 {
		t.Fatalf("boats after the summon = %v, want one", ids)
	}
	held := ids[0]
	servertest.RunTicks(t, srv, 1)
	if holder, _ := srv.BoatLeashHolder(held); holder != c.PlayerEntityID {
		t.Errorf("the boat's holder after a tick = %d, want the player %d", holder, c.PlayerEntityID)
	}

	c.ChatCommand("summon minecraft:oak_boat 8.5 64 5.5 {leash:{UUID:[I;1,2,3,4]}}")
	c.Settle()
	var lone int32
	for _, id := range srv.BoatIDs() {
		if id != held {
			lone = id
		}
	}
	if lone == 0 {
		t.Fatal("the second summon stood no boat")
	}
	servertest.RunTicks(t, srv, 100)
	if tag, _ := srv.BoatSaveTag(lone); tag["leash"] == nil {
		t.Fatal("the lead was given up within the hundred-tick grace")
	}
	servertest.RunTicks(t, srv, 1)
	drop := c.ExpectLeashDrop()
	if drop.Stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:lead") || drop.Stack.Count != 1 {
		t.Errorf("the grace's end dropped %+v, want one lead", drop.Stack)
	}
	if tag, _ := srv.BoatSaveTag(lone); tag["leash"] != nil {
		t.Errorf("the boat still saves the lead %v after it dropped", tag["leash"])
	}
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

// TestAFrozenTickKeepsASavedBoatLeadWaiting summons a boat whose saved lead
// names nobody under /tick freeze: the frozen hull does not count its grace,
// so it still holds the lead well past a hundred ticks.
// vanilla: TickRateManager.isEntityFrozen, Leashable.tickLeash
func TestAFrozenTickKeepsASavedBoatLeadWaiting(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.RunLine("tick freeze")
	srv.RunLine("summon minecraft:oak_boat 5.5 64 5.5 {leash:{UUID:[I;1,2,3,4]}}")
	ids := srv.BoatIDs()
	if len(ids) != 1 {
		t.Fatalf("boats after the summon = %v, want one", ids)
	}

	servertest.RunTicks(t, srv, 150)
	if tag, _ := srv.BoatSaveTag(ids[0]); tag["leash"] == nil {
		t.Error("the frozen boat gave up its waiting lead")
	}
	if n := srv.ItemCount(); n != 0 {
		t.Errorf("items after the frozen ticks = %d, want none", n)
	}
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

// TestAnEmptyHandTakesABoatBackFromItsKnot uses a knot holding a boat with
// an empty hand: the boat goes back to the player and the emptied knot comes
// down.
// vanilla: LeashFenceKnotEntity.interact
func TestAnEmptyHandTakesABoatBackFromItsKnot(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	boatID, knot := boatOnAKnot(t, srv, c)

	srv.StopTick()
	c.UseOn(knot.EntityID)
	if attached, holding := c.ExpectLinkPacket(); attached != boatID || holding != c.PlayerEntityID {
		t.Errorf("the take-back links (%d, %d), want the boat %d on the player", attached, holding, boatID)
	}
	if !c.AwaitKnotRemoval(knot.EntityID) {
		t.Error("the knot stayed up after its boat was taken back")
	}
	if holder, _ := srv.BoatLeashHolder(boatID); holder != c.PlayerEntityID {
		t.Errorf("the boat's holder after the take-back = %d, want the player %d", holder, c.PlayerEntityID)
	}
}

// TestABoatTeleportedAcrossLeavesItsPlayerAndDropsAfterTheGrace sends a boat
// tied to a player into the nether: the overworld is told the link is gone
// with no lead laid, and the twin, its holder left behind, keeps the lead
// for the grace and then drops it.
// vanilla: Entity.removeAfterChangingDimensions, Leashable.restoreLeashFromSave
func TestABoatTeleportedAcrossLeavesItsPlayerAndDropsAfterTheGrace(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	boatID := boatTiedToThePlayer(t, srv, c)

	// No player stands in the nether to keep the twin's chunk loaded.
	defer srv.HoldChunkInDimension(world.NetherName, 8, 8)()
	servertest.HoldTicks(t, srv)
	srv.RunLine("execute in minecraft:the_nether run tp @e[type=minecraft:oak_boat,limit=1] 8.5 70 8.5")
	if attached, holding := c.ExpectLinkPacket(); attached != boatID || holding != 0 {
		t.Errorf("the crossing links (%d, %d), want the boat %d let go", attached, holding, boatID)
	}
	ids := srv.BoatIDs()
	if len(ids) != 1 || ids[0] == boatID {
		t.Fatalf("boats after the crossing = %v, want one twin", ids)
	}
	twin := ids[0]
	if tag, _ := srv.BoatSaveTag(twin); tag["leash"] == nil {
		t.Fatal("the twin did not carry the lead across")
	}

	servertest.RunTicks(t, srv, 100)
	if tag, _ := srv.BoatSaveTag(twin); tag["leash"] == nil {
		t.Fatal("the twin gave up its lead within the hundred-tick grace")
	}
	if n := srv.ItemCount(); n != 0 {
		t.Fatalf("items within the grace = %d, want none: the crossing lays no lead", n)
	}
	servertest.RunTicks(t, srv, 1)
	if tag, _ := srv.BoatSaveTag(twin); tag["leash"] != nil {
		t.Errorf("the twin still saves the lead %v after the grace", tag["leash"])
	}
	if n := srv.ItemCount(); n != 1 {
		t.Errorf("items after the grace = %d, want the one lead", n)
	}
}

// TestABoatTeleportedAcrossIsReTiedToItsPlayerThere sends a boat tied to a
// player into the nether and the player after it: the twin is tied to them
// again there.
// vanilla: Entity.restoreFrom, Leashable.restoreLeashFromSave
func TestABoatTeleportedAcrossIsReTiedToItsPlayerThere(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	boatID := boatTiedToThePlayer(t, srv, c)

	// The twin's chunk is held so that it ticks without waiting on the
	// player's chunk sender, whose nether generation runs on the wall clock
	// while the grace counts ticks.
	defer srv.HoldChunkInDimension(world.NetherName, 8, 8)()
	servertest.HoldTicks(t, srv)
	srv.RunLine("execute in minecraft:the_nether run tp @e[type=minecraft:oak_boat,limit=1] 8.5 70 8.5")
	srv.RunLine("execute in minecraft:the_nether run tp Herder 8.5 70 10.5")
	servertest.WaitFor(t, srv, 100, func() bool {
		ids := srv.BoatIDs()
		if len(ids) != 1 || ids[0] == boatID || srv.PlayerDimension("Herder") != "minecraft:the_nether" {
			return false
		}
		holder, _ := srv.BoatLeashHolder(ids[0])
		return holder == c.PlayerEntityID
	})
}

// TestASneakingClickMovesABoatsLeadOntoAMobAndItStays ties a boat to the player,
// sneak-clicks a cow, and finds the boat held by the cow after ticks.
// vanilla: Entity.interact, Leashable.tickLeash
func TestASneakingClickMovesABoatsLeadOntoAMobAndItStays(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	boatID := boatTiedToThePlayer(t, srv, c)
	cow := srv.SpawnMob("minecraft:cow", 3.5, 64, 7.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOnSneaking(cow)
	if attached, holding := c.ExpectLinkPacket(); attached != boatID || holding != cow {
		t.Fatalf("the sneaking click links (%d, %d), want (%d, %d)", attached, holding, boatID, cow)
	}
	srv.TickBoats(5)
	if holder, ok := srv.BoatLeashHolder(boatID); !ok || holder != cow {
		t.Errorf("the boat's holder after ticks = %d (%v), want the cow %d", holder, ok, cow)
	}
}

// TestASavedBoatLeadFindsItsMob summons a boat whose saved lead names a living
// cow by UUID and finds it tied to the cow after a tick.
// vanilla: Leashable.restoreLeashFromSave
func TestASavedBoatLeadFindsItsMob(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)

	cow := srv.SpawnMob("minecraft:cow", 3.5, 64, 7.5)
	id, ok := srv.MobUUID(cow)
	if !ok {
		t.Fatal("the cow has no UUID")
	}
	u, err := uuid.Parse(id)
	if err != nil {
		t.Fatal(err)
	}
	b := u
	c.ChatCommand(fmt.Sprintf("summon minecraft:oak_boat 5.5 64 5.5 {leash:{UUID:[I;%d,%d,%d,%d]}}",
		int32(binary.BigEndian.Uint32(b[0:4])), int32(binary.BigEndian.Uint32(b[4:8])),
		int32(binary.BigEndian.Uint32(b[8:12])), int32(binary.BigEndian.Uint32(b[12:16]))))
	c.Settle()
	ids := srv.BoatIDs()
	if len(ids) != 1 {
		t.Fatalf("boats after the summon = %v, want one", ids)
	}
	servertest.RunTicks(t, srv, 1)
	if holder, _ := srv.BoatLeashHolder(ids[0]); holder != cow {
		t.Errorf("the boat's holder after a tick = %d, want the cow %d", holder, cow)
	}
}
