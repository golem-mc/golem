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

// TestAZombieWithoutFrostWalkerBootsLeavesTheWaterAlone: twenty ticks without
// the enchantment change no cell.
func TestAZombieWithoutFrostWalkerBootsLeavesTheWaterAlone(t *testing.T) {
	t.Parallel()
	srv, _, _ := frostWalkerMobServer(t, "Barefoot")

	servertest.RunTicks(t, srv, 20)
	servertest.ExpectBlockNamed(t, srv, 6, 63, 5, "minecraft:water", nil)
	servertest.ExpectBlockNamed(t, srv, 4, 63, 5, "minecraft:water", nil)
	servertest.ExpectBlockNamed(t, srv, 9, 63, 5, "minecraft:water", nil)
}

// TestAZombieLandingInFrostWalkerBootsFreezesTheWaterBesideIt: a landing in
// the same feet cell triggers the effect even though baseTick has already
// run and travel is about to spend the fall.
// vanilla: LivingEntity.checkFallDamage
func TestAZombieLandingInFrostWalkerBootsFreezesTheWaterBesideIt(t *testing.T) {
	t.Parallel()
	srv, c, zombie := frostWalkerMobServer(t, "Landing")
	uuid, _ := srv.MobUUID(zombie)
	c.ChatCommand(`item replace entity ` + uuid + ` armor.feet with diamond_boots[enchantments={"minecraft:frost_walker":2}]`)
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	servertest.ExpectBlockNamed(t, srv, 6, 63, 5, "minecraft:frosted_ice", nil)

	c.ChatCommand(`attribute ` + uuid + ` minecraft:movement_speed base set 0`)
	c.ChatCommand(`data merge entity ` + uuid + ` {NoAI:0b,Motion:[0.0d,0.2d,0.0d]}`)
	c.Settle()
	srv.SetBlock(6, 63, 5, servertest.BlockState(t, "minecraft:water"))
	landed := false
	for tick := 1; tick <= 8; tick++ {
		servertest.RunTicks(t, srv, 1)
		x, y, z, _ := srv.MobPos(zombie)
		if x != 5.5 || y < 64 || y >= 65 || z != 5.5 {
			t.Fatalf("landing tick %d left the feet cell: position=(%g,%g,%g)", tick, x, y, z)
		}
		if y == 64 {
			landed = true
			break
		}
	}
	if !landed {
		t.Fatal("the zombie did not land within eight ticks")
	}
	servertest.ExpectBlockNamed(t, srv, 6, 63, 5, "minecraft:frosted_ice", nil)
}

// TestAnArmorStandInFrostWalkerBootsFreezesTheWaterBesideIt: an armor stand
// is a living body too, and a change in its feet slot triggers the effect.
// vanilla: ArmorStand, LivingEntity.collectEquipmentChanges
func TestAnArmorStandInFrostWalkerBootsFreezesTheWaterBesideIt(t *testing.T) {
	t.Parallel()
	srv, c, _ := frostWalkerMobServer(t, "Stand")
	srv.SpawnArmorStand(5.5, 64, 5.5)
	srv.TickArmorStands(2)
	c.ChatCommand(`item replace entity @e[type=minecraft:armor_stand,limit=1] armor.feet with diamond_boots[enchantments={"minecraft:frost_walker":2}]`)
	c.Settle()

	servertest.RunTicks(t, srv, 1)
	servertest.ExpectBlockNamed(t, srv, 6, 63, 5, "minecraft:frosted_ice", nil)
	servertest.ExpectBlockNamed(t, srv, 4, 63, 5, "minecraft:frosted_ice", nil)
}

// vanilla: LivingEntity.checkFallDamage, ArmorStand.travel
func TestAnArmorStandLandingInLavaRefreezesTheWaterBesideIt(t *testing.T) {
	t.Parallel()
	srv, c, _ := frostWalkerMobServer(t, "LavaLanding")
	id := srv.SpawnArmorStand(5.5, 64, 5.5)
	srv.TickArmorStands(2)
	c.ChatCommand(`item replace entity @e[type=minecraft:armor_stand,limit=1] armor.feet with diamond_boots[enchantments={"minecraft:frost_walker":2}]`)
	c.Settle()
	srv.TickArmorStands(1)
	servertest.ExpectBlockNamed(t, srv, 6, 63, 5, "minecraft:frosted_ice", nil)

	srv.SetBlock(5, 64, 5, servertest.BlockState(t, "minecraft:lava"))
	c.ChatCommand(`data merge entity @e[type=minecraft:armor_stand,limit=1] {Motion:[0.0d,0.2d,0.0d]}`)
	c.Settle()
	srv.SetBlock(6, 63, 5, servertest.BlockState(t, "minecraft:water"))
	airborne, landed := false, false
	for tick := 1; tick <= 20; tick++ {
		srv.TickArmorStands(1)
		stands := srv.ArmorStandsInDimension("minecraft:overworld")
		if len(stands) != 1 || stands[0].ID != id {
			t.Fatalf("stand disappeared: %v", stands)
		}
		stand := stands[0]
		if stand.X != 5.5 || stand.Y < 64 || stand.Y >= 65 || stand.Z != 5.5 {
			t.Fatalf("tick %d left the feet cell: %+v", tick, stand)
		}
		airborne = airborne || stand.Y > 64
		if airborne && stand.Y == 64 {
			landed = true
			break
		}
	}
	if !landed {
		t.Fatal("the armor stand did not jump and land within twenty ticks")
	}
	servertest.ExpectBlockNamed(t, srv, 6, 63, 5, "minecraft:frosted_ice", nil)
}

// The cow's centre is in the next grid cell, but its box overlaps the
// outer half of a water block whose centre is inside the level-II disk.
// vanilla: ReplaceDisk.apply, UnobstructedPredicate.test
func TestFrostWalkerKeepsWaterObstructedAtTheGridEdge(t *testing.T) {
	t.Parallel()
	srv, c, zombie := frostWalkerMobServer(t, "GridEdge")
	uuid, _ := srv.MobUUID(zombie)
	c.ChatCommand(`data merge entity ` + uuid + ` {Pos:[5.51d,64.0d,5.5d]}`)
	cow := srv.SpawnMob("minecraft:cow", 10.4, 63.5, 5.5)
	if !srv.SetMobNoAI(cow) {
		t.Fatal("the obstructing cow could not be held still")
	}
	c.ChatCommand(`item replace entity ` + uuid + ` armor.feet with diamond_boots[enchantments={"minecraft:frost_walker":2}]`)
	c.Settle()

	servertest.RunTicks(t, srv, 1)
	servertest.ExpectBlockNamed(t, srv, 8, 63, 5, "minecraft:frosted_ice", nil)
	servertest.ExpectBlockNamed(t, srv, 9, 63, 5, "minecraft:water", nil)
}
