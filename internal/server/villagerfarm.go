package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

type mobFarm struct {
	w      *world.World
	m      *entity.Mob
	action entity.FarmAction
	at     entity.Waypoint
}

// mobFarmed applies farm actions in collection order, so a later farmer
// sees the earlier one's edit before spending inventory.
func (s *Server) mobFarmed(f mobFarm) {
	w, p := f.w, f.at
	old := w.BlockAt(p.X, p.Y, p.Z)
	s.mobsMu.Lock()
	r := f.m.Farm(w, f.action, p)
	s.mobsMu.Unlock()
	if r.Break && s.destroyBlockContentsBy(w, p.X, p.Y, p.Z, old, entity.TypeVillager.Name()) {
		s.setBlockAndTell(w, p.X, p.Y, p.Z, 0)
		s.gameEvent(w, rules.GameEventBlockDestroy, float64(p.X)+0.5, float64(p.Y)+0.5, float64(p.Z)+0.5, rules.GameEventContext{Affected: old})
	}
	if r.Changed {
		s.setBlockAndTell(w, p.X, p.Y, p.Z, r.State)
		event := rules.GameEventBlockChange
		if f.action == entity.FarmHarvest {
			event = rules.GameEventBlockPlace
			s.broadcastSoundID(w, registry.SoundItemCropPlant, protocol.SoundCategoryBlock, float64(p.X)+0.5, float64(p.Y)+0.5, float64(p.Z)+0.5, 1, 1)
		}
		s.gameEvent(w, event, float64(p.X)+0.5, float64(p.Y)+0.5, float64(p.Z)+0.5, rules.GameEventContext{Affected: r.State})
	}
	if r.Bonemeal {
		changes, pos, drops, used := s.physicsFor(w).DispenseBoneMeal(p.X, p.Y, p.Z)
		if used {
			s.boneMealResults(w, pos, drops, changes)
		}
	}
	for _, stack := range r.Drops {
		s.spawnPoppedItem(w, p.X, p.Y, p.Z, stack)
	}
	if r.Emptied {
		s.broadcastSoundID(w, registry.SoundBlockComposterEmpty, protocol.SoundCategoryBlock, float64(p.X)+0.5, float64(p.Y)+0.5, float64(p.Z)+0.5, 1, 1)
	}
	if r.Composted {
		s.broadcastLevelEvent(w, rules.LevelEventComposterFill, p.X, p.Y, p.Z, r.Fill)
	}
}
