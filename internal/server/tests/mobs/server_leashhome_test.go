package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	server "github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// The leash-home tests: a lead coming off clears the mob's home, whichever
// way the lead came off — snapped by the walk that stretched it out, untied
// by the holder's own click, dropped for a holder that died, or spent by a
// boat's seat taking the body aboard — and a mount with no lead to spend
// leaves the home alone. The home cleared is whatever the mob had, whether the
// lead set it (PathfinderMob.whenLeashedTo) or not, as vanilla's clearHome is
// blind to where the home came from.
// vanilla: Leashable.dropLeash, Mob.onLeashRemoved

// aHomeCowTiedToTheHolder stands a cow with its home at its own cell and
// ties it to the player's lead, and returns it.
func aHomeCowTiedToTheHolder(t *testing.T, srv *server.Server, c *servertest.Client) int32 {
	t.Helper()
	giveLead(t, srv, "Herder", 1)
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	if !srv.RestrictMob(cowID, 5, 64, 5, 4) {
		t.Fatal("the cow was not found to restrict")
	}
	if _, _, _, _, set, ok := srv.MobHome(cowID); !ok || !set {
		t.Fatalf("the restricted cow's home is set %t (mob found %t), want one", set, ok)
	}
	c.UseOn(cowID)
	if _, holding := c.ExpectLinkPacket(); holding != c.PlayerEntityID {
		t.Fatalf("the tie holds entity %d, want the holder", holding)
	}
	return cowID
}

// homeGone reports whether the mob's home is gone, the way a lead coming
// off takes it.
func homeGone(srv *server.Server, cowID int32) bool {
	_, _, _, _, set, ok := srv.MobHome(cowID)
	return ok && !set
}

// TestASnappedLeadClearsTheLeashedMobsHome checks the lead run out of reach:
// the cow led thirteen blocks out is past the twelve of the lead, which
// drops at it — and the home its holder had the mob in is gone with the
// hold.
// vanilla: Leashable.tickLeash, Mob.onLeashRemoved
func TestASnappedLeadClearsTheLeashedMobsHome(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	cowID := aHomeCowTiedToTheHolder(t, srv, c)

	// Thirteen blocks: past the twelve a lead runs out at.
	c.MoveToGround(-7.5, 64, 5.5, true)
	c.Settle()
	srv.TickMobs(2)

	drop := c.ExpectLeashDrop()
	if drop.Stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:lead") || drop.Stack.Count != 1 {
		t.Errorf("the dropped lead = %+v, want one lead", drop.Stack)
	}
	if attached, holding := c.ExpectLinkPacket(); attached != cowID || holding != 0 {
		t.Errorf("the snap links (%d, %d), want the cow %d held by none", attached, holding, cowID)
	}
	if !homeGone(srv, cowID) {
		t.Error("the cow kept its home after its lead snapped")
	}
}

// TestAKilledHoldersLeadLeavesNoHome checks the holder's death: the lead a
// dead holder cannot hold drops at the mob, and the home goes with it.
// vanilla: Entity.canInteractWithLevel, Leashable.tickLeash, Mob.onLeashRemoved
func TestAKilledHoldersLeadLeavesNoHome(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	cowID := aHomeCowTiedToTheHolder(t, srv, c)

	srv.Hurt("Herder", 1000)
	srv.TickMobs(2)

	drop := c.ExpectLeashDrop()
	if drop.Stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:lead") || drop.Stack.Count != 1 {
		t.Errorf("the dropped lead = %+v, want one lead", drop.Stack)
	}
	if attached, holding := c.ExpectLinkPacket(); attached != cowID || holding != 0 {
		t.Errorf("the holder's death links (%d, %d), want the cow %d held by none", attached, holding, cowID)
	}
	if !homeGone(srv, cowID) {
		t.Error("the cow kept its home after its holder died")
	}
}

// TestABoardingLeashedMobLosesItsHome checks the lead Mob.startRiding drops:
// the boat's seat that takes the leashed cow aboard spends its lead, and
// the home goes with it.
// vanilla: Mob.startRiding, Mob.onLeashRemoved
func TestABoardingLeashedMobLosesItsHome(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	boatID := srv.SpawnBoat(5.5, 64, 5.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	srv.TickBoats(1)
	servertest.AwaitBoatSpawn(c, protocol.EntityTypeOakBoat)

	cowID := aHomeCowTiedToTheHolder(t, srv, c)

	srv.TickBoats(1)
	if got := srv.BoatPassengerIDs(boatID); len(got) != 1 || got[0] != cowID {
		t.Fatalf("the boat's passengers = %v, want the cow %d pushed aboard", got, cowID)
	}
	drop := c.ExpectLeashDrop()
	if drop.Stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:lead") || drop.Stack.Count != 1 {
		t.Errorf("the boarded cow dropped %+v, want one lead", drop.Stack)
	}
	if attached, holding := c.ExpectLinkPacket(); attached != cowID || holding != 0 {
		t.Errorf("the boarding links (%d, %d), want the cow %d held by none", attached, holding, cowID)
	}
	if !homeGone(srv, cowID) {
		t.Error("the cow kept its home after its lead was boarded off")
	}
}
