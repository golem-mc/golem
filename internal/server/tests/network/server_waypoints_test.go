package network

import (
	"iter"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/google/uuid"
)

// The locator bar's waypoints: every player carries one on the bar of every
// other player in their dimension, settled by the tick the way the bodies'
// pairing is. The Waypoint packet the bar reads is the one vanilla's
// ServerWaypointManager sends — a track naming the transmitter's UUID and the
// block it stands at, an update when the block moves, an untrack when the
// pairing falls away — and these tests read it from both sides of a pair, the
// way a client's locator bar does.

// waypointPacket is one Waypoint packet decoded: the operation, the body it
// names, the icon style and colour it draws with, and the place or angle it
// points at.
type waypointPacket struct {
	operation int32
	id        uuid.UUID
	style     string
	colored   bool
	kind      int32
	x, y, z   int32
	angle     float32
}

// parseWaypoint decodes the Waypoint packet's fields: an operation, the identifier the
// Either form carries (the players' UUIDs here), the icon style, a colour that
// may ride with it, and the coordinate of the type it names.
func parseWaypoint(t *testing.T, data []byte) waypointPacket {
	t.Helper()
	r := protocol.NewReader(data)
	wp := waypointPacket{operation: r.ReadVarInt()}
	if !r.ReadBool() {
		t.Fatal("the waypoint's identifier is a string, want a UUID")
	}
	wp.id = r.ReadUUID()
	wp.style = r.ReadString(32767)
	wp.colored = r.ReadBool()
	if wp.colored {
		for range 3 {
			r.ReadU8() // red, green, blue
		}
	}
	switch wp.kind = r.ReadVarInt(); wp.kind {
	case protocol.WaypointTypeEmpty:
	case protocol.WaypointTypeVec3i:
		wp.x, wp.y, wp.z = r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt()
	case protocol.WaypointTypeChunk:
		wp.x, wp.z = r.ReadVarInt(), r.ReadVarInt()
	case protocol.WaypointTypeAzimuth:
		wp.angle = r.ReadF32()
	default:
		t.Fatalf("waypoint of unknown type %d", wp.kind)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("waypoint packet: %v", err)
	}
	if r.Remaining() != 0 {
		t.Errorf("waypoint packet has %d bytes left over", r.Remaining())
	}
	return wp
}

// at reports whether the waypoint names the block or the chunk given.
func (wp waypointPacket) at(x, y, z int32) bool {
	return wp.x == x && wp.y == y && wp.z == z
}

// blockOf floors a position into the block a waypoint names it by.
type blockCell struct{ x, y, z int32 }

func blockOf(pos player.Position) blockCell {
	return blockCell{int32(math.Floor(pos.X)), int32(math.Floor(pos.Y)), int32(math.Floor(pos.Z))}
}

// waypointPackets is packets with the Waypoint packets visible: the ones a
// join or a settle stepped over come first, in order.
func waypointPackets(c *servertest.Client, d time.Duration) iter.Seq2[int32, []byte] {
	return func(yield func(int32, []byte) bool) {
		c.T.Helper()
		backlog := c.WaypointBacklog
		c.WaypointBacklog = nil
		for i, p := range backlog {
			if !yield(p.Id, p.Data) {
				c.WaypointBacklog = append(backlog[i+1:], c.WaypointBacklog...)
				return
			}
		}
		c.WatchWaypoints = true
		defer func() { c.WatchWaypoints = false }()
		for id, data := range c.Packets(d) {
			if !yield(id, data) {
				return
			}
		}
	}
}

// trackWaypoint reads packets until a Waypoint of the operation given arrives
// for id, answering keep-alives and stepping over the join and movement
// traffic it shares the wire with, and checks the rest of its fields. It
// returns the packet's place or angle for the caller to check.
func trackWaypoint(t *testing.T, c *servertest.Client, id uuid.UUID, operation int32) waypointPacket {
	t.Helper()
	for packetID, data := range waypointPackets(c, servertest.ExpectTimeout) {
		t.Helper()
		if packetID != protocol.IDPlayWaypoint {
			continue
		}
		wp := parseWaypoint(t, data)
		if wp.id != id {
			continue
		}
		if wp.operation != operation {
			t.Errorf("waypoint operation = %d, want %d", wp.operation, operation)
		}
		if wp.style != "minecraft:default" {
			t.Errorf("waypoint style = %q, want the default", wp.style)
		}
		if wp.colored {
			t.Error("waypoint carried a colour, want none")
		}
		return wp
	}
	t.Fatalf("no %d waypoint for %s arrived within %s", operation, id, servertest.ExpectTimeout)
	return waypointPacket{}
}

