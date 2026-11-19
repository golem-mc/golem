package server

import (
	"time"

	"github.com/golem-mc/golem/internal/protocol"
)

// idleMoveSq is the squared distance a step must exceed to count as acting.
// vanilla: ServerGamePacketListenerImpl.handlePlayerKnownMovement
const idleMoveSq = 1.0e-5

// isIdleResetPacket reports whether a play packet counts as acting. Several
// count only once the client has loaded, chat only when not hidden.
// vanilla: ServerGamePacketListenerImpl resetLastActionTime callers
func (c *client) isIdleResetPacket(id int32) bool {
	switch id {
	case protocol.IDPlayPlayerInput,
		protocol.IDPlayPlayerAction,
		protocol.IDPlayUseItemOn,
		protocol.IDPlayUseItem,
		protocol.IDPlayPlayerCommand,
		protocol.IDPlayAttackIn,
		protocol.IDPlayInteractIn,
		protocol.IDPlaySpectatorAction:
		return c.clientLoaded()
	case protocol.IDPlayChatMessage:
		return c.chatMode.Load() != protocol.ChatModeHidden
	case protocol.IDPlayHeldItemIn,
		protocol.IDPlayChatCommand,
		protocol.IDPlaySignedChatCommand,
		protocol.IDPlayPunch,
		protocol.IDPlayClientCommandIn,
		protocol.IDPlayClickContainer,
		protocol.IDPlayPlaceRecipe,
		protocol.IDPlayContainerButtonClickIn,
		protocol.IDPlaySignUpdateIn:
		return true
	}
	return false
}

// noteMovement counts a client-reported move over idleMoveSq as acting;
// server-driven moves never reach here.
// vanilla: ServerGamePacketListenerImpl.handlePlayerKnownMovement
func (c *client) noteMovement(dx, dy, dz float64) {
	if dx*dx+dy*dy+dz*dz > idleMoveSq {
		c.resetLastAction()
	}
}

// resetLastAction notes that the player has just acted.
// vanilla: ServerPlayer.resetLastActionTime
func (c *client) resetLastAction() {
	c.lastAction.Store(c.spamTime().UnixNano())
}

// tickIdle kicks a player idle longer than the idle timeout, sparing one
// who won the game. vanilla: ServerGamePacketListenerImpl.tick
func (c *client) tickIdle() {
	last := c.lastAction.Load()
	minutes := c.srv.idleTimeout.Load()
	if last <= 0 || minutes <= 0 || c.wonGame.Load() || c.kicked.Load() {
		return
	}
	if c.spamTime().UnixNano()-last <= int64(time.Duration(minutes)*time.Minute) {
		return
	}
	c.kicked.Store(true)
	// Disconnect after the queue drains, so the tick never waits on a client
	// that stopped reading.
	c.disconnectAfterDrain(protocol.Text{Translate: "multiplayer.disconnect.idling"})
}
