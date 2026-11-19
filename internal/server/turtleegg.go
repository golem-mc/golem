package server

import (
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/registry"

	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

// trampleTurtleEgg breaks an egg underfoot at 1 in odds and tells
// everyone; spectators touch nothing. vanilla: TurtleEggBlock.destroyEgg
func (c *client) trampleTurtleEgg(odds int) {
	if c.player.GameMode() == protocol.GameModeSpectator {
		return
	}
	w := c.theWorld()
	pos := c.player.Position()
	halfWidth := c.dims(c.player.Appearance().Pose).Width / 2
	x, y, z := rules.StandingBlock(w, pos.X, pos.Y, pos.Z, halfWidth)
	if !world.IsTurtleEgg(w.BlockAt(x, y, z)) {
		return
	}
	broke, ok := c.srv.physicsFor(w).TurtleEggTrampled(x, y, z, odds)
	if !ok {
		return
	}
	c.srv.announceTurtleEggBreak(w, x, y, z, broke)
}

// mobTrampledTurtleEgg is the mob half of the egg's fallOn and stepOn: only
// a mob mob_griefing allows breaks eggs, and a landing also spares zombies
// besides turtles and bats.
// vanilla: TurtleEggBlock.fallOn, TurtleEggBlock.stepOn, TurtleEggBlock.canDestroyEgg
func (s *Server) mobTrampledTurtleEgg(t mobTrample) {
	odds := world.TurtleEggStepOdds
	allowed := entity.BreaksTurtleEggsStepping(t.kind)
	if t.fall > 0 {
		odds = world.TurtleEggLandOdds
		allowed = entity.BreaksTurtleEggsLanding(t.kind)
	}
	if !allowed || !t.w.BoolRule(world.RuleMobGriefing) {
		return
	}
	x := int32(math.Floor(t.x))
	z := int32(math.Floor(t.z))
	y := int32(math.Floor(t.y - 0.2))
	broke, ok := s.physicsFor(t.w).TurtleEggTrampled(x, y, z, odds)
	if ok {
		s.announceTurtleEggBreak(t.w, x, y, z, broke)
	}
}

// announceTurtleEggBreak sends a broken egg's block writes, sound and break
// event.
func (s *Server) announceTurtleEggBreak(w *world.World, x, y, z int32, broke world.TurtleEggBreak) {
	s.tellBlockChanges(w, broke.Changes)
	s.broadcastSoundID(w, registry.SoundEntityTurtleEggBreak, protocol.SoundCategoryBlock,
		float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 0.7, broke.Pitch)
	if broke.EggsRemain {
		// vanilla: TurtleEggBlock.decreaseEggs emits a source-free event after the write.
		s.gameEvent(w, rules.GameEventBlockDestroy, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5,
			rules.GameEventContext{Affected: broke.State})
	}
	if !broke.EggsRemain && len(broke.Changes) > 0 {
		// vanilla: TurtleEggBlock.decreaseEggs, Level.destroyBlock
		s.gameEvent(w, rules.GameEventBlockDestroy, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5,
			rules.GameEventContext{Affected: broke.State})
	}
	// Mining already removed the last egg and emitted its player-context effects.
	if len(broke.Changes) > 0 {
		s.broadcastLevelEvent(w, protocol.WorldEventBlockBreak, x, y, z, int32(broke.State))
	}
}
