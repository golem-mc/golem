package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestRestingBodyKeepsTriggeringASculkSensor: a minecart, primed TNT or armor
// stand resting on a sensor schedules its step again every time the sensor
// can activate, so the sensor is never inactive for long.
// vanilla: Entity.applyEffectsFromBlocks, SculkSensorBlock.stepOn
func TestRestingBodyKeepsTriggeringASculkSensor(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, block, summon string
		ticks               int
	}{
		{"minecart on calibrated", "calibrated_sculk_sensor", "summon minecraft:minecart 4.5 65 4.5", 45},
		{"tnt on calibrated", "calibrated_sculk_sensor", "summon minecraft:tnt 4.5 65 4.5 {fuse:400s}", 45},
		{"armor stand on sensor", "sculk_sensor", "summon minecraft:armor_stand 4.5 65 4.5", 60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.BlockTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, _ := servertest.Join(t, cfg, "Actor")
			srv.SetBlock(4, 64, 4, servertest.MustState(t, "minecraft:"+tc.block, nil))
			servertest.HoldTicks(t, srv)
			srv.RunRconLine(tc.summon)
			servertest.RunTicks(t, srv, tc.ticks)
			if got := registry.StateValue(srv.BlockAt(4, 64, 4), registry.PropSculkSensorPhase); got == "inactive" {
				t.Fatalf("phase at tick %d = %q, want not inactive", tc.ticks, got)
			}
			if got := sensorFrequency(t, srv); got != 1 {
				t.Fatalf("frequency = %d, want 1", got)
			}
		})
	}
}

// TestBoatLightsRedstoneOre: a boat landing on redstone ore lights it.
// vanilla: RedStoneOreBlock.stepOn
func TestBoatLightsRedstoneOre(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, _ := servertest.Join(t, cfg, "Actor")
	srv.SetBlock(4, 64, 4, servertest.MustState(t, "minecraft:redstone_ore", nil))
	servertest.HoldTicks(t, srv)
	srv.RunRconLine("summon minecraft:oak_boat 4.5 65.1 4.5")
	servertest.UntilTicks(t, srv, 12, "the ore is lit", func() bool {
		return registry.StateLit(srv.BlockAt(4, 64, 4))
	})
}
