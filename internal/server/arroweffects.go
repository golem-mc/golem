package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// arrowEffects is what one tick of the arrows leaves to do once the arrow
// and mob locks are released: each kind is collected under the locks, arrow
// by arrow and event by event, and applied after them by apply.
type arrowEffects struct {
	hitAsks     []arrowHitAsk
	blockHits   []arrowBlockHit
	channelHits []tridentChannelHit
	travels     []arrowPortalTravel
	gateways    []gatewayTouch
	track       trackSends
	metas       sendList
	sounds      []pendingSound
	splashes    []projectileSplash
	deflects    []breezeDeflect
	landings    []projectileLanding
	shots       []projectileShot
	pickups     []arrowPickup
	playerHits  []arrowPlayerHit
	mobHits     []arrowMobHit
	// ownerHurts is each hit's victim, for the shooting player's last-hurt entity.
	ownerHurts    []ownerHurtMark
	frameHits     []arrowFrameHit
	standHits     []arrowStandHit
	mannequinHits []arrowMannequinHit
	crystalHits   []arrowCrystalHit
	bulletHits    []arrowBulletHit
	redirects     []redirectTell
	knotHits      []arrowKnotHit
	paintingHits  []paintingHit
	cushionHits   []cushionProjectileHit
	boatHits      []boatProjectileBlow
	// targetHits is the target blocks the flights met, for the wake phase.
	targetHits []targetBlockHit
	// cauldronDouses is the layered cauldrons burning arrows were put out
	// in.
	cauldronDouses []arrowCauldron
	// flakes are the points the flights through powder snow may kick up a
	// snowflake at.
	flakes []snowflakeSpot
	// bubbles are the column surfaces the flights were pushed at.
	bubbles []bubbleSurface
	// snowMelts is the powder snow burning arrows were put out in.
	snowMelts []projectileBlockHit
	// projectileHits is the TNT, campfires and candles the flights met, for
	// the burning-projectile phase.
	projectileHits []projectileBlockHit
	// refused is the plain shafts whose hurt after the locks was refused,
	// bounced together in the refusal pass.
	refused []*arrowEntity
	// tridentBounces are the tridents a hit left standing: the hurt they
	// dealt is settled and only the deflection remains. The ones the step
	// queued come first, then the ones the hurts after the locks queue.
	tridentBounces []*arrowEntity
	// orphanDrops are the loyalty tridents whose owner cannot take the
	// return — dead, or spectating — whose stacks are laid where the bodies
	// stood.
	orphanDrops []*arrowEntity
}

