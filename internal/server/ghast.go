package server

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/google/uuid"
)

// ghastShot is a ghast's warning or large fireball, spawned outside the
// mob lock.
type ghastShot struct {
	kind    entity.MobEventKind
	w       *world.World
	warn    bool
	shooter int32
	uuid    uuid.UUID
	silent  bool
	// bx, by, bz is the ghast's block, where the level events ring.
	bx, by, bz int32
	x, y, z    float64
	dx, dy, dz float64
	power      int
	seed       int64
}

func ghastShotOf(w *world.World, ev entity.MobEvent, ent *mobEntity) ghastShot {
	shot := ghastShot{w: w, kind: ev.Kind, warn: ev.Kind == entity.MobGhastWarned, shooter: ev.Mob.ID, silent: ev.Mob.Silent,
		bx: int32(math.Floor(ev.Mob.X)), by: int32(math.Floor(ev.Mob.Y)), bz: int32(math.Floor(ev.Mob.Z)),
		x: ev.X, y: ev.Y, z: ev.Z, dx: ev.VX, dy: ev.VY, dz: ev.VZ, power: int(ev.Radius), seed: ev.Mob.Rand.Int63()}
	if ent != nil {
		shot.uuid = ent.uuid
	}
	return shot
}

// ghastShoot plays the warning or launch sound and spawns the fireball.
// vanilla: Ghast.GhastShootFireballGoal.tick
func (s *Server) ghastShoot(shot ghastShot) {
	if shot.warn {
		if !shot.silent {
			s.broadcastLevelEvent(shot.w, protocol.WorldEventGhastWarn, shot.bx, shot.by, shot.bz, 0)
		}
		return
	}
	if !shot.silent {
		s.broadcastLevelEvent(shot.w, protocol.WorldEventGhastShoot, shot.bx, shot.by, shot.bz, 0)
	}
	s.spawnFireball(entity.ThrownLargeFireball, shot.w, shot.x, shot.y, shot.z, shot.dx, shot.dy, shot.dz,
		shot.shooter, shot.uuid, shot.power, shot.seed)
}