// expectNoWaypoint reads the connection for the window given and fails if any
// waypoint for id passes: a pair the locator bar dropped stays dropped.
func expectNoWaypoint(t *testing.T, c *servertest.Client, window time.Duration, id uuid.UUID) {
	t.Helper()
	for packetID, data := range waypointPackets(c, window) {
		t.Helper()
		if packetID != protocol.IDPlayWaypoint {
			continue
		}
		if got := parseWaypoint(t, data); got.id == id {
			t.Fatalf("a waypoint for %s arrived, want the bar to hold none", id)
		}
	}
}

// TestPlayersAppearOnEachOthersLocatorBars is the pairing itself: two players
// in one dimension each receive a Waypoint track for the other at the block
// they stand on, a move of one block arrives as an update, and a longer
// step — a drift the block connection can only repair by remaking itself — as
// a fresh track.
func TestPlayersAppearOnEachOthersLocatorBars(t *testing.T) {
	t.Parallel()
	_, connect := servertest.Start(t, servertest.BlockTestConfig())

	watcher := connect().Join("Watcher")
	watcher.Settle()

	mover := connect().Join("Mover")
	moverID := protocol.OfflineUUID("Mover")
	watcherID := protocol.OfflineUUID("Watcher")

	// Where each body stands, as the join's own teleport reported it.
	moverStart := mover.LastTeleport
	watcherStart := watcher.LastTeleport
	moverAt := blockOf(moverStart)
	watcherAt := blockOf(watcherStart)

	// Each side receives the other's waypoint, pointing at the block the
	// other stands on.
	wp := trackWaypoint(t, watcher, moverID, protocol.WaypointTrack)
	if wp.kind != protocol.WaypointTypeVec3i {
		t.Fatalf("the watcher's waypoint is of type %d, want a position", wp.kind)
	}
	if !wp.at(moverAt.x, moverAt.y, moverAt.z) {
		t.Errorf("the mover's waypoint points at (%d, %d, %d), want the block they stand on (%d, %d, %d)",
			wp.x, wp.y, wp.z, moverAt.x, moverAt.y, moverAt.z)
	}
	wp = trackWaypoint(t, mover, watcherID, protocol.WaypointTrack)
	if wp.kind != protocol.WaypointTypeVec3i {
		t.Fatalf("the watcher's waypoint is of type %d, want a position", wp.kind)
	}
	if !wp.at(watcherAt.x, watcherAt.y, watcherAt.z) {
		t.Errorf("the watcher's waypoint points at (%d, %d, %d), want the block they stand on (%d, %d, %d)",
			wp.x, wp.y, wp.z, watcherAt.x, watcherAt.y, watcherAt.z)
	}

	// One block over is an update of the same waypoint.
	mover.MoveToGround(moverStart.X+1, moverStart.Y, moverStart.Z, true)
	wp = trackWaypoint(t, watcher, moverID, protocol.WaypointUpdate)
	if wp.kind != protocol.WaypointTypeVec3i {
		t.Fatalf("the update is of type %d, want a position", wp.kind)
	}
	if !wp.at(moverAt.x+1, moverAt.y, moverAt.z) {
		t.Errorf("the update points at (%d, %d, %d), want one block east of the track's (%d, %d, %d)",
			wp.x, wp.y, wp.z, moverAt.x+1, moverAt.y, moverAt.z)
	}

	// Several blocks in one step break the block connection: the next
	// packet is a fresh track, not an update.
	// vanilla: WaypointTransmitter.BlockConnection.isBroken
	mover.MoveToGround(moverStart.X+5, moverStart.Y, moverStart.Z, true)
	wp = trackWaypoint(t, watcher, moverID, protocol.WaypointTrack)
	if wp.kind != protocol.WaypointTypeVec3i {
		t.Fatalf("the re-track is of type %d, want a position", wp.kind)
	}
	if !wp.at(moverAt.x+5, moverAt.y, moverAt.z) {
		t.Errorf("the re-track points at (%d, %d, %d), want five blocks east of the track's (%d, %d, %d)",
			wp.x, wp.y, wp.z, moverAt.x+5, moverAt.y, moverAt.z)
	}
}

