package redstone

// The calibrated sculk sensor: a sixteen-block range, ten active ticks, a
// frequency filter read off the powered back face, and no signal out of it.
// vanilla: CalibratedSculkSensorBlock, CalibratedSculkSensorBlockEntity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// calibratedAt stands a calibrated sensor facing north at (x, 64, 4).
func calibratedAt(t *testing.T, srv *server.Server, x int32) {
	t.Helper()
	srv.SetBlock(x, 64, 4, servertest.MustState(t, "minecraft:calibrated_sculk_sensor",
		map[string]string{"facing": "north"}))
}

func expectCalibrated(t *testing.T, srv *server.Server, x int32, phase, power string) {
	t.Helper()
	servertest.ExpectBlockNamed(t, srv, x, 64, 4, "minecraft:calibrated_sculk_sensor",
		map[string]string{"sculk_sensor_phase": phase, "power": power, "facing": "north"})
}

// TestACalibratedSculkSensorHearsSixteenBlocksForTenTicks places stone twelve
// blocks from a calibrated sensor and from a plain one: only the calibrated
// one hears it, with power 4, and goes quiet after ten active ticks.
// vanilla: CalibratedSculkSensorBlockEntity.VibrationUser.getListenerRadius,
// CalibratedSculkSensorBlock.getActiveTicks
func TestACalibratedSculkSensorHearsSixteenBlocksForTenTicks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	calibratedAt(t, srv, 14)
	srv.SetBlock(14, 64, 6, servertest.MustState(t, "minecraft:sculk_sensor", nil))
	servertest.HoldTicks(t, srv)

	placeStoneAt(c, 2)
	servertest.RunTicks(t, srv, 11)
	expectCalibrated(t, srv, 14, "inactive", "0")
	servertest.RunTicks(t, srv, 1)
	// 15 - floor(15/16 * 12)
	expectCalibrated(t, srv, 14, "active", "4")
	servertest.ExpectBlockNamed(t, srv, 14, 64, 6, "minecraft:sculk_sensor",
		map[string]string{"sculk_sensor_phase": "inactive", "power": "0"})
	servertest.RunTicks(t, srv, 9)
	expectCalibrated(t, srv, 14, "active", "4")
	servertest.RunTicks(t, srv, 1)
	expectCalibrated(t, srv, 14, "cooldown", "0")
	servertest.RunTicks(t, srv, 10)
	expectCalibrated(t, srv, 14, "inactive", "0")
}

// TestACalibratedSculkSensorFiltersByItsBackSignal powers the back of the
// sensor with 13, the frequency of a placed block: a placed block is heard, a
// broken one (12) is not.
// vanilla: CalibratedSculkSensorBlockEntity.VibrationUser.canReceiveVibration
func TestACalibratedSculkSensorFiltersByItsBackSignal(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	calibratedAt(t, srv, 4)
	srv.SetBlock(4, 64, 5, servertest.MustState(t, "minecraft:target", map[string]string{"power": "13"}))
	servertest.HoldTicks(t, srv)

	c.BreakBlock(1, 63, 4)
	c.Settle()
	servertest.RunTicks(t, srv, 8)
	expectCalibrated(t, srv, 4, "inactive", "0")
	// Unfiltered, the same break is heard.
	srv.SetBlock(4, 64, 5, servertest.MustState(t, "minecraft:air", nil))
	c.BreakBlock(3, 63, 4)
	c.Settle()
	servertest.RunTicks(t, srv, 8)
	expectCalibrated(t, srv, 4, "active", "14")
	servertest.RunTicks(t, srv, 40)
	srv.SetBlock(4, 64, 5, servertest.MustState(t, "minecraft:target", map[string]string{"power": "13"}))

	placeStoneAt(c, 2)
	servertest.RunTicks(t, srv, 6)
	expectCalibrated(t, srv, 4, "active", "14")
}

// TestACalibratedSculkSensorGivesNoSignalOutItsBack lights the lamp beside an
// active sensor and leaves the one behind it dark.
// vanilla: CalibratedSculkSensorBlock.getSignal
func TestACalibratedSculkSensorGivesNoSignalOutItsBack(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	calibratedAt(t, srv, 4)
	lamp := servertest.MustState(t, "minecraft:redstone_lamp", nil)
	srv.SetBlock(5, 64, 4, lamp)
	srv.SetBlock(4, 64, 5, lamp)
	servertest.HoldTicks(t, srv)

	placeStoneAt(c, 2)
	servertest.RunTicks(t, srv, 6)
	expectCalibrated(t, srv, 4, "active", "14")
	if !servertest.LampLitAt(srv, 5, 64, 4) {
		t.Error("the lamp beside the calibrated sensor is not lit")
	}
	if servertest.LampLitAt(srv, 4, 64, 5) {
		t.Error("the lamp behind the calibrated sensor is lit")
	}
}

// TestACalibratedSculkSensorsComparatorReadsTheFrequency reads the frequency
// through a comparator, as for the plain sensor.
// vanilla: SculkSensorBlock.getAnalogOutputSignal
func TestACalibratedSculkSensorsComparatorReadsTheFrequency(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.LayComparatorScene(t, c, srv, 5, 64, 4)
	calibratedAt(t, srv, 4)
	servertest.HoldTicks(t, srv)

	placeStoneAt(c, 2)
	servertest.RunTicks(t, srv, 6)
	expectCalibrated(t, srv, 4, "active", "14")
	if got := servertest.WirePowerAt(srv, 6, 64, 4); got != "13" {
		t.Fatalf("the comparator gives %q, want 13", got)
	}
}
