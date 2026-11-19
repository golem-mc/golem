package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestASweptOrJabbedGuardianPricksThePlayer lands a sword sweep and a spear
// jab on a guardian with its spikes out: the player loses the thorns damage
// either way.
// vanilla: Guardian.hurtServer, Player.doSweepAttack, Player.stabAttack
func TestASweptOrJabbedGuardianPricksThePlayer(t *testing.T) {
	t.Parallel()
	t.Run("sweep", func(t *testing.T) {
		t.Parallel()
		lost := func(withGuardian bool) float64 {
			srv, attacker, watcher := feedbackFixture(t)
			srv.GiveSlot("Fencer", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:iron_sword"), Count: 1})
			servertest.AwaitFullSwing(t, srv, "Fencer")
			cow := feedbackCow(srv, attacker, watcher)
			if withGuardian {
				srv.SetMobNoAI(srv.SpawnMob("minecraft:guardian", 4.5, 64, 6.7))
				attacker.Settle()
			}
			before := srv.PlayerHealth("Fencer")
			attacker.Attack(cow)
			attacker.Settle()
			return before - srv.PlayerHealth("Fencer")
		}
		base, got := lost(false), lost(true)
		if got-base != 2 {
			t.Errorf("health lost = %v with the guardian, %v without, want 2 more", got, base)
		}
	})
	t.Run("jab", func(t *testing.T) {
		t.Parallel()
		srv, connect := servertest.Start(t, servertest.CombatTestConfig())
		c := connect()
		jabber(t, srv, c, "Jabber")
		// The guardian is under eye level: look down at it.
		c.FacePlayer(5.5, 64, 5.5, 0, 22)
		c.Settle()
		srv.SetMobNoAI(srv.SpawnMob("minecraft:guardian", 5.5, 64, 8.5))
		c.Settle()
		before := srv.PlayerHealth("Jabber")
		chargeByHand(t, srv, "Jabber", 13)
		c.Stab()
		c.Settle()
		if lost := before - srv.PlayerHealth("Jabber"); lost != 2 {
			t.Errorf("jabber lost %v health, want 2", lost)
		}
	})
}