// TestAWaypointIsBlockFormInsideTheReceiversViewAndChunkFormOutside moves a
// transmitter along one axis to the last chunk of the receiver's view and then
// one chunk past it: each move rebuilds the link, which is made in the block
// form while the transmitter's chunk is in view and in the chunk form once it
// is not.
// vanilla: LivingEntity.makeWaypointConnectionWith
func TestAWaypointIsBlockFormInsideTheReceiversViewAndChunkFormOutside(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")
	watcher.Settle()
	mover := connect().Join("Mover")
	moverID := protocol.OfflineUUID("Mover")
	watcher.WatchWaypoints = true
	servertest.HoldTicks(t, srv)

	// The watcher stands in chunk 0, so the view reaches chunk 2 along x.
	view := int32(config.MinDistance)
	moveTo := func(chunkX int32) waypointPacket {
		t.Helper()
		x := chunkX*16 + 8
		// Settle has the server apply the move before the watcher counts
		// its tick: a tick counted on one connection does not wait for a
		// packet still queued on another.
		mover.MoveToGround(float64(x)+0.5, mover.LastTeleport.Y, 0.5, true)
		mover.Settle()
		var got waypointPacket
		if !watcher.ReadTicks(srv, 1, func(id int32, data []byte) bool {
			if id != protocol.IDPlayWaypoint {
				return false
			}
			wp := parseWaypoint(t, data)
			if wp.id != moverID || wp.operation != protocol.WaypointTrack {
				return false
			}
			// A track left over from the previous move may still be in flight.
			if wp.kind == protocol.WaypointTypeChunk && wp.x != chunkX {
				return false
			}
			if wp.kind == protocol.WaypointTypeVec3i && wp.x != x {
				return false
			}
			got = wp
			return true
		}) {
			t.Fatalf("no waypoint track for the mover at chunk %d within a tick", chunkX)
		}
		return got
	}

	if wp := moveTo(view); wp.kind != protocol.WaypointTypeVec3i {
		t.Errorf("the waypoint at the view's edge is of type %d, want the block form", wp.kind)
	}
	wp := moveTo(view + 1)
	if wp.kind != protocol.WaypointTypeChunk {
		t.Fatalf("the waypoint just past the view is of type %d, want the chunk form", wp.kind)
	}
	if wp.x != view+1 || wp.z != 0 {
		t.Errorf("the chunk waypoint names (%d, %d), want (%d, 0)", wp.x, wp.z, view+1)
	}
}

// vanilla: LivingEntity.makeWaypointConnectionWith, ChunkTrackingView.isInViewDistance
func TestLocatorBarUsesTheReceiversChunkTrackingView(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = 4
	srv, connect := servertest.Start(t, cfg)
	srv.SetGameRule("locator_bar", false)
	watcher := connect().Join("Watcher")
	watcher.WatchWaypoints = true
	diagonal := connect().Join("Diagonal")
	axis := connect().Join("Axis")
	servertest.HoldTicks(t, srv)

	diagonal.MoveToGround(64.5, 64, 64.5, true)
	axis.MoveToGround(64.5, 64, 0.5, true)
	diagonal.Settle()
	axis.Settle()
	srv.SetGameRule("locator_bar", true)

	want := map[uuid.UUID]waypointPacket{
		protocol.OfflineUUID("Diagonal"): {kind: protocol.WaypointTypeChunk, x: 4, z: 4},
		protocol.OfflineUUID("Axis"):     {kind: protocol.WaypointTypeVec3i, x: 64, y: 64},
	}
	if !watcher.ReadTicks(srv, 5, func(id int32, data []byte) bool {
		if id != protocol.IDPlayWaypoint {
			return false
		}
		got := parseWaypoint(t, data)
		expect, ok := want[got.id]
		if !ok {
			return false
		}
		if got.operation != protocol.WaypointTrack || got.kind != expect.kind || !got.at(expect.x, expect.y, expect.z) {
			t.Errorf("waypoint for %s = operation %d, form %d at (%d, %d, %d), want track in form %d at (%d, %d, %d)",
				got.id, got.operation, got.kind, got.x, got.y, got.z, expect.kind, expect.x, expect.y, expect.z)
		}
		delete(want, got.id)
		return len(want) == 0
	}) {
		t.Fatalf("missing waypoint tracks after 5 ticks: %v", want)
	}
}

