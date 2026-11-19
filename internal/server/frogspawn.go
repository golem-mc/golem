package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func (fx *mobEffects) laidSpawn(w *world.World, ev entity.MobEvent) {
	switch ev.Kind {
	case entity.MobLaidTurtleEgg:
		fx.turtleEggs = append(fx.turtleEggs, turtleEgg{w: w, x: int32(ev.X), y: int32(ev.Y), z: int32(ev.Z), eggs: ev.Amount})
		fx.sends = append(fx.sends, mobSend{w: w, id: protocol.IDPlaySoundEffect,
			data: soundEffectIDAt(registry.SoundEntityTurtleLayEgg, protocol.SoundCategoryBlock, ev.X+0.5, ev.Y+0.5, ev.Z+0.5, 0.3, ev.Pitch),
			near: true, x: ev.X, y: ev.Y, z: ev.Z, radius: soundRange(0.3)})
	case entity.MobStompingEgg, entity.MobStompedEgg:
		fx.eggStomped(w, ev)
	case entity.MobLaidFrogspawn:
		fx.frogspawns = append(fx.frogspawns, frogspawn{w: w, x: int32(ev.X), y: int32(ev.Y), z: int32(ev.Z), sourceX: ev.Mob.X, sourceY: ev.Mob.Y, sourceZ: ev.Mob.Z})
	}
}

type frogspawn struct {
	w                         *world.World
	x, y, z                   int32
	sourceX, sourceY, sourceZ float64
}

// layFrogspawn places the block before its game event and laying sound.
// vanilla: TryLaySpawnOnFluidNearLand
func (s *Server) layFrogspawn(spawn frogspawn) {
	state := registry.BlockFrogspawn.DefaultState()
	s.setBlockAndTell(spawn.w, spawn.x, spawn.y, spawn.z, state)
	s.gameEvent(spawn.w, rules.GameEventBlockPlace, float64(spawn.x), float64(spawn.y), float64(spawn.z), rules.GameEventContext{Affected: state})
	s.broadcastSoundID(spawn.w, registry.SoundEntityFrogLaySpawn, protocol.SoundCategoryBlock, spawn.sourceX, spawn.sourceY, spawn.sourceZ, 1, 1)
}
