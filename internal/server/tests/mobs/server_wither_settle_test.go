package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAWitherWithNoMoveOrderSinksOntoTheGround: a wither built half a block
// above the ground falls while it charges, because its flying move control
// turns gravity back on when no order stands.
// vanilla: WitherBoss (FlyingMoveControl hoversInPlace=false), LivingEntity.travelInAir
func TestAWitherWithNoMoveOrderSinksOntoTheGround(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetSpawning(false)
	c.ChatCommand("summon minecraft:wither 1.5 64.45 1.5 {Invul:220}")
	w := c.ExpectMobSpawn(protocol.EntityTypeWither)
	servertest.UntilTicks(t, srv, 12, "the wither to settle on y 64", func() bool {
		_, minY, _, _, _, _, ok := srv.MobBox(w.EntityID)
		return ok && minY == 64
	})
}
