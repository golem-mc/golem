package combat

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestAPiercingArrowFliesOnThroughAnArmorStand checks a stand the arrow
// breaks does not use the arrow up when it has a pierce level, which a plain
// arrow's does.
// vanilla: AbstractArrow.onHitEntity
func TestAPiercingArrowFliesOnThroughAnArmorStand(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		level int32
		alive bool
	}{{"plain", 0, false}, {"piercing", 1, true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Archer")
			c.MoveToGrounded(3.5, 64, 6.5, 0, 0, true)
			c.Settle()
			srv.SpawnArmorStand(3.5, 64, 3.5)

			shaft := srv.ShootPiercingArrow(1.5, 64.5, 3.5, 1, 0, 0, tc.level, false)
			servertest.UntilTicks(t, srv, 200, "the stand broken", func() bool { return srv.ArmorStandCount() == 0 })
			servertest.AwaitTicks(t, srv, 3)
			if got := srv.ArrowAlive(shaft); got != tc.alive {
				t.Errorf("after breaking the stand the arrow alive = %v, want %v", got, tc.alive)
			}
		})
	}
}

// TestAPiercingArrowIsNotBlockedByAShield checks the raised shield lets a
// pierce level through.
// vanilla: LivingEntity.applyItemBlocking
func TestAPiercingArrowIsNotBlockedByAShield(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDifficulty(2)
	c := shieldBlocker(t, connect, srv, "Archerider")
	c.PumpTicks(srv, 6, func(int32, []byte) bool { return false })
	srv.ShootPiercingArrow(0.5, 65, 3.5, 0, 0, -6, 1, false)

	var hurt bool
	c.ReadTicksSkipping(srv, shieldDeadlineTicks, shieldSkip, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySetHealth {
			hurt = servertest.ExpectHealthReport(c, data).Health < 20
		}
		return hurt
	})
	if !hurt {
		t.Error("a piercing arrow was turned aside by the raised shield")
	}
}

// TestACrossbowArrowPlaysTheCrossbowHitSound checks a body hit plays
// item.crossbow.hit for a crossbow's arrow.
// vanilla: CrossbowItem.createProjectile
func TestACrossbowArrowPlaysTheCrossbowHitSound(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Archer")
	c.MoveToGrounded(3.5, 64, 6.5, 0, 0, true)
	c.Settle()
	srv.SpawnArmorStand(3.5, 64, 3.5)
	srv.ShootPiercingArrow(1.5, 64.5, 3.5, 1, 0, 0, 0, true)
	servertest.ExpectSoundEvent(t, c, "minecraft:item.crossbow.hit")
}