// TestTheLocatorBarRuleTakesWaypointsOffAndBack checks the gate: with
// locator_bar off, every pairing falls away with an untrack each way, nothing
// re-tracks while the rule stands off, and the rule coming back re-tracks both
// sides where the move of the interval left them.
func TestTheLocatorBarRuleTakesWaypointsOffAndBack(t *testing.T) {
	t.Parallel()
	_, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Op"))

	op := connect().Join("Op")
	op.Settle()

	mover := connect().Join("Mover")
	moverID := protocol.OfflineUUID("Mover")
	opID := protocol.OfflineUUID("Op")
	moverStart := mover.LastTeleport
	moverAt := blockOf(moverStart)

	trackWaypoint(t, op, moverID, protocol.WaypointTrack)
	trackWaypoint(t, mover, opID, protocol.WaypointTrack)

	op.ChatCommand("gamerule locator_bar false")
	servertest.AwaitCommandFeedback(t, op, "commands.gamerule.set", "locator_bar", "false")

	// Both sides' waypoints fall away, the entry an untrack leaves naming
	// no place at all.
	wp := trackWaypoint(t, op, moverID, protocol.WaypointUntrack)
	if wp.kind != protocol.WaypointTypeEmpty {
		t.Errorf("the untrack is of type %d, want the empty entry", wp.kind)
	}
	wp = trackWaypoint(t, mover, opID, protocol.WaypointUntrack)
	if wp.kind != protocol.WaypointTypeEmpty {
		t.Errorf("the untrack is of type %d, want the empty entry", wp.kind)
	}

	// While the rule stands off, moves bring no waypoints back.
	mover.MoveToGround(moverStart.X+2, moverStart.Y, moverStart.Z, true)
	expectNoWaypoint(t, op, 500*time.Millisecond, moverID)
	expectNoWaypoint(t, mover, 500*time.Millisecond, opID)

	op.ChatCommand("gamerule locator_bar true")
	servertest.AwaitCommandFeedback(t, op, "commands.gamerule.set", "locator_bar", "true")

	wp = trackWaypoint(t, op, moverID, protocol.WaypointTrack)
	if !wp.at(moverAt.x+2, moverAt.y, moverAt.z) {
		t.Errorf("the re-track points at (%d, %d, %d), want where the move left the body (%d, %d, %d)",
			wp.x, wp.y, wp.z, moverAt.x+2, moverAt.y, moverAt.z)
	}
	trackWaypoint(t, mover, opID, protocol.WaypointTrack)
}

// TestAWaypointUntracksWhenItsPlayerCrossesDimensions checks the leaving: a
// player crossing to the nether falls off every overworld bar with an untrack,
// and their return is a fresh track at the block the arrival lands them on.
func TestAWaypointUntracksWhenItsPlayerCrossesDimensions(t *testing.T) {
	// Not parallel: the crossing generates nether chunks.
	srv, connect := servertest.Start(t, servertest.MobTestConfig())

	watcher := connect().Join("Watcher")
	watcher.Settle()

	traveller := connect().Join("Traveller")
	travellerID := protocol.OfflineUUID("Traveller")
	trackWaypoint(t, watcher, travellerID, protocol.WaypointTrack)

	watcherID := protocol.OfflineUUID("Watcher")
	trackWaypoint(t, traveller, watcherID, protocol.WaypointTrack)

	srv.MovePlayerToDimension("Traveller", world.NetherName, 8, 40, 8)
	traveller.Settle()
	trackWaypoint(t, watcher, travellerID, protocol.WaypointUntrack)
	// The client keeps its waypoints through the Respawn, so the traveller's
	// own bar is emptied by an untrack, not left holding the overworld's.
	// vanilla: ServerWaypointManager.removePlayer
	trackWaypoint(t, traveller, watcherID, protocol.WaypointUntrack)

	// Back in the overworld, the watcher's bar is re-filled at the block
	// the return lands the traveller on.
	srv.MovePlayerToDimension("Traveller", world.DimensionName, 0.5, 64, 0.5)
	wp := trackWaypoint(t, watcher, travellerID, protocol.WaypointTrack)
	if wp.kind != protocol.WaypointTypeVec3i {
		t.Fatalf("the returning waypoint is of type %d, want a position", wp.kind)
	}
	if !wp.at(0, 64, 0) {
		t.Errorf("the returning waypoint points at (%d, %d, %d), want the arrival (0, 64, 0)",
			wp.x, wp.y, wp.z)
	}
}

