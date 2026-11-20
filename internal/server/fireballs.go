package server

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// fireballPass gathers what the fireballs of one thrown tick do after the
// tick's locks are released.
type fireballPass struct {
	landings   []fireballLanding
	playerHits []fireballPlayerHit
	effects    []fireballEffectTell
	bulletHits []bulletPlayerHit
	spitHits   []bulletPlayerHit
	// strikes are bullets a thrown body hit, gone at once.
	strikes []bulletStrike
}

// bulletStrike is a shulker bullet broken where it flew.
type bulletStrike struct {
	w       *world.World
	id      int32
	x, y, z float64
}

// fireballLanding is a fireball's flight ended, on a block or a body.
type fireballLanding struct {
	// tags are the projectile's scoreboard tags, read under its lock.
	tags                   []string
	cloudX, cloudY, cloudZ float64
	silent                 bool
	w                      *world.World
	id                     int32
	uuid                   uuid.UUID
	custom                 customName
	kind                   entity.ThrownKind
	ownerID                int32
	ownerUUID              uuid.UUID
	ownerMob               bool
	// ownerPlayer marks a fireball a player owns: one they turned back.
	ownerPlayer bool
	power       int
	// dangerous is a charged wither skull's flag.
	dangerous bool
	rng       *rand.Rand
	x, y, z   float64
	block     entity.BlockHit
	onBlock   bool
}

// fireballPlayerHit is a fireball striking a player.
type fireballPlayerHit struct {
	victim      *client
	kind        entity.ThrownKind
	ownerID     int32
	ownerLiving bool
	ownerMob    bool
	id          int32
	uuid        uuid.UUID
	custom      customName
	x, y, z     float64
	vx, vz      float64
	// heal is the owner a wither skull heals when the blow kills.
	heal skullHeal
}

// fireballEffectTell is the effect a skull or bullet left on a mob, told to
// the players tracking and riding it.
type fireballEffectTell struct {
	w *world.World
	splashMobTell
}

// fireballHitMob deals a fireball's blow to a mob under the mob lock: the
// burn of a small fireball (put back if the hurt is refused) and the Wither
// of a skull.
func (s *Server) fireballHitMob(pass *fireballPass, dim *dimension, it *thrownEntity, m *entity.Mob,
	shooterPlayer, shooterCreative, ownerLiving bool, from ...*entity.HitSource) (landed, softened bool, damageType registry.DamageType) {
	t := it.throwable
	damage, damageType := entity.FireballDamage(t.Kind, t.OwnerID != 0, ownerLiving)
	if t.Kind == entity.ThrownLargeFireball && shooterPlayer && m.ReflectedFireballKills() {
		landed, softened = m.HurtByReflectedFireball(t.VX, t.VZ, t.OwnerID, shooterCreative)
		if landed {
			m.NoteBlowDirect("minecraft:fireball")
		}
		return landed, softened, damageType
	}
	prior := m.FireTicks
	if t.Kind == entity.ThrownSmallFireball {
		m.IgniteForTicks(entity.SmallFireballBurnTicks)
	}
	landed, softened = m.HurtByProjectile(damage, damageType, t.VX, t.VZ, t.OwnerID, shooterPlayer, shooterCreative, from...)
	if !landed {
		m.SetMetadataFireTicks(prior)
		return landed, softened, damageType
	}
	if t.Kind != entity.ThrownWitherSkull {
		return landed, softened, damageType
	}
	seconds := entity.WitherSkullEffectSeconds(it.w.Difficulty())
	if seconds == 0 {
		return landed, softened, damageType
	}
	in := effect.New(registry.MobEffectWither, 20*seconds, 1, false, true, true)
	if ch, ok := m.AddEffect(in); ok {
		if ent := dim.mobs.book[m]; ent != nil {
			pass.effects = append(pass.effects, fireballEffectTell{it.w, mobEffectTellOf(ent, []effect.Change{ch})})
		}
	}
	return landed, softened, damageType
}

