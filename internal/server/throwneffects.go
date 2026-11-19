package server

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// thrownEffects is what one thrown tick leaves to do once the arrow and mob
// locks are released, one slice per kind, applied by apply.
type thrownEffects struct {
	hitAsks   []arrowHitAsk
	blockHits []arrowBlockHit
	travels   []thrownPortalTravel
	gateways  []gatewayTouch
	track     trackSends
	// tells is each told body's packets, for the watchers tellTo[from:to].
	tells   []thrownTell
	tellTo  []*client
	mobHits []thrownMobHit
	// ownerHurts is each wind charge hit's victim, for the owner's last-hurt entity.
	ownerHurts []ownerHurtMark
	// skullHeals are the owners of the wither skulls that killed a mob.
	skullHeals     []skullHeal
	paintingHits   []paintingHit
	cushionHits    []cushionProjectileHit
	frameHits      []arrowFrameHit
	standHits      []thrownStandHit
	boatHits       []boatProjectileBlow
	crystalHits    []crystalProjectileHit
	redirects      []redirectTell
	windPlayerHits []windPlayerHit
	fireballs      fireballPass
	windBursts     []windBurst
	deflects       []breezeDeflect
	splashes       []projectileSplash
	breaks         []thrownBreak
	targetHits     []targetBlockHit
	projectileHits []projectileBlockHit
	blockLandings  []thrownLanding
	bodyLandings   []projectileLanding
	shots          []projectileShot
	// vanished are the bodies discarded unlanded.
	vanished   []*thrownEntity
	landings   []pearlLanding
	pearlHolds []*thrownEntity
	// bubbles are the column surfaces the bodies were pushed at.
	bubbles []bubbleSurface
}

// takeBubbles moves a step's column surfaces into bubbles and returns its
// other events, whose last is the flight's end.
func (fx *thrownEffects) takeBubbles(w *world.World, events []entity.ThrownEvent) []entity.ThrownEvent {
	return slices.DeleteFunc(events, func(ev entity.ThrownEvent) bool {
		if ev.Kind != entity.ThrownBubbleSurface {
			return false
		}
		fx.bubbles = append(fx.bubbles, bubbleSurface{w, int32(ev.X), int32(ev.Y), int32(ev.Z)})
		return true
	})
}

// thrownTell is one body's packets, built once for all its watchers: a first
// pass's zero-delta position and metadata, then the motion and teleport, in
// thrownTellIDs' order; a nil one is not sent. power is a hurting
// projectile's Projectile Power, bundled with its motion.
type thrownTell struct {
	data     [4][]byte
	power    []byte
	from, to int
}

var thrownTellIDs = [4]int32{protocol.IDPlayEntityPosition, protocol.IDPlayEntityMetadata,
	protocol.IDPlaySetEntityMotion, protocol.IDPlayTeleportEntity}

