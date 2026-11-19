package redstone

// The sculk sensor hearing vibrations: a placed, broken or landed-on block
// within eight blocks reaches it a block a tick later, activates it for
// thirty ticks with power by distance, then ten ticks of cooldown.
// vanilla: VibrationSystem, SculkSensorBlock, SculkSensorBlockEntity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// sensorScene stands an inactive sculk sensor at (4, 64, 4) on the held loop.
func sensorScene(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.AttachActor(t)
	standAtScene(c)
	srv.SetBlock(4, 64, 4, servertest.MustState(t, "minecraft:sculk_sensor", nil))
	servertest.HoldTicks(t, srv)
	return srv, c
}

// standAtScene walks the actor to its post before a sensor is laid, so the
// walk is not heard.
func standAtScene(c *servertest.Client) {
	c.T.Helper()
	c.MoveToGround(0.5, 64, 4.5, true)
	c.Settle()
}

func expectSensor(t *testing.T, srv *server.Server, x, y, z int32, phase, power string) {
	t.Helper()
	servertest.ExpectBlockNamed(t, srv, x, y, z, "minecraft:sculk_sensor",
		map[string]string{"sculk_sensor_phase": phase, "power": power})
}

// placeStoneAt has the actor, standing at (0.5, 64, 4.5), set stone on the
// ground at (x, 64, 4), and waits for the server to have handled it.
func placeStoneAt(c *servertest.Client, x int32) {
	c.T.Helper()
	servertest.PlaceFrom(c, "minecraft:stone", 0, 4, -90, x, 63, 4, protocol.FaceTop)
	c.Settle()
}

// TestAPlacedBlockActivatesASculkSensor places stone two blocks from the
// sensor: the vibration travels two ticks, gives power 12, holds thirty
// ticks, cools ten. The lamp beside the sensor takes the weak signal, the
// lamp beside the block under it the strong one.
// vanilla: VibrationSystem.Ticker.tick, SculkSensorBlock.activate, SculkSensorBlock.tick
func TestAPlacedBlockActivatesASculkSensor(t *testing.T) {
	t.Parallel()
	srv, c := sensorScene(t)
	lamp := servertest.MustState(t, "minecraft:redstone_lamp", nil)
	srv.SetBlock(5, 64, 4, lamp)
	srv.SetBlock(5, 63, 4, lamp)

	placeStoneAt(c, 2)
	servertest.RunTicks(t, srv, 1)
	expectSensor(t, srv, 4, 64, 4, "inactive", "0")
	servertest.RunTicks(t, srv, 1)
	// 15 - floor(15/8 * 2)
	expectSensor(t, srv, 4, 64, 4, "active", "12")
	if !servertest.LampLitAt(srv, 5, 64, 4) {
		t.Error("the lamp beside the active sensor is not lit")
	}
	if !servertest.LampLitAt(srv, 5, 63, 4) {
		t.Error("the lamp beside the block under the active sensor is not lit")
	}

	servertest.RunTicks(t, srv, 29)
	expectSensor(t, srv, 4, 64, 4, "active", "12")
	servertest.RunTicks(t, srv, 1)
	expectSensor(t, srv, 4, 64, 4, "cooldown", "0")
	servertest.RunTicks(t, srv, 9)
	expectSensor(t, srv, 4, 64, 4, "cooldown", "0")
	servertest.RunTicks(t, srv, 1)
	expectSensor(t, srv, 4, 64, 4, "inactive", "0")
	if servertest.LampLitAt(srv, 5, 64, 4) || servertest.LampLitAt(srv, 5, 63, 4) {
		t.Error("a lamp stayed lit after the sensor went quiet")
	}
}

// TestSculkSensorPowerFallsWithDistance places stone seven blocks from one
// sensor and nine from another: the near one takes power 2 after seven
// ticks of travel, the far one is out of its eight-block range.
// vanilla: VibrationSystem.getRedstoneStrengthForDistance,
// SculkSensorBlockEntity.VibrationUser.LISTENER_RANGE
func TestSculkSensorPowerFallsWithDistance(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	standAtScene(c)
	sensor := servertest.MustState(t, "minecraft:sculk_sensor", nil)
	srv.SetBlock(-5, 64, 4, sensor)
	srv.SetBlock(11, 64, 4, sensor)
	servertest.HoldTicks(t, srv)

	placeStoneAt(c, 2)
	servertest.RunTicks(t, srv, 6)
	expectSensor(t, srv, -5, 64, 4, "inactive", "0")
	servertest.RunTicks(t, srv, 1)
	expectSensor(t, srv, -5, 64, 4, "active", "2")
	servertest.RunTicks(t, srv, 5)
	expectSensor(t, srv, 11, 64, 4, "inactive", "0")
}

// TestASculkSensorsComparatorReadsTheFrequency reads the sensor through a
// comparator: 13 for a placed block, then 12 for that block broken.
// vanilla: SculkSensorBlock.getAnalogOutputSignal, VibrationSystem.VIBRATION_FREQUENCY_FOR_EVENT
func TestASculkSensorsComparatorReadsTheFrequency(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.LayComparatorScene(t, c, srv, 5, 64, 4)
	srv.SetBlock(4, 64, 4, servertest.MustState(t, "minecraft:sculk_sensor", nil))
	servertest.HoldTicks(t, srv)

	placeStoneAt(c, 2)
	servertest.RunTicks(t, srv, 6)
	expectSensor(t, srv, 4, 64, 4, "active", "12")
	if got := servertest.WirePowerAt(srv, 6, 64, 4); got != "13" {
		t.Fatalf("the comparator gives %q for a placed block, want 13", got)
	}

	servertest.RunTicks(t, srv, 40)
	expectSensor(t, srv, 4, 64, 4, "inactive", "0")
	c.BreakBlock(2, 64, 4)
	c.Settle()
	servertest.RunTicks(t, srv, 6)
	expectSensor(t, srv, 4, 64, 4, "active", "12")
	if got := servertest.WirePowerAt(srv, 6, 64, 4); got != "12" {
		t.Fatalf("the comparator gives %q for a broken block, want 12", got)
	}
}

// TestALandingActivatesASculkSensor drops the player six blocks three blocks
// from the sensor: hit_ground reaches it with power 10.
// vanilla: Entity.checkFallDamage
func TestALandingActivatesASculkSensor(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Lander")
	srv.SetBlock(3, 64, 0, servertest.MustState(t, "minecraft:sculk_sensor", nil))
	servertest.HoldTicks(t, srv)

	c.FallSix(0.5)
	servertest.RunTicks(t, srv, 3)
	// 15 - floor(15/8 * 3)
	expectSensor(t, srv, 3, 64, 0, "active", "10")
}
