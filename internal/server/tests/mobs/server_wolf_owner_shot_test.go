package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestATameWolfTakesUpWhatItsOwnerShoots: an arrow or wind charge of the owner's that hits a
// mob or a player makes it the owner's last-hurt entity, which the wolf
// attacks.
// vanilla: AbstractArrow.onHitEntity, AbstractWindCharge.onHitEntity, OwnerHurtTargetGoal
func TestATameWolfTakesUpWhatItsOwnerShoots(t *testing.T) {
	t.Parallel()
	for _, windCharge := range []bool{false, true} {
		for _, victimIsPlayer := range []bool{false, true} {
			srv, connect, owner := wolfSceneWith(t, "Owner", servertest.MobTestConfig())
			srv.GiveSlot("Owner", player.HotbarSlot, protocol.ItemStack{})
			wolf := srv.SpawnMob(wolfType, 0.5, 64, 3.5)
			srv.TameMobFor(wolf, "Owner")
			var want int32
			if victimIsPlayer {
				other := connect().Join("Foe")
				owner.ExpectPlayerAdded("Foe")
				other.MoveToGround(4.5, 64, 0.5, true)
				owner.Settle()
				want = other.PlayerEntityID
			} else {
				want = srv.SpawnMob("minecraft:zombie", 4.5, 64, 0.5)
				srv.SetMobNoAI(want)
			}
			if windCharge {
				srv.ThrowPlayerWindCharge("Owner", 2.5, 64.9, 0.5, -90, 0)
			} else {
				srv.ShootPlayerArrow("Owner", 2.5, 64.9, 0.5, 1.5, 0, 0)
			}
			servertest.RunTicks(t, srv, 8)
			if got := srv.MobTargetID(wolf); got != want {
				t.Errorf("wind charge=%v victim player=%v: the wolf targets %d, want %d", windCharge, victimIsPlayer, got, want)
			}
		}
	}
}