// apply runs the effects with the locks released, kind by kind, each in the
// order collected: the pairings, each tell packet for every body before the
// next, the hurts (mobs, paintings, cushions, frames, stands, hulls, players) ahead of the
// bodies' ends as onHit orders them, the blocks met, the pearls, and the
// portal crossings last.
func (fx *thrownEffects) apply(s *Server) {
	fx.track.send()
	for i, id := range thrownTellIDs {
		for _, tell := range fx.tells {
			data := tell.data[i]
			if data == nil {
				continue
			}
			for _, c := range fx.tellTo[tell.from:tell.to] {
				if id == protocol.IDPlaySetEntityMotion && tell.power != nil {
					// vanilla: ServerEntity.sendChanges
					c.send(protocol.IDPlayBundleDelimiter, nil)
					c.send(id, data)
					c.send(protocol.IDPlayProjectilePower, tell.power)
					c.send(protocol.IDPlayBundleDelimiter, nil)
					continue
				}
				c.send(id, data)
			}
		}
	}
	applyEach(s, fx.bubbles, (*Server).sendBubbleSurface)
	applyEach(s, fx.splashes, (*Server).splashProjectile)
	applyEach(s, fx.deflects, (*Server).playBreezeDeflect)
	applyEach(s, fx.ownerHurts, (*Server).noteOwnerHurt)
	applyEach(s, fx.mobHits, (*Server).tellThrownMobHit)
	applyEach(s, fx.redirects, (*Server).tellRedirect)
	applyEach(s, fx.skullHeals, (*Server).healSkullOwner)
	applyEach(s, fx.paintingHits, func(s *Server, hit paintingHit) { s.hitPainting(hit) })
	applyEach(s, fx.cushionHits, func(s *Server, hit cushionProjectileHit) { s.projectileHitCushion(hit) })
	applyEach(s, fx.frameHits, func(s *Server, hit arrowFrameHit) { s.arrowHitFrame(hit) })
	applyEach(s, fx.standHits, (*Server).thrownHitArmorStand)
	applyEach(s, fx.crystalHits, func(s *Server, hit crystalProjectileHit) { _, _ = s.projectileHitEndCrystal(hit) })
	applyEach(s, fx.boatHits, func(s *Server, hit boatProjectileBlow) { s.boatProjectileHit(hit) })
	applyEach(s, fx.windPlayerHits, (*Server).windHitPlayer)
	// Out of the locks the engines' own must not be taken under. Block hooks
	// run before a fireball's blast: vanilla Projectile.onHit precedes explode.
	s.hitTargetBlocks(fx.targetHits)
	s.hitProjectileBlocks(fx.projectileHits)
	s.finishFireballs(&fx.fireballs)
	applyEach(s, fx.windBursts, (*Server).burstWindCharge)
	applyEach(s, fx.breaks, (*Server).breakThrown)
	applyEach(s, fx.blockLandings, (*Server).thrownLandEvent)
	applyEach(s, fx.shots, (*Server).projectileShootEvent)
	applyEach(s, fx.bodyLandings, (*Server).projectileLandEvent)
	applyEach(s, fx.vanished, (*Server).removeVanishedThrown)
	applyEach(s, fx.landings, (*Server).landPearl)
	applyEach(s, fx.pearlHolds, (*Server).followPearlHold)
	s.touchGateways(fx.gateways)
	// A crossing with nowhere to arrive puts the body back, cooldown spent.
	applyEach(s, fx.travels, func(s *Server, tr thrownPortalTravel) { s.finishThrownTravel(tr.it, tr.kind) })
}

// vanish discards a body unlanded and reports that it flies no further.
func (fx *thrownEffects) vanish(it *thrownEntity) bool {
	it.throwable.Dead = true
	fx.vanished = append(fx.vanished, it)
	return false
}

// thrownStandHit is a thrown body's blow on a stand; heal is a wither
// skull's living owner, healed by the break.
type thrownStandHit struct {
	arrowStandHit
	heal skullHeal
}

// thrownHitArmorStand breaks the stand and, when it broke, heals a wither
// skull's owner as for any kill.
// vanilla: WitherSkull.onHitEntity
func (s *Server) thrownHitArmorStand(hit thrownStandHit) {
	if landed, _ := s.arrowHitArmorStand(hit.arrowStandHit); landed {
		s.healSkullOwner(hit.heal)
	}
}

// skullHeal is a wither skull's living owner, healed when the skull's blow
// kills: mob is a mob's entity ID in w, player a player; zero heals no one.
type skullHeal struct {
	w      *world.World
	mob    int32
	player *client
}

// skullHealOf is the owner of the skull it, when it is living.
func skullHealOf(tw *thrownWorld, it *thrownEntity) skullHeal {
	h := skullHeal{w: it.w, player: tw.bodies.clients[it.throwable.OwnerID]}
	if tw.bodies.mobs[it.throwable.OwnerID] != nil {
		h.mob = it.throwable.OwnerID
	}
	return h
}

// healSkullOwner gives the owner its kill's health, with no tick lock held.
// vanilla: WitherSkull.onHitEntity
func (s *Server) healSkullOwner(h skullHeal) {
	if h.mob != 0 {
		s.mobsMu.Lock()
		if ent := s.mobByEntityIDLocked(h.w, h.mob); ent != nil {
			ent.mob.Heal(entity.WitherSkullKillHeal)
		}
		s.mobsMu.Unlock()
	}
	if h.player != nil {
		h.player.changeEffects(func(v *player.Vitals, e *effect.Set, a *player.Attributes) []effect.Change {
			rules.Heal(v, e, a, entity.WitherSkullKillHeal)
			return nil
		})
	}
}

