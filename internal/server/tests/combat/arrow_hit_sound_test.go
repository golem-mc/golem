package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAnArrowThatHurtsAPlayerPlaysItsHitSound checks that the shaft's hit
// voice is sent at the body it landed in.
// vanilla: AbstractArrow.onHitEntity
func TestAnArrowThatHurtsAPlayerPlaysItsHitSound(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Victim")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	srv.ShootArrow(4.5, 65, 0.5, -2, 0, 0)
	servertest.ExpectSoundEvent(t, c, "minecraft:entity.arrow.hit")
}
