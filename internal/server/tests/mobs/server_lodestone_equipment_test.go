package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: EntityEquipment.tick, CompassItem.inventoryTick, LodestoneTracker.tick
func TestMobBodyAndSaddleCompassesLoseInvalidTargets(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Tracker", servertest.OpsListing(t, "Tracker"))
	servertest.HoldTicks(t, srv)
	if !srv.ChunkLoadedInDimension("minecraft:overworld", 0, 0) || registry.BlockOf(srv.BlockAt(0, 200, 0)) != registry.BlockAir {
		t.Fatal("the compass target needs to be loaded air")
	}

	const selector = "@e[type=minecraft:husk,limit=1]"
	const trackerKey = "minecraft:lodestone_tracker"
	c.ChatCommand("summon minecraft:husk 4.5 64 7.5 {NoAI:1b,PersistenceRequired:1b}")
	c.ChatCommand("item replace entity " + selector + ` armor.body with minecraft:compass[equippable={slot:"body"},lodestone_tracker={target:{dimension:"minecraft:overworld",pos:[0,200,0]}}]`)
	c.ChatCommand("item replace entity " + selector + ` saddle with minecraft:compass[equippable={slot:"saddle"},lodestone_tracker={target:{dimension:"minecraft:overworld",pos:[0,200,0]}}]`)
	checkTrackers := func(wantTarget bool) {
		t.Helper()
		equipment, _ := nbt.Get[nbt.Compound](entityCompound(t, c, selector), "equipment")
		for _, slot := range []string{"body", "saddle"} {
			stack, _ := nbt.Get[nbt.Compound](equipment, slot)
			components, _ := nbt.Get[nbt.Compound](stack, "components")
			tracker, ok := nbt.Get[nbt.Compound](components, trackerKey)
			if !ok {
				t.Errorf("%s compass has no tracker: %v", slot, stack)
				continue
			}
			if _, hasTarget := tracker["target"]; hasTarget != wantTarget {
				t.Errorf("%s compass tracker = %v, want target present = %t", slot, tracker, wantTarget)
			}
		}
	}
	checkTrackers(true)
	servertest.RunTicks(t, srv, 1)
	checkTrackers(false)
}

// TestMobCompassInSavedChunkLosesTargetInOneTick checks a held compass aimed
// at a never-generated chunk is cleared by the tick that first asks, not the
// next.
// vanilla: EntityEquipment.tick, CompassItem.inventoryTick, LodestoneTracker.tick
func TestMobCompassInSavedChunkLosesTargetInOneTick(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Tracker", servertest.OpsListing(t, "Tracker"))
	servertest.HoldTicks(t, srv)

	const selector = "@e[type=minecraft:husk,limit=1]"
	c.ChatCommand("summon minecraft:husk 4.5 64 7.5 {NoAI:1b,PersistenceRequired:1b}")
	c.ChatCommand("item replace entity " + selector + ` weapon.mainhand with minecraft:compass[lodestone_tracker={target:{dimension:"minecraft:overworld",pos:[98765,63,98765]}}]`)
	hasTarget := func() bool {
		t.Helper()
		equipment, _ := nbt.Get[nbt.Compound](entityCompound(t, c, selector), "equipment")
		stack, _ := nbt.Get[nbt.Compound](equipment, "mainhand")
		components, _ := nbt.Get[nbt.Compound](stack, "components")
		tracker, ok := nbt.Get[nbt.Compound](components, "minecraft:lodestone_tracker")
		if !ok {
			t.Fatalf("main hand compass has no tracker: %v", stack)
		}
		_, has := tracker["target"]
		return has
	}
	if !hasTarget() {
		t.Fatal("the compass has no target before the tick")
	}
	servertest.RunTicks(t, srv, 1)
	if hasTarget() {
		t.Error("the compass kept its target after one tick")
	}
}
