package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestATargetHitCountsForTheShooterOnly: an arrow a player shot counts
// target_hit for that player; an arrow nobody owns counts for nobody.
// vanilla: TargetBlock.onProjectileHit
func TestATargetHitCountsForTheShooterOnly(t *testing.T) {
	t.Parallel()
	srv, _ := targetPhysicsScene(t)
	hits := func() int32 { return srv.StatValue("Actor", registry.StatCustom, "minecraft:target_hit") }

	srv.ShootArrow(7.5, 65.5, 8.5, -3, 0, 0)
	awaitTargetHit(t, srv)
	if got := hits(); got != 0 {
		t.Fatalf("target_hit = %d after an ownerless arrow, want 0", got)
	}
	servertest.DriveTargetTicks(srv, 20)

	srv.ShootPlayerArrow("Actor", 7.5, 65.5, 8.5, -3, 0, 0)
	awaitTargetHit(t, srv)
	if got := hits(); got != 1 {
		t.Errorf("target_hit = %d after the player's arrow, want 1", got)
	}
}
