package advancements

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// The criteria triggers the gameplay fires: each test does the thing in the
// world and reads the criterion off the player's progress.

// killWithSword plants a mob of the kind beside the player, who strikes it
// down with one blow, and waits for the death to be handled.
func killWithSword(t *testing.T, name, kind string, entityType int32) {
	t.Helper()
	srv, c := servertest.BreedingPlayer(t, "survival", name, "", 0)
	srv.GiveSlot(name, player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:netherite_sword", "minecraft:sharpness", 5))
	srv.SpawnMob(kind, 1.5, 64, 2.5)
	mob := c.ExpectMobSpawn(entityType)
	// Twenty health takes two blows of the sword.
	for range 2 {
		servertest.AwaitFullSwing(t, srv, name)
		c.Attack(mob.EntityID)
		c.Settle()
	}
	servertest.UntilTicks(t, srv, 100, "the kill is credited", func() bool {
		return srv.AdvancementObtained(name, "minecraft:adventure/root", "killed_something")
	})
	if got := srv.AdvancementObtained(name, "minecraft:adventure/kill_a_mob", kind); got != (kind == "minecraft:zombie") {
		t.Errorf("killing a %s: kill_a_mob's criterion of that kind obtained = %t", kind, got)
	}
	if srv.AdvancementObtained(name, "minecraft:adventure/kill_a_mob", "minecraft:skeleton") {
		t.Errorf("killing a %s obtained the skeleton criterion", kind)
	}
	if srv.AdvancementObtained(name, "minecraft:adventure/root", "killed_by_something") {
		t.Error("killed_by_something was obtained by a kill")
	}
}

// TestKillingAMobObtainsItsCriteria: a cow's death is a kill for the root's
// killed_something, a zombie's also for its own kill_a_mob criterion and no
// other. vanilla: ServerPlayer.awardKillScore, KilledTrigger
func TestKillingAMobObtainsItsCriteria(t *testing.T) {
	t.Parallel()
	t.Run("cow", func(t *testing.T) { t.Parallel(); killWithSword(t, "Butcher", "minecraft:cow", protocol.EntityTypeCow) })
	t.Run("zombie", func(t *testing.T) {
		t.Parallel()
		killWithSword(t, "Slayer", "minecraft:zombie", protocol.EntityTypeZombie)
	})
}

// TestEatingObtainsConsumeItemCriteria: a finished meal is a consume_item
// for the root's any-item criterion and for the meal's own.
// vanilla: Consumable.onConsume, ConsumeItemTrigger
func TestEatingObtainsConsumeItemCriteria(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BowTestConfig(), "Eater")
	servertest.HoldTicks(t, srv)
	srv.SetFood("Eater", 10)
	srv.GiveSlot("Eater", player.HotbarSlot, servertest.ItemStack(t, "minecraft:bread", 2))
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	if !srv.Eating("Eater") {
		t.Fatal("the meal begins")
	}
	servertest.UntilTicks(t, srv, 100, "the meal is eaten", func() bool {
		return srv.AdvancementObtained("Eater", "minecraft:husbandry/root", "consumed_item")
	})
	if !srv.AdvancementObtained("Eater", "minecraft:husbandry/balanced_diet", "bread") {
		t.Error("eating bread did not obtain balanced_diet's bread")
	}
	if srv.AdvancementObtained("Eater", "minecraft:husbandry/balanced_diet", "apple") {
		t.Error("eating bread obtained balanced_diet's apple")
	}
}

// TestBreedingObtainsBredAnimals: two cows fed by the player breed into a
// calf, for the root of the breeding and the cow's own criterion.
// vanilla: Animal.finalizeSpawnChildFromBreeding, BredAnimalsTrigger
func TestBreedingObtainsBredAnimals(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:wheat", 64)
	srv.SpawnMob("minecraft:cow", 1.5, 64, 2.5)
	srv.SpawnMob("minecraft:cow", 2.5, 64, 2.5)
	cow1 := c.ExpectMobSpawn(protocol.EntityTypeCow)
	cow2 := c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(cow1.EntityID)
	c.AwaitEntityEvent(cow1.EntityID, 18)
	c.UseOn(cow2.EntityID)
	c.AwaitEntityEvent(cow2.EntityID, 18)
	if srv.AdvancementObtained("Rancher", "minecraft:husbandry/breed_an_animal", "bred") {
		t.Fatal("feeding alone obtained bred")
	}
	servertest.UntilTicks(t, srv, 700, "the cows breed", func() bool {
		return srv.AdvancementObtained("Rancher", "minecraft:husbandry/breed_an_animal", "bred")
	})
	if !srv.AdvancementObtained("Rancher", "minecraft:husbandry/bred_all_animals", "minecraft:cow") {
		t.Error("breeding cows did not obtain bred_all_animals' cow")
	}
	if srv.AdvancementObtained("Rancher", "minecraft:husbandry/bred_all_animals", "minecraft:pig") {
		t.Error("breeding cows obtained bred_all_animals' pig")
	}
}

// TestStandingInABiomeObtainsItsLocationCriterion: once a second the biome
// under the player is a location trigger.
// vanilla: ServerPlayer.doTick, PlayerTrigger
func TestStandingInABiomeObtainsItsLocationCriterion(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Walker")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	biome := srv.OverworldBiomeAt(0, 64, 0)
	if biome == "" {
		t.Fatal("the spawn chunk has no biome")
	}
	servertest.UntilTicks(t, srv, 60, "the biome is visited", func() bool {
		return srv.AdvancementObtained("Walker", "minecraft:adventure/adventuring_time", biome)
	})
	if srv.AdvancementObtained("Walker", "minecraft:adventure/adventuring_time", "minecraft:nether_wastes") {
		t.Error("an unvisited biome was obtained")
	}
}

// killBesideACatalyst strikes a zombie down within reach of a sculk catalyst
// (or of bare stone) and reports whether the player got "Sculk Catalyst's
// first kill". vanilla: CatalystListener.tryAwardItSpreadsAdvancement
func killBesideACatalyst(t *testing.T, catalyst bool) bool {
	t.Helper()
	const name = "Sculker"
	srv, c := servertest.BreedingPlayer(t, "survival", name, "", 0)
	srv.GiveSlot(name, player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:netherite_sword", "minecraft:sharpness", 5))
	if catalyst {
		srv.SetBlock(4, 64, 2, servertest.MustState(t, "minecraft:sculk_catalyst", map[string]string{"bloom": "false"}))
	}
	srv.SpawnMob("minecraft:zombie", 1.5, 64, 2.5)
	mob := c.ExpectMobSpawn(protocol.EntityTypeZombie)
	for range 2 {
		servertest.AwaitFullSwing(t, srv, name)
		c.Attack(mob.EntityID)
		c.Settle()
	}
	servertest.UntilTicks(t, srv, 100, "the kill is credited", func() bool {
		return srv.AdvancementObtained(name, "minecraft:adventure/root", "killed_something")
	})
	return srv.AdvancementObtained(name, "minecraft:adventure/kill_mob_near_sculk_catalyst", "kill_mob_near_sculk_catalyst")
}