// tellThrownMobHit tells the mob's watchers of a mob hurt taken under the mob
// lock; a blow that landed only its excess is told by its health alone.
func (s *Server) tellThrownMobHit(hit thrownMobHit) {
	if !hit.softened {
		s.tellWatchers(hit.w, hit.watchers, []outbound{
			{id: protocol.IDPlayDamageEvent, data: damageEventPayload(hit.victimID, hit.damageType, hit.causeID, hit.directID)},
			{id: protocol.IDPlaySetEntityMotion, data: entityVelocityPayload(hit.victimID, hit.vx, hit.vy, hit.vz)},
		})
		s.broadcastVoice(hit.w, hit.kind, hit.silent, hit.x, hit.y, hit.z, hit.sound, hit.volume, hit.pitch)
	}
	if hit.health != nil {
		s.tellWatchers(hit.w, hit.watchers, []outbound{{id: protocol.IDPlayEntityMetadata, data: hit.health}})
	}
}

// windHitPlayer runs a wind charge's direct blow on a player.
func (s *Server) windHitPlayer(hit windPlayerHit) {
	w := hit.victim.theWorld()
	damage := hit.damage
	if hit.ownerMob {
		damage = rules.ScaledWithDifficulty(damage, w.Difficulty())
	}
	killer := s.entityKiller(w, hit.ownerID)
	if landed, softened := hit.victim.hurt(damage, rules.DamageTypeWindCharge, &damageCause{
		entityID: hit.ownerID, directID: hit.id + 1, x: hit.x, y: hit.y, z: hit.z,
		killer: killer, directName: s.directNameOf(killer, hit.kind.EntityType(), hit.uuid, hit.custom),
		flying: true, flightVX: hit.vx, flightVZ: hit.vz}); landed {
		hit.victim.arrowKnockback(w, hit.vx, hit.vz, 0, !softened, syncVelocityAfterTravel)
		s.thornsOnOwner(w, hit.victim, hit.ownerID, rules.DamageTypeWindCharge, -1)
	}
}

// burstWindCharge removes a wind charge and runs its burst, with no entity
// event: the client draws the burst from the Explosion packet.
func (s *Server) burstWindCharge(b windBurst) {
	if b.radius == 0 {
		b.radius = windChargeRadius
	}
	knockback, directType, typeID := windChargeKnockback, "minecraft:wind_charge", int32(protocol.EntityTypeWindCharge)
	if b.breeze {
		knockback, directType, typeID = 1, "minecraft:breeze_wind_charge", protocol.EntityTypeBreezeWindCharge
	}
	killer := s.entityKiller(b.w, b.ownerID)
	s.broadcastWorld(b.w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{b.id}))
	s.runExplosion(blast{w: b.w, x: b.x, y: b.y, z: b.z, radius: b.radius,
		causeID: b.ownerID, causeKiller: killer, causeUUID: b.ownerUUID, directID: b.id,
		directType: directType, directName: s.directNameOf(killer, typeID, b.uuid, b.custom),
		// vanilla: Level.ExplosionInteraction.TRIGGER
		interaction: rules.ExplosionTrigger, damageType: rules.DamageTypePlayerExplosion, rng: b.rng,
		breezeBurst: b.breeze, harmless: true, knockbackScale: knockback, burst: true, blockResist: windChargeResistanceOf(b.w)})
}

