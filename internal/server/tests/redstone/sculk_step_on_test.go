package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestStandingOnASculkSensorActivatesIt: a player who sneaks onto a sensor and
// stands still, and a zombie standing on one, even by the edge of its box,
// activate it within ten ticks; a warden does not.
// vanilla: SculkSensorBlock.stepOn
func TestStandingOnASculkSensorActivatesIt(t *testing.T) {
	t.Parallel()
	join := func(t *testing.T) (*server.Server, *servertest.Client) {
		t.Helper()
		cfg := servertest.BlockTestConfig()
		cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
		return servertest.Join(t, cfg, "Actor")
	}
	phaseOf := func(srv interface{ BlockAt(x, y, z int32) uint16 }) string {
		return registry.StateValue(srv.BlockAt(4, 64, 4), registry.PropSculkSensorPhase)
	}
	t.Run("sneaking player", func(t *testing.T) {
		t.Parallel()
		srv, c := join(t)
		srv.SetBlock(4, 64, 4, servertest.MustState(t, "minecraft:sculk_sensor", nil))
		servertest.HoldTicks(t, srv)
		c.SendSneak(true)
		c.Settle()
		c.MoveToGround(4.5, 64.5, 4.5, true)
		c.Settle()
		servertest.UntilTicks(t, srv, 10, "the sensor is active", func() bool { return phaseOf(srv) == "active" })
	})
	for _, tc := range []struct {
		name, kind string
		x          float64
		want       bool
	}{
		{"zombie", "minecraft:zombie", 4.5, true},
		// The centre is over the air beside the sensor.
		{"zombie off centre", "minecraft:zombie", 5.1, true},
		{"warden", "minecraft:warden", 4.5, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := join(t)
			srv.SetBlock(4, 64, 4, servertest.MustState(t, "minecraft:sculk_sensor", nil))
			servertest.HoldTicks(t, srv)
			id := srv.SpawnMob(tc.kind, tc.x, 64.5, 4.5)
			srv.SetMobNoAI(id)
			if tc.want {
				servertest.UntilTicks(t, srv, 10, "the sensor is active", func() bool { return phaseOf(srv) == "active" })
				return
			}
			servertest.RunTicks(t, srv, 10)
			if got := phaseOf(srv); got != "inactive" {
				t.Fatalf("phase = %q, want inactive", got)
			}
		})
	}
}
