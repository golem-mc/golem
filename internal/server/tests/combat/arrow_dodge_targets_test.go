package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestLaterArrowsRespectAnEndermansDodge compares one arrow with two in the
// same tick: a shaft through the old box must not trigger a second teleport.
func TestLaterArrowsRespectAnEndermansDodge(t *testing.T) {
	var positions [2][3]float64
	for i := range positions {
		cfg := servertest.BowTestConfig()
		cfg.ViewDistance = config.MinDistance
		cfg.SimulationDistance = config.MinDistance
		srv, c := servertest.Join(t, cfg, "Fletcher", func(s *server.Server) {
			servertest.FreezeUntilHeld(t, s)
		})
		servertest.HoldTicks(t, srv)
		id := srv.SpawnMobSeeded("minecraft:enderman", 8.5, 64, 8.5, 1)
		srv.SetMobNoAI(id)
		// The mob needs one tick to bind the world used by its immediate dodge.
		c.ReadTicks(srv, 1, func(int32, []byte) bool { return false })
		for range i + 1 {
			srv.ShootArrow(6.5, 65, 8.5, 3, 0, 0)
		}
		c.ReadTicks(srv, 1, func(int32, []byte) bool { return false })
		x, y, z, ok := srv.MobPos(id)
		if !ok || (x == 8.5 && z == 8.5) {
			t.Fatal("the first arrow did not make the enderman dodge")
		}
		positions[i] = [3]float64{x, y, z}
	}
	if positions[0] != positions[1] {
		t.Fatalf("second arrow hit the old box: one-arrow destination %v, two-arrow destination %v", positions[0], positions[1])
	}
}
