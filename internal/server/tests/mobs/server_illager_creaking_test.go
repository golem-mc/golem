package mobs

import (
	"math"
	"testing"
)

// TestIllagersFleeACreaking: a pillager, a vindicator and an evoker three
// blocks from a creaking run until it is out of their eight-block watch.
// vanilla: Pillager.registerGoals, Vindicator.registerGoals, Evoker.registerGoals
func TestIllagersFleeACreaking(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"minecraft:pillager", "minecraft:vindicator", "minecraft:evoker"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, _ := creakingWorld(t)
			pos := srv.PlayerPosition(creakingPlayer)
			x, z := pos.X+40, pos.Z
			creaking := srv.SpawnMob("minecraft:creaking", x+3, pos.Y, z)
			illager := srv.SpawnMobSeeded(kind, x, pos.Y, z, 1)
			srv.TickMobs(120)
			cx, _, cz, _ := srv.MobPos(creaking)
			ix, _, iz, ok := srv.MobPos(illager)
			if !ok {
				t.Fatal("the illager is gone")
			}
			if d := math.Hypot(ix-cx, iz-cz); d < 8 {
				t.Errorf("%s stands %.1f blocks from the creaking after 120 ticks, want it out of the 8-block watch", kind, d)
			}
		})
	}
}