// apply runs the effects with the arrow and mob locks released, kind by
// kind, each kind in the order it was collected: the order the packets and
// world changes reach the clients in. The hurts run after the locks go in
// one fixed order — frames, stands, knots, hulls, paintings, cushions,
// mannequins, players — each
// settling its shaft as it answers: spent at once, or queued to refused or
// to tridentBounces. Only then does the refusal pass bounce refused, its
// seam's window seeing every refusal of the tick, and only after it do the
// tridents deflect, the ones the step queued first. removals is the arrows
// the tick ended, per world.
func (fx *arrowEffects) apply(s *Server, removals map[*world.World][]int32) {
	fx.track.send()
	fx.metas.send()
	applyEach(s, fx.sounds, (*Server).playPending)
	applyEach(s, fx.splashes, (*Server).splashProjectile)
	applyEach(s, fx.deflects, (*Server).playBreezeDeflect)
	// The target blocks, the cauldrons and the TNT, campfires and candles
	// the flights met, out of the locks the engine's own must not be taken
	// under.
	s.hitTargetBlocks(fx.targetHits)
	applyEach(s, fx.cauldronDouses, (*Server).douseCauldron)
	s.rollSnowflakes(fx.flakes)
	applyEach(s, fx.bubbles, (*Server).sendBubbleSurface)
	applyEach(s, fx.snowMelts, (*Server).arrowMeltedSnow)
	s.hitProjectileBlocks(fx.projectileHits)
	// vanilla sets the owner's last-hurt entity before the hurt.
	applyEach(s, fx.ownerHurts, (*Server).noteOwnerHurt)
	applyEach(s, fx.mobHits, (*Server).tellArrowMobHit)
	tellArrowRemovals(s, removals)
	applyEach(s, fx.frameHits, func(s *Server, hit arrowFrameHit) {
		fx.settle(s, hit.w, hit.arrow, hit.hitSound, s.arrowFrameOutcome(hit))
	})
	applyEach(s, fx.bulletHits, func(s *Server, hit arrowBulletHit) {
		s.bulletStruck(hit.w, hit.id, hit.x, hit.y, hit.z)
		fx.settle(s, hit.w, hit.arrow, hit.hitSound, landedOutcome(hit.arrow))
	})
	applyEach(s, fx.redirects, (*Server).tellRedirect)
	applyEach(s, fx.standHits, func(s *Server, hit arrowStandHit) {
		fx.settle(s, hit.w, hit.arrow, hit.hitSound, s.arrowStandOutcome(hit))
	})
	applyEach(s, fx.crystalHits, func(s *Server, hit arrowCrystalHit) {
		fx.settle(s, hit.w, hit.arrow, hit.hitSound, s.arrowCrystalOutcome(hit))
	})
	applyEach(s, fx.knotHits, func(s *Server, hit arrowKnotHit) {
		fx.settle(s, hit.w, hit.arrow, hit.hitSound, s.arrowKnotOutcome(hit))
	})
	applyEach(s, fx.boatHits, func(s *Server, hit boatProjectileBlow) {
		fx.settle(s, hit.w, hit.arrow, hit.hitSound, s.arrowBoatOutcome(hit))
	})
	applyEach(s, fx.paintingHits, func(s *Server, hit paintingHit) {
		fx.settle(s, hit.w, hit.arrow, hit.hitSound, s.arrowPaintingOutcome(hit))
	})
	applyEach(s, fx.cushionHits, func(s *Server, hit cushionProjectileHit) {
		fx.settle(s, hit.w, hit.arrow, hit.hitSound, s.arrowCushionOutcome(hit))
	})
	applyEach(s, fx.mannequinHits, func(s *Server, hit arrowMannequinHit) {
		fx.settle(s, hit.arrow.w, hit.arrow, hit.event.HitSound, s.arrowMannequinOutcome(hit))
	})
	applyEach(s, fx.playerHits, func(s *Server, hit arrowPlayerHit) {
		fx.settle(s, hit.w, hit.arrow, hit.hitSound, s.arrowPlayerOutcome(hit))
	})
	applyEach(s, fx.channelHits, (*Server).applyTridentChanneling)
	fx.bounceRefused(s)
	applyEach(s, fx.tridentBounces, (*Server).deflectTrident)
	applyEach(s, fx.shots, (*Server).projectileShootEvent)
	applyEach(s, fx.landings, (*Server).projectileLandEvent)
	applyEach(s, fx.orphanDrops, (*Server).dropOrphanedTrident)
	// Pickups run under the collector's lock, collect and removal going
	// out in that order.
	applyEach(s, fx.pickups, (*Server).arrowPickup)
	s.touchGateways(fx.gateways)
	// Each arrow the portal pass caught arrives as a new shaft in the
	// dimension its portal goes to; a crossing with nowhere to arrive
	// puts the arrow back in the list, the cooldown spent.
	applyEach(s, fx.travels, func(s *Server, tr arrowPortalTravel) { s.finishArrowTravel(tr.it, tr.kind) })
}

// douseCauldron empties one layer of the cauldron a burning arrow was put
// out in.
func (s *Server) douseCauldron(c arrowCauldron) {
	s.cauldronDoused(c.w, c.x, c.y, c.z)
}

// arrowMeltedSnow destroys the powder snow a burning arrow was put out in.
// An arrow is no player, so mob_griefing gates even an ownerless one.
// vanilla: PowderSnowBlock.entityInside
func (s *Server) arrowMeltedSnow(hit projectileBlockHit) {
	if hit.w.BoolRule(world.RuleMobGriefing) && s.mayProjectileInteract(hit) {
		s.meltPowderSnow(hit.w, hit.block.X, hit.block.Y, hit.block.Z)
	}
}

