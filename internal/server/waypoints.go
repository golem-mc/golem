package server

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The locator bar (vanilla's ServerWaypointManager): every player in range
// is a waypoint on every other player's bar in the dimension, at a precision
// set by visibility (a block within the receiver's chunks, a chunk beyond,
// an angle past everything). Pairing follows the locator_bar rule and the
// two waypoint range attributes, whose zero is how crouching, invisibility
// and head-worn items take a player off the bar.
//
// Vanilla re-settles a pairing on each step either side takes; here the tick
// settles it, sending the same packets at most a tick later.

// waypointLink is a receiver's copy of a transmitter's waypoint as last
// sent. trackMu guards a transmitter's slot-indexed set of them.
type waypointLink struct {
	viewer *client
	rules.WaypointConnection
}

// waypointPayload builds Waypoint: operation, the transmitter's UUID in the
// identifier's Either, icon style and colour, and the form's fields; a nil
// fill is the formless untrack.
// vanilla: ClientboundTrackedWaypointPacket, TrackedWaypoint.write
func waypointPayload(op int32, id uuid.UUID, icon player.LocatorIcon, kind int32, fill func(b *protocol.Buffer)) []byte {
	var b protocol.Buffer
	b.PutVarInt(op)
	b.PutBool(true) // the identifier's Either: a UUID
	b.PutUUID(id)
	b.PutString(icon.StyleName())
	b.PutBool(icon.HasColor)
	if icon.HasColor {
		b.PutU8(uint8(icon.Color >> 16))
		b.PutU8(uint8(icon.Color >> 8))
		b.PutU8(uint8(icon.Color))
	}
	b.PutVarInt(kind)
	if fill != nil {
		fill(&b)
	}
	return b.Bytes()
}

// waypointPosition builds the block-position form, used for nearby pairs.
func waypointPosition(op int32, id uuid.UUID, icon player.LocatorIcon, x, y, z int32) []byte {
	return waypointPayload(op, id, icon, protocol.WaypointTypeVec3i, func(b *protocol.Buffer) {
		b.PutVarInt(x)
		b.PutVarInt(y)
		b.PutVarInt(z)
	})
}

// waypointChunkNames builds the chunk form, for a transmitter beyond the
// receiver's chunks.
func waypointChunkNames(op int32, id uuid.UUID, icon player.LocatorIcon, x, z int32) []byte {
	return waypointPayload(op, id, icon, protocol.WaypointTypeChunk, func(b *protocol.Buffer) {
		b.PutVarInt(x)
		b.PutVarInt(z)
	})
}

// waypointBearing builds the angle-only form, for a very distant
// transmitter.
func waypointBearing(op int32, id uuid.UUID, icon player.LocatorIcon, angle float32) []byte {
	return waypointPayload(op, id, icon, protocol.WaypointTypeAzimuth, func(b *protocol.Buffer) {
		b.PutF32(angle)
	})
}

// waypointUntrackPayload is the formless untrack removing a waypoint.
func waypointUntrackPayload(id uuid.UUID) []byte {
	return waypointPayload(protocol.WaypointUntrack, id, player.LocatorIcon{}, protocol.WaypointTypeEmpty, nil)
}

// waypointPoint is a transmitter's position as a pairing reads it.
type waypointPoint struct {
	x, y, z                float64
	blockX, blockY, blockZ int32
	chunk                  world.ChunkPos
}

func waypointPointAt(x, y, z float64) waypointPoint {
	return waypointPoint{x: x, y: y, z: z,
		blockX: int32(math.Floor(x)), blockY: int32(math.Floor(y)), blockZ: int32(math.Floor(z)),
		chunk: world.ChunkPos{X: world.ChunkOf(x), Z: world.ChunkOf(z)}}
}

// waypointPlayer is a player as the waypoint pass reads them: body and
// chunk position, and the locator bar's two current ranges.
type waypointPlayer struct {
	c *client
	w *world.World
	waypointPoint
	radius            int32
	transmit, receive float64
	spectator         bool
	changed           bool
	// visible reports whether a chunk is within this player's view, built
	// once per tick so a pairing's view does not allocate.
	visible func(x, z int32) bool
	// Every track/update in this pass names the snapshot's coordinates.
	// Queued payloads are immutable and shared by receivers of the same form.
	payloads [protocol.WaypointUpdate + 1][2][]byte
}

// waypointStamp is everything a pairing's settling reads of one side.
type waypointStamp struct {
	w                 *world.World
	x, y, z           float64
	radius            int32
	transmit, receive float64
	spectator, joined bool
	rule              bool
}

// tickWaypoints settles each player's locator bar against locator_bar and
// the two range attributes. A pairing falling out (rule off, spectator,
// range crossed) is untracked; one coming back is tracked whole; one that
// holds is updated when its position moved and retracked when it moved too
// far for its form. vanilla: ServerWaypointManager.updateWaypoint, updatePlayer
func (s *Server) tickWaypoints() {
	rule := s.world.BoolRule(world.RuleLocatorBar)
	s.mu.RLock()
	present := make([]waypointPlayer, 0, len(s.clients))
	for _, c := range s.clients {
		if c.player == nil || c.keepAlive == nil {
			continue
		}
		pos := c.player.Position()
		present = append(present, waypointPlayer{
			c: c, w: c.theWorld(), waypointPoint: waypointPointAt(pos.X, pos.Y, pos.Z),
			radius: c.chunkRadius(),
		})
	}
	s.mu.RUnlock()

	// Read the attribute map after releasing the player list lock: its lock
	// sits below that one, and the tick's attribute refresh takes it in
	// between.
	anyChanged := false
	for i := range present {
		p := &present[i]
		p.c.player.Attributes(func(a *player.Attributes) {
			p.transmit = a.Value(registry.AttributeWaypointTransmitRange)
			p.receive = a.Value(registry.AttributeWaypointReceiveRange)
		})
		p.spectator = p.c.player.GameMode() == protocol.GameModeSpectator
		stamp := waypointStamp{w: p.w, x: p.x, y: p.y, z: p.z, radius: p.radius,
			transmit: p.transmit, receive: p.receive, spectator: p.spectator,
			joined: p.c.joined(), rule: rule}
		forgot := p.c.waypointsForgot.Swap(false)
		p.changed = forgot || stamp != p.c.waypointSeen
		p.c.waypointSeen = stamp
		anyChanged = anyChanged || p.changed
	}
	if anyChanged {
		setWaypointVisibility(present)
		for i := range present {
			t := &present[i]
			t.c.trackMu.Lock()
			for j := range present {
				if i != j && (t.changed || present[j].changed) {
					settleWaypointLocked(rule, t, &present[j])
				}
			}
			t.c.trackMu.Unlock()
		}
	}
	s.tickMobWaypoints(rule, present)
}

// setWaypointVisibility builds each receiver's view test once per tick.
func setWaypointVisibility(present []waypointPlayer) {
	for i := range present {
		chunk, radius := present[i].chunk, present[i].radius
		present[i].visible = func(x, z int32) bool {
			return world.InChunkTrackingView(world.ChunkPos{X: x, Z: z}, chunk, radius)
		}
	}
}

// waypointViewOf is the transmitter t as the receiver v sees it.
func waypointViewOf(t *waypointPoint, v *waypointPlayer) rules.WaypointView {
	dx, dy, dz := t.x-v.x, t.y-v.y, t.z-v.z
	return rules.WaypointView{
		Dist:   math.Sqrt(dx*dx + dy*dy + dz*dz),
		BlockX: t.blockX, BlockY: t.blockY, BlockZ: t.blockZ,
		ChunkX: t.chunk.X, ChunkZ: t.chunk.Z,
		DX: v.x - t.x, DZ: v.z - t.z,
		ChunkVisible: v.visible,
	}
}

// sendWaypoint sends the link as a track or update with the transmitter's
// icon.
func sendWaypoint(t, v *waypointPlayer, op int32, l *waypointLink) {
	var data []byte
	if l.Form == rules.WaypointAzimuth {
		data = waypointData(t.c, op, l)
	} else {
		cached := &t.payloads[op][l.Form]
		if *cached == nil {
			*cached = waypointData(t.c, op, l)
		}
		data = *cached
	}
	v.c.sendIn(t.w, protocol.IDPlayWaypoint, data)
}

// waypointData builds the transmitter's waypoint at the link's coordinates.
func waypointData(t *client, op int32, l *waypointLink) []byte {
	return waypointFormData(t.player.UUID, t.player.LocatorIcon(), op, l)
}

// waypointFormData builds the waypoint of transmitter id at the link's
// coordinates.
func waypointFormData(id uuid.UUID, icon player.LocatorIcon, op int32, l *waypointLink) []byte {
	switch l.Form {
	case rules.WaypointAzimuth:
		return waypointBearing(op, id, icon, l.Angle)
	case rules.WaypointChunk:
		return waypointChunkNames(op, id, icon, l.X, l.Z)
	default:
		return waypointPosition(op, id, icon, l.X, l.Y, l.Z)
	}
}

// retrackWaypoints untracks and retracks c's waypoint for each viewer so a
// changed icon arrives at once; viewers not yet paired get it with their
// track. vanilla: WaypointCommand.mutateIcon
func (c *client) retrackWaypoints() {
	w := c.theWorld()
	c.trackMu.Lock()
	defer c.trackMu.Unlock()
	for _, link := range c.waypoints.all() {
		link.viewer.sendIn(w, protocol.IDPlayWaypoint, waypointUntrackPayload(c.player.UUID))
		link.viewer.sendIn(w, protocol.IDPlayWaypoint, waypointData(c, protocol.WaypointTrack, link))
	}
}

// settleWaypointLocked settles transmitter t's waypoint on receiver v's bar:
// kept, untracked, updated in place, or retracked after a drift its form
// cannot carry. The caller holds t.c.trackMu.
func settleWaypointLocked(rule bool, t, v *waypointPlayer) {
	if !v.c.joined() {
		return
	}
	view := waypointViewOf(&t.waypointPoint, v)
	hold := rule && t.transmit > 0 && v.receive > 0 &&
		!rules.WaypointIgnoresReceiver(t.spectator, v.spectator, view.Dist, t.transmit, v.receive)

	link, held := t.c.waypoints.get(v.c)
	// Two players leaving one world for two others at once skip each other in
	// forgetWaypoints, so a cross-world link is dropped here. The untrack is
	// queued past the world check: the client keeps waypoints through Respawn.
	if held && link.viewer == v.c && t.c.theWorld() != v.c.theWorld() {
		t.c.waypoints.delete(v.c)
		v.c.sendIn(nil, protocol.IDPlayWaypoint, waypointUntrackPayload(t.c.player.UUID))
		return
	}
	if t.w != v.w {
		return
	}
	// A crossing or leave since the snapshot would leave an orphan waypoint, so
	// recheck the world under the lock forgetting takes.
	if t.c.theWorld() != t.w || v.c.theWorld() != t.w || !t.c.joined() || !v.c.joined() {
		return
	}
	if !hold {
		if held {
			t.c.waypoints.delete(v.c)
			v.c.sendIn(t.w, protocol.IDPlayWaypoint, waypointUntrackPayload(t.c.player.UUID))
		}
		return
	}
	if !held {
		trackWaypointPair(t, v, view)
		return
	}
	switch link.Step(view) {
	case rules.WaypointRetrack:
		trackWaypointPair(t, v, view)
	case rules.WaypointUpdate:
		sendWaypoint(t, v, protocol.WaypointUpdate, link)
	}
}

// trackWaypointPair pairs a receiver with a transmitter and sends the
// track, replacing any record of the pair.
func trackWaypointPair(t, v *waypointPlayer, view rules.WaypointView) {
	link := &waypointLink{viewer: v.c, WaypointConnection: rules.NewWaypointConnection(view)}
	sendWaypoint(t, v, protocol.WaypointTrack, link)
	t.c.waypoints.set(v.c, link)
}

// forgetWaypoints removes a leaving, respawning or crossing player from
// every pairing in w, the world they leave: others' waypoints of them are
// untracked, and theirs of others are untracked to their own client, whose
// respawn or crossing would otherwise leave ghosts. Untracks to c are queued
// past the world check: a crossing calls this once in the new world, and the
// client keeps waypoints through Respawn.
// vanilla: ServerWaypointManager.removePlayer
func (s *Server) forgetWaypoints(c *client, w *world.World) {
	id := c.player.UUID
	s.mu.RLock()
	for _, other := range s.clients {
		if other == c || other.player == nil || other.theWorld() != w {
			continue
		}
		other.trackMu.Lock()
		if _, held := other.waypoints.get(c); held {
			other.waypoints.delete(c)
			c.sendIn(nil, protocol.IDPlayWaypoint, waypointUntrackPayload(other.player.UUID))
		}
		other.trackMu.Unlock()
	}
	s.mu.RUnlock()

	c.trackMu.Lock()
	for _, link := range c.waypoints.all() {
		if link.viewer.theWorld() != w {
			continue
		}
		c.waypoints.delete(link.viewer)
		link.viewer.sendIn(w, protocol.IDPlayWaypoint, waypointUntrackPayload(id))
	}
	c.trackMu.Unlock()
	// Publish after deletion: a concurrent tick may consume the flag before
	// taking the tracking locks, so an earlier store can lose the repair.
	c.waypointsForgot.Store(true)
}
