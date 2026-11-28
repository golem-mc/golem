package server

import (
	"math"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
)

// teleportResendTicks is how long a teleport awaits acceptance before a
// move packet resends it.
// vanilla: ServerGamePacketListenerImpl.updateAwaitingTeleport
const teleportResendTicks = 20

// leaveVehicleForTeleport removes the seat without an intermediate step-off
// teleport, so Set Passengers precedes the destination's position packet.
// vanilla: ServerPlayer.teleport, removeVehicle
func (c *client) leaveVehicleForTeleport() {
	c.dismountBoat(false) // also removes mob mounts
	c.dismountCushion(false)
	c.dismountMinecart(false)
}

// teleportWait is the teleport awaiting the client's accept; until then the
// client's moves, sent from before it saw the teleport, are dropped.
// vanilla: ServerGamePacketListenerImpl.awaitingPositionFromClient
type teleportWait struct {
	// mu makes a teleport's write and a move's check-and-write each one step,
	// so no move lands between a teleport's write and its wait. Lock order:
	// lookMu, then mu, then the player's lock and s.mu.
	mu      sync.Mutex
	id      int32 // the last teleport ID sent
	pending bool
	move    command.Move
	sentAt  uint64 // s.ticksRun when it was last sent

	// check is the move checks' per-tick state; fall holds the positions a
	// step refused for entering a block banks its fall from.
	check rules.MoveCheck
	fly   rules.FlightCheck
	fall  *[2]player.Position
}

// awaitTeleport places the player at the move's point and awaits the
// client's accept, returning the Synchronize Player Position. c.tp.mu is
// held. vanilla: ServerGamePacketListenerImpl.teleport
func (c *client) awaitTeleport(move command.Move) []byte {
	if c.tp.id++; c.tp.id == math.MaxInt32 {
		c.tp.id = 0
	}
	// The packet carries the rotation as given (wrapped) while the body keeps a
	// clamped pitch; later teleports' local coordinates measure from the body's
	// angles. vanilla: Entity.setXRot
	payload := syncPositionMovePayload(move, c.tp.id)
	pos := move.Pos
	pos.Pitch = clampBodyPitch(pos.Pitch)
	// A teleport leaves onGround as it was until the next move packet.
	// vanilla: ServerPlayer.teleportSetPosition
	pos.OnGround = c.player.Position().OnGround
	c.player.SetPosition(pos)
	c.portalPrevStale.Store(true)
	c.tp.pending, c.tp.move, c.tp.sentAt = true, move, c.srv.ticksRun.Load()
	// vanilla: ServerGamePacketListenerImpl.resetPosition, run by every
	// ServerPlayer teleport.
	c.tp.check.Reset(pos.X, pos.Y, pos.Z)
	return payload
}

// teleport places the player at the move's point and queues the
// Synchronize Player Position. Queued under the lock, two teleports reach
// the client in ID order. A placeAt user writes its packet later, so a
// teleport during a wake or crossing can overtake an older ID; the client
// then ends at the older position until its next moves set it, as before
// the wait existed.
func (c *client) teleport(move command.Move) {
	c.tp.mu.Lock()
	defer c.tp.mu.Unlock()
	// A jump inside one section leaves the tracking stamp as it was, yet the
	// ranges to every body changed.
	c.tracking.invalidate()
	c.send(protocol.IDPlaySyncPosition, c.awaitTeleport(move))
	c.tp.fly.Reset()
}

// placeAt is teleport for join, respawn, dimension change and wake, which
// write the packet themselves at the right point in their sequence.
func (c *client) placeAt(pos player.Position) []byte {
	return c.placeAtMove(command.Move{Pos: pos})
}

// placeAtMove is placeAt for a move with a dimension change /tp's
// relatives: position absolute, rotation as the move names.
func (c *client) placeAtMove(move command.Move) []byte {
	c.tp.mu.Lock()
	defer c.tp.mu.Unlock()
	c.tp.fly.Reset()
	return c.awaitTeleport(move)
}

// ignoreMove reports whether a move is dropped for a pending teleport,
// resending a long-pending teleport before taking the move's rotation.
// c.tp.mu is held.
// vanilla: ServerGamePacketListenerImpl.handleMovePlayer, updateAwaitingTeleport
func (c *client) ignoreMove(move player.Position) bool {
	if !c.tp.pending {
		return false
	}
	// The resend first, with the rotation from before this packet: vanilla
	// resends in updateAwaitingTeleport and then applies the packet's rotation.
	if c.srv.ticksRun.Load()-c.tp.sentAt > teleportResendTicks {
		cur := c.player.Position()
		// Absolute, with the old rotation and no relative flags: a relative
		// resend would apply the offset a second time.
		to := command.Move{Pos: player.Position{X: c.tp.move.Pos.X, Y: c.tp.move.Pos.Y, Z: c.tp.move.Pos.Z, Yaw: cur.Yaw, Pitch: cur.Pitch}}
		c.send(protocol.IDPlaySyncPosition, c.awaitTeleport(to))
	}
	pos := c.player.Position()
	pos.Yaw, pos.Pitch = move.Yaw, move.Pitch
	c.player.SetPosition(pos)
	return true
}

// handleAcceptTeleport ends the wait when the client accepts the latest
// teleport; an older ID keeps waiting.
// vanilla: ServerGamePacketListenerImpl.handleAcceptTeleportPacket
func (c *client) handleAcceptTeleport(r *protocol.Reader) error {
	id := r.ReadVarInt()
	x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
	yaw, pitch := r.ReadF32(), r.ReadF32()
	if err := r.Err(); err != nil {
		return c.decodeFailure("accept_teleportation", err)
	}
	c.tp.mu.Lock()
	if id != c.tp.id {
		c.tp.mu.Unlock()
		return nil
	}
	if !c.tp.pending || containsInvalidValues(x, y, z, yaw, pitch) {
		c.tp.mu.Unlock()
		return c.invalidPlayerMovement()
	}
	pos := c.player.Position()
	pos.X, pos.Y, pos.Z = c.tp.move.Pos.X, c.tp.move.Pos.Y, c.tp.move.Pos.Z
	pos.Yaw, pos.Pitch = absSnapLook(pos.Yaw, pos.Pitch)
	c.player.SetPosition(pos)
	c.tp.pending = false
	c.tp.mu.Unlock()
	return c.applyPositionAndRotation(x, y, z, yaw, pitch, false)
}
