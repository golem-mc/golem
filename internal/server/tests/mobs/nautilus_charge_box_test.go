package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestANautilusChargeHitsTheSwimmersBoxNotAStandingOne puts an angry nautilus
// one block over a player in the pond. A standing player's box reaches up to
// it at once; a swimmer's 0.6-tall box does not, so the charge needs a tick
// more to come down to it.
// vanilla: ChargeAttack.tick (the target's own bounding box)
func TestANautilusChargeHitsTheSwimmerBoxNotAStandingOne(t *testing.T) {
	t.Parallel()
	ticksToHit := func(swim bool) int {
		srv, c := squidTests(t)
		servertest.HoldTicks(t, srv)
		c.MoveToGround(pondCentreX, 62, -8.5, false)
		if swim {
			c.SendSprint(c.PlayerEntityID, true)
			if m := c.ExpectMetadata(); m.Pose != protocol.PoseSwimming {
				t.Fatalf("pose %d, want SWIMMING", m.Pose)
			}
		}
		c.Settle()
		id := fishType(t, "minecraft:nautilus")
		c.ChatCommand(`summon minecraft:nautilus 8.5 63 -8.5 {Brain:{memories:{"minecraft:angry_at":{value:uuid("` +
			protocol.OfflineUUID("Striker").String() + `"),ttl:400L}}}}`)
		servertest.AwaitSummonFeedback(t, c, "minecraft:nautilus", "entity.minecraft.nautilus")
		c.ExpectMobSpawn(id)
		full := srv.PlayerHealth("Striker")
		for tick := 1; tick <= 20; tick++ {
			servertest.RunTicks(t, srv, 1)
			if srv.PlayerHealth("Striker") < full {
				return tick
			}
		}
		t.Fatalf("swim=%v: the nautilus never hit the player", swim)
		return 0
	}
	standing, swimming := ticksToHit(false), ticksToHit(true)
	if swimming <= standing {
		t.Fatalf("hit after %d ticks swimming, %d standing; a swimmer's box is lower", swimming, standing)
	}
}
