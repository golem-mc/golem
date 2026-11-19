package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAFoxKitFromASpawnEggTrustsItsSpawner: a fox egg used on a grown fox
// makes a kit that trusts the player.
// vanilla: Fox.onOffspringSpawnedFromEgg
func TestAFoxKitFromASpawnEggTrustsItsSpawner(t *testing.T) {
	t.Parallel()
	srv, c := servertest.EggPlayer(t, "survival", "Breeder", "minecraft:fox_spawn_egg", 2)
	srv.StopTick()
	srv.SpawnMob(foxType, 2.5, 64, 1.5)
	parent := c.ExpectMobSpawn(protocol.EntityTypeFox)
	if parent.Baby {
		t.Fatal("the planted fox is a baby")
	}
	c.UseOn(parent.EntityID)
	kit := c.AwaitSpawn(protocol.EntityTypeFox)
	if !srv.FoxTrusts(kit.EntityID, "Breeder") {
		t.Error("the kit does not trust the player whose egg made it")
	}
}
