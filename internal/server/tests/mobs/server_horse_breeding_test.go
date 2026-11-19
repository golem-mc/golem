package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestATameHorseAndDonkeyFedGoldenCarrotsBreedAMule: a golden carrot sends
// each tame adult into love, the pair courts across kinds, and the foal is a
// mule.
// vanilla: AbstractHorse.handleEating, Horse.canMate, Horse.getBreedOffspring
func TestATameHorseAndDonkeyFedGoldenCarrotsBreedAMule(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:golden_carrot", 8)
	horseType, _ := registry.EntityTypeID("minecraft:horse")
	donkeyType, _ := registry.EntityTypeID("minecraft:donkey")
	muleType, _ := registry.EntityTypeID("minecraft:mule")
	horse := srv.SpawnMob("minecraft:horse", 1.5, 64, 2.5)
	donkey := srv.SpawnMob("minecraft:donkey", 3.5, 64, 2.5)
	c.AwaitSpawn(horseType)
	c.AwaitSpawn(donkeyType)
	srv.TameHorse(horse)
	srv.TameHorse(donkey)

	c.UseOn(horse)
	c.AwaitEntityEvent(horse, loveHearts)
	c.UseOn(donkey)
	c.AwaitEntityEvent(donkey, loveHearts)
	if got := srv.InventorySlot("Rancher", player.HotbarSlot); got.Count != 6 {
		t.Errorf("%d golden carrots left, want 6: each love spends one", got.Count)
	}

	foal := c.AwaitSpawn(muleType)
	if flag := servertest.MetaOf(c.AwaitMetadataFor(foal.EntityID), protocol.MetadataIndexBaby); flag == nil || !flag.FlagVal {
		t.Error("the bred mule is not a foal")
	}
}

// TestAWildHorseTakesNoLoveFromAGoldenCarrot: an untamed horse eats the
// carrot for temper, with no hearts.
// vanilla: AbstractHorse.handleEating
func TestAWildHorseTakesNoLoveFromAGoldenCarrot(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:golden_carrot", 8)
	horseType, _ := registry.EntityTypeID("minecraft:horse")
	horse := srv.SpawnMob("minecraft:horse", 1.5, 64, 2.5)
	srv.SetMobNoAI(horse)
	c.AwaitSpawn(horseType)

	c.UseOn(horse)
	seen := watchWolf(t, c, srv, horse, 2)
	if !seen.heard("minecraft:entity.horse.eat") {
		t.Errorf("no eating sound in %v", seen.sounds)
	}
	if got := srv.InventorySlot("Rancher", player.HotbarSlot); got.Count != 7 {
		t.Errorf("%d golden carrots left, want 7", got.Count)
	}
	if m, ok := mobWithID(srv, horse); !ok || m.InLove != 0 {
		t.Errorf("the wild horse loves for %d ticks, want none", m.InLove)
	}
}

// TestAFedMulePlaysItsOwnEatingSound: a mule eats with the mule's sound.
// vanilla: Mule.getEatingSound
func TestAFedMulePlaysItsOwnEatingSound(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:wheat", 8)
	muleType, _ := registry.EntityTypeID("minecraft:mule")
	mule := srv.SpawnMob("minecraft:mule", 1.5, 64, 2.5)
	srv.SetMobNoAI(mule)
	c.AwaitSpawn(muleType)

	c.UseOn(mule)
	seen := watchWolf(t, c, srv, mule, 2)
	if !seen.heard("minecraft:entity.mule.eat") {
		t.Errorf("no mule eating sound in %v", seen.sounds)
	}
}
