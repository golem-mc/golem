package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The camel husk: a monster camel that cannot breed, and the camel husk a
// natural husk may ride.
// vanilla: CamelHusk, Husk.finalizeSpawn

// camelHuskSeed makes a natural husk's roll for a camel husk land.
const camelHuskSeed = 4

func mobByID(srv *server.Server, id int32) server.TestMob {
	for _, m := range srv.Mobs() {
		if m.ID == id {
			return m
		}
	}
	return server.TestMob{}
}

// TestCamelHuskSpawnsAndDoesNotBreed: a summoned camel husk arrives with a
// camel's 32 health and feeding it a rabbit foot never sends it into love.
// vanilla: CamelHusk.canFallInLove, isFood
func TestCamelHuskSpawnsAndDoesNotBreed(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:rabbit_foot", 8, func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)
	typeID, _ := registry.EntityTypeID("minecraft:camel_husk")
	id := srv.SpawnMob("minecraft:camel_husk", 1.5, 64, 2.5)
	got := c.ExpectMobSpawn(typeID)
	if got.EntityID != id || got.Health != 32 {
		t.Fatalf("spawn = %+v, want entity %d with 32 health", got, id)
	}
	c.UseOn(id)
	c.Settle()
	if mobByID(srv, id).InLove != 0 {
		t.Fatal("a camel husk fed a rabbit foot fell in love")
	}
	if !srv.MobPersistent(id) {
		t.Fatal("interacting did not make the camel husk persistent")
	}
}

// TestNaturalHuskRollsCamelHuskJockey: a natural husk whose roll lands holds
// an iron spear on a camel husk, a parched behind it; a summoned one never
// rolls.
// vanilla: Husk.finalizeSpawn
func TestNaturalHuskRollsCamelHuskJockey(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.MobTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMobSeeded("minecraft:husk", 5.5, 64, 0.5, camelHuskSeed, true)
	if hand := srv.MobHand(id); registry.Item(hand.ItemID) != registry.ItemIronSpear {
		t.Fatalf("hand = %+v", hand)
	}
	vehicle := srv.MobVehicleID(id)
	if vehicle == 0 || mobByID(srv, vehicle).Kind != "minecraft:camel_husk" {
		t.Fatalf("husk rides %d (%s), want a camel husk", vehicle, mobByID(srv, vehicle).Kind)
	}
	riders := srv.MobPassengerIDs(vehicle)
	if len(riders) != 2 || riders[0] != id || mobByID(srv, riders[1]).Kind != "minecraft:parched" {
		t.Fatalf("riders = %v, want the husk then a parched", riders)
	}
	if other := srv.SpawnMobSeeded("minecraft:husk", 7.5, 64, 0.5, camelHuskSeed); srv.MobVehicleID(other) != 0 {
		t.Fatal("a summoned husk rolled a camel husk")
	}
}