// breakThrown tells one body's break, its removal after the break's own
// telling and before an egg's chicks.
func (s *Server) breakThrown(br thrownBreak) {
	switch {
	case br.kind == entity.ThrownExperienceBottle:
		// vanilla: ThrownExperienceBottle.onHit
		s.broadcastLevelEvent(br.w, protocol.WorldEventPotionSplash,
			rules.BlockOf(br.x), rules.BlockOf(br.y), rules.BlockOf(br.z), thrownBottleColour)
		s.shatterSound(br, protocol.WorldEventPotionBreakSound)
		s.spawnOrbsDirected(br.w, br.x, br.y, br.z, br.dirX, br.dirY, br.dirZ, s.fireExpBottle(br))
	case br.kind.IsPotion():
		if br.potion != nil {
			s.shatterPotion(br)
		}
	default:
		s.broadcastWorld(br.w, protocol.IDPlayEntityEvent, entityEventPayload(br.id, protocol.EntityStatusDeath))
	}
	s.broadcastWorld(br.w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{br.id}))
	chicks := br.chicks
	if br.kind == entity.ThrownEgg {
		chicks = s.fireEggThrow(br)
	}
	if chicks == 0 {
		return
	}
	// vanilla: ThrownEgg.onHit (snapTo with the egg's yaw, pitch 0; the head keeps its own)
	dim := s.dimensionOfWorld(br.w)
	for range chicks {
		s.spawnMobArriving(dim, entity.TypeChicken, br.x, br.y, br.z, 0, nil, true, br.variant, false, func(e *mobEntity) {
			e.mob.Yaw, e.mob.Pitch = br.yaw, 0
		})
	}
}

// shatterPotion tells a potion's shatter in onHit's order: what the spray
// left in the bodies, a lingering potion's cloud, then the world event the
// client plays the shatter from. vanilla: AbstractThrownPotion.onHit
func (s *Server) shatterPotion(br thrownBreak) {
	shl := br.potion
	applyEach(s, shl.mannequins, (*Server).landMannequinEffects)
	for _, ent := range shl.waterMannequins {
		s.douseMannequin(br.w, ent)
	}
	for _, tell := range shl.mobs {
		s.tellSplashMob(br.w, tell)
	}
	for _, tell := range shl.players {
		s.landPlayerTell(tell)
	}
	for _, hit := range shl.instantHurts {
		s.tellThrownMobHit(hit)
	}
	for _, hit := range shl.waterHurts {
		s.tellThrownMobHit(hit)
	}
	for _, douse := range shl.waterMobs {
		s.broadcastSoundPacketExcept(douse.w, nil, s.extinguishSound(mobSoundCategory(douse.kind), douse.x, douse.y, douse.z))
	}
	for _, c := range shl.waterPlayers {
		c.extinguishFire()
	}
	if shl.water && br.onBlock {
		s.dowseWaterFire(br.w, br.blockHit.X, br.blockHit.Y, br.blockHit.Z, br.blockHit.Face)
	}
	if shl.cloud != nil {
		s.spawnCloud(br.w, shl.cloudX, shl.cloudY, shl.cloudZ, shl.cloud, shl.colour, shl.contents, shl.ownerUUID)
	}
	event, sound := int32(protocol.WorldEventPotionSplash), int32(protocol.WorldEventPotionBreakSound)
	if shl.instant {
		event, sound = protocol.WorldEventPotionSplashInstant, protocol.WorldEventInstantPotionBreakSound
	}
	s.broadcastLevelEvent(br.w, event, rules.BlockOf(br.x), rules.BlockOf(br.y), rules.BlockOf(br.z), shl.colour)
	s.shatterSound(br, sound)
}

// shatterSound tells the glass break that follows a shatter's particle
// event, unless the body is silent.
func (s *Server) shatterSound(br thrownBreak, event int32) {
	if !br.silent {
		s.broadcastLevelEvent(br.w, event, rules.BlockOf(br.x), rules.BlockOf(br.y), rules.BlockOf(br.z), 0)
	}
}