// finishFireballs runs what the tick's fireballs did to players, blocks and
// the air around them, with no tick lock held.
func (s *Server) finishFireballs(pass *fireballPass) {
	for _, hit := range pass.playerHits {
		w := hit.victim.theWorld()
		damage, damageType := entity.FireballDamage(hit.kind, hit.ownerID != 0, hit.ownerLiving)
		// All three damage types scale only when a living non-player caused them.
		if hit.ownerMob {
			damage = rules.ScaledWithDifficulty(damage, w.Difficulty())
		}
		prior := hit.victim.player.Vitals().FireTicks
		if hit.kind == entity.ThrownSmallFireball {
			hit.victim.igniteForTicks(entity.SmallFireballBurnTicks)
		}
		killer := s.entityKiller(w, hit.ownerID)
		landed, softened := hit.victim.hurt(damage, damageType, &damageCause{
			entityID: hit.ownerID, directID: hit.id + 1, x: hit.x, y: hit.y, z: hit.z,
			killer: killer, directName: s.directNameOf(killer, hit.kind.EntityType(), hit.uuid, hit.custom),
			flying: true, flightVX: hit.vx, flightVZ: hit.vz})
		// vanilla: LargeFireball.onHitEntity runs the walk whether or not the hurt landed
		if hit.kind == entity.ThrownLargeFireball {
			s.thornsOnOwner(w, hit.victim, hit.ownerID, damageType, -1)
		}
		if !landed {
			hit.victim.player.UpdateVitals(func(v *player.Vitals) { v.FireTicks = prior })
			continue
		}
		hit.victim.arrowKnockback(w, hit.vx, hit.vz, 0, !softened, syncVelocityAfterTravel)
		if hit.kind == entity.ThrownWitherSkull {
			if seconds := entity.WitherSkullEffectSeconds(w.Difficulty()); seconds > 0 {
				hit.victim.AddEffect(effect.New(registry.MobEffectWither, 20*seconds, 1, false, true, true))
			}
			switch {
			case hit.victim.player.Vitals().Dead:
				s.healSkullOwner(hit.heal)
			case hit.ownerLiving:
				s.thornsOnOwner(w, hit.victim, hit.ownerID, damageType, -1)
			}
		}
		if hit.kind == entity.ThrownSmallFireball {
			s.thornsOnOwner(w, hit.victim, hit.ownerID, damageType, -1)
		}
	}
	for _, hit := range pass.strikes {
		s.bulletStruck(hit.w, hit.id, hit.x, hit.y, hit.z)
	}
	for _, hit := range pass.bulletHits {
		s.bulletHitPlayer(hit)
	}
	for _, hit := range pass.spitHits {
		s.spitHitPlayer(hit)
	}
	for _, tell := range pass.effects {
		s.tellSplashMob(tell.w, tell.splashMobTell)
	}
	for _, land := range pass.landings {
		s.broadcastWorld(land.w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{land.id}))
		switch land.kind {
		case entity.ThrownDragonFireball:
			s.spawnDragonCloud(land.w, land.cloudX, land.cloudY, land.cloudZ, land.ownerUUID, false)
			data := int32(1)
			if land.silent {
				data = -1
			}
			s.broadcastLevelEvent(land.w, 2006, int32(math.Floor(land.x)), int32(math.Floor(land.y)), int32(math.Floor(land.z)), data)
		case entity.ThrownShulkerBullet:
			s.bulletLand(land)
		case entity.ThrownLlamaSpit:
			// vanilla: LlamaSpit.onHitBlock discards the spit, nothing more

		case entity.ThrownSmallFireball:
			s.fireballLight(land)
		default:
			s.fireballExplode(land)
		}
	}
}

// fireballLight sets fire on the air in front of the block a small fireball
// struck; a mob's fireball does so only while mob_griefing allows it.
// vanilla: SmallFireball.onHitBlock
func (s *Server) fireballLight(land fireballLanding) {
	if !land.onBlock || (land.ownerMob && !land.w.BoolRule(world.RuleMobGriefing)) {
		return
	}
	dx, dy, dz := faceStep(land.block.Face)
	x, y, z := land.block.X+dx, land.block.Y+dy, land.block.Z+dz
	if state := land.w.BlockAt(x, y, z); !world.IsKnownAirState(state) {
		return
	}
	s.setBlockAndTell(land.w, x, y, z, rules.FireStateForPlacement(land.w, x, y, z))
}

