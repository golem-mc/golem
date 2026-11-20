package server

import (
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
)

// transformReason is why a body of kind from turns into kind into.
func transformReason(from, into entity.MobType) plugin.TransformReason {
	switch {
	case into == entity.TypeDrowned || (from == entity.TypeHusk && into == entity.TypeZombie):
		return plugin.TransformDrowned
	case into == entity.TypeStray:
		return plugin.TransformFrozen
	case from == entity.TypeZombieVillager && into == entity.TypeVillager:
		return plugin.TransformCured
	case into == entity.TypeZombieVillager:
		return plugin.TransformInfection
	case into == entity.TypeZombifiedPiglin || into == entity.TypeZoglin:
		return plugin.TransformPiglinZombified
	case into == entity.TypeFrog:
		return plugin.TransformMetamorphosis
	}
	return plugin.TransformOther
}

// transformListened reports whether a conversion asks anyone.
func (s *Server) transformListened() bool {
	return s.plugins != nil && s.plugins.Has(pluginruntime.EntityTransform)
}

// transformEvent builds EntityTransformEvent for a body about to turn into
// another kind. Into is empty: the new body does not exist until the event
// is done.
func transformEvent(ent *mobEntity, reason plugin.TransformReason) *plugin.EntityTransformEvent {
	return &plugin.EntityTransformEvent{Entity: pendingMob{id: ent.uuid}, Reason: reason}
}

// mobMayChange puts a mob's change of the block at x, y, z to
// EntityChangeBlockEvent listeners, outside any lock, and reports whether it
// may happen. to is the state it would leave there.
// vanilla: EnderMan.EndermanTakeBlockGoal, EatBlockGoal, SnowGolem.aiStep, Level.destroyBlock
func (s *Server) mobMayChange(w *world.World, m *entity.Mob, x, y, z int32, to uint16) bool {
	if m == nil || s.plugins == nil || !s.plugins.Has(pluginruntime.EntityChangeBlock) {
		return true
	}
	ent := s.mobEntityOf(m)
	if ent == nil {
		return true
	}
	e := &plugin.EntityChangeBlockEvent{
		Entity:   pendingMob{id: ent.uuid},
		Block:    plugin.Block{World: pluginWorld{srv: s, dim: s.dimensionOfWorld(w), host: s.pluginAdapter}, Pos: plugin.BlockPos{X: int(x), Y: int(y), Z: int(z)}},
		NewState: plugin.BlockState(to),
	}
	s.firePluginEvent(e)
	return !e.Cancelled()
}

// askTransform puts a conversion from inside a mob's step to listeners, with
// every lock released like a hurt's ask, and reports whether it may happen.
// vanilla: Mob.convertTo
func (s *Server) askTransform(m *entity.Mob, into entity.MobType) bool {
	if !s.transformListened() {
		return true
	}
	ent := s.mobEntityOf(m)
	if ent == nil {
		return true
	}
	e := transformEvent(ent, transformReason(m.Type, into))
	s.askUnlocked(func() { s.plugins.Fire(e) })
	return !e.Cancelled() && !m.Dead
}

// askTransformEnt is askTransform for a body already out of the herd's book.
func (s *Server) askTransformEnt(ent *mobEntity, into entity.MobType) bool {
	if !s.transformListened() {
		return true
	}
	e := transformEvent(ent, transformReason(ent.mob.Type, into))
	s.askUnlocked(func() { s.plugins.Fire(e) })
	return !e.Cancelled()
}

// transformAllowed puts a conversion a block or a bolt causes, outside any
// mob step, to listeners and reports whether it may happen.
// vanilla: Pig.thunderHit, Villager.thunderHit, MushroomCow.shear
func (s *Server) transformAllowed(ent *mobEntity, into entity.MobType, reason plugin.TransformReason) bool {
	if !s.transformListened() {
		return true
	}
	e := transformEvent(ent, reason)
	s.firePluginEvent(e)
	return !e.Cancelled()
}

// combustTicks puts a body catching fire to EntityCombustEvent listeners and
// returns the ticks it burns, 0 when cancelled.
// vanilla: Entity.igniteForTicks
func (s *Server) combustTicks(who plugin.Entity, ticks int, byEntity plugin.Entity, byBlock *plugin.Block) int {
	if s.plugins == nil || !s.plugins.Has(pluginruntime.EntityCombust) {
		return ticks
	}
	e := &plugin.EntityCombustEvent{Entity: who, Duration: ticks, ByEntity: byEntity, ByBlock: byBlock}
	s.firePluginEvent(e)
	if e.Cancelled() {
		return 0
	}
	return max(e.Duration, 0)
}

// askCombust is combustTicks for a mob, with every lock released.
func (s *Server) askCombust(m *entity.Mob, ticks int, src entity.CombustSource) int {
	if s.plugins == nil || !s.plugins.Has(pluginruntime.EntityCombust) {
		return ticks
	}
	ent := s.mobEntityOf(m)
	if ent == nil {
		return ticks
	}
	e := &plugin.EntityCombustEvent{Entity: pendingMob{id: ent.uuid}, Duration: ticks}
	if src.HasBlock {
		b := plugin.Block{
			World: pluginWorld{srv: s, dim: s.dimensionOfWorld(ent.w), host: s.pluginAdapter},
			Pos:   plugin.BlockPos{X: int(src.X), Y: int(src.Y), Z: int(src.Z)},
		}
		e.ByBlock = &b
	}
	if by, ok := src.By.(plugin.Entity); ok {
		e.ByEntity = by
	}
	s.askUnlocked(func() { s.plugins.Fire(e) })
	if e.Cancelled() || m.Dead {
		return 0
	}
	return max(e.Duration, 0)
}
