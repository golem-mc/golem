package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: Level.destroyBlock posts block_destroy at the cell.
func TestAPoppedTorchActivatesTheSculkSensor(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))
	srv.StopTick()
	servertest.SeedPhysics(t, srv, 21)
	servertest.Setblock(t, c, 5, 64, 0, "stone")
	servertest.Setblock(t, c, 5, 65, 0, "torch")
	servertest.Setblock(t, c, 8, 64, 0, "sculk_sensor")
	c.Settle()
	srv.TickPhysics(60)
	if _, props := servertest.StatePropsOf(t, srv, 8, 64, 0); props["sculk_sensor_phase"] != "inactive" {
		t.Fatalf("setup: the sensor is %s", props["sculk_sensor_phase"])
	}
	servertest.Setblock(t, c, 5, 64, 0, "air")
	c.Settle()
	for range 6 {
		srv.TickPhysics(1)
		if _, props := servertest.StatePropsOf(t, srv, 8, 64, 0); props["sculk_sensor_phase"] == "active" {
			return
		}
	}
	t.Error("the torch popped and the sensor never became active")
}

// vanilla: BaseRailBlock.neighborChanged drops the rail without block_destroy.
func TestARailDroppedByItsSupportStaysSilent(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))
	srv.StopTick()
	servertest.SeedPhysics(t, srv, 21)
	servertest.Setblock(t, c, 5, 64, 0, "stone")
	servertest.Setblock(t, c, 5, 65, 0, "rail")
	servertest.Setblock(t, c, 8, 64, 0, "sculk_sensor")
	c.Settle()
	srv.TickPhysics(60)
	if _, props := servertest.StatePropsOf(t, srv, 8, 64, 0); props["sculk_sensor_phase"] != "inactive" {
		t.Fatalf("setup: the sensor is %s", props["sculk_sensor_phase"])
	}
	servertest.Setblock(t, c, 5, 64, 0, "air")
	c.Settle()
	for range 6 {
		srv.TickPhysics(1)
		if _, props := servertest.StatePropsOf(t, srv, 8, 64, 0); props["sculk_sensor_phase"] != "inactive" {
			t.Fatalf("the sensor is %s after a dropped rail", props["sculk_sensor_phase"])
		}
	}
}

// vanilla: VibrationSystem.User.isValidVibration rejects a destroyed carpet
// (dampens_vibrations).
func TestAPoppedCarpetStaysSilent(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))
	srv.StopTick()
	servertest.SeedPhysics(t, srv, 21)
	servertest.Setblock(t, c, 5, 64, 0, "stone")
	servertest.Setblock(t, c, 5, 65, 0, "white_carpet")
	servertest.Setblock(t, c, 8, 64, 0, "sculk_sensor")
	c.Settle()
	srv.TickPhysics(60)
	if _, props := servertest.StatePropsOf(t, srv, 8, 64, 0); props["sculk_sensor_phase"] != "inactive" {
		t.Fatalf("setup: the sensor is %s", props["sculk_sensor_phase"])
	}
	servertest.Setblock(t, c, 5, 64, 0, "air")
	c.Settle()
	for range 6 {
		srv.TickPhysics(1)
		if _, props := servertest.StatePropsOf(t, srv, 8, 64, 0); props["sculk_sensor_phase"] != "inactive" {
			t.Fatalf("the sensor is %s after a popped carpet", props["sculk_sensor_phase"])
		}
	}
}
