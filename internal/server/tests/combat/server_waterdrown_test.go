package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestASquidSuffocatingOutOfWaterBurstsNoBubble checks the water kinds'
// dry-land bite: the damage event goes out but the bubble burst of a
// walking mob's drowning does not.
// vanilla: AgeableWaterCreature.handleAirSupply
func TestASquidSuffocatingOutOfWaterBurstsNoBubble(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), "Diver", servertest.OpsListing(t, "Diver"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	srv.StopTick()
	release := srv.HoldChunk(5, 5)
	t.Cleanup(release)

	c.ChatCommand("summon minecraft:squid 5.5 64 5.5 {NoAI:1b}")
	squid := c.ExpectMobSpawn(protocol.EntityTypeSquid).EntityID
	c.Settle()

	srv.SetMobAir(squid, rules.AirDrownFloor+1)
	srv.TickMobs(1)

	damages, bubbles := 0, 0
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayDamageEvent:
			if protocol.NewReader(data).ReadVarInt() == squid {
				damages++
			}
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			if r.ReadI32() == squid && r.ReadU8() == protocol.EntityEventDrown {
				bubbles++
			}
		}
	})
	if damages != 1 {
		t.Errorf("the dry squid's bite told %d damage events, want the one", damages)
	}
	if bubbles != 0 {
		t.Errorf("the dry squid's bite sent %d bubble bursts, want none", bubbles)
	}
}
