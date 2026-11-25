package advancements

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestASpearChargeThroughFiveMobsObtainsSpearManyMobs: a charge whose stab
// meets five zombies at once counts, one that meets four does not.
// vanilla: KineticWeapon.damageEntities, SpearMobsTrigger.TriggerInstance.matches
func TestASpearChargeThroughFiveMobsObtainsSpearManyMobs(t *testing.T) {
	t.Parallel()
	const adv, crit = "minecraft:adventure/spear_many_mobs", "spear_many_mobs"
	for _, tc := range []struct {
		zombies int
		want    bool
	}{{5, true}, {4, false}} {
		t.Run(map[bool]string{true: "five", false: "four"}[tc.want], func(t *testing.T) {
			t.Parallel()
			srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Charger")
			c.FacePlayer(5.5, 64, 5.5, 0, 0) // yaw 0 looks along +Z
			c.Settle()
			srv.StopTick()
			srv.GiveSlot("Charger", player.HotbarSlot, protocol.ItemStack{
				ItemID: servertest.MustItemID(t, "minecraft:iron_spear"), Count: 1})
			srv.TickSurvival()
			// The stopped loop never moves the mobs, so the stacked zombies
			// meet the spear on the same tick.
			for range tc.zombies {
				srv.SetMobBaby(srv.SpawnMob("minecraft:zombie", 5.5, 64, 12.5), false)
			}
			c.UseItemRightClicks(protocol.HandMain)
			c.Settle()
			z := 5.5
			for range 24 {
				z += 0.3 // six blocks a second, past the damage condition's speed
				c.MoveTo(5.5, 64, z, 0, 0)
				c.Settle()
				srv.TickSurvival()
			}
			if got := srv.AdvancementObtained("Charger", adv, crit); got != tc.want {
				t.Errorf("obtained = %v, want %v", got, tc.want)
			}
		})
	}
}
