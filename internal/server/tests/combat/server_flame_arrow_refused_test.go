package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// A flaming arrow whose hurt is refused puts back the burn the body had
// before the arrow lit it.
// vanilla: AbstractArrow.onHitEntity

// TestAFlamingArrowInTheHurtCooldownLeavesAMobUnlit hits a cow with a plain
// fast arrow, then a burning slow one inside the cooldown that the first
// blow's damage outweighs: the second is refused and lights nothing.
func TestAFlamingArrowInTheHurtCooldownLeavesAMobUnlit(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)

	id := srv.SpawnMob("minecraft:cow", 2.5, 65, 0.5)
	srv.ShootArrow(5.5, 65.5, 0.5, -3, 0, 0)
	servertest.DriveTargetTicks(srv, 4)
	arrow := srv.ShootArrow(5.5, 65.5, 0.5, -1, 0, 0)
	srv.IgniteArrow(arrow, 100)
	servertest.DriveTargetTicks(srv, 8)

	if got := srv.MobFire(id); got != 0 {
		t.Errorf("the cow's fire = %d after a refused burning arrow, want 0", got)
	}
}
