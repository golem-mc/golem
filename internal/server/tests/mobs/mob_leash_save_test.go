package mobs

import (
	"fmt"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// TestACowTiedToAFenceIsStillTiedAfterItsChunkReloads ties a cow to a fence
// knot, lets its chunk unload with the player's leaving, and finds the
// knot's cell saved as the cow's lead and the reloaded cow tied to the knot
// on that fence again.
// vanilla: Mob.addAdditionalSaveData, Leashable.restoreLeashFromSave
func TestACowTiedToAFenceIsStillTiedAfterItsChunkReloads(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Herder"))
	c := connect().Join("Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	leashedCowOnAKnot(t, srv, c, 6)

	c.Disconnect()
	servertest.WaitFor(t, srv, 200, func() bool { return srv.MobCount() == 0 })
	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:cow", func(compound nbt.Compound) {
		if got, _ := nbt.Get[nbt.IntArray](compound, "leash"); !slices.Equal(got, nbt.IntArray{6, 64, 5}) {
			t.Errorf("the saved cow's leash = %v, want the fence's cell [6 64 5]", compound["leash"])
		}
	})

	connect().Join("Herder")
	servertest.WaitFor(t, srv, 200, func() bool {
		ids := srv.MobIDsOfKind("minecraft:cow")
		if len(ids) != 1 {
			return false
		}
		holder, _ := srv.MobLeashHolder(ids[0])
		knotID, ok := srv.KnotAt(6, 64, 5)
		return ok && holder == knotID
	})
}

// TestASavedMobLeadFindsItsPlayerOrDropsAfterTheGrace summons two cows whose
// saved lead names a holder by UUID: the one naming the player in its world
// is tied to them on its first tick, and the one naming nobody waits a
// hundred ticks, a /data write in between, and then lays its lead.
// vanilla: Leashable.restoreLeashFromSave
func TestASavedMobLeadFindsItsPlayerOrDropsAfterTheGrace(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)

	w := servertest.OfflineUUIDWords("Herder")
	srv.RunLine(fmt.Sprintf("summon minecraft:cow 5.5 64 5.5 {NoAI:1b,leash:{UUID:[I;%d,%d,%d,%d]}}",
		int32(w[0]), int32(w[1]), int32(w[2]), int32(w[3])))
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	ids := srv.MobIDsOfKind("minecraft:cow")
	if len(ids) != 1 {
		t.Fatalf("cows after the summon = %v, want one", ids)
	}
	held := ids[0]
	servertest.RunTicks(t, srv, 1)
	if holder, _ := srv.MobLeashHolder(held); holder != c.PlayerEntityID {
		t.Errorf("the cow's holder after a tick = %d, want the player %d", holder, c.PlayerEntityID)
	}
	if attached, holding := c.ExpectLinkPacket(); attached != held || holding != c.PlayerEntityID {
		t.Errorf("the restore links (%d, %d), want the cow %d on the player", attached, holding, held)
	}

	srv.RunLine("summon minecraft:cow 8.5 64 5.5 {NoAI:1b,leash:{UUID:[I;1,2,3,4]}}")
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	var lone int32
	for _, id := range srv.MobIDsOfKind("minecraft:cow") {
		if id != held {
			lone = id
		}
	}
	if lone == 0 {
		t.Fatal("the second summon stood no cow")
	}
	// A /data write on the waiting cow keeps one grace, not a second
	// count of it.
	srv.RunLine("data merge entity @e[type=minecraft:cow,x=8.5,y=64,z=5.5,distance=..1,limit=1] {Silent:1b}")
	servertest.RunTicks(t, srv, 100)
	if n := srv.ItemCount(); n != 0 {
		t.Fatalf("items within the hundred-tick grace = %d, want none", n)
	}
	servertest.RunTicks(t, srv, 1)
	drop := c.ExpectLeashDrop()
	if drop.Stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:lead") || drop.Stack.Count != 1 {
		t.Errorf("the grace's end dropped %+v, want one lead", drop.Stack)
	}
	if holder, _ := srv.MobLeashHolder(lone); holder != 0 {
		t.Errorf("the lone cow's holder after the grace = %d, want none", holder)
	}
}

// TestDataRemoveTakesAMobsLeadOff removes a tied cow's leash key with /data:
// the lead comes off with the link told gone and no item laid, and the
// emptied knot comes down.
// vanilla: Leashable.readLeashData
func TestDataRemoveTakesAMobsLeadOff(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	cowID, knot := leashedCowOnAKnot(t, srv, c, 6)

	servertest.HoldTicks(t, srv)
	srv.RunLine("data remove entity @e[type=minecraft:cow,limit=1] leash")
	if attached, holding := c.ExpectLinkPacket(); attached != cowID || holding != 0 {
		t.Errorf("the removal links (%d, %d), want the cow %d let go", attached, holding, cowID)
	}
	if !c.AwaitKnotRemoval(knot.EntityID) {
		t.Error("the knot stood after its only lead was removed")
	}
	if holder, _ := srv.MobLeashHolder(cowID); holder != 0 {
		t.Errorf("the cow's holder after the removal = %d, want none", holder)
	}
	if n := srv.ItemCount(); n != 0 {
		t.Errorf("items after the removal = %d, want none", n)
	}
}
