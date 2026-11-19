package server

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// shulkerMetadata is a shulker's attach face, peek and colour less its
// default 16 (see Mob.ShulkerColor), as compared for metadata.
type shulkerMetadata [3]uint8

// putShulkerMetadata writes the fields of now that differ from was; a
// spawn passes the defaults (down, shut, undyed).
// vanilla: Shulker.DATA_ATTACH_FACE_ID, DATA_PEEK_ID, DATA_COLOR_ID
func putShulkerMetadata(b *protocol.Buffer, now, was shulkerMetadata) {
	if now[0] != was[0] {
		b.PutMetadata(protocol.MetadataIndexShulkerFace, protocol.MetadataDirection)
		b.PutVarInt(int32(now[0]))
	}
	if now[1] != was[1] {
		b.PutMetadata(protocol.MetadataIndexShulkerPeek, protocol.MetadataByte)
		b.PutU8(now[1])
	}
	if now[2] != was[2] {
		b.PutMetadata(protocol.MetadataIndexShulkerColor, protocol.MetadataByte)
		b.PutU8(now[2] + 16)
	}
}

// shulkerShot is a shulker's bullet, spawned after the mob lock.
type shulkerShot struct {
	w       *world.World
	owner   int32
	uuid    uuid.UUID
	x, y, z float64
	target  entity.ArrowTarget
	axis    int
	seed    int64
	// color is the copy's DyeColor id, 16 for none.
	color uint8
}

// spawnShulkerBullet stands the bullet in the world, owned by the shulker.
// vanilla: Shulker.ShulkerAttackGoal.tick
func (s *Server) spawnShulkerBullet(shot shulkerShot) {
	t := entity.NewShulkerBullet(s.nextEntity(), shot.w, shot.x, shot.y, shot.z, shot.owner, shot.target, shot.axis, shot.seed)
	it := thrownShell(t, shot.w, shot.uuid, protocol.ItemStack{ItemID: int32(registry.ItemShulkerShell), Count: 1})
	if it != nil {
		it.targetUUID = s.entityIdentityOf(shot.target.EntityID)
		s.launchThrown(it)
	}
}

// spawnShulkerCopy stands up the shulker a hit shulker's duplication made,
// dyed as the original.
// vanilla: Shulker.hitByShulkerBullet
func (s *Server) spawnShulkerCopy(c shulkerShot) {
	s.spawnMobArriving(s.dimensionOfWorld(c.w), entity.TypeShulker, c.x, c.y, c.z, 0, nil, false, "", false,
		func(ent *mobEntity) { ent.mob.SetShulkerColor(c.color) })
}

// bulletHitMob deals a bullet's blow to a mob under the mob lock: 4 damage
// and Levitation; a shulker hit with its shell open may teleport and
// duplicate. vanilla: ShulkerBullet.onHitEntity
func (s *Server) bulletHitMob(pass *fireballPass, dim *dimension, it *thrownEntity, m *entity.Mob,
	shooterPlayer, shooterCreative bool, from ...*entity.HitSource) (landed, softened bool) {
	t := it.throwable
	landed, softened = m.HurtByShulkerBullet(entity.ShulkerBulletDamage, t.VX, t.VZ, t.OwnerID, shooterPlayer, shooterCreative, from...)
	if !landed {
		return landed, softened
	}
	in := effect.New(registry.MobEffectLevitation, entity.ShulkerBulletLevitation, 0, false, true, true)
	if ch, ok := m.AddEffect(in); ok {
		if ent := dim.mobs.book[m]; ent != nil {
			pass.effects = append(pass.effects, fireballEffectTell{it.w, mobEffectTellOf(ent, []effect.Change{ch})})
		}
	}
	return landed, softened
}