// TestSpectatorPlayersSendNoWaypoints checks the spectator branches: a player
// switched to spectator transmits no waypoint, their return to survival
// restores it, and a receiver spectating keeps receiving the others'.
func TestSpectatorPlayersSendNoWaypoints(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Op"))

	op := connect().Join("Op")
	op.Settle()

	mover := connect().Join("Mover")
	moverID := protocol.OfflineUUID("Mover")
	opID := protocol.OfflineUUID("Op")
	moverStart := mover.LastTeleport
	moverAt := blockOf(moverStart)

	trackWaypoint(t, op, moverID, protocol.WaypointTrack)
	trackWaypoint(t, mover, opID, protocol.WaypointTrack)

	// A spectator transmits nothing: the pairing falls away on the op's side.
	op.ChatCommand("gamemode spectator Mover")
	op.SettleKeeping()
	if srv.PlayerGameMode("Mover") != protocol.GameModeSpectator {
		t.Fatal("the switch to spectator applies")
	}
	trackWaypoint(t, op, moverID, protocol.WaypointUntrack)

	// A spectator receiver sees everyone, spectators included: both now
	// spectate, and the mover's step still reaches the op.
	// vanilla: WaypointTransmitter.doesSourceIgnoreReceiver
	op.ChatCommand("gamemode spectator Op")
	op.SettleKeeping()
	if srv.PlayerGameMode("Op") != protocol.GameModeSpectator {
		t.Fatal("the op's switch to spectator applies")
	}
	// The switch pairs the mover again where it stands; that pairing is
	// waited for before the step, which would otherwise race it and land
	// either in this track or in the next. A step of more than one block
	// breaks a block connection, so it is tracked afresh.
	wp := trackWaypoint(t, op, moverID, protocol.WaypointTrack)
	if !wp.at(moverAt.x, moverAt.y, moverAt.z) {
		t.Errorf("the waypoint the spectate pairs points at (%d, %d, %d), want the mover's block (%d, %d, %d)",
			wp.x, wp.y, wp.z, moverAt.x, moverAt.y, moverAt.z)
	}
	mover.MoveToGround(moverStart.X+5, moverStart.Y, moverStart.Z, true)
	wp = trackWaypoint(t, op, moverID, protocol.WaypointTrack)
	if !wp.at(moverAt.x+5, moverAt.y, moverAt.z) {
		t.Errorf("the waypoint during the spectate points at (%d, %d, %d), want where the step landed (%d, %d, %d)",
			wp.x, wp.y, wp.z, moverAt.x+5, moverAt.y, moverAt.z)
	}

	// Back to survival, the mover stays on the spectating op's bar: nothing
	// is re-sent for a pairing that holds.
	op.ChatCommand("gamemode survival Mover")
	op.SettleKeeping()
	if srv.PlayerGameMode("Mover") != protocol.GameModeSurvival {
		t.Fatal("the switch back to survival applies")
	}
	expectNoWaypoint(t, op, 500*time.Millisecond, moverID)
}

// teleportTo runs the teleport a client's own /tp @s is — its own player, the
// coordinates java's doubles spell in the feedback — and reads through the
// feedback and the position sync that lands the client's body.
func teleportTo(t *testing.T, c *servertest.Client, name string, x, y, z float64) {
	t.Helper()
	coords := []string{strconv.FormatFloat(x, 'f', 6, 64), strconv.FormatFloat(y, 'f', 6, 64), strconv.FormatFloat(z, 'f', 6, 64)}
	c.ChatCommand("tp @s " + coords[0] + " " + coords[1] + " " + coords[2])
	servertest.AwaitCommandFeedbackText(t, c, "commands.teleport.success.location.single",
		servertest.PlayerDisplayComponent(name),
		protocol.Text{Text: coords[0]}, protocol.Text{Text: coords[1]}, protocol.Text{Text: coords[2]})
	servertest.ExpectTeleport(t, c)
	c.MoveToGround(x, y, z, true)
}
