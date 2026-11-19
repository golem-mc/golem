package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestDamageCommandCreditsByEntity checks the by clause on a mob: a player
// named by it is credited with the kill (an experience orb drops), and a
// whole hit shoves the mob away from that player.
func TestDamageCommandCreditsByEntity(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)

	c.Run(t, "kill by a player drops experience", func(t *testing.T) {
		c.ChatCommand("summon minecraft:zombie 3.5 64 0.5 {NoAI:1b}")
		c.Settle()
		c.ChatCommand("damage @e[type=minecraft:zombie,limit=1] 100 minecraft:player_attack by Testificate")
		c.Settle()
		servertest.UntilTicks(t, srv, 40, "an experience orb from the zombie", func() bool { return srv.OrbCount() > 0 })
	})

	c.Run(t, "hit by a player shoves away from them", func(t *testing.T) {
		zombie := srv.SpawnMob("minecraft:zombie", 3.5, 64, 0.5)
		servertest.RunTicks(t, srv, 2)
		x0, _, _, ok := srv.MobPos(zombie)
		if !ok {
			t.Fatal("zombie not found")
		}
		c.ChatCommand("damage " + mobTarget(t, srv, zombie) + " 1 minecraft:mob_attack by Testificate")
		c.Settle()
		servertest.UntilTicks(t, srv, 5, "the zombie shoved toward +x", func() bool {
			x, _, _, _ := srv.MobPos(zombie)
			return x > x0+0.2
		})
	})
	c.Disconnect()
}