// tellArrowMobHit tells of a mob hurt the step already took under the mob
// lock: damage event naming the arrow and shooter, knockback, health left,
// sound. A blow that landed only its excess is told by its health alone,
// the whole hurt's packets staying back. The effect a tip left in the body
// rides the same way out.
func (s *Server) tellArrowMobHit(hit arrowMobHit) {
	if hit.kb != nil {
		hit.vx, hit.vy, hit.vz = s.applyMobKnockback(hit.kb)
	}
	if !hit.softened {
		s.tellWatchers(hit.w, hit.watchers, []outbound{
			{id: protocol.IDPlayDamageEvent, data: damageEventPayload(hit.victimID, hit.damageType, hit.causeID, hit.directID)},
			{id: protocol.IDPlayHurtAnimation, data: hurtAnimationPayload(hit.victimID, hit.yaw)},
			{id: protocol.IDPlaySetEntityMotion, data: entityVelocityPayload(hit.victimID, hit.vx, hit.vy, hit.vz)},
		})
		s.broadcastVoice(hit.w, hit.kind, hit.silent, hit.x, hit.y, hit.z, hit.sound, hit.volume, hit.pitch)
	}
	// A softened blow tells of no hurt, but the squid's ink it squeezed
	// out still stands.
	s.sendInk(hit.w, hit.kind, hit.ink)
	if hit.shaftSound != nil {
		s.playPending(*hit.shaftSound)
	}
	s.tellWatchers(hit.w, hit.watchers, []outbound{{id: protocol.IDPlayEntityMetadata, data: hit.health}})
	for _, t := range hit.effects {
		s.tellSplashMob(hit.w, t)
	}
	if hit.victimID != 0 {
		thrown := rules.LootEntity{Type: hit.direct}
		blow := advancement.Blow{Dealt: hit.dealt, Taken: hit.taken, Type: hit.damageType, Indirect: true,
			Attacker: s.entityLoot(hit.w, hit.causeID), Direct: func() rules.LootEntity { return thrown }}
		victim := advancement.Entity{Type: hit.kind.Name(), Loot: s.mobLootByEntityID(hit.w, hit.victimID), Blow: &blow}
		s.arrowHitAdvancements(s.entityClient(hit.shooter), hit.direct, hit.killed, hit.weapon, &victim)
	}
	if hit.thorns.mob == nil {
		return
	}
	if shooter := s.clientByEntityID(hit.shooter); shooter != nil && shooter.theWorld() == hit.w {
		shooter.thornsFromMob(hit.w, hit.thorns, syncVelocityAfterTravel)
	}
}

// tellArrowRemovals sends each world's players one removal packet for the
// arrows the tick ended.
func tellArrowRemovals(s *Server, removals map[*world.World][]int32) {
	for w, ids := range removals {
		s.broadcastWorld(w, protocol.IDPlayRemoveEntities, removeEntitiesPayload(ids))
	}
}

// arrowOutcome is what a hurt run after the locks leaves of the shaft that
// dealt it.
type arrowOutcome uint8

const (
	// arrowFliesOn is a body gone between the sweep and the hurt: the
	// flight goes on as if it had never met it.
	arrowFliesOn arrowOutcome = iota
	// arrowSpent is a landed hurt: the shaft's hit voice, then its removal.
	arrowSpent
	// arrowRefused is a refused hurt, bounced in the refusal pass.
	arrowRefused
	// arrowDeflects is a trident's hit, landed or refused, deflected after
	// the refusal pass.
	arrowDeflects
	// arrowChannels is a landed trident hit, eligible for post-attack effects.
	arrowChannels
)

// landedOutcome is a landed hurt's outcome: a trident is never spent on a
// hit, its deflection queued instead.
func landedOutcome(it *arrowEntity) arrowOutcome {
	if it.arrow.Trident {
		return arrowChannels
	}
	return arrowSpent
}

// refusedOutcome is a refused hurt's outcome: a trident deflects instead of
// bouncing.
func refusedOutcome(it *arrowEntity) arrowOutcome {
	if it.arrow.Trident {
		return arrowDeflects
	}
	return arrowRefused
}

// settle does what out leaves of the shaft it, which flew in w.
func (fx *arrowEffects) settle(s *Server, w *world.World, it *arrowEntity, hitSound registry.Sound, out arrowOutcome) {
	switch out {
	case arrowSpent:
		s.playPending(arrowHitSound(w, it.arrow, hitSound))
		s.spendArrow(it)
	case arrowRefused:
		fx.refused = append(fx.refused, it)
	case arrowChannels:
		fx.landChanneling(it)
		fx.tridentBounces = append(fx.tridentBounces, it)
	case arrowDeflects:
		fx.tridentBounces = append(fx.tridentBounces, it)
	}
}

// arrowFrameOutcome runs a frame's hurt: the pop's or the break's broadcast,
// sound, drops and comparators' wake all live outside the tick's holdings.
func (s *Server) arrowFrameOutcome(hit arrowFrameHit) arrowOutcome {
	if !s.arrowHitFrame(hit) {
		return refusedOutcome(hit.arrow)
	}
	return landedOutcome(hit.arrow)
}

