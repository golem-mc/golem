package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: BellBlockEntity.updateEntities, ReactToBell, LocateHidingPlace, SetHiddenState
func TestABellMakesAVillagerHideWithoutOccupyingTheBed(t *testing.T) {
	t.Parallel()
	srv, c := villagerYard(t, func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
	srv.SetDayTime(3000)
	srv.SetBlock(3, 64, 0, servertest.BellState(t, "floor", "north", false))
	srv.SetBlock(8, 64, 0, servertest.BedStateName("red", "head", "east"))
	srv.SetBlock(7, 64, 0, servertest.BedStateName("red", "foot", "east"))
	id := srv.SpawnMobSeeded("minecraft:villager", 1.5, 64, 2.5, 2162)
	seen := c.WatchVariant(srv, "minecraft:villager")
	c.PlaceBlockAtCursor(3, 64, 0, protocol.FaceNorth, 0.5, 0.5, 0.5)
	c.Settle()
	srv.SetPlayerGameMode("Villager", protocol.GameModeSpectator)
	advanceVillager(t, srv, c, 2, &seen)
	if got := srv.VillagerActivity(id); got != "hide" {
		t.Fatalf("activity %q after bell, want hide", got)
	}
	hid := false
	for range 25 {
		advanceVillager(t, srv, c, 10, &seen)
		x, y, z, ok := srv.MobPos(id)
		if ok && (x-8.5)*(x-8.5)+(y-64.5)*(y-64.5)+(z-0.5)*(z-0.5) < 9 {
			hid = true
			break
		}
	}
	if !hid {
		t.Fatal("villager did not reach the hiding bed")
	}
	if registry.StateBool(srv.BlockAt(8, 64, 0), registry.PropOccupied) {
		t.Fatal("hiding occupied the bed")
	}
	if pose := seen.Meta[protocol.MetadataIndexPose]; len(pose) != 0 && pose[0] == protocol.PoseSleeping {
		t.Fatal("hiding villager slept")
	}
	advanceVillager(t, srv, c, 300, &seen)
	if got := srv.VillagerActivity(id); got == "hide" {
		t.Fatal("villager did not leave HIDE after the bell timeout")
	}
}

// vanilla: BellBlock.neighborChanged, BellBlockEntity.updateEntities
func TestARedstoneBellAlsoMakesAVillagerHide(t *testing.T) {
	t.Parallel()
	srv, c := villagerYard(t, func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
	srv.SetDayTime(3000)
	srv.SetBlock(3, 64, 0, servertest.BellState(t, "floor", "north", false))
	srv.SetBlock(8, 64, 0, servertest.BedStateName("red", "head", "east"))
	srv.SetBlock(7, 64, 0, servertest.BedStateName("red", "foot", "east"))
	id := srv.SpawnMobSeeded("minecraft:villager", 1.5, 64, 2.5, 2162)
	seen := c.WatchVariant(srv, "minecraft:villager")
	srv.SetPlayerGameMode("Villager", protocol.GameModeCreative)
	c.SetCreativeSlot(player.HotbarSlot, servertest.ItemStack(t, "minecraft:redstone_block", 1))
	c.PlaceBlock(3, 63, 1, protocol.FaceTop)
	c.Settle()
	srv.TickPhysics(3)
	advanceVillager(t, srv, c, 2, &seen)
	if got := srv.VillagerActivity(id); got != "hide" {
		t.Fatalf("activity %q after redstone bell, want hide", got)
	}
}