// fireballExplode is a large fireball's or a wither skull's blast. The
// skull's power is fixed at 1 and sets no fires; the fireball's fires follow
// mob_griefing.
// vanilla: LargeFireball.onHit, WitherSkull.onHit
func (s *Server) fireballExplode(land fireballLanding) {
	radius, fire := 1.0, false
	if land.kind == entity.ThrownLargeFireball {
		radius, fire = float64(land.power), land.w.BoolRule(world.RuleMobGriefing)
	}
	directType, directTypeID := "minecraft:fireball", int32(protocol.EntityTypeFireball)
	if land.kind == entity.ThrownWitherSkull {
		directType, directTypeID = "minecraft:wither_skull", protocol.EntityTypeWitherSkull
	}
	killer := s.entityKiller(land.w, land.ownerID)
	// vanilla: DamageSources.explosion picks player_explosion when both entities exist
	damageType := rules.DamageTypeExplosion
	if killer != nil {
		damageType = rules.DamageTypePlayerExplosion
	}
	sourceTeam, _ := s.scoreboard.PlayerTeam(land.uuid.String())
	b := blast{
		w: land.w, x: land.x, y: land.y, z: land.z,
		radius:      math.Max(radius, 0),
		causeID:     land.ownerID,
		causeKiller: killer,
		causeUUID:   land.ownerUUID,
		directID:    land.id,
		directUUID:  land.uuid,
		directName:  s.directNameOf(killer, directTypeID, land.uuid, land.custom),
		silent:      land.silent,
		directType:  directType,
		directLoot:  &rules.LootEntity{Type: directType, Tags: land.tags},
		damageType:  damageType,
		rng:         land.rng,
		// vanilla: Level.ExplosionInteraction.MOB
		interaction: rules.MobExplosionInteraction(s.world),
		fire:        fire,
		// vanilla: Ghast.isReflectedFireball
		reflectedFireball: land.kind == entity.ThrownLargeFireball && land.ownerPlayer,
		sourceTeam:        sourceTeam,
	}
	if land.kind == entity.ThrownWitherSkull && land.dangerous {
		b.blockResist = dangerousSkullResistanceOf(land.w)
	}
	s.runExplosion(b)
}

// dangerousSkullResistanceOf caps a cell's blast resistance at 0.8 unless
// the block is wither-immune, so a charged skull breaks what the Wither can.
// vanilla: WitherSkull.getBlockExplosionResistance, WitherBoss.canDestroy
func dangerousSkullResistanceOf(w *world.World) func(bx, by, bz int32) (float64, bool) {
	return func(bx, by, bz int32) (float64, bool) {
		state := w.BlockAt(bx, by, bz)
		resistance, present := rules.BlastResistance(state)
		if !present || registry.BlockOf(state).InTag(registry.TagIDWitherImmune) {
			return 0, false
		}
		if resistance <= 0.8 {
			return 0, false
		}
		return 0.8, true
	}
}

// spawnFireball puts a hurting projectile of the kind given into the world,
// flying along the direction given at its acceleration power.
// vanilla: AbstractHurtingProjectile.assignDirectionalMovement
func (s *Server) spawnFireball(kind entity.ThrownKind, w *world.World, x, y, z, dx, dy, dz float64,
	ownerID int32, ownerUUID uuid.UUID, explosionPower int, seed int64, prep ...func(*entity.Thrown)) int32 {
	item := "minecraft:fire_charge"
	if kind == entity.ThrownWitherSkull {
		item = "minecraft:wither_skeleton_skull"
	}
	id, ok := registry.ItemID(item)
	if !ok {
		return 0
	}
	length := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if length > 0 {
		scale := entity.FireballAcceleration / length
		dx, dy, dz = dx*scale, dy*scale, dz*scale
	}
	t := entity.NewThrown(s.nextEntity(), kind, x, y, z, dx, dy, dz, ownerID, seed)
	t.ExplosionPower = explosionPower
	for _, p := range prep {
		p(t)
	}
	it := thrownShell(t, w, ownerUUID, protocol.ItemStack{ItemID: id, Count: 1})
	if it == nil {
		return 0
	}
	s.launchThrown(it)
	return t.ID
}

