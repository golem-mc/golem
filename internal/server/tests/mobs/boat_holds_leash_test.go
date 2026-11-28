package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestABoatPullsABoatLeashedToIt summons a boat and, eight blocks east, one
// whose lead names the first by UUID, and finds the second drawn in.
// vanilla: Leashable.restoreLeashFromSave, Leashable.tickLeash
func TestABoatPullsABoatLeashedToIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	// Blue ice leaves the hulls the slip a lead's pull needs to reach them.
	srv.RunLine(`fill -2 63 -2 12 63 3 minecraft:blue_ice`)
	srv.RunLine(`summon minecraft:oak_boat 0.5 64 0.5 {UUID:[I;1,2,3,4]}`)
	srv.RunLine(`summon minecraft:oak_boat 8.5 64 0.5 {leash:{UUID:[I;1,2,3,4]}}`)
	ids := srv.BoatIDs()
	if len(ids) != 2 {
		t.Fatalf("boats = %v, want two", ids)
	}
	// The hull slides on past the one that holds it and shoves it, so the
	// pull shows as the closest the two come, not where they rest.
	closest := math.Inf(1)
	for range 8 {
		servertest.RunTicks(t, srv, 5)
		x0, _, z0, _, _, _ := srv.BoatState(ids[0])
		x1, _, z1, _, _, _ := srv.BoatState(ids[1])
		closest = math.Min(closest, math.Hypot(x1-x0, z1-z0))
	}
	if closest > 2 {
		t.Errorf("the led boat came no nearer than %.2f blocks to its holder, want within 2", closest)
	}
}

// TestASneakingClickOnABoatTiesTheLedMobToIt leads a pig, sneak-uses a boat
// and finds the pig's lead held by the boat.
// vanilla: Entity.interact, Leashable.leashableInArea
func TestASneakingClickOnABoatTiesTheLedMobToIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()
	giveLead(t, srv, "Herder", 1)
	pig := srv.SpawnMob("minecraft:pig", 5.5, 64, 5.5)
	boat := srv.SpawnBoat(3.5, 64, 7.5, "minecraft:oak_boat")
	c.ExpectMobSpawn(protocol.EntityTypePig)
	servertest.AwaitBoatSpawn(c, protocol.EntityTypeOakBoat)
	c.UseOn(pig)
	c.ExpectLinkPacket()
	c.UseOnSneaking(boat)
	if attached, holding := c.ExpectLinkPacket(); attached != pig || holding != boat {
		t.Fatalf("the sneaking click links (%d, %d), want (%d, %d)", attached, holding, pig, boat)
	}
	if holder, _ := srv.MobLeashHolder(pig); holder != boat {
		t.Errorf("the pig's holder = %d, want the boat %d", holder, boat)
	}
	srv.TickMobs(5)
	if holder, _ := srv.MobLeashHolder(pig); holder != boat {
		t.Errorf("after ticks the pig's holder = %d, want the boat %d", holder, boat)
	}
}
