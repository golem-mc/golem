package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestAnArrowRingingABellCountsForItsShooter: a bell an arrow rings counts
// bell_ring for the player who shot it.
// vanilla: BellBlock.onProjectileHit
func TestAnArrowRingingABellCountsForItsShooter(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)
	if !srv.SetBlock(6, 65, 0, servertest.BellState(t, "floor", "north", false)) {
		t.Fatal("could not stand the bell")
	}
	srv.ShootArrow(6.5, 65.4, 3, 0, 0, -3)
	servertest.DriveTargetTicks(srv, 8)
	if got := srv.StatValue("Actor", registry.StatCustom, "minecraft:bell_ring"); got != 0 {
		t.Fatalf("bell_ring = %d after an ownerless arrow, want 0", got)
	}
	srv.ShootPlayerArrow("Actor", 6.5, 65.4, 3, 0, 0, -3)
	servertest.DriveTargetTicks(srv, 8)
	if got := srv.StatValue("Actor", registry.StatCustom, "minecraft:bell_ring"); got != 1 {
		t.Errorf("bell_ring = %d after the player's arrow, want 1", got)
	}
}
