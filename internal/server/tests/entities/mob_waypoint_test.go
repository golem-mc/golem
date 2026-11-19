package entities

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// A mob whose waypoint_transmit_range is above 0 is a transmitter on every
// player's locator bar, and untracks when the range drops back to 0.
// vanilla: LivingEntity.isTransmittingWaypoint, onAttributeUpdated
func TestAMobWithATransmitRangeIsOnTheLocatorBar(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	c.WatchWaypoints = true
	servertest.HoldTicks(t, srv)

	srv.SpawnMob("minecraft:pig", 5.5, 64, 0.5)
	var pig = srv.Mobs()[0]
	c.ChatCommand("attribute @e[type=minecraft:pig,limit=1] minecraft:waypoint_transmit_range base set 1000")

	waypoint := func(op int32) {
		t.Helper()
		var got bool
		c.ReadTicks(srv, 5, func(id int32, data []byte) bool {
			if id != protocol.IDPlayWaypoint {
				return false
			}
			r := protocol.NewReader(data)
			if r.ReadVarInt() != op || !r.ReadBool() || r.ReadUUID().String() != pig.UUID {
				return false
			}
			r.ReadString(32767)
			if r.ReadBool() {
				r.ReadU8()
				r.ReadU8()
				r.ReadU8()
			}
			got = true
			return true
		})
		if !got {
			t.Fatalf("no waypoint operation %d for the pig within 5 ticks", op)
		}
	}
	waypoint(protocol.WaypointTrack)

	// A new icon arrives as an untrack and a track.
	c.ChatCommand("waypoint modify @e[type=minecraft:pig,limit=1] color hex FF0000")
	waypoint(protocol.WaypointUntrack)
	waypoint(protocol.WaypointTrack)

	// The icon is saved with the mob, and the mob is on /waypoint list.
	c.ChatCommand("data get entity @e[type=minecraft:pig,limit=1] locator_bar_icon.color")
	servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.query")
	c.ChatCommand("waypoint list")
	var list []byte
	c.ReadTicks(srv, 20, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySystemChat && bytes.Contains(data, []byte("commands.waypoint.list.success")) {
			list = data
			return true
		}
		return false
	})
	if !bytes.Contains(list, []byte("Pig")) {
		t.Errorf("the waypoint list = %q, want it to name the pig", list)
	}

	c.ChatCommand("attribute @e[type=minecraft:pig,limit=1] minecraft:waypoint_transmit_range base set 0")
	waypoint(protocol.WaypointUntrack)
}

// The tick loop keeps running when the last player leaves while a mob
// transmits.
func TestATransmittingMobWithNobodyOnlineDoesNotStopTheLoop(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	srv.SpawnMob("minecraft:pig", 5.5, 64, 0.5)
	c.ChatCommand("attribute @e[type=minecraft:pig,limit=1] minecraft:waypoint_transmit_range base set 1000")
	servertest.AwaitSystemChatSaying(t, c, "commands.attribute.base_value.set")
	c.Disconnect()
	servertest.RunTicks(t, srv, 5)
}

// A transmitting mob without an icon colour wears its team's colour. The
// chunk and bearing forms are the players' (waypointFormData), covered by
// TestFarWaypointsDropToChunksAndAngles; a selector cannot reach a mob that far.
func TestAMobWaypointWearsItsTeamColour(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	c.WatchWaypoints = true
	servertest.HoldTicks(t, srv)

	srv.SpawnMob("minecraft:pig", 48.5, 64, 0.5)
	pig := srv.Mobs()[0]
	c.ChatCommand("team add reds")
	c.ChatCommand("team modify reds color red")
	c.ChatCommand("team join reds " + pig.UUID)
	c.ChatCommand("attribute @e[type=minecraft:pig,limit=1] minecraft:waypoint_transmit_range base set 1000")
	servertest.AwaitSystemChatSaying(t, c, "commands.attribute")

	var color [3]byte
	c.ReadTicks(srv, 40, func(id int32, data []byte) bool {
		if id != protocol.IDPlayWaypoint {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != protocol.WaypointTrack || !r.ReadBool() || r.ReadUUID().String() != pig.UUID {
			return false
		}
		r.ReadString(32767)
		if !r.ReadBool() {
			t.Fatalf("the pig's track has no colour, want its team's")
		}
		color = [3]byte{r.ReadU8(), r.ReadU8(), r.ReadU8()}
		return true
	})
	if color != [3]byte{0xFF, 0x55, 0x55} {
		t.Errorf("colour = %x, want ff5555", color)
	}
}
