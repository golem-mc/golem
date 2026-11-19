package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestLaterThrownBodiesRespectAnEndermansSplashDodge compares a water splash
// alone with a splash and a snowball through the enderman's box in the same
// tick: the snowball must fly through the spot the splash's teleport left.
// vanilla: Enderman.hurtServer
func TestLaterThrownBodiesRespectAnEndermansSplashDodge(t *testing.T) {
	water := registry.MustPotion("minecraft:water")
	var positions [2][3]float64
	for i := range positions {
		cfg := servertest.BowTestConfig()
		cfg.ViewDistance = config.MinDistance
		cfg.SimulationDistance = config.MinDistance
		srv, c := servertest.Join(t, cfg, "Splasher", func(s *server.Server) {
			servertest.FreezeUntilHeld(t, s)
		})
		servertest.HoldTicks(t, srv)
		id := srv.SpawnMobSeeded("minecraft:enderman", 8.5, 64, 8.5, 1)
		srv.SetMobNoAI(id)
		// The mob needs one tick to bind the world used by its immediate dodge.
		c.ReadTicks(srv, 1, func(int32, []byte) bool { return false })
		srv.ThrowPotion(entity.ThrownSplashPotion, water, 8.5, 67.1, 8.5, 0, 90, 0, 1)
		if i == 1 {
			srv.ThrowThrown(entity.ThrownSnowball, 7.0, 65, 8.5, -90, 0, 0, 1)
		}
		c.ReadTicks(srv, 1, func(int32, []byte) bool { return false })
		x, y, z, ok := srv.MobPos(id)
		if !ok || (x == 8.5 && z == 8.5) {
			t.Fatal("the splash did not make the enderman dodge")
		}
		positions[i] = [3]float64{x, y, z}
	}
	if positions[0] != positions[1] {
		t.Fatalf("the snowball hit the old box: splash-only destination %v, splash-and-snowball destination %v", positions[0], positions[1])
	}
}
