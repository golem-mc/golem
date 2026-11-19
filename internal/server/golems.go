package server

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// Golem building: what a placed carved pumpkin (or jack o'lantern) does with
// the pattern it completes. The shapes are read in internal/server/rules/golems.go,
// cleared here, and the mob spawned whole.

// trySpawnGolem builds the golem, if any, a placed pumpkin completes. The
// pattern is cleared with each removal told (vanilla's flags-2 write plus
// its 2001 break event) and the golem stands at the foot cell's half-block,
// lifted 0.05, an iron golem marked player-created. A write that left the
// same block (a /setblock facing turn) never builds, vanilla onPlace's guard.
// vanilla: CarvedPumpkinBlock.onPlace, trySpawnGolem, spawnGolemInWorld
func (s *Server) trySpawnGolem(w *world.World, x, y, z int32, oldState uint16) {
	block := registry.BlockOf(w.BlockAt(x, y, z))
	if block != registry.BlockCarvedPumpkin && block != registry.BlockJackOLantern {
		return
	}
	if registry.BlockOf(oldState) == block {
		return
	}
	spawn, ok := rules.GolemSpawnOf(w, x, y, z)
	if !ok {
		return
	}

	for _, cell := range spawn.Clear {
		s.clearGolemCell(w, cell[0], cell[1], cell[2])
	}
	dim := s.dimensionOfWorld(w)
	gx, gy, gz := float64(spawn.Feet[0])+0.5, float64(spawn.Feet[1])+0.05, float64(spawn.Feet[2])+0.5
	golem := s.spawnBuiltGolem(dim, entity.MobTypeOf(string(spawn.Kind)), gx, gy, gz, spawn.Weather)
	s.summonedBuildAdvancements(w, entity.MobTypeOf(string(spawn.Kind)), gx, gy, gz, 5)
	if spawn.Kind == rules.CopperGolem {
		// The chest goes in after the golem is placed, over the copper block's
		// cell, with flag 2.
		// vanilla: CarvedPumpkinBlock.replaceCopperBlockWithChest, CopperGolem.spawn
		cell := spawn.Clear[1]
		state := rules.CopperChestState(w, spawn, cell[0], cell[1], cell[2])
		s.setBlockAndTell(w, cell[0], cell[1], cell[2], state)
		s.ensureBlockEntity(w, cell[0], cell[1], cell[2], state)
		s.broadcastEntitySoundID(w, nil, golem, registry.SoundEntityCopperGolemSpawn, protocol.SoundCategoryNeutral, gx, gy, gz, 1, 1)
	}
}

// summonedBuildAdvancements fires summoned_entity for every player whose box
// (by its pose and scale) meets a built mob's box grown by reach.
// vanilla: CarvedPumpkinBlock.trySpawnGolem, WitherSkullBlock.checkSpawn
func (s *Server) summonedBuildAdvancements(w *world.World, kind entity.MobType, x, y, z, reach float64) {
	width, height := entity.KindDims(kind)
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()
	for _, c := range clients {
		if c.theWorld() != w {
			continue
		}
		p := c.player.Position()
		d := c.dims(c.player.Appearance().Pose)
		if math.Abs(p.X-x) < (width+d.Width)/2+reach && math.Abs(p.Z-z) < (width+d.Width)/2+reach &&
			p.Y < y+height+reach && p.Y+d.Height > y-reach {
			c.summonedEntityAdvancements(kind.Name())
		}
	}
}

// clearGolemCell removes one pattern cell: block update, light, neighbour
// shapes and updates, and the 2001 event with the old state. Vanilla clears
// the whole shape before the mob appears. It also announces the iron shape's
// four empty corners with air's id, which clients ignore; those stay unsent.
// vanilla: CarvedPumpkinBlock.clearPatternBlocks
func (s *Server) clearGolemCell(w *world.World, x, y, z int32) {
	state := w.BlockAt(x, y, z)
	if state == airBlockState {
		return
	}
	s.setBlockAndTell(w, x, y, z, airBlockState)
	s.broadcastLevelEvent(w, 2001, x, y, z, int32(state))
}

// spawnBuiltGolem spawns a pattern-built golem plain, with no finalizeSpawn
// rolls, an iron golem getting the player-created bit and a copper golem its
// weathering stage.
// vanilla: CarvedPumpkinBlock.trySpawnGolem
func (s *Server) spawnBuiltGolem(dim *dimension, kind entity.MobType, x, y, z float64, weather int) int32 {
	return s.spawnMobArriving(dim, kind, x, y, z, 0, nil, false, "", false, func(ent *mobEntity) {
		if kind == entity.TypeIronGolem {
			// vanilla: CarvedPumpkinBlock.trySpawnGolem's setPlayerCreated
			ent.mob.SetMetadataPlayerCreated(true)
		}
		if kind == entity.TypeCopperGolem {
			ent.mob.SetCopperWeather(weather)
		}
	})
}

// mobLaidSnow lays a snow golem's trail where mob_griefing allows and the
// layer has air to occupy and ground to survive on.
func (s *Server) mobLaidSnow(lay mobGraze) {
	if lay.w.BoolRule(world.RuleMobGriefing) && lay.w.IsAirAt(lay.x, lay.y, lay.z) &&
		lay.w.SnowLayerSurvives(lay.x, lay.y, lay.z) {
		snow := registry.BlockSnow.DefaultState()
		if !s.mobMayChange(lay.w, lay.mob, lay.x, lay.y, lay.z, snow) {
			return
		}
		s.setBlockAndTell(lay.w, lay.x, lay.y, lay.z, snow)
		// vanilla: SnowGolem.aiStep's gameEvent(BLOCK_PLACE, pos, Context.of(this, snow))
		s.gameEvent(lay.w, rules.GameEventBlockPlace, float64(lay.x)+0.5, float64(lay.y)+0.5, float64(lay.z)+0.5,
			rules.GameEventContext{Affected: snow})
	}
}

// dispenseCarvedPumpkin sets the pumpkin into the air cell in front of the
// dispenser when a golem base stands near it, and reports whether it did.
// The write's wake builds the golem.
// vanilla: DispenseItemBehavior.bootStrap (Blocks.CARVED_PUMPKIN)
func (s *Server) dispenseCarvedPumpkin(w *world.World, d world.Dispense) bool {
	tx, ty, tz := faceOffset(d.X, d.Y, d.Z, int32(d.Dir))
	if !world.IsKnownAirState(w.BlockAt(tx, ty, tz)) || !rules.GolemBaseNear(w, tx, ty, tz) {
		return false
	}
	state := registry.BlockCarvedPumpkin.DefaultState()
	s.setBlockAndTell(w, tx, ty, tz, state)
	s.gameEvent(w, rules.GameEventBlockPlace, float64(tx)+0.5, float64(ty)+0.5, float64(tz)+0.5, rules.GameEventContext{Affected: state})
	return true
}
