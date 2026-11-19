package commands

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestWaypointListNamesTheTrackedPlayers checks that /waypoint list names
// every player the world tracks and that a player without operator rights
// has no such command.
func TestWaypointListNamesTheTrackedPlayers(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Painter"))
	painter := connect().Join("Painter")
	watcher := connect().Join("Watcher")
	servertest.HoldTicks(t, srv)

	painter.ChatCommand("waypoint list")
	var text []byte
	painter.ReadTicks(srv, 100, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySystemChat && bytes.Contains(data, []byte("commands.waypoint.list.success")) {
			text = data
			return true
		}
		return false
	})
	if text == nil {
		t.Fatal("no waypoint list arrived")
	}
	for _, want := range []string{"Painter", "Watcher", "minecraft:overworld", "chat.coordinates.tooltip"} {
		if !bytes.Contains(text, []byte(want)) {
			t.Errorf("the list does not name %q", want)
		}
	}

	// A player whose transmit range is zero is off the list.
	painter.ChatCommand("attribute Watcher minecraft:waypoint_transmit_range base set 0")
	servertest.AwaitSystemChatSaying(t, painter, "commands.attribute.base_value.set.success")
	painter.ChatCommand("waypoint list")
	text = nil
	painter.ReadTicks(srv, 100, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySystemChat && bytes.Contains(data, []byte("commands.waypoint.list.success")) {
			text = data
			return true
		}
		return false
	})
	if text == nil {
		t.Fatal("no second waypoint list arrived")
	}
	if bytes.Contains(text, []byte("Watcher")) || !bytes.Contains(text, []byte("Painter")) {
		t.Errorf("the list after the range went to zero = %q, want only Painter", text)
	}

	watcher.ChatCommand("waypoint list")
	servertest.AwaitCommandError(t, watcher, "command.unknown.command")
	painter.Disconnect()
	watcher.Disconnect()
}