// landPlayerTell gives a player what a splash or cloud left for them, in
// potion order: timed effects added, instant ones applied in place (a heal, a
// saturation, or a harm as indirect magic with the potion or cloud as the
// direct source and its owner, if any, as the cause).
// vanilla: HealOrHarmMobEffect.applyInstantaneousEffect, DamageSources.indirectMagic
func (s *Server) landPlayerTell(tell splashPlayerTell) {
	w := tell.c.theWorld()
	for _, in := range tell.effects {
		if !registry.MobEffectOf(in.ID).Instant {
			tell.c.effectCause = effect.CauseSplashPotion
			tell.c.AddEffect(in)
			continue
		}
		saturated := false
		tell.c.player.UpdateBody(func(v *player.Vitals, _ *effect.Set, _ *player.Attributes) {
			saturated = rules.ApplyInstantSaturation(v, in)
		})
		if saturated {
			continue
		}
		amount, hurt := rules.InstantEffect(protocol.EntityTypePlayer, in.ID, in.Amplifier, tell.scale)
		if !hurt {
			tell.c.changeEffects(func(v *player.Vitals, e *effect.Set, a *player.Attributes) []effect.Change {
				rules.Heal(v, e, a, amount)
				return nil
			})
			continue
		}
		src := tell.src
		cause := &damageCause{entityID: src.ownerID, directID: src.bodyID + 1, killer: s.entityKiller(w, src.ownerID)}
		if src.ownerID == 0 {
			// No causing entity: the event names the body alone, and the death
			// message names it.
			cause = &damageCause{posOnly: true, directID: src.bodyID + 1,
				directName: &protocol.Text{Translate: src.bodyName}}
		}
		cause.x, cause.z = src.bodyX, src.bodyZ
		if src.ownerMob {
			amount = rules.ScaledWithDifficulty(amount, w.Difficulty())
		}
		// A whole harm shoves the player away from the potion or cloud.
		// vanilla: LivingEntity.dealDefaultKnockback,
		// AbstractThrownPotion.calculateHorizontalHurtKnockbackDirection
		if landed, softened := tell.c.hurt(amount, rules.DamageTypeIndirectMagic, cause); landed && !softened {
			tell.c.knockbackFromPoint(w, src.bodyX, tell.c.player.Position().Y, src.bodyZ, syncVelocityAfterTravel)
		}
	}
}

// thrownLandEvent emits a landing block's projectile_land event.
// vanilla: Projectile.hitTargetOrDeflectSelf
func (s *Server) thrownLandEvent(l thrownLanding) {
	s.projectileLandEvent(blockProjectileLanding(l.w, l.hit, l.ownerUUID))
}

// shotMark is a projectile body's HasBeenShot: its first tick posts the
// shoot event, once per body and never again after a reload.
type shotMark struct{ shot bool }

// queue adds the shoot event at the body's position to shots on the body's
// first tick, and marks the body shot.
func (m *shotMark) queue(shots *[]projectileShot, w *world.World, x, y, z float64, owner uuid.UUID) {
	if !m.shot {
		*shots = append(*shots, projectileShot{w, x, y, z, owner})
	}
	m.shot = true
}

// projectileShot is a first-tick shoot event, posted after the locks.
type projectileShot struct {
	w         *world.World
	x, y, z   float64
	ownerUUID uuid.UUID
}

// projectileShootEvent posts the shot with its owner, a player or a mob, as
// the source. vanilla: Projectile.tick
func (s *Server) projectileShootEvent(p projectileShot) {
	var ctx rules.GameEventContext
	if c := s.clientByUUID(p.ownerUUID); c != nil {
		ctx = c.gameEventContext(0)
	} else if p.ownerUUID != uuid.Nil {
		ctx.Mob = p.ownerUUID
	}
	s.gameEvent(p.w, rules.GameEventProjectileShoot, p.x, p.y, p.z, ctx)
}

// projectileLanding is a hit point, or a block centre with its affected state.
type projectileLanding struct {
	w         *world.World
	x, y, z   float64
	state     uint16
	ownerUUID uuid.UUID
}

func blockProjectileLanding(w *world.World, hit entity.BlockHit, owner uuid.UUID) projectileLanding {
	return projectileLanding{w, float64(hit.X) + 0.5, float64(hit.Y) + 0.5, float64(hit.Z) + 0.5, hit.State, owner}
}

// projectileLandEvent posts a hit after the body locks have been released.
// vanilla: Projectile.onHit
func (s *Server) projectileLandEvent(l projectileLanding) {
	s.gameEvent(l.w, rules.GameEventProjectileLand, l.x, l.y, l.z,
		rules.GameEventContext{Affected: l.state, Player: l.ownerUUID, ProjectileOwner: l.ownerUUID})
}

// removeVanishedThrown takes a discarded body out of every view.
func (s *Server) removeVanishedThrown(it *thrownEntity) {
	s.broadcastWorld(it.w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{it.throwable.ID}))
}
