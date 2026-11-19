package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// mobFlagsOf returns the Mob flags byte a Set Entity Metadata packet carries
// for entityID, and whether it carries one.
func mobFlagsOf(c *servertest.Client, data []byte, entityID int32) (uint8, bool) {
	r := protocol.NewReader(data)
	if r.ReadVarInt() != entityID {
		return 0, false
	}
	entries, ok := servertest.ReadMetadata(r)
	if !ok {
		c.T.Fatalf("metadata for entity %d carries a field this reader does not know", entityID)
	}
	for _, e := range entries {
		if e.Index == protocol.MetadataIndexMobFlags {
			return e.ByteVal, true
		}
	}
	return 0, false
}

// A vindicator's MeleeAttackGoal raises the aggressive bit when the hunt
// starts and holds it until the goal stops, so the client draws the axe for
// every blow. The goal itself stops once its path is done (it does not follow
// an unseen target), so the bit may drop between hunts; it must never be
// down on a tick a blow lands.
// vanilla: MeleeAttackGoal.start, MeleeAttackGoal.canContinueToUse, VindicatorRenderer
func TestAVindicatorKeepsItsAxeRaisedWhileItHunts(t *testing.T) {
	t.Parallel()
	srv, c, playerID := servertest.JoinAs(t, servertest.HostileTestConfig(), "Prey")
	srv.GiveEffect("Prey", "minecraft:resistance", 600, 4, false)
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob("minecraft:vindicator", 4.5, 64, 4.5)

	raised, blows := false, 0
	for tick := range 100 {
		blow := false
		c.ReadTicksSkipping(srv, 1, servertest.HostileSkip, func(pid int32, data []byte) bool {
			switch pid {
			case protocol.IDPlayEntityMetadata:
				if flags, ok := mobFlagsOf(c, data, id); ok {
					raised = flags&protocol.EntityFlagAggressive != 0
				}
			case protocol.IDPlayDamageEvent:
				blow = blow || protocol.NewReader(data).ReadVarInt() == playerID
			}
			return false
		})
		if blow {
			blows++
			if !raised {
				t.Fatalf("the vindicator's blow on tick %d landed with its aggressive bit down", tick)
			}
		}
	}
	if blows == 0 {
		t.Fatal("the vindicator landed no blow in the window")
	}
}

// A piglin's brain sets the aggressive bit every tick it has an attack
// target, so a piglin chasing with a sword holds it up and never flickers.
// vanilla: PiglinAi.updateActivity, Piglin.getArmPose
func TestAPiglinWithASwordIsAggressiveWhileItHasATarget(t *testing.T) {
	t.Parallel()
	c, playerID, srv := piglinWorld(t)
	srv.GiveEffect("Slayer", "minecraft:resistance", 600, 4, false)
	id := srv.SpawnMob(barterPiglin, 6.5, 64, 0.5)
	srv.SetMobBaby(id, false)
	srv.SetMobEquipment(id, "mainhand", protocol.ItemStack{ItemID: int32(registry.ItemGoldenSword), Count: 1})

	e, ok := c.MetaEntryWithinTicks(srv, 40, id, protocol.MetadataIndexMobFlags)
	if !ok || e.ByteVal&protocol.EntityFlagAggressive == 0 {
		t.Fatalf("the piglin's aggressive bit was not set within 40 ticks (flags %#x, found %v)", e.ByteVal, ok)
	}
	for range 60 {
		c.ReadTicksSkipping(srv, 1, servertest.HostileSkip, func(pid int32, data []byte) bool {
			if pid != protocol.IDPlayEntityMetadata {
				return false
			}
			if flags, ok := mobFlagsOf(c, data, id); ok && flags&protocol.EntityFlagAggressive == 0 {
				c.T.Fatal("the piglin cleared its aggressive bit")
			}
			return false
		})
		if srv.MobTargetID(id) != playerID {
			t.Fatalf("the piglin dropped its target (now %d)", srv.MobTargetID(id))
		}
	}
}
