package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAHurtGoatKeepsFleeingForFiveSeconds: AnimalPanic runs 100-120 ticks
// and picks a new leg whenever the walk ends, so a goat, armadillo or copper
// golem hurt once is still running at tick 60, after the hurt has gone stale.
// vanilla: AnimalPanic, GoatAi.initCoreActivity, ArmadilloAi.ArmadilloPanic
func TestAHurtGoatKeepsFleeingForFiveSeconds(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"minecraft:goat", "minecraft:armadillo", "minecraft:copper_golem"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, _ := servertest.Join(t, servertest.MobTestConfig(), "Watcher")
			servertest.HoldTicks(t, srv)
			// Ten blocks off: past the six a glance at the player reaches. With seed 2
			// a goat whose panic is one leg is idle by tick 60.
			id := srv.SpawnMobSeeded(kind, 10.5, 64, 0.5, 2)
			servertest.RunTicks(t, srv, 1)
			srv.HurtMobByWorld(id, 1, "minecraft:cactus")
			servertest.RunTicks(t, srv, 60)
			lastX, _, lastZ, ok := srv.MobPos(id)
			if !ok {
				t.Fatal("the mob is gone")
			}
			moved := 0.0
			for range 20 {
				servertest.RunTicks(t, srv, 2)
				x, _, z, _ := srv.MobPos(id)
				moved += math.Hypot(x-lastX, z-lastZ)
				lastX, lastZ = x, z
			}
			if moved < 3 {
				t.Errorf("%s moved %.2f blocks over ticks 60-100 after a hurt, want at least 3 (still panicking)", kind, moved)
			}
		})
	}
}
