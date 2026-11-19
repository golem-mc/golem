package redstone

// The sculk sensor's vibration listener in its block entity: saved while a
// vibration is in flight and taken up again by a load.
// vanilla: VibrationSystem.Data.CODEC, SculkSensorBlockEntity.loadAdditional

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// sensorListener reads the listener the sensor at (4, 64, 4) saves.
func sensorListener(t *testing.T, srv *server.Server) nbt.Compound {
	t.Helper()
	tag, ok := srv.BlockEntityTagAt(4, 64, 4)
	if !ok {
		t.Fatal("the sensor has no block entity")
	}
	listener, ok := nbt.Get[nbt.Compound](tag, "listener")
	if !ok {
		t.Fatalf("the sensor saved no listener: %v", tag)
	}
	return listener
}

// TestASculkSensorSavesTheVibrationInFlight places stone two blocks from the
// sensor: a tick later the vibration is in flight, and the sensor's block
// entity saves it with its game event, distance, source and the tick of
// travel left.
// vanilla: VibrationSystem.Data.CODEC, VibrationInfo.CODEC
func TestASculkSensorSavesTheVibrationInFlight(t *testing.T) {
	t.Parallel()
	srv, c := sensorScene(t)
	placeStoneAt(c, 2)
	servertest.RunTicks(t, srv, 1)

	listener := sensorListener(t, srv)
	if delay, _ := nbt.Get[nbt.Int](listener, "event_delay"); delay != 1 {
		t.Errorf("event_delay = %d, want the one tick of travel left", delay)
	}
	event, ok := nbt.Get[nbt.Compound](listener, "event")
	if !ok {
		t.Fatalf("the listener saved no vibration in flight: %v", listener)
	}
	if name, _ := nbt.Get[nbt.String](event, "game_event"); name != "minecraft:block_place" {
		t.Errorf("game_event = %q, want minecraft:block_place", name)
	}
	if dist, _ := nbt.Get[nbt.Float](event, "distance"); dist < 2 || dist >= 3 {
		t.Errorf("distance = %v, want the two blocks and a little to the stone", dist)
	}
	if pos, _ := nbt.Get[nbt.List](event, "pos"); len(pos.Items) != 3 {
		t.Errorf("pos = %v, want three doubles", event["pos"])
	}
	if source, _ := nbt.Get[nbt.IntArray](event, "source"); len(source) != 4 {
		t.Errorf("source = %v, want the placing player's UUID", event["source"])
	}
	selector, _ := nbt.Get[nbt.Compound](listener, "selector")
	if tick, _ := nbt.Get[nbt.Long](selector, "tick"); tick != -1 {
		t.Errorf("selector tick = %d, want -1 with the candidate taken", tick)
	}

	servertest.RunTicks(t, srv, 1)
	expectSensor(t, srv, 4, 64, 4, "active", "12")
	if _, ok := sensorListener(t, srv)["event"]; ok {
		t.Error("the listener still saves the vibration it received")
	}
}

// TestASculkSensorTakesUpALoadedVibration loads a listener holding a door's
// opening two blocks away with two ticks of travel left: the sensor takes it
// up and activates when the travel runs out, with power 12 by the distance
// and the event's frequency for the comparator.
// vanilla: SculkSensorBlockEntity.loadAdditional, VibrationSystem.Ticker.tick
func TestASculkSensorTakesUpALoadedVibration(t *testing.T) {
	t.Parallel()
	srv, _ := comparatorSensorScene(t)
	srv.SetBlockEntityTagAt(4, 64, 4, "minecraft:sculk_sensor", nbt.Compound{
		"listener": nbt.Compound{
			"event": nbt.Compound{
				"game_event": nbt.String("minecraft:block_open"),
				"distance":   nbt.Float(2),
				"pos":        nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(2.5), nbt.Double(64.5), nbt.Double(4.5)}},
			},
			"selector":    nbt.Compound{"tick": nbt.Long(-1)},
			"event_delay": nbt.Int(2),
		},
	})

	servertest.RunTicks(t, srv, 1)
	expectSensor(t, srv, 4, 64, 4, "inactive", "0")
	if delay, _ := nbt.Get[nbt.Int](sensorListener(t, srv), "event_delay"); delay != 1 {
		t.Errorf("event_delay = %d after a tick, want 1", delay)
	}
	servertest.RunTicks(t, srv, 1)
	expectSensor(t, srv, 4, 64, 4, "active", "12")
	// The comparator takes its two ticks to read the sensor.
	servertest.RunTicks(t, srv, 2)
	expectComparator(t, srv, "10")
}
