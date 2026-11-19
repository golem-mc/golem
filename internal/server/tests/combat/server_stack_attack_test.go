package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestStackMinimumAttackChargeGatesTheJab jabs with a wooden spear that
// carries minimum_attack_charge=0 and with one that has it removed against
// the spear's own 1: a fresh jab lands only without the minimum.
// vanilla: Player.cannotAttackWithItem
func TestStackMinimumAttackChargeGatesTheJab(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		item string
		hurt bool
	}{
		{`wooden_spear[minimum_attack_charge=0.0f]`, true},
		{`wooden_spear[!minimum_attack_charge]`, true},
		{`wooden_spear[minimum_attack_charge=1.0f]`, false},
		{`wooden_spear`, false},
	} {
		t.Run(tc.item, func(t *testing.T) {
			t.Parallel()
			srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Smith", servertest.OpsListing(t, "Smith"))
			servertest.HoldTicks(t, srv)
			c.FacePlayer(5.5, 64, 5.5, 0, 0)
			c.Settle()
			c.ChatCommand("give @s " + tc.item)
			c.Settle()
			if srv.InventorySlot("Smith", player.HotbarSlot).Empty() {
				t.Fatalf("/give %s left the hand empty", tc.item)
			}
			srv.TickSurvival()
			zombie := srv.SpawnMob("minecraft:zombie", 5.5, 64, 8.5)
			full := mobHealthNow(t, srv, zombie)
			c.Stab()
			c.Settle()
			if got := mobHealthNow(t, srv, zombie); (got < full) != tc.hurt {
				t.Errorf("a fresh jab left the zombie at %v of %v, hurt want %v", got, full, tc.hurt)
			}
		})
	}
}
