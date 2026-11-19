package server

import (
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/google/uuid"
	"maps"
)

// askBreed puts a mating to EntityBreedEvent listeners and applies the edited
// experience; false means a listener cancelled it.
// vanilla: Animal.spawnChildFromBreeding
func (s *Server) askBreed(birth *mobBirth) bool {
	if s.plugins == nil || !s.plugins.Has(pluginruntime.EntityBreed) {
		return true
	}
	e := &plugin.EntityBreedEvent{Mother: pluginLivingHandle(birth.motherID), Father: pluginLivingHandle(birth.partnerID), Experience: birth.amount}
	if c := s.clientByUUID(birth.cause); birth.cause != (uuid.UUID{}) && c != nil {
		e.Breeder = pluginPlayer{c: c}
	}
	s.plugins.Fire(e)
	birth.amount = max(e.Experience, 0)
	return !e.Cancelled()
}

// hangingPlaced asks HangingPlaceEvent about a painting or item frame the
// placing player just hung and discards it on a cancel, reporting whether it
// stands. The body is registered first so the handle resolves; a refused one
// is removed in the same tick. The clicked block and face are the event's.
// vanilla: HangingEntityItem.useOn
func (s *Server) hangingPlaced(c *client, w *world.World, id uuid.UUID, entityID, x, y, z, face int32) bool {
	r := s.plugins
	if r == nil || !r.Has(pluginruntime.HangingPlace) {
		return true
	}
	e := &plugin.HangingPlaceEvent{
		Entity: pendingHanging{id: id},
		Player: pluginPlayer{c: c},
		Block:  plugin.Block{World: pluginWorld{srv: s, dim: s.dimensionOfWorld(w), host: s.pluginAdapter}, Pos: plugin.BlockPos{X: int(x), Y: int(y), Z: int(z)}},
		Face:   plugin.BlockFace(face),
	}
	s.firePluginEvent(e)
	if !e.Cancelled() {
		return true
	}
	if f := s.frameByEntityID(w, entityID); f != nil {
		s.killFrame(f)
		return false
	}
	s.paintingsMu.Lock()
	var gone *paintingEntity
	for _, p := range s.paintings {
		if p.w == w && p.id == entityID {
			gone = p
		}
	}
	s.paintingsMu.Unlock()
	if gone != nil {
		s.killPainting(gone)
	}
	return false
}

// askCareer puts a villager's new profession to VillagerCareerChangeEvent
// listeners with every lock the mob step holds released, and reports whether
// it may change.
// vanilla: Villager.setVillagerData
func (s *Server) askCareer(m *entity.Mob, profession string) bool {
	if s.plugins == nil || !s.plugins.Has(pluginruntime.VillagerCareerChange) {
		return true
	}
	ent := s.mobEntityOf(m)
	if ent == nil {
		return true
	}
	key, _ := plugin.ParseKey(profession)
	e := &plugin.VillagerCareerChangeEvent{Villager: pendingVillager{id: ent.uuid}, Profession: key}
	s.askUnlocked(func() { s.plugins.Fire(e) })
	return !e.Cancelled() && !m.Dead
}

// askAcquiredTrades puts each trade a level's draw added to
// VillagerAcquireTradeEvent listeners, with no lock held: a cancel drops the
// trade and a changed one is written back.
// vanilla: Villager.updateTrades
func (s *Server) askAcquiredTrades(draw mobTradeDraw, tag nbt.Compound) nbt.Compound {
	if tag == nil || draw.id == (uuid.UUID{}) || s.plugins == nil || !s.plugins.Has(pluginruntime.VillagerAcquireTrade) || draw.mob.Type != entity.TypeVillager {
		return tag
	}
	added, _ := nbt.Get[nbt.List](tag, "Recipes")
	kept := make([]nbt.Tag, 0, len(added.Items))
	for _, item := range added.Items {
		r, ok := nbt.CompoundView(item)
		if !ok {
			kept = append(kept, item)
			continue
		}
		e := &plugin.VillagerAcquireTradeEvent{Villager: pendingVillager{id: draw.id}, Trade: tradeOfRecipe(r)}
		s.firePluginEvent(e)
		if e.Cancelled() {
			continue
		}
		if edited, ok := recipeOfTrade(e.Trade); ok {
			kept = append(kept, edited)
		} else {
			kept = append(kept, item)
		}
	}
	out := maps.Clone(tag)
	out["Recipes"] = nbt.List{Elem: nbt.TypeCompound, Items: kept}
	return out
}
