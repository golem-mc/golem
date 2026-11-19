package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestOnlyANaturalZombieHorseCarriesAZombieRider: a naturally spawned zombie
// horse has a zombie holding an iron spear; a summoned one has no rider.
// vanilla: ZombieHorse.finalizeSpawn
func TestOnlyANaturalZombieHorseCarriesAZombieRider(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Rider")
	c.Settle()
	srv.SetSpawning(false)
	servertest.HoldTicks(t, srv)
	pos := srv.PlayerPosition("Rider")

	natural := srv.SpawnMobNatural("minecraft:zombie_horse", pos.X+6, pos.Y, pos.Z)
	summoned := srv.SpawnMob("minecraft:zombie_horse", pos.X-6, pos.Y, pos.Z)
	servertest.RunTicks(t, srv, 2)

	riders := srv.MobPassengerIDs(natural)
	if len(riders) != 1 {
		t.Fatalf("natural zombie horse has %d riders, want 1", len(riders))
	}
	if got := srv.MobMainHand(riders[0]); got.ItemID != int32(registry.ItemIronSpear) {
		t.Errorf("rider holds item %d, want an iron spear", got.ItemID)
	}
	if got := srv.MobPassengerIDs(summoned); len(got) != 0 {
		t.Errorf("summoned zombie horse has %d riders, want none", len(got))
	}
}
