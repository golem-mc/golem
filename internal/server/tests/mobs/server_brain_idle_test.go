package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: ArmadilloAi.initIdleActivity, CamelAi.initIdleActivity
func TestIdleArmadillosAndCamelsStartWalkingWithinTwoHundredTicks(t *testing.T) {
	for _, kind := range []string{"minecraft:armadillo", "minecraft:camel", "minecraft:camel_husk"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.MobTestConfig(), "Observer", func(s *server.Server) {
				servertest.FreezeUntilHeld(t, s)
			})
			c.MoveToGround(8.5, 64, 40.5, true)
			c.Settle()
			servertest.HoldTicks(t, srv)
			srv.SetDayTime(18000) // Camel husks must not burn while idling.
			moved := 0
			for _, seed := range []int64{1, 2, 3, 4, 5} {
				// LastPoseTick defaults to zero: start after standing up has
				// finished, with the twenty-second sitting cooldown still live.
				srv.SetWorldAge(100)
				id := srv.SpawnMobSeeded(kind, 8.5, 64, 8.5, seed)
				srv.SetMobBaby(id, false)
				maxDistance := 0.0
				for range 200 {
					servertest.RunTicks(t, srv, 1)
					x, _, z, ok := srv.MobPos(id)
					if !ok {
						t.Fatal("idle mob disappeared")
					}
					maxDistance = max(maxDistance, math.Hypot(x-8.5, z-8.5))
				}
				t.Logf("seed %d: furthest displacement %.2f blocks", seed, maxDistance)
				if maxDistance >= 1 {
					moved++
				}
				srv.KillMob(id)
				servertest.RunTicks(t, srv, 20)
			}
			if moved < 4 {
				t.Errorf("%s moved at least one block within 200 ticks in %d/5 seeds, want at least 4/5", kind, moved)
			}
		})
	}
}
