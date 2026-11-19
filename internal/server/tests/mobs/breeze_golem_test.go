package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestBreezeShootsIronGolem stands a breeze and an iron golem six blocks
// apart with the player in creative, so the golem is the only target, and
// waits for the breeze's wind charge to fly toward the golem.
// vanilla: Breeze.canAttack, BreezeAttackEntitySensor
func TestBreezeShootsIronGolem(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.ItemCreativeConfig(), "Calm")
	if srv.SpawnMob("minecraft:breeze", 0.5, 64, 6.5) == 0 {
		t.Fatal("could not spawn the breeze")
	}
	if srv.SpawnMob("minecraft:iron_golem", 6.5, 64, 6.5) == 0 {
		t.Fatal("could not spawn the golem")
	}
	charge := c.AwaitSpawn(protocol.EntityTypeBreezeWindCharge)
	// The breeze may have slid before it shot, so aim is judged from where the
	// charge appeared.
	if toward := (6.5-charge.X)*charge.Vx + (6.5-charge.Z)*charge.Vz; toward <= 0 {
		t.Errorf("charge at %.1f,%.1f velocity %.2f,%.2f does not head for the golem near 6.5,6.5", charge.X, charge.Z, charge.Vx, charge.Vz)
	}
}