// arrowStandOutcome runs a stand's hurt: the always-kills break takes the
// body down at once. A stand already gone leaves the shaft flying on.
func (s *Server) arrowStandOutcome(hit arrowStandHit) arrowOutcome {
	landed, wasRefused := s.arrowHitArmorStand(hit)
	switch {
	case wasRefused:
		return refusedOutcome(hit.arrow)
	case !landed:
		return arrowFliesOn
	}
	return landedOutcome(hit.arrow)
}

// arrowCrystalHit is a crystal an arrow's sweep met.
type arrowCrystalHit struct {
	hitSound registry.Sound
	crystalProjectileHit
	arrow *arrowEntity
}

// arrowCrystalOutcome runs a crystal's blast; one gone leaves the shaft
// flying on, an invulnerable one refuses the hurt.
func (s *Server) arrowCrystalOutcome(hit arrowCrystalHit) arrowOutcome {
	landed, wasRefused := s.projectileHitEndCrystal(hit.crystalProjectileHit)
	switch {
	case wasRefused:
		return refusedOutcome(hit.arrow)
	case !landed:
		return arrowFliesOn
	}
	return landedOutcome(hit.arrow)
}

// arrowKnotOutcome runs a knot's hurt. A trident is spent on a knot and
// bounces off a refusing one, as a plain shaft does.
func (s *Server) arrowKnotOutcome(hit arrowKnotHit) arrowOutcome {
	if s.arrowHitKnot(hit) {
		return arrowSpent
	}
	return arrowRefused
}

// arrowBoatOutcome runs a hull's hurt in the boat lock's own phase. A hull
// broken earlier this tick leaves the shaft flying through; the removal and
// the drop of a broken hull are the boat tick's next pass.
func (s *Server) arrowBoatOutcome(hit boatProjectileBlow) arrowOutcome {
	if !s.boatProjectileHit(hit) {
		return arrowFliesOn
	}
	return landedOutcome(hit.arrow)
}

// arrowPaintingOutcome runs a painting's hurt: the break's broadcast, sound
// and drop, ahead of the shaft's own hit sound.
func (s *Server) arrowPaintingOutcome(hit paintingHit) arrowOutcome {
	if !s.hitPainting(hit) {
		return refusedOutcome(hit.arrow)
	}
	return landedOutcome(hit.arrow)
}

// arrowCushionOutcome runs a cushion's hurt; a refused one bounces the shaft.
func (s *Server) arrowCushionOutcome(hit cushionProjectileHit) arrowOutcome {
	if !s.projectileHitCushion(hit) {
		return refusedOutcome(hit.arrow)
	}
	return landedOutcome(hit.arrow)
}

