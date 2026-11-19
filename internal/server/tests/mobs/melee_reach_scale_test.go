package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestZombieReachUsesThePlayersBox stands a zombie on a one-block step beside
// the player: a standing player's box reaches its feet, a half-scale player's
// box ends below them.
// vanilla: Mob.isWithinMeleeAttackRange
func TestZombieReachUsesThePlayersBox(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, scale string
		hit         bool
	}{
		{"half scale", "0.5", false},
		{"full scale", "1", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Target", servertest.OpsListing(t, "Target"))
			servertest.HoldTicks(t, srv)
			c.ChatCommand("attribute @s minecraft:scale base set " + tc.scale)
			servertest.AwaitSystemChatSaying(t, c, "commands.attribute.base_value.set.success")
			srv.SetBlock(1, 64, 0, servertest.BlockState(t, "minecraft:stone"))
			// A pace of zero keeps the zombie on the step.
			srv.SpawnMob("minecraft:zombie", 1.5, 65, 0.5)
			c.ChatCommand("attribute @e[type=minecraft:zombie,limit=1] minecraft:movement_speed base set 0")
			servertest.AwaitSystemChatSaying(t, c, "commands.attribute.base_value.set.success")
			if tc.hit {
				c.ExpectWithinTicks(srv, 60, protocol.IDPlaySetHealth)
				return
			}
			servertest.ExpectNoHealth(t, srv, c, 60)
		})
	}
}
