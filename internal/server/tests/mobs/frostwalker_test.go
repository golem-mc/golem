package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// Frost Walker's location_changed effect on a mob, black box.
// vanilla: LivingEntity.baseTick/checkFallDamage/collectEquipmentChanges,
// EnchantmentHelper.runLocationChangedEffects, frost_walker.json

// frostWalkerMobServer fills the five-by-five water field around the stone
// the zombie stands on, holds the loop there, and returns the server.
func frostWalkerMobServer(t *testing.T, name string) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	t.Cleanup(srv.HoldChunk(5, 5))
	servertest.HoldTicks(t, srv)
	c.Settle()

	water := servertest.BlockState(t, "minecraft:water")
	stone := servertest.BlockState(t, "minecraft:stone")
	air := servertest.BlockState(t, "minecraft:air")
	for x := int32(3); x <= 7; x++ {
		for z := int32(3); z <= 7; z++ {
			if !srv.SetBlock(x, 63, z, water) {
				t.Fatal("the water field's chunk was not loaded")
			}
			srv.SetBlock(x, 64, z, air)
		}
	}
	srv.SetBlock(8, 63, 5, water)
	srv.SetBlock(9, 63, 5, water)
	if !srv.SetBlock(5, 63, 5, stone) {
		t.Fatal("the zombie's chunk was not loaded")
	}
	zombie := srv.SpawnMob("minecraft:zombie", 5.5, 64, 5.5)
	if !srv.SetMobNoAI(zombie) {
		t.Fatal("the zombie could not be held still")
	}
	return srv, c, zombie
}

// TestAZombieInFrostWalkerBootsFreezesTheWaterBesideIt: the first tick in
// the boots freezes the source water around the stone, but not beyond the
// second level's radius.
func TestAZombieInFrostWalkerBootsFreezesTheWaterBesideIt(t *testing.T) {
	t.Parallel()
	srv, c, zombie := frostWalkerMobServer(t, "Freezer")
	uuid, _ := srv.MobUUID(zombie)
	c.ChatCommand(`item replace entity ` + uuid + ` armor.feet with diamond_boots[enchantments={"minecraft:frost_walker":2}]`)
	c.Settle()

	servertest.RunTicks(t, srv, 1)
	servertest.ExpectBlockNamed(t, srv, 6, 63, 5, "minecraft:frosted_ice", nil)
	servertest.ExpectBlockNamed(t, srv, 4, 63, 5, "minecraft:frosted_ice", nil)
	// Level 2 reaches four blocks; x=9 is the first source past that radius.
	if got, water := srv.BlockAt(9, 63, 5), servertest.BlockState(t, "minecraft:water"); got != water {
		t.Errorf("block at (9, 63, 5) = %d, want the untouched water %d", got, water)
	}
}
