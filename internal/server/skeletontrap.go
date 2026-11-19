package server

import (
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
)

// skeletonTrap is a trap horse a player came near this tick.
type skeletonTrap struct {
	w     *world.World
	horse *mobEntity
}

// trapHorseOdds is the share of the effective difficulty a strike's trap
// roll succeeds at. vanilla: ServerLevel.tickThunder
const trapHorseOdds = 0.01

// rollTrapHorse decides whether a thunderstorm strike at the cell brings a
// skeleton trap, and spawns the waiting horse if so; the bolt that comes
// with a trap is visual only. vanilla: ServerLevel.tickThunder
func (s *Server) rollTrapHorse(dim *dimension, x, y, z int32) bool {
	w := dim.w
	odds := float64(s.effectiveDifficultyAt(w, float64(x), float64(z))) * trapHorseOdds
	if s.trapOdds > 0 {
		odds = s.trapOdds
	}
	trap := s.world.BoolRule(world.RuleSpawnMobs) && s.levelRng.Float64() < odds && !w.LightningRodAt(x, y-1, z)
	if trap {
		s.spawnMobArriving(dim, entity.TypeSkeletonHorse, float64(x), float64(y), float64(z), 0, nil, false, "", false,
			func(ent *mobEntity) { ent.mob.Horse.Trap = true })
	}
	return trap
}

// springSkeletonTrap strikes a sprung trap horse with a harmless bolt, seats
// an armed skeleton on it and brings three more horses with riders, pushed
// apart. A horse that died or left since its tick springs nothing.
// vanilla: SkeletonTrapGoal.tick
func (s *Server) springSkeletonTrap(trap skeletonTrap) {
	dim := s.dimensionOfWorld(trap.w)
	s.mobsMu.Lock()
	h := trap.horse.mob
	alive := s.herdOf(trap.w).book[h] == trap.horse && !h.Dead
	x, y, z := h.X, h.Y, h.Z
	pushes := h.TrapPushes()
	s.mobsMu.Unlock()
	if !alive {
		return
	}
	local := rules.NewLocalDifficulty(s.world.Difficulty(), s.effectiveDifficultyAt(trap.w, x, z))
	rider := func() *mobEntity {
		return s.standMobArriving(dim, entity.TypeSkeleton, x, y, z, 0, nil, false, "", true,
			func(ent *mobEntity) { ent.mob.ArmTrapRider(local) })
	}
	s.spawnBolt(trap.w, x, y, z, true, plugin.LightningSkeletonTrap)
	s.linkMobPassengers(trap.horse, rider())
	for _, push := range pushes {
		horse := s.standMobArriving(dim, entity.TypeSkeletonHorse, x, y, z, 0, nil, false, "", true,
			func(ent *mobEntity) {
				ent.mob.ReadyTrapHorse()
				ent.mob.VX += push[0]
				ent.mob.VZ += push[1]
			})
		s.linkMobPassengers(horse, rider())
	}
}
