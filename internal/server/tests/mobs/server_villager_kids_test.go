package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Timelines.VILLAGER_SCHEDULE, PlayTagWithOtherKids, SleepInBed
func TestAChildVillagerChasesAnotherChildAtPlaytimeThenSleepsAtNight(t *testing.T) {
	t.Parallel()
	const name = "Playtime"
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	c.MoveToGround(0.5, 64, 10.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetDayTime(3000)
	srv.SetBlock(3, 64, 3, servertest.BedStateName("red", "head", "east"))
	srv.SetBlock(2, 64, 3, servertest.BedStateName("red", "foot", "east"))
	c.ChatCommand(`summon minecraft:villager 6.5 64 0.5 {Age:-24000,NoAI:1b}`)
	c.WatchVariant(srv, "minecraft:villager")
	c.ChatCommand(`summon minecraft:villager 0.5 64 0.5 {Age:-24000}`)
	child := c.WatchVariant(srv, "minecraft:villager")
	if !srv.MobIsBaby(child.EntityID) {
		t.Fatal("summoned villager was not a child")
	}
	chased := false
	for range 30 {
		advanceVillager(t, srv, c, 10, &child)
		x, _, z, ok := srv.MobPos(child.EntityID)
		if !ok {
			t.Fatal("child disappeared")
		}
		if dx, dz := x-6.5, z-0.5; dx*dx+dz*dz < 4 {
			chased = true
			break
		}
	}
	if !chased {
		t.Fatal("child did not approach its stationary playmate within 300 ticks")
	}
	srv.SetDayTime(12500)
	for range 40 {
		advanceVillager(t, srv, c, 10, &child)
		pose := child.Meta[protocol.MetadataIndexPose]
		if len(pose) == 1 && pose[0] == protocol.PoseSleeping {
			if !registry.StateBool(srv.BlockAt(3, 64, 3), registry.PropOccupied) {
				t.Fatal("sleeping child did not occupy its bed")
			}
			return
		}
	}
	t.Fatal("child did not leave PLAY and sleep at night")
}
