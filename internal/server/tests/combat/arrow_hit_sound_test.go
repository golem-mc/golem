package combat

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
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

// TestAnArrowThatHurtsAMobPlaysItsHitSound checks the same for a mob body.
// vanilla: AbstractArrow.onHitEntity
func TestAnArrowThatHurtsAMobPlaysItsHitSound(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Watcher")
	c.MoveToGround(0.5, 64, 8.5, true)
	c.Settle()

	srv.SpawnMob("minecraft:cow", 4.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.ShootArrow(10.5, 65, 0.5, -2, 0, 0)
	servertest.ExpectSoundEvent(t, c, "minecraft:entity.arrow.hit")
}
