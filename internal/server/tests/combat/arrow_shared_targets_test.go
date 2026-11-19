package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestArrowsShareTargetsWithoutLosingOwnersOrStandHits exercises shared mob
// targets, shooter exclusion and different appended stands in one held tick.
func TestArrowsShareTargetsWithoutLosingOwnersOrStandHits(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BowTestConfig(), "Fletcher", func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)
	shooter := srv.SpawnMobSeeded("minecraft:cow", 4.5, 64, 4.5, 1)
	victim := srv.SpawnMobSeeded("minecraft:cow", 6.5, 64, 4.5, 2)
	srv.SetMobNoAI(shooter)
	srv.SetMobNoAI(victim)
	srv.SpawnArmorStand(6.5, 64, 8.5)
	srv.SpawnArmorStand(6.5, 64, 12.5)
	c.ReadTicks(srv, 1, func(int32, []byte) bool { return false })
	arrows := []int32{
		srv.ShootMobArrow(shooter, 4.5, 65, 4.5, 3, 0, 0),
		srv.ShootMobArrow(shooter, 4.5, 65, 4.5, 3, 0, 0),
		srv.ShootArrow(4.5, 65, 8.5, 3, 0, 0),
		srv.ShootArrow(4.5, 65, 12.5, 3, 0, 0),
	}
	c.ReadTicks(srv, 1, func(int32, []byte) bool { return false })
	if hp, _ := srv.MobHealth(shooter); hp != 10 {
		t.Errorf("shooter health = %v, want 10 (owner excluded)", hp)
	}
	if hp, _ := srv.MobHealth(victim); hp >= 10 {
		t.Errorf("victim health = %v, want damage", hp)
	}
	if n := srv.ArmorStandCount(); n != 0 {
		t.Errorf("%d stands remain, want both per-arrow targets hit", n)
	}
	if srv.ArrowAlive(arrows[0]) || !srv.ArrowAlive(arrows[1]) || srv.ArrowAlive(arrows[2]) || srv.ArrowAlive(arrows[3]) {
		t.Error("want first arrow consumed, second refused, and stand arrows consumed")
	}
}