// bulletLand tells a bullet's end: on a block it bursts with a sound.
// vanilla: ShulkerBullet.onHitBlock
func (s *Server) bulletLand(land fireballLanding) {
	if land.onBlock {
		s.sendParticles(land.w, protocol.ParticleExplosion, land.x, land.y, land.z, 0.2, 0.2, 0.2, 0, 2)
		s.broadcastSoundID(land.w, registry.SoundEntityShulkerBulletHit, protocol.SoundCategoryHostile, land.x, land.y, land.z, 1, 1)
	}
	s.gameEvent(land.w, rules.GameEventEntityDamage, land.x, land.y, land.z, rules.GameEventContext{})
}

// bulletPlayerHit is a bullet striking a player.
type bulletPlayerHit struct {
	victim   *client
	ownerID  int32
	ownerMob bool
	id       int32
	uuid     uuid.UUID
	custom   customName
	x, y, z  float64
	vx, vz   float64
}

// bulletHitPlayer hurts the player and, if the hurt landed, lifts them.
// vanilla: ShulkerBullet.onHitEntity
func (s *Server) bulletHitPlayer(hit bulletPlayerHit) {
	w := hit.victim.theWorld()
	damage := entity.ShulkerBulletDamage
	if hit.ownerMob {
		// vanilla: Player.hurtServer, DamageEffects scaling of mob_projectile
		damage = rules.ScaledWithDifficulty(damage, w.Difficulty())
	}
	killer := s.entityKiller(w, hit.ownerID)
	landed, softened := hit.victim.hurt(damage, rules.DamageTypeMobProjectile, &damageCause{
		entityID: hit.ownerID, directID: hit.id + 1, x: hit.x, y: hit.y, z: hit.z,
		killer: killer, directName: s.directNameOf(killer, protocol.EntityTypeShulkerBullet, hit.uuid, hit.custom),
		flying: true, flightVX: hit.vx, flightVZ: hit.vz})
	if !landed {
		return
	}
	hit.victim.arrowKnockback(w, hit.vx, hit.vz, 0, !softened, syncVelocityAfterTravel)
	hit.victim.AddEffect(effect.New(registry.MobEffectLevitation, entity.ShulkerBulletLevitation, 0, false, true, true))
	s.thornsOnOwner(w, hit.victim, hit.ownerID, rules.DamageTypeMobProjectile, -1)
}

// shulkerFired queues the bullet a shulker fired at a player.
func (t *mobDimTick) shulkerFired(fx *mobEffects, ev entity.MobEvent) {
	h, w := t.h, t.w
	shot := shulkerShot{w: w, owner: ev.Mob.ID, x: ev.X, y: ev.Y, z: ev.Z,
		target: entity.SizedPlayerTarget(ev.Player.EntityID, ev.Player.X, ev.Player.Y, ev.Player.Z, ev.Player.Width, ev.Player.Height, ev.Player.EyeHeight, false),
		axis:   int(ev.Status), seed: ev.Seed}
	if ent, ok := h.book[ev.Mob]; ok {
		shot.uuid = ent.uuid
	}
	fx.bullets = append(fx.bullets, shot)
}

// shulkerShell tells the shell's opening or closing: its sound and the
// container game event, which a silent shulker still posts.
// vanilla: Shulker.setRawPeekAmount
func (t *mobDimTick) shulkerShell(fx *mobEffects, ev entity.MobEvent) {
	m := ev.Mob
	sound, event := registry.SoundEntityShulkerOpen, rules.GameEventContainerOpen
	if ev.Status == 0 {
		sound, event = registry.SoundEntityShulkerClose, rules.GameEventContainerClose
	}
	fx.voiceID(t.w, m, sound, 1, 1)
	fx.gameEvent(t.w, event, m.X, m.Y, m.Z, rules.GameEventContext{Mob: t.h.MobUUID(m)})
}

