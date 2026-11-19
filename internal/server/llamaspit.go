package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// mobProjectileShot is a llama's spit or a snow golem's snowball, spawned
// outside the mob lock.
type mobProjectileShot struct {
	w       *world.World
	shooter int32
	uuid    uuid.UUID
	silent  bool
	x, y, z float64
	// soundX, soundY, soundZ is the llama's own position: the sound plays
	// there, not at the spit's start.
	soundX, soundY, soundZ float64
	vx, vy, vz             float64
	pitch                  float32
	seed                   int64
}

func spitOf(w *world.World, ev entity.MobEvent, ent *mobEntity) mobProjectileShot {
	shot := mobProjectileShot{w: w, shooter: ev.Mob.ID, silent: ev.Mob.Silent,
		x: ev.X, y: ev.Y, z: ev.Z, soundX: ev.Mob.X, soundY: ev.Mob.Y, soundZ: ev.Mob.Z, vx: ev.VX, vy: ev.VY, vz: ev.VZ, pitch: ev.Pitch, seed: ev.Mob.Rand.Int63()}
	if ent != nil {
		shot.uuid = ent.uuid
	}
	return shot
}

// spitShoot plays the spit's sound at the llama and puts the spit in the
// world. The thrown shell wants an item, which a spit never sends.
// vanilla: Llama.spit
func (s *Server) spitShoot(shot mobProjectileShot) {
	if !shot.silent {
		s.broadcastSoundID(shot.w, registry.SoundEntityLlamaSpit, protocol.SoundCategoryNeutral,
			shot.soundX, shot.soundY, shot.soundZ, 1, shot.pitch)
	}
	t := entity.NewThrown(s.nextEntity(), entity.ThrownLlamaSpit, shot.x, shot.y, shot.z,
		shot.vx, shot.vy, shot.vz, shot.shooter, shot.seed)
	if it := thrownShell(t, shot.w, shot.uuid, protocol.ItemStack{ItemID: int32(registry.ItemShulkerShell), Count: 1}); it != nil {
		s.launchThrown(it)
	}
}

// snowballShoot puts a snow golem's snowball in the world; the sound was
// queued with the event.
// vanilla: SnowGolem.performRangedAttack
func (s *Server) snowballShoot(shot mobProjectileShot) {
	t := entity.NewThrown(s.nextEntity(), entity.ThrownSnowball, shot.x, shot.y, shot.z,
		shot.vx, shot.vy, shot.vz, shot.shooter, shot.seed)
	if it := thrownShell(t, shot.w, shot.uuid, protocol.ItemStack{ItemID: int32(registry.ItemSnowball), Count: 1}); it != nil {
		s.launchThrown(it)
	}
}

// spitHitPlayer hurts a player the spit struck for 1, scaled by difficulty.
// vanilla: LlamaSpit.onHitEntity
func (s *Server) spitHitPlayer(hit bulletPlayerHit) {
	w := hit.victim.theWorld()
	damage := entity.LlamaSpitDamage
	if hit.ownerMob {
		damage = rules.ScaledWithDifficulty(damage, w.Difficulty())
	}
	killer := s.entityKiller(w, hit.ownerID)
	landed, softened := hit.victim.hurt(damage, rules.DamageTypeSpit, &damageCause{
		entityID: hit.ownerID, directID: hit.id + 1, x: hit.x, y: hit.y, z: hit.z,
		killer: killer, directName: s.directNameOf(killer, protocol.EntityTypeLlamaSpit, hit.uuid, hit.custom),
		flying: true, flightVX: hit.vx, flightVZ: hit.vz})
	if landed {
		hit.victim.arrowKnockback(w, hit.vx, hit.vz, 0, !softened, syncVelocityAfterTravel)
		s.thornsOnOwner(w, hit.victim, hit.ownerID, rules.DamageTypeSpit, -1)
	}
}
