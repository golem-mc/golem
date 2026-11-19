package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func mobY(t *testing.T, srv *server.Server, id int32) float64 {
	t.Helper()
	for _, m := range srv.Mobs() {
		if m.ID == id {
			return m.Y
		}
	}
	t.Fatalf("mob %d is gone", id)
	return 0
}

// A loaded chunk outside the simulation distance holds its mobs still until a
// player's simulation distance reaches it.
// vanilla: ServerLevel.tick, DistanceManager.inEntityTickingRange
func TestMobsBeyondTheSimulationDistanceStandStill(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = 10, 4
	srv, connect := servertest.Start(t, cfg)
	c := connect()
	c.ViewAsk = 10
	c.Join("Watcher")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)

	far := srv.SpawnMob("minecraft:pig", 6*16+8.5, 84, 8.5)
	near := srv.SpawnMob("minecraft:pig", 2*16+8.5, 74, 8.5)
	servertest.RunTicks(t, srv, 40)
	if y := mobY(t, srv, far); y != 84 {
		t.Errorf("far pig y = %v, want it frozen at 84", y)
	}
	if y := mobY(t, srv, near); y > 64.5 {
		t.Errorf("near pig y = %v, want it landed", y)
	}

	c.MoveToGround(6*16+8.5, 64, 0.5, true)
	c.Settle()
	servertest.UntilTicks(t, srv, 40, "the far pig falls", func() bool {
		return mobY(t, srv, far) < 84
	})
}