// shulkerTeleported tells a teleport from the cell the shulker left.
// vanilla: Shulker.teleportSomewhere
func (t *mobDimTick) shulkerTeleported(fx *mobEffects, ev entity.MobEvent) {
	if ent := t.h.book[ev.Mob]; ent != nil && (ent.vehicle != nil || ent.cart != nil) {
		fx.releases = append(fx.releases, mobSeatRelease{ent: ent, keepPosition: true})
	}
	if send, ok := mobVoiceSendAt(t.w, ev.Mob, ev.X, ev.Y, ev.Z, registeredSoundHolder(registry.SoundEntityShulkerTeleport), 1, 1); ok {
		fx.sends = append(fx.sends, send)
	}
	// The event is posted at the centre of the old block.
	fx.gameEvent(t.w, rules.GameEventTeleport, math.Floor(ev.X)+0.5, math.Floor(ev.Y)+0.5, math.Floor(ev.Z)+0.5,
		rules.GameEventContext{Mob: t.h.MobUUID(ev.Mob)})
	x, y, z := blockOf(ev.X, ev.Y, ev.Z)
	tx, ty, tz := blockOf(ev.VX, ev.VY, ev.VZ)
	fx.levelEvent(t.w, protocol.WorldEventShulkerTeleport, x, y, z, protocol.PackPosDiff(x, y, z, tx, ty, tz, 8, 8, 8))
}

// bulletStruck tells a bullet's end by a hit: it is gone, with a crit burst
// and its own sound, and the damage is a vibration.
// vanilla: ShulkerBullet.hurtServer, destroy
func (s *Server) bulletStruck(w *world.World, id int32, x, y, z float64) {
	s.broadcastWorld(w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{id}))
	s.broadcastSoundID(w, registry.SoundEntityShulkerBulletHurt, protocol.SoundCategoryHostile, x, y, z, 1, 1)
	s.sendParticles(w, protocol.ParticleCrit, x, y, z, 0.2, 0.2, 0.2, 0, 15)
	s.gameEvent(w, rules.GameEventEntityDamage, x, y, z, rules.GameEventContext{})
}

// bulletByIDLocked is the live bullet with the entity ID in w. The arrow lock
// is held.
func (s *Server) bulletByIDLocked(w *world.World, id int32) *thrownEntity {
	for _, it := range s.thrown {
		if it.throwable.ID == id && it.w == w && !it.throwable.Dead && it.throwable.Kind == entity.ThrownShulkerBullet {
			return it
		}
	}
	return nil
}

// attackBullet is a player's blow at a shulker bullet in reach: any blow
// that deals damage destroys it. A non-living target takes no wear on the
// held item.
// vanilla: Player.attack, ShulkerBullet.hurtServer
func (c *client) attackBullet(it *thrownEntity) {
	s := c.srv
	s.arrowsMu.Lock()
	t := it.throwable
	minX, minY, minZ, maxX, maxY, maxZ := t.BlastBox()
	dead := t.Dead
	s.arrowsMu.Unlock()
	if dead || !c.withinAttackRange(minX, minY, minZ, maxX, maxY, maxZ) {
		return
	}
	held := c.heldItem(protocol.HandMain)
	blow, ok := c.prepareAttack(held, protocol.EntityTypeShulkerBullet, false)
	if !ok {
		return
	}
	s.arrowsMu.Lock()
	if t.Dead {
		s.arrowsMu.Unlock()
		return
	}
	t.Dead = true
	x, y, z := t.X, t.Y, t.Z
	s.arrowsMu.Unlock()
	s.bulletStruck(it.w, t.ID, x, y, z)
	c.endSprintAfterKnockback(rules.AttackKnockback(rules.EnchantedKnockback(held, &blow.context, c.attackKnockbackBase()), blow.feedback.Knockback))
	if blow.feedback.Sweep {
		c.sweepHurt(it.w, held, blow.baseDamage, t.ID, minX, minY, minZ, maxX, maxY, maxZ, blow.strength)
	}
	c.landedBlowFeedback(blow.feedback, t.ID)
	c.finishMeleeItemEffects(held, blow.context, c.player.Vitals().FallDistance, false)
	c.exhaust(rules.AttackExhaustion)
}

// arrowBulletHit is a shaft that broke a bullet at x, y, z.
type arrowBulletHit struct {
	hitSound registry.Sound
	w        *world.World
	arrow    *arrowEntity
	id       int32
	x, y, z  float64
}
