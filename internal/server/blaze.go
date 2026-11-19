package server

import (
	"github.com/golem-mc/golem/registry"
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/google/uuid"
)

// blazeShot is a blaze's small fireball, spawned outside the mob lock.
type blazeShot struct {
	w       *world.World
	shooter int32
	uuid    uuid.UUID
	silent  bool
	x, y, z float64
	// eventY is the blaze's height: the level event plays at its block, not
	// the fireball's.
	eventY     float64
	dx, dy, dz float64
	seed       int64
}

func blazeShotOf(w *world.World, ev entity.MobEvent, ent *mobEntity) blazeShot {
	shot := blazeShot{w: w, shooter: ev.Mob.ID, silent: ev.Mob.Silent,
		x: ev.X, y: ev.Y, z: ev.Z, eventY: ev.Mob.Y, dx: ev.VX, dy: ev.VY, dz: ev.VZ, seed: ev.Mob.Rand.Int63()}
	if ent != nil {
		shot.uuid = ent.uuid
	}
	return shot
}

// blazeShoot plays the launch sound and puts the fireball in the world.
// vanilla: Blaze.BlazeAttackGoal.tick
func (s *Server) blazeShoot(shot blazeShot) {
	if !shot.silent {
		s.broadcastLevelEvent(shot.w, protocol.WorldEventBlazeShoot, int32(math.Floor(shot.x)), int32(math.Floor(shot.eventY)), int32(math.Floor(shot.z)), 0)
	}
	s.spawnFireball(entity.ThrownSmallFireball, shot.w, shot.x, shot.y, shot.z, shot.dx, shot.dy, shot.dz,
		shot.shooter, shot.uuid, 0, shot.seed)
}

// breezeShoot puts the already aimed charge in the shared thrown tracker.
func (s *Server) breezeShoot(shot blazeShot) {
	yaw, pitch := entity.ArrowAngles(shot.dx, shot.dy, shot.dz)
	s.standThrown(entity.ThrownBreezeWindCharge, shot.w, shot.x, shot.y, shot.z, shot.dx, shot.dy, shot.dz,
		yaw, pitch, shot.shooter, shot.uuid, shot.seed, entity.NewRolls(shot.seed), protocol.ItemStack{ItemID: int32(registry.ItemWindCharge), Count: 1})
}
