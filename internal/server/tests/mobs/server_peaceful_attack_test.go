package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// On Peaceful no mob may take a player as its combat target.
// vanilla: LivingEntity.canAttack, TargetingConditions.test

func TestAGoatDoesNotRamAPlayerOnPeaceful(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rammed", "", 0)
	srv.SetDifficulty(0)
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob("minecraft:goat", 6.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeGoat)
	srv.SetGoatRamCooldown(id, 0)
	var w goatWatch
	c.ReadTicks(srv, 200, func(packetID int32, data []byte) bool {
		readGoat(t, c, id, &w, data, packetID)
		return false
	})
	if w.lowered != 0 || w.hurt || w.motion {
		t.Errorf("a goat on peaceful lowered its head %d times, hurt %v, shoved %v (motion x %.2f)", w.lowered, w.hurt, w.motion, w.motionX)
	}
	if h := srv.PlayerHealth("Rammed"); h != 20 {
		t.Errorf("health = %v, want 20", h)
	}
}
