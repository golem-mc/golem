package commands

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/google/uuid"
)

// waypointSeen is one Waypoint packet as a locator bar reads it: the
// operation, the transmitter, the icon, and whether the packet carries a colour.
type waypointSeen struct {
	operation int32
	id        uuid.UUID
	style     string
	colored   bool
	rgb       [3]uint8
}

// nextWaypointFor reads the viewer until a Waypoint packet naming id arrives.
func nextWaypointFor(t *testing.T, c *servertest.Client, id uuid.UUID) waypointSeen {
	t.Helper()
	for {
		var data []byte
		if len(c.WaypointBacklog) > 0 {
			data = c.WaypointBacklog[0].Data
			c.WaypointBacklog = c.WaypointBacklog[1:]
		} else {
			var ok bool
			if data, ok = servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayWaypoint); !ok {
				t.Fatalf("no waypoint for %s arrived", id)
			}
		}
		if wp := parseWaypointIcon(t, data); wp.id == id {
			return wp
		}
	}
}

func parseWaypointIcon(t *testing.T, data []byte) waypointSeen {
	t.Helper()
	r := protocol.NewReader(data)
	wp := waypointSeen{operation: r.ReadVarInt()}
	if !r.ReadBool() {
		t.Fatal("the waypoint is named by a string, want a UUID")
	}
	wp.id = r.ReadUUID()
	wp.style = r.ReadString(32767)
	if wp.colored = r.ReadBool(); wp.colored {
		wp.rgb = [3]uint8{r.ReadU8(), r.ReadU8(), r.ReadU8()}
	}
	return wp
}

// expectRetrack reads the untrack and the track a changed icon sends the
// viewer, and checks the track's icon.
func expectRetrack(t *testing.T, viewer *servertest.Client, id uuid.UUID, style string, colored bool, rgb [3]uint8) {
	t.Helper()
	if got := nextWaypointFor(t, viewer, id); got.operation != protocol.WaypointUntrack {
		t.Fatalf("first waypoint operation = %d, want the untrack", got.operation)
	}
	got := nextWaypointFor(t, viewer, id)
	if got.operation != protocol.WaypointTrack {
		t.Fatalf("second waypoint operation = %d, want the track", got.operation)
	}
	if got.style != style || got.colored != colored || got.rgb != rgb {
		t.Errorf("track icon = %q colored=%v %v, want %q colored=%v %v",
			got.style, got.colored, got.rgb, style, colored, rgb)
	}
}

// TestWaypointModifyRetracksTheIcon checks that /waypoint modify changes the
// style and colour of a player's waypoint and sends the viewers holding it an
// untrack and a track at once, with the colour as three bytes of RGB.
func TestWaypointModifyRetracksTheIcon(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Painter"))
	painter := connect().Join("Painter")
	watcher := connect().Join("Watcher")
	servertest.HoldTicks(t, srv)
	id := protocol.OfflineUUID("Painter")

	// The join's ticks paired the two; their track waits in the backlog.
	watcher.Settle()
	if got := nextWaypointFor(t, watcher, id); got.operation != protocol.WaypointTrack || got.style != "minecraft:default" || got.colored {
		t.Fatalf("first track = %+v, want the default icon with no colour", got)
	}

	// No tick runs from here: the change reaches the viewer on its own.
	watcher.WatchWaypoints = true
	painter.ChatCommand("waypoint modify @s color red")
	servertest.AwaitCommandFeedbackText(t, painter, "commands.waypoint.modify.color",
		protocol.Text{Text: "red", Color: "red"})
	expectRetrack(t, watcher, id, "minecraft:default", true, [3]uint8{0xFF, 0x55, 0x55})

	painter.ChatCommand("waypoint modify @s color hex 3a7")
	servertest.AwaitCommandFeedbackText(t, painter, "commands.waypoint.modify.color",
		protocol.Text{Text: "33AA77", Color: "#33aa77"})
	expectRetrack(t, watcher, id, "minecraft:default", true, [3]uint8{0x33, 0xAA, 0x77})

	painter.ChatCommand("waypoint modify @s style set foo:bar")
	servertest.AwaitSystemChatSaying(t, painter, "commands.waypoint.modify.style")
	expectRetrack(t, watcher, id, "foo:bar", true, [3]uint8{0x33, 0xAA, 0x77})

	painter.ChatCommand("waypoint modify @s color reset")
	servertest.AwaitSystemChatSaying(t, painter, "commands.waypoint.modify.color.reset")
	expectRetrack(t, watcher, id, "foo:bar", false, [3]uint8{})

	painter.ChatCommand("waypoint modify @s style reset")
	servertest.AwaitSystemChatSaying(t, painter, "commands.waypoint.modify.style")
	expectRetrack(t, watcher, id, "minecraft:default", false, [3]uint8{})

	painter.ChatCommand("waypoint modify @s color hex 12")
	servertest.AwaitSystemChatSaying(t, painter, "argument.hexcolor.invalid")
	painter.ChatCommand("waypoint modify @s color mauve")
	servertest.AwaitSystemChatSaying(t, painter, "argument.color.invalid")

	painter.ChatCommand(`summon minecraft:item 6.5 64 0.5 {Item:{id:"minecraft:stone",count:1}}`)
	painter.ChatCommand("waypoint modify @e[type=minecraft:item,limit=1] color red")
	servertest.AwaitCommandError(t, painter, "argument.waypoint.invalid")
	painter.Disconnect()
	watcher.Disconnect()
}

// TestWaypointIconSurvivesARelog checks that the icon is saved with the
// player and rides the track the next session sends.
func TestWaypointIconSurvivesARelog(t *testing.T) {
	t.Parallel()
	_, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Painter"))
	painter := connect().Join("Painter")
	painter.ChatCommand("waypoint modify @s color hex 0f0")
	servertest.AwaitSystemChatSaying(t, painter, "commands.waypoint.modify.color")
	painter.ChatCommand("waypoint modify @s style set foo:bar")
	servertest.AwaitSystemChatSaying(t, painter, "commands.waypoint.modify.style")
	painter.Disconnect()

	watcher := connect().Join("Watcher")
	painter = connect()
	painter.Join("Painter")
	watcher.WatchWaypoints = true

	got := nextWaypointFor(t, watcher, protocol.OfflineUUID("Painter"))
	if got.operation != protocol.WaypointTrack || got.style != "foo:bar" || !got.colored || got.rgb != [3]uint8{0, 0xFF, 0} {
		t.Errorf("track after the relog = %+v, want foo:bar in 00ff00", got)
	}
	painter.Disconnect()
	watcher.Disconnect()
}

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
