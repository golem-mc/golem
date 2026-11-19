package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAnArrowStaysStuckInTheMobItHitsThenFallsOut checks the arrow count a
// landed arrow adds to a mob's metadata and the 20 * (30 - count) ticks after
// which it goes back to nought.
// vanilla: AbstractArrow.onHitEntity, LivingEntity.tick
func TestAnArrowStaysStuckInTheMobItHitsThenFallsOut(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Watcher")
	c.MoveToGround(0.5, 64, 8.5, true)
	c.Settle()

	id := srv.SpawnMob("minecraft:iron_golem", 4.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeIronGolem)
	srv.ShootArrow(10.5, 65, 0.5, -2, 0, 0)
	if got := servertest.StuckArrows(t, c, srv, id, 40); len(got) != 1 || got[0] != 1 {
		t.Fatalf("arrow counts told after the hit = %v, want [1]", got)
	}

	srv.StopTick()
	c.ReadTicks(srv, 0, func(int32, []byte) bool { return false })
	srv.TickMobs(400)
	if got := servertest.StuckArrows(t, c, srv, id, 0); len(got) != 0 {
		t.Fatalf("arrow counts told before the arrow's time = %v, want none", got)
	}
	srv.TickMobs(200)
	if got := servertest.StuckArrows(t, c, srv, id, 0); len(got) != 1 || got[0] != 0 {
		t.Fatalf("arrow counts told when the arrow fell out = %v, want [0]", got)
	}
}