// arrowPlayerOutcome runs a player's hurt through the same hurt a player's
// own blows do: armour, difficulty scaling, and the arrow's own death
// message.
func (s *Server) arrowPlayerOutcome(hit arrowPlayerHit) arrowOutcome {
	shooterID, shooterKiller, shooterClient := hit.shooterOf()
	// The blow's own damage type — the trident's for the trident, the
	// arrow's for the shaft — named by the event and picked for the
	// death message's words.
	damageType := rules.DamageTypeArrow
	if hit.arrow.arrow.Trident {
		damageType = rules.DamageTypeTrident
	}
	damage := hit.damage
	// Only a living non-player shooter scales the blow.
	// vanilla: DamageSource.scalesWithDifficulty
	if shooterClient == nil && shooterID != 0 && s.mobByEntityID(hit.w, shooterID) != nil {
		damage = rules.ScaledWithDifficulty(damage, hit.w.Difficulty())
	}
	directType := int32(protocol.EntityTypeArrow)
	if hit.arrow.arrow.Trident {
		directType = protocol.EntityTypeTrident
	}
	// The shaft's own ID, one above on the wire, is the direct the
	// event names — the body that carried the blow.
	cause := &damageCause{entityID: shooterID, x: hit.x, y: hit.y, z: hit.z, killer: shooterKiller, directID: hit.arrow.arrow.ID + 1,
		directName: s.directNameOf(shooterKiller, directType, hit.arrow.uuid, hit.custom),
		weapon:     hit.arrow.arrow.Weapon,
		piercing:   hit.arrow.piercingHit,
		flying:     true, flightVX: hit.vx, flightVZ: hit.vz}
	priorFire := hit.victim.player.Vitals().FireTicks
	if hit.igniteTicks > 0 {
		// The ignite runs ahead of the hurt; a refused hurt puts the
		// burn back below.
		hit.victim.igniteForTicksBy(hit.igniteTicks, pluginEntity{id: hit.arrow.uuid})
	}
	// The shaft's own push follows the flight, not the arrow's position:
	// a whole hit's default shove along it, and the Punch enchantment's
	// extra on every landed hit, the window-softened ones included.
	punch := rules.EnchantedKnockback(hit.arrow.arrow.Weapon, &rules.EnchContext{
		DamageType:     damageType,
		Victim:         protocol.EntityTypePlayer,
		DirectAttacker: directType,
	}, 0)
	landedHit, softened := hit.victim.hurt(damage, damageType, cause)
	switch {
	case landedHit:
		hit.victim.arrowKnockbackBy(hit.w, hit.arrow.uuid, hit.vx, hit.vz, punch, !softened, syncVelocityAfterTravel)
		for _, in := range hit.effects {
			hit.victim.effectCause = effect.CauseArrow
			hit.victim.AddEffect(in)
		}
		// vanilla: AbstractArrow.onHitEntity doPostAttackEffectsWithItemSource
		s.thornsOnOwner(hit.w, hit.victim, shooterID, damageType, directType)
		if shooterClient != nil {
			s.arrowsMu.Lock()
			killed := hit.arrow.killedByArrow(hit.victim.player.Vitals().Health <= 0, "minecraft:player")
			s.arrowsMu.Unlock()
			s.arrowHitAdvancements(shooterClient, arrowTypeName(hit.arrow), killed, hit.arrow.arrow.Weapon.ItemID, nil)
		}
		if hit.arrow.arrow.Trident {
			// The trident flies on out of the hit, its deflection —
			// the bounce of the blow the vanilla playSound's own
			// branch of comes with — queued.
			return landedOutcome(hit.arrow)
		}
		if !hit.arrow.piercingHit {
			// vanilla: AbstractArrow.onHitEntity
			hit.victim.player.UpdateVitals(func(v *player.Vitals) { v.ArrowCount++ })
		}
		if shooterClient != nil && shooterClient != hit.victim {
			shooterClient.send(protocol.IDPlayGameEvent,
				gameEventPayload(protocol.GameEventArrowHit, 0))
		}
		return arrowSpent
	case cause.freshBlock:
		// A fresh fully blocked hit still fires the shooter's
		// player_hurt_entity. vanilla: LivingEntity.hurtServer
		s.arrowHitAdvancements(shooterClient, arrowTypeName(hit.arrow), nil, 0, nil)
	}
	if hit.arrow.arrow.Trident {
		return arrowDeflects
	}
	// vanilla: AbstractArrow.onHitEntity
	hit.victim.player.UpdateVitals(func(v *player.Vitals) { v.FireTicks = priorFire })
	return arrowRefused
}

// bounceRefused is the refusal pass: the refused shafts bounce under the
// arrow lock, as the hurts ran without it and a /kill, a teleport or an
// explosion's push that landed while they did must not write the same shaft
// as the bounce. The seam is taken out under the lock, for the tests arm it
// whenever they will; it runs outside it, so its window stays lock-free.
func (fx *arrowEffects) bounceRefused(s *Server) {
	if len(fx.refused) > 0 {
		s.arrowsMu.Lock()
		seam := s.arrowRefusalSeam
		s.arrowsMu.Unlock()
		if seam != nil {
			seam()
		}
	}
	s.arrowsMu.Lock()
	for _, it := range fx.refused {
		it.arrow.Bounce(it.w)
		it.needsSync = true
	}
	s.arrowsMu.Unlock()
}

// deflectTrident deflects a trident a hit left standing: the hurt the hit
// dealt or failed to is done either way, and what a hit leaves a trident is
// the dealt mark, the near-dead bounce and the hit's own voice.
// vanilla: ThrownTrident.onHitEntity
func (s *Server) deflectTrident(it *arrowEntity) {
	s.broadcastSoundID(it.w, registry.SoundItemTridentHit, protocol.SoundCategoryNeutral,
		it.arrow.X, it.arrow.Y, it.arrow.Z, 1.0, 1.0)
	s.arrowsMu.Lock()
	it.arrow.TridentDeflect(it.w)
	it.needsSync = true
	s.arrowsMu.Unlock()
}

// dropOrphanedTrident lays an orphaned return's stack where the trident
// stood, dropped the block's own way.
// vanilla: ThrownTrident.tick
func (s *Server) dropOrphanedTrident(it *arrowEntity) {
	s.spawnItem(it.w, it.arrow.Weapon,
		it.arrow.X, it.arrow.Y+0.1, it.arrow.Z,
		(s.popRng.Float64()-0.5)*0.2, 0.2, (s.popRng.Float64()-0.5)*0.2,
		entity.BreakPickupDelay)
}
