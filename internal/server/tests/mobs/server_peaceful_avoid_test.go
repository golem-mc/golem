package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestNothingFleesAPlayerInPeaceful: in Peaceful no mob can attack a player,
// so the avoid goals that look for one through forCombat targeting never
// find one; a fox and a cod stay put beside the player.
// vanilla: LivingEntity.canAttack, TargetingConditions.test
func TestNothingFleesAPlayerInPeaceful(t *testing.T) {
	t.Parallel()
	t.Run("fox", func(t *testing.T) {
		t.Parallel()
		srv := foxScene(t, false)
		srv.SetSpawning(false)
		srv.SetDifficulty(0)
		fox := srv.SpawnMobSeeded(foxType, 8.5, 64, 0.5, 8)
		servertest.RunTicks(t, srv, 80)
		if st := foxState(t, srv, fox); math.Hypot(st.X-0.5, st.Z-0.5) > 14 {
			t.Errorf("the fox fled to (%.1f, %.1f)", st.X, st.Z)
		}
	})
	t.Run("cod", func(t *testing.T) {
		t.Parallel()
		srv, c := squidTests(t)
		srv.SetDifficulty(0)
		layLake(t, srv)
		id := summonSquid(t, c, "minecraft:cod", fishType(t, "minecraft:cod"), pondCentreX, pondNorth)
		for tick := range 40 {
			srv.TickMobs(1)
			// the avoid goal swims at 1.6 or 1.4; the random swim at 1
			if f := srv.MobNavSpeedFactor(id); f > 1.3 {
				t.Fatalf("tick %d: the cod set off at speed factor %.1f, fleeing the player", tick, f)
			}
		}
	})
}
