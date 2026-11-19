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

// TestABurningCamelRunsToAPoolNotAPuddle: a body two blocks wide takes a
// water cell only with water to its south, east and south-east, so a
// burning camel passes the nearer one-cell puddle for the pool.
// vanilla: AnimalPanic.lookForWater
func TestABurningCamelRunsToAPoolNotAPuddle(t *testing.T) {
	t.Parallel()
	srv, _ := striderWorld(t)
	// A camel younger than its stand-up time refuses to move.
	srv.SetWorldAge(1000)
	water := servertest.BlockState(t, "minecraft:water")
	// The puddle is 3 off by manhattan distance, the pool's north-west
	// corner 5; its north-east cell, at 4, has no water to the east.
	for _, c := range [][2]int32{{2, 0}, {-4, 0}, {-3, 0}, {-4, 1}, {-3, 1}} {
		if !srv.SetBlock(c[0], 63, c[1], water) {
			t.Fatalf("could not lay water at %v", c)
		}
	}
	id := srv.SpawnMob("minecraft:camel", 0.5, 64, 0.5)
	srv.TickMobs(1)
	srv.SetMobFire(id, 1000)
	srv.HurtMobByWorld(id, 1, "minecraft:on_fire")
	// Once in the pool the camel is put out and panics on dry land, so the
	// test reads the westmost point it reached.
	west := math.Inf(1)
	for range 60 {
		srv.TickMobs(1)
		x, _, _, ok := srv.MobPos(id)
		if !ok {
			t.Fatal("the camel is gone")
		}
		if x > 1.5 {
			t.Fatalf("camel at x=%.2f, heading east to the one-cell puddle", x)
		}
		west = min(west, x)
	}
	if west > -2 {
		t.Errorf("camel reached x=%.2f at most in 60 ticks alight, want it at the pool's edge (x < -2)", west)
	}
}
