package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: VillagerHostilesSensor, VillagerPanicTrigger, WakeUp, VillagerCalmDown
func TestASleepingVillagerFleesAPillagerThenReturnsToBed(t *testing.T) {
	t.Parallel()
	srv, c := villagerYard(t)
	srv.SetDayTime(12500)
	srv.SetBlock(3, 64, 0, servertest.BedStateName("red", "head", "east"))
	srv.SetBlock(2, 64, 0, servertest.BedStateName("red", "foot", "east"))
	id := srv.SpawnMobSeeded("minecraft:villager", 1.5, 64, 2.5, 2162)
	seen := c.WatchVariant(srv, "minecraft:villager")
	sleeping := func() bool {
		p := seen.Meta[protocol.MetadataIndexPose]
		return len(p) == 1 && p[0] == protocol.PoseSleeping
	}
	occupied := func() bool { return registry.StateBool(srv.BlockAt(3, 64, 0), registry.PropOccupied) }
	for range 30 {
		advanceVillager(t, srv, c, 10, &seen)
		if sleeping() {
			break
		}
	}
	if !sleeping() || !occupied() {
		t.Fatal("villager did not sleep before the danger arrived")
	}
	pillager := srv.SpawnMob("minecraft:pillager", 6.5, 64, 0.5)
	srv.SetMobNoAI(pillager)
	advanceVillager(t, srv, c, 22, &seen)
	if sleeping() || occupied() || len(seen.Meta[protocol.MetadataIndexSleepingPos]) != 0 {
		t.Fatal("pillager did not wake the villager and free its bed")
	}
	fled := false
	for range 15 {
		advanceVillager(t, srv, c, 10, &seen)
		x, _, z, ok := srv.MobPos(id)
		if !ok {
			t.Fatal("villager disappeared")
		}
		if dx, dz := x-6.5, z-0.5; dx*dx+dz*dz > 36 {
			fled = true
			break
		}
	}
	if !fled {
		t.Fatal("villager did not flee beyond six blocks from the pillager")
	}
	srv.KillMob(pillager)
	// The stationary observer must not push the villager off its path home.
	srv.SetPlayerGameMode("Villager", protocol.GameModeSpectator)
	for range 60 {
		advanceVillager(t, srv, c, 10, &seen)
		if sleeping() && occupied() {
			return
		}
	}
	t.Fatal("villager did not calm down and return to bed after the pillager died")
}
