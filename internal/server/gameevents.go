package server

import (
	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// gameEvent posts a game event at a position to the world's vibration
// listeners. vanilla: ServerLevel.gameEvent
func (s *Server) gameEvent(w *world.World, ev rules.GameEvent, x, y, z float64, ctx rules.GameEventContext) {
	if seam := s.gameEventSeam.Load(); seam != nil {
		(*seam)(ev.Name(), x, y, z, ctx)
	}
	// radius caps how far a listener hears the event; 0 is vanilla's own range.
	radius := 0
	if s.plugins != nil && s.plugins.Has(pluginruntime.GenericGameEvent) {
		r, ok := s.pluginGameEvent(w, ev, x, y, z, ctx)
		if !ok {
			return
		}
		if r < gameEventRadius {
			radius = r
		}
	}
	if rules.HiddenBySneaking(ev, ctx) && s.physicsFor(w).SneakedPast(x, y, z) {
		if c := s.clientByUUID(ctx.Player); c != nil {
			c.avoidVibrationAdvancements()
		}
	}
	if source, owner := wardenVibrationSource(ev, ctx); s.herdOf(w).wardens.Load() != 0 &&
		entity.WardenListensFor(ev) &&
		!ctx.Spectator && !ctx.Dampens && !rules.HiddenBySneaking(ev, ctx) &&
		!registry.BlockOf(ctx.Affected).InTag(registry.TagIDDampensVibrations) {
		s.offerWardenVibrations(w, ev, x, y, z, source, owner, radius)
	}
	if v, ok := rules.VibrationOf(ev, x, y, z, ctx); ok {
		v.Radius = radius
		s.physicsFor(w).Vibrate(v)
	}
}

// wardenVibrationSource names a vibration's source and owner as vanilla's
// VibrationInfo does. Only a landing's source is the projectile, which this
// event seam does not persist, so its owner is what reacts; a shot's source is
// the shooter.
func wardenVibrationSource(event registry.GameEvent, ctx rules.GameEventContext) (source, owner uuid.UUID) {
	if event == registry.GameEventProjectileLand {
		return uuid.Nil, ctx.Player
	}
	if ctx.Mob != uuid.Nil {
		return ctx.Mob, ctx.ProjectileOwner
	}
	return ctx.Player, ctx.ProjectileOwner
}

// offerWardenVibrations gives one event to every warden in the world. A source
// that is neither a player nor a mob in this world has left; the event is still
// delivered, without a living source to anger the warden at.
// vanilla: ServerLevel.gameEvent, DynamicGameEventListener, VibrationInfo
func (s *Server) offerWardenVibrations(w *world.World, event registry.GameEvent, x, y, z float64, sourceID, owner uuid.UUID, radius int) {
	h := s.herdOf(w)
	// ENTITY_DIE's living source is already dead and cannot be targeted.
	// vanilla: LivingEntity.die, Warden.VibrationUser.canReceiveVibration
	if h.wardens.Load() == 0 || event == rules.GameEventEntityDie {
		return
	}
	var source *entity.WardenSource
	if sourceID != uuid.Nil {
		source = &entity.WardenSource{ID: sourceID, Player: s.wardenVibrationSourcePlayer(w, sourceID)}
	}
	s.mobsMu.Lock()
	if source != nil && source.Player == nil {
		source.Mob = h.byUUID[sourceID]
	}
	for _, m := range h.herd {
		if m.Type != entity.TypeWarden {
			continue
		}
		if radius > 0 && (m.X-x)*(m.X-x)+(m.Y-y)*(m.Y-y)+(m.Z-z)*(m.Z-z) > float64(radius*radius) {
			continue
		}
		m.WardenIdentities = h
		m.WardenOfferVibration(w, event, x, y, z, source, owner)
	}
	s.mobsMu.Unlock()
}

// wardenVibrationSourcePlayer snapshots a player source for the warden's
// targeting checks; nil if id is no player in w.
func (s *Server) wardenVibrationSourcePlayer(w *world.World, id uuid.UUID) *entity.Player {
	c := s.clientInWorld(w, id)
	if c == nil {
		return nil
	}
	pos, vitals, mode := c.player.Position(), c.player.Vitals(), c.player.GameMode()
	team := ""
	if name, onTeam := s.scoreboard.PlayerTeam(c.player.Name); onTeam {
		team = name
	}
	dims := c.dims(c.player.Appearance().Pose)
	return &entity.Player{
		Width: dims.Width, Height: dims.Height, EyeHeight: dims.EyeHeight,
		X: pos.X, Y: pos.Y, Z: pos.Z, UUID: c.player.UUID, EntityID: c.player.EntityID,
		Team: team, Huntable: !vitals.Dead && mode != protocol.GameModeCreative &&
			mode != protocol.GameModeSpectator,
		Spectator: mode == protocol.GameModeSpectator, Dead: vitals.Dead,
		Creative: mode == protocol.GameModeCreative,
	}
}

// containerEvent posts a container open or close at the cell when viewers
// is reached: the first player in, the last out.
// vanilla: ContainerOpenersCounter.incrementOpeners, decrementOpeners
func (c *client) containerEvent(ev rules.GameEvent, w *world.World, cell [3]int32, viewers int) {
	if c.srv.containerOpenerCount(w, cell[0], cell[1], cell[2]) != viewers {
		return
	}
	c.srv.gameEvent(w, ev, float64(cell[0])+0.5, float64(cell[1])+0.5, float64(cell[2])+0.5, c.gameEventContext(0))
}

// gameEventAtCell posts a player-caused event at the centre of a block.
// vanilla: Level.gameEvent(Entity, GameEvent, BlockPos)
func (c *client) gameEventAtCell(ev rules.GameEvent, x, y, z int32) {
	c.srv.gameEvent(c.theWorld(), ev, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, c.gameEventContext(0))
}

// gameEventAt posts a player-caused event at a point, on the affected state.
// vanilla: Level.gameEvent(Entity, GameEvent, Vec3)
func (c *client) gameEventAt(ev rules.GameEvent, x, y, z float64, affected uint16) {
	c.srv.gameEvent(c.theWorld(), ev, x, y, z, c.gameEventContext(affected))
}

// attackerEventContext is the event context of the player with entity ID id,
// the zero context for none (a mob or another body is not resolved).
func (s *Server) attackerEventContext(id int32) rules.GameEventContext {
	if c := s.clientByEntityID(id); c != nil {
		return c.gameEventContext(0)
	}
	return rules.GameEventContext{}
}

// gameEventContext is a player-caused event's context on the affected
// state. vanilla: GameEvent.Context.of(Entity, BlockState)
func (c *client) gameEventContext(affected uint16) rules.GameEventContext {
	return rules.GameEventContext{
		Spectator: c.player.GameMode() == protocol.GameModeSpectator,
		Sneaking:  c.player.Appearance().Sneaking,
		Affected:  affected,
		Player:    c.player.UUID,
	}
}