// attackFireball is a player's blow on a thrown body: a redirectable one in
// reach turns along the player's look and becomes theirs, and the blow
// deals no damage. A fresh wind charge refuses the turn, and the blow's
// hurt, which it refuses too, plays the same sound. It reports whether the
// target was a thrown body at all.
// vanilla: Player.attack, Player.deflectProjectile
func (c *client) attackFireball(target int32) bool {
	s := c.srv
	pos := c.player.Position()
	lx, ly, lz := entity.LookVector(pos.Yaw, pos.Pitch)

	s.arrowsMu.Lock()
	var it *thrownEntity
	for _, cand := range s.thrown {
		if cand.throwable.ID == target && cand.w == c.theWorld() && !cand.throwable.Dead {
			it = cand
			break
		}
	}
	if it == nil {
		s.arrowsMu.Unlock()
		return false
	}
	t := it.throwable
	if t.Kind == entity.ThrownShulkerBullet {
		s.arrowsMu.Unlock()
		c.attackBullet(it)
		return true
	}
	if !t.Redirectable() || !c.withinAttackRange(t.BlastBox()) {
		s.arrowsMu.Unlock()
		return true
	}
	c.attackStrengthTicks.Store(0)
	tell := redirectLocked(it, lx, ly, lz, true, c.player.EntityID, c.player.UUID)
	s.arrowsMu.Unlock()

	s.tellRedirect(tell)
	c.attackSound(soundAttackNoDamage)
	return true
}

// redirectTell is the motion a redirection sends the body's watchers; w is
// nil when the body kept its motion.
type redirectTell struct {
	w        *world.World
	watchers []uuid.UUID
	packets  []outbound
}

// redirectLocked deflects a redirectable body along the look (aimed false:
// the deflector is gone, the motion stays) and hands it to the owner. The
// arrow lock is held.
// vanilla: Projectile.deflect, ProjectileDeflection.AIM_DEFLECT
func redirectLocked(it *thrownEntity, lx, ly, lz float64, aimed bool, ownerID int32, ownerUUID uuid.UUID) redirectTell {
	t := it.throwable
	if !t.Deflect(lx, ly, lz, aimed, ownerID) {
		return redirectTell{}
	}
	it.ownerUUID = ownerUUID
	if !aimed {
		return redirectTell{}
	}
	return redirectTell{w: it.w, watchers: trackedWatchers(it.tracked),
		packets: hurtingMotion(t)}
}

// tellRedirect sends a redirection's motion with no lock held.
func (s *Server) tellRedirect(tell redirectTell) {
	if tell.w != nil {
		s.tellWatchers(tell.w, tell.watchers, tell.packets)
	}
}

// redirectByHitLocked is a projectile's hit on a redirectable body: it turns
// along the look of the hitter's owner, when that body is here, and takes
// the hitter's owner. The arrow and mob locks are held.
// vanilla: Projectile.onRedirectProjectile
func (s *Server) redirectByHitLocked(w *world.World, id int32, bodies *arrowBodies, ownerID int32, ownerUUID uuid.UUID) redirectTell {
	it := s.redirectableByIDLocked(w, id)
	if it == nil {
		return redirectTell{}
	}
	var lx, ly, lz float64
	aimed := true
	if c := bodies.clients[ownerID]; c != nil {
		pos := c.player.Position()
		lx, ly, lz = entity.LookVector(pos.Yaw, pos.Pitch)
	} else if m := bodies.mobs[ownerID]; m != nil {
		lx, ly, lz = entity.LookVector(m.Yaw, m.Pitch)
	} else {
		aimed = false
	}
	return redirectLocked(it, lx, ly, lz, aimed, ownerID, ownerUUID)
}

// redirectableByIDLocked finds the live redirectable body with the ID in
// the world. The arrow lock is held.
func (s *Server) redirectableByIDLocked(w *world.World, id int32) *thrownEntity {
	for _, it := range s.thrown {
		if it.throwable.ID == id && it.w == w && !it.throwable.Dead && it.throwable.Redirectable() {
			return it
		}
	}
	return nil
}
