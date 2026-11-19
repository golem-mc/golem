package server

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// noteFloating tells the flight check about a step from was to pos; c.tp.mu
// is held. Not modelled: the "stands on something" term
// (verticalCollisionBelow), which needs the collision move.
// vanilla: ServerGamePacketListenerImpl.handleMovePlayer
func (c *client) noteFloating(was, pos player.Position) {
	mode := c.player.GameMode()
	dims := c.dims(c.player.Appearance().Pose)
	hw := dims.Width/2 + 0.0625
	step := rules.FloatingStep{
		DY:          pos.Y - was.Y,
		Spectator:   mode == protocol.GameModeSpectator,
		AllowFlight: c.srv.allowFlight.Load(),
		MayFly:      c.abilityFlags()&protocol.AbilityAllowFlying != 0,
		Levitation:  rules.HasLevitation(c.player.Effects()),
		FallFlying:  c.fallFlying.Load(),
		SpinAttack:  c.player.Appearance().SpinAttack,
	}
	if step.Candidate() {
		w := c.theWorld()
		box := [6]float64{
			pos.X - hw, pos.Y - 0.0625 - 0.55, pos.Z - hw,
			pos.X + hw, pos.Y + dims.Height + 0.0625, pos.Z + hw}
		// A chunk still loading reads as air, but vanilla's scan loads it and finds
		// ground, so a step over one is not floating.
		// vanilla: ServerGamePacketListenerImpl.noBlocksAround
		loaded := true
		for _, x := range [2]float64{box[0], box[3]} {
			for _, z := range [2]float64{box[2], box[5]} {
				loaded = loaded && w.ChunkLoadedAt(int32(math.Floor(x)), int32(math.Floor(z)))
			}
		}
		step.NoBlocksAround = loaded && w.AirIn(box)
	}
	c.tp.fly.Moved(step)
}

// tickFlying counts a hovering tick and kicks a body that hovered too long.
// vanilla: ServerGamePacketListenerImpl.tickPlayer
func (c *client) tickFlying() {
	exempt := c.player.Sleep().Lying || c.player.Vitals().Dead || c.isPassenger()
	var gravity float64
	c.player.Attributes(func(a *player.Attributes) { gravity = a.Value(registry.AttributeGravity) })
	c.tp.mu.Lock()
	kick := c.tp.fly.Tick(exempt, rules.MaxFlyingTicks(gravity))
	c.tp.mu.Unlock()
	if kick && c.kicked.CompareAndSwap(false, true) {
		c.log.Warn("was kicked for floating too long!")
		c.disconnectAfterDrain(protocol.Text{Translate: "multiplayer.disconnect.flying"})
	}
}
