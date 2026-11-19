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

// TestATraderLlamaTargetsAZombie: a trader llama sets its mind on a zombie
// standing near it.
// vanilla: TraderLlama.registerGoals
func TestATraderLlamaTargetsAZombie(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Trader")
	c.Settle()
	srv.SetSpawning(false)
	servertest.HoldTicks(t, srv)
	pos := srv.PlayerPosition("Trader")

	llama := srv.SpawnMob("minecraft:trader_llama", pos.X+8, pos.Y, pos.Z)
	zombie := srv.SpawnMob("minecraft:zombie", pos.X+11, pos.Y, pos.Z)
	servertest.UntilTicks(t, srv, 100, "the trader llama targets the zombie", func() bool {
		return srv.MobTargetID(llama) == zombie
	})
}

// TestAZombieHorseFoalNeverGrowsUp: a zombie horse foal three ticks from
// adulthood stays a foal; a horse foal at the same age grows up.
// vanilla: ZombieHorse.canAgeUp
func TestAZombieHorseFoalNeverGrowsUp(t *testing.T) {
	t.Parallel()
	srv, c := striderWorld(t)

	for _, tc := range []struct {
		kind     string
		wantBaby bool
	}{
		{"minecraft:zombie_horse", true},
		{"minecraft:horse", false},
	} {
		id := srv.SpawnMob(tc.kind, 5.5, 64, 5.5)
		c.ChatCommand("data merge entity @e[type=" + tc.kind + ",limit=1] {Age:-3}")
		c.Settle()
		if !srv.MobIsBaby(id) {
			t.Fatalf("%s is not a foal after Age:-3", tc.kind)
		}
		srv.TickMobs(10)
		if got := srv.MobIsBaby(id); got != tc.wantBaby {
			t.Errorf("%s after 10 ticks: baby = %v, want %v", tc.kind, got, tc.wantBaby)
		}
	}
}
