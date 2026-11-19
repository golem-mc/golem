package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

// refuseStep runs the moved-too-quickly and moved-wrongly checks
// (rules.MoveCheck) on a reported step, sending a refused one back with a
// teleport; c.tp.mu is held. Not modelled: server-side velocity (so the
// step budget has none), moved-wrongly's replay through the body's own
// collision, and vanilla's removeLatestMovementRecording (no recording
// here). A step refused for entering a block leaves its zero-delta fall
// check in c.tp.fall for moveStep to run after the lock.
// vanilla: ServerGamePacketListenerImpl.handleMovePlayer
func (c *client) refuseStep(to player.Position) bool {
	if c.srv.moveChecksOff {
		return false
	}
	was := c.player.Position()
	w := c.srv.world
	fallFlying := c.fallFlying.Load()
	v := c.tp.check.Judge(rules.MoveStep{
		Tick: c.srv.ticksRun.Load(), Was: was, To: to,
		TickRunning:   c.srv.tickRate.Running(),
		MovementCheck: w.BoolRule(world.RulePlayerMovementCheck),
		ElytraCheck:   w.BoolRule(world.RuleElytraMovementCheck),
		FallFlying:    fallFlying,
		Spectator:     c.player.GameMode() == protocol.GameModeSpectator,
		Collides: func() bool {
			dims := c.dims(c.player.Appearance().Pose)
			hw := dims.Width / 2
			return c.theWorld().CollidesAnew(
				[6]float64{was.X - hw, was.Y, was.Z - hw, was.X + hw, was.Y + dims.Height, was.Z + hw},
				[6]float64{to.X - hw, to.Y, to.Z - hw, to.X + hw, to.Y + dims.Height, to.Z + hw})
		},
	})
	if !v.Refused {
		return false
	}
	if v.TooQuick {
		c.log.Warn("moved too quickly!", "dx", v.Delta[0], "dy", v.Delta[1], "dz", v.Delta[2])
	} else {
		c.log.Warn("moved wrongly!", "x", to.X, "y", to.Y, "z", to.Z)
		// vanilla: doCheckFallDamage with zero deltas after the teleport back; only
		// the packet's on-ground flag can land an earlier fall.
		c.tp.fall = &[2]player.Position{was, {X: was.X, Y: was.Y, Z: was.Z, OnGround: to.OnGround}}
	}
	c.send(protocol.IDPlaySyncPosition, c.awaitTeleport(command.Move{Pos: v.Back}))
	return true
}
