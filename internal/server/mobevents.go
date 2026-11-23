package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/scoreboard"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
	"math"
)

// despawn removes m from the books and from view with no corpse and no drops,
// returning its entity, or nil when m is not tracked.
func (t *mobDimTick) despawn(fx *mobEffects, m *entity.Mob) *mobEntity {
	ent, ok := t.h.book[m]
	if !ok {
		return nil
	}
	t.h.forget(m)
	fx.scoreDrops = append(fx.scoreDrops, goneMob{ent.uuid, plugin.RemoveDespawn})
	fx.remove(t.w, m.ID)
	fx.release(ent)
	return ent
}

// blockAt is the block ev names, in this dimension.
func (t *mobDimTick) blockAt(ev entity.MobEvent) mobGraze {
	return mobGraze{w: t.w, mob: ev.Mob, x: int32(ev.X), y: int32(ev.Y), z: int32(ev.Z)}
}

// event turns one tick event into effects, settling bookkeeping that cannot
// wait for the lock's release.
func (t *mobDimTick) event(fx *mobEffects, in *mobTickInputs, ev entity.MobEvent) {
	s, h, w := t.s, t.h, t.w
	switch ev.Kind {
	case entity.MobDolphinTreasure, entity.MobDolphinGrace, entity.MobBlinded:
		t.dolphinEvent(fx, in, ev)
	case entity.MobAxolotlSupported:
		t.axolotlSupported(fx, in, ev)
	case entity.MobDied, entity.MobDying:
		t.died(fx, in, ev)
	case entity.MobKillingBlow:
		if ent := h.book[ev.Mob]; ent != nil {
			t.killCredit(fx, in, ev, ent)
			if !ev.Softened {
				fx.voiceHolder(w, ev.Mob, mobSound(ev.Mob, false), ev.Mob.SoundVolume(), ev.Mob.VoicePitch())
			}
			fx.entityEvent(w, ev.Mob.ID, protocol.EntityStatusDeath)
		}
	case entity.MobLandedOnShoulder:
		t.landedOnShoulder(fx, in, ev)
	case entity.MobConverted:
		if old := h.book[ev.Mob]; old != nil {
			h.forget(ev.Mob)
			fx.convert(mobConversion{w: w, old: old, into: ev.Into})
		}
	case entity.MobTurnedToStatue, entity.MobChestOpened, entity.MobChestClosed, entity.MobChestChanged:
		t.copperGolemEvent(fx, ev)
	case entity.MobCalledReinforcement:
		fx.calls = append(fx.calls, reinforcementCall{w: w, caller: ev.Mob, target: ev.Target, seed: ev.Seed})
	case entity.MobVanished:
		t.despawn(fx, ev.Mob)
	case entity.MobEnteredHive:
		t.enteredHive(fx, ev)
	case entity.MobGrewCrop:
		fx.cropGrowth = append(fx.cropGrowth, cropGrow{w: w, x: int32(ev.X), y: int32(ev.Y), z: int32(ev.Z), state: ev.BlockState})
	case entity.MobHorseTamed, entity.MobHorseThrewRider:
		t.horseEvent(fx, ev)
	case entity.MobSkeletonTrapSprung:
		t.trapSprung(fx, ev)
	case entity.MobPassengersChanged:
		// A mob vehicle's passengers changed (a rider left, or the vehicle died),
		// so resend the list.
		fx.sends = append(fx.sends, mobSend{w: w, id: protocol.IDPlaySetPassengers,
			data: setPassengersPayload(ev.Mob.ID, ev.Mob.PassengerIDs())})
	case entity.MobGuardianCharged:
		fx.entityEvent(w, ev.Mob.ID, protocol.EntityStatusGuardianAttack)
	case entity.MobCreakingHurt:
		if ent, ok := h.book[ev.Mob]; ok {
			if x, y, z, bound := ev.Mob.CreakingHome(); bound {
				fx.creakingHurts = append(fx.creakingHurts, creakingHurtCall{w: w, id: ent.uuid, x: x, y: y, z: z})
			}
		}
	case entity.MobShotBreezeCharge:
		fx.breezeShots = append(fx.breezeShots, blazeShotOf(w, ev, h.book[ev.Mob]))
	case entity.MobShotFireball:
		fx.blazeShots = append(fx.blazeShots, blazeShotOf(w, ev, h.book[ev.Mob]))
	case entity.MobSpat:
		fx.spits = append(fx.spits, spitOf(w, ev, h.book[ev.Mob]))
	case entity.MobDragonShot, entity.MobDragonFlame, entity.MobDragonFlameEnded, entity.MobDragonDeath:
		t.dragonEvent(fx, ev)
	case entity.MobThrewSnowball:
		fx.voiceID(w, ev.Mob, registry.SoundEntitySnowGolemShoot, 1, ev.Pitch)
		fx.snowballs = append(fx.snowballs, spitOf(w, ev, h.book[ev.Mob]))
	case entity.MobGolemCracked:
		fx.voiceID(w, ev.Mob, registry.SoundEntityIronGolemDamage, 1, 1)
	case entity.MobShotLargeFireball, entity.MobGhastWarned:
		fx.ghastShots = append(fx.ghastShots, ghastShotOf(w, ev, h.book[ev.Mob]))
	case entity.MobShotWitherSkull:
		fx.witherShots = append(fx.witherShots, witherShotOf(w, ev, h.book[ev.Mob]))
	case entity.MobWitherExploded:
		t.witherBlast(fx, ev)
	case entity.MobWitherBar:
		if ent, ok := h.book[ev.Mob]; ok {
			fx.witherBars = append(fx.witherBars, witherBarOf(w, ent))
		}
	case entity.MobWardenDarkness:
		fx.darkness = append(fx.darkness, wardenDarkness{w: w, x: ev.X, y: ev.Y, z: ev.Z, radius: ev.Radius})
	case entity.MobWardenVibration:
		_, eye, _ := ev.Mob.EyePosition()
		fx.vibrations = append(fx.vibrations, wardenVibrationSend{w: w, id: ev.Mob.ID,
			particle: ev.Vibration, eye: eye - ev.Mob.Y})
	case entity.MobElderCurse:
		fx.curses = append(fx.curses, elderCurse{w: w, x: ev.X, y: ev.Y, z: ev.Z, silent: ev.Mob.Silent})
	case entity.MobContactHurt:
		fx.contacts = append(fx.contacts, t.contactOf(ev))
	case entity.MobAttackedPlayer, entity.MobGuardianBeam:
		t.attackedPlayer(fx, ev)
	case entity.MobGoatRammedStand:
		fx.standRams = append(fx.standRams, standRam{w: w, id: ev.Player.EntityID, yaw: ev.RamYaw, push: ev.RamPush})
	case entity.MobGoatLoweredHead:
		fx.entityEvent(w, ev.Mob.ID, protocol.EntityStatusGoatLowerHead)
	case entity.MobGoatRaisedHead:
		fx.entityEvent(w, ev.Mob.ID, protocol.EntityStatusGoatRaiseHead)
	case entity.MobGoatSounded:
		volume := ev.Volume
		if volume == 0 {
			volume = 1
		}
		fx.voiceID(w, ev.Mob, ev.SoundID, volume, ev.Pitch)
	case entity.MobOffersChanged:
		if ent, ok := h.book[ev.Mob]; ok {
			fx.resends = append(fx.resends, ent)
		}
	case entity.MobEntityAction:
		t.gameEvent(fx, ev.Mob, rules.GameEventEntityAction, rules.GameEventContext{})
	case entity.MobStepped:
		t.gameEvent(fx, ev.Mob, rules.GameEventStep, rules.GameEventContext{Affected: ev.BlockState, Sneaking: ev.Mob.Crouching})
	case entity.MobSwamStride:
		t.gameEvent(fx, ev.Mob, rules.GameEventSwim, rules.GameEventContext{Sneaking: ev.Mob.Crouching})
	case entity.MobSplashed, entity.MobFlapped, entity.MobBouncedOff:
		t.bodyGameEvent(fx, ev)
	case entity.MobSounded, entity.MobBounced:
		t.sounded(fx, ev)
	case entity.MobFoxFaceplantDust:
		fx.sends = append(fx.sends, t.foxDust(ev))
	case entity.MobStatus:
		fx.entityEvent(w, ev.Mob.ID, byte(ev.Status))
	case entity.MobSeated, entity.MobDismountedUnderwater:
		t.seatEvent(fx, ev)
	case entity.MobSwungOffHand:
		fx.sends = append(fx.sends, mobSend{w: w, id: protocol.IDPlaySwingAnimation, data: swingAnimationPayload(ev.Mob.ID, protocol.HandOff),
			near: true, x: ev.Mob.X, y: ev.Mob.Y, z: ev.Mob.Z, radius: levelEventRange})
	case entity.MobLevelEvent:
		fx.sends = append(fx.sends, mobSend{w: w, id: protocol.IDPlayWorldEvent, data: worldEvent(ev.Status, int32(ev.X), int32(ev.Y), int32(ev.Z), 0), near: true, x: ev.X, y: ev.Y, z: ev.Z, radius: levelEventRange})
	case entity.MobRemovedBlock:
		fx.breaks = append(fx.breaks, mobBlockBreak{w: w, x: int32(ev.X), y: int32(ev.Y), z: int32(ev.Z), remove: true})
	case entity.MobBrokeBlock:
		fx.breaks = append(fx.breaks, mobBlockBreak{w: w, mob: ev.Mob, x: int32(ev.X), y: int32(ev.Y), z: int32(ev.Z), by: ev.Mob.Type})
	case entity.MobWitherSmashed:
		fx.smashes = append(fx.smashes, witherSmashOf(w, ev))
	case entity.MobFarm:
		fx.farming = append(fx.farming, mobFarm{w: w, m: ev.Mob, action: ev.Farm, at: entity.Waypoint{X: int32(ev.X), Y: int32(ev.Y), Z: int32(ev.Z)}})
	case entity.MobActed:
		fx.acts = append(fx.acts, mobAct{w: w, x: ev.X, y: ev.Y, z: ev.Z, mob: h.MobUUID(ev.Mob)})
	case entity.MobGoatDroppedHorn:
		if ent, ok := h.book[ev.Mob]; ok {
			fx.horns = append(fx.horns, goatHorn{w: w, event: ev,
				instrument: rules.GoatHornInstrument(ent.uuid, ev.Mob.Goat.Screaming)})
		}
	case entity.MobRangBell, entity.MobCelebrated:
		fx.villagerSignals = append(fx.villagerSignals, villagerSignal{w: w, event: ev})
	case entity.MobWantsGolem:
		fx.golems = append(fx.golems, mobGolemRequest{w: w, x: ev.Mob.X, y: ev.Mob.Y, z: ev.Mob.Z, mob: ev.Mob})
	case entity.MobThrewItem, entity.MobGaveGift:
		t.threwItem(fx, ev)
	case entity.MobBurned:
		t.burned(fx, ev)
	case entity.MobExtinguished:
		fx.voiceID(w, ev.Mob, registry.SoundEntityGenericExtinguishFire, extinguishVolume,
			s.extinguishPitch())
	case entity.MobTotemSaved:
		fx.entityEvent(w, ev.Mob.ID, protocol.EntityStatusTotem)
	case entity.MobArmourBroke:
		// The helmet broke: head break event, sound and particles.
		// vanilla: LivingEntity.onEquippedItemBroken
		fx.entityEvent(w, ev.Mob.ID, protocol.EntityStatusBreakHead)
	case entity.MobBodyBroke:
		// vanilla: LivingEntity.onEquippedItemBroken
		fx.entityEvent(w, ev.Mob.ID, protocol.EntityStatusBreakBody)
	case entity.MobHandBroke:
		// vanilla: LivingEntity.onEquippedItemBroken
		fx.entityEvent(w, ev.Mob.ID, protocol.EntityStatusBreakMainHand)
	case entity.MobLanded, entity.MobLandingDust:
		t.landed(fx, ev)
	case entity.MobJumped, entity.MobSquished:
		// vanilla: CubeMobMoveControl's hop and AbstractCubeMob.
		// tick's landing, Rabbit.setJumping
		var sound registry.Sound
		if ev.Mob.Type == entity.TypeRabbit {
			sound = registry.SoundEntityRabbitJump
		} else {
			sound = cubeSound(ev.Mob, ev.Kind == entity.MobJumped)
		}
		fx.voiceID(w, ev.Mob, sound, ev.Mob.SoundVolume(), ev.Pitch)
	case entity.MobHopped:
		fx.entityEvent(w, ev.Mob.ID, protocol.EntityStatusRabbitJump)
	case entity.MobRaidedCrop:
		fx.raids = append(fx.raids, t.blockAt(ev))
	case entity.MobPickedBerries:
		fx.berryPicks = append(fx.berryPicks, mobBerryPick{w: w, mob: ev.Mob, x: int32(ev.X), y: int32(ev.Y), z: int32(ev.Z)})
	case entity.MobMoaned:
		// Ambient sound, with the event, volume and pitch AmbientVoice rolls.
		event, volume, pitch := ev.Mob.AmbientVoice()
		if ev.SoundID != registry.SoundNone {
			fx.voiceID(w, ev.Mob, ev.SoundID, ev.Volume, ev.Pitch)
			break
		}
		fx.voiceID(w, ev.Mob, event, volume, pitch)
	case entity.MobWarned:
		fx.voiceID(w, ev.Mob, registry.SoundEntityPolarBearWarning, ev.Mob.SoundVolume(), ev.Mob.VoicePitch())
	case entity.MobFlopped:
		// vanilla: AbstractFish.aiStep, Mob.makeSound
		fx.voiceID(w, ev.Mob, entity.FishFlopSound(ev.Mob.Type), ev.Mob.SoundVolume(), ev.Mob.VoicePitch())
	case entity.MobSwam:
		// vanilla: Entity.playSwimSound, AbstractFish.getSwimSound
		fx.voiceID(w, ev.Mob, registry.SoundEntityFishSwim, ev.Volume, ev.Pitch)
	case entity.MobTeleported:
		t.endermanTeleported(fx, ev)
	case entity.MobPhantomSwooped:
		// vanilla: Phantom's PhantomAttackStrategyGoal.tick, playSound
		fx.voiceID(w, ev.Mob, registry.SoundEntityPhantomSwoop, 10.0, ev.Pitch)
	case entity.MobPhantomBit:
		// The bite level event, heard within the 64 blocks level events carry.
		// vanilla: PhantomSweepAttackGoal.tick, Level.levelEvent
		fx.sends = append(fx.sends, mobSend{w: w, id: protocol.IDPlayWorldEvent,
			data: worldEvent(phantomBiteEvent, rules.BlockOf(ev.Mob.X), rules.BlockOf(ev.Mob.Y), rules.BlockOf(ev.Mob.Z), 0),
			near: true, x: ev.Mob.X, y: ev.Mob.Y, z: ev.Mob.Z, radius: 64})
	case entity.MobBatTookOff:
		// vanilla: Bat.customServerAiStep, Level.levelEvent
		fx.sends = append(fx.sends, mobSend{w: w, id: protocol.IDPlayWorldEvent,
			data: worldEvent(batTakeoffEvent, rules.BlockOf(ev.Mob.X), rules.BlockOf(ev.Mob.Y), rules.BlockOf(ev.Mob.Z), 0),
			near: true, x: ev.Mob.X, y: ev.Mob.Y, z: ev.Mob.Z, radius: 64})
	case entity.MobTentacleReset:
		// The squid's tentacle cycle wrapped; the client restarts the animation on
		// this event. vanilla: Squid.aiStep's broadcastEntityEvent
		fx.entityEvent(w, ev.Mob.ID, protocol.EntityStatusSquidTentacles)
	case entity.MobWolfShook:
		fx.entityEvent(w, ev.Mob.ID, protocol.EntityStatusWolfShake)
	case entity.MobWolfShakeSound:
		// vanilla: Wolf.tick's playSound(WOLF_SHAKE, volume, (r - r) * 0.2 + 1)
		fx.voiceID(w, ev.Mob, registry.SoundEntityWolfShake, entity.MobSoundVolume(ev.Mob.Type), ev.Pitch)
	case entity.MobWolfArmorCracked:
		// vanilla: Wolf.actuallyHurt plays WOLF_ARMOR_CRACK and sprays
		// 20 armadillo scute particles.
		fx.voiceID(w, ev.Mob, registry.SoundItemWolfArmorCrack, 1, 1)
		fx.sends = append(fx.sends, mobSend{w: w, id: protocol.IDPlayLevelParticles,
			data: wolfScuteParticles(ev.Mob), near: true, x: ev.Mob.X, y: ev.Mob.Y + 1, z: ev.Mob.Z, radius: particleRange})
	case entity.MobWolfArmorBroke:
		fx.entityEvent(w, ev.Mob.ID, protocol.EntityStatusBreakBody)
		fx.voiceID(w, ev.Mob, registry.SoundItemWolfArmorBreak, 1, 1)
	case entity.MobWolfShakeCancelled:
		fx.entityEvent(w, ev.Mob.ID, protocol.EntityStatusWolfStopShake)
	case entity.MobBubbleSurface, entity.MobSparkled:
		t.particles(fx, ev)
	case entity.MobPrimed:
		// The fuse started: vanilla plays the priming as swell leaves 0.
		// vanilla: SulfurCube.primeTime's TNT_PRIMED, Creeper.tick
		sound, volume, src := registry.SoundEntityTntPrimed, float32(1), rules.GameEventContext{}
		var pitch float32
		if ev.Mob.Type == entity.TypeSulfurCube {
			volume, pitch = ev.Mob.SoundVolume(), ev.Mob.VoicePitch()
		} else {
			sound, pitch, src = registry.SoundEntityCreeperPrimed, 0.5, rules.GameEventContext{Mob: t.h.MobUUID(ev.Mob)}
		}
		fx.voiceID(w, ev.Mob, sound, volume, pitch)
		fx.gameEvent(w, rules.GameEventPrimeFuse, ev.Mob.X, ev.Mob.Y, ev.Mob.Z, src)
	case entity.MobExploded:
		t.creeperExploded(fx, ev)
	case entity.MobShedScute:
		t.shedScute(fx, ev.Mob)
	case entity.MobDroppedItem, entity.MobPandaAte:
		t.pandaEvent(fx, ev)
	case entity.MobSnifferDug:
		t.snifferDug(fx, ev)
	case entity.MobLaidEgg:
		t.chickenLaid(fx, ev.Mob)
	case entity.MobGrewUp:
		t.turtleGrew(fx, ev.Mob)
	case entity.MobLaidTurtleEgg, entity.MobLaidFrogspawn, entity.MobStompingEgg, entity.MobStompedEgg:
		fx.laidSpawn(w, ev)
	case entity.MobDugSand:
		fx.sends = append(fx.sends, mobSend{w: w, id: protocol.IDPlayWorldEvent,
			data: worldEvent(protocol.WorldEventBlockBreak, int32(ev.X), int32(ev.Y+1), int32(ev.Z), int32(w.BlockAt(int32(ev.X), int32(ev.Y), int32(ev.Z)))),
			near: true, x: ev.X, y: ev.Y, z: ev.Z, radius: 64})
	case entity.MobBred:
		t.bred(fx, ev)
	case entity.MobThrewTrident:
		t.shotArrow(fx, ev)
		fx.voiceID(w, ev.Mob, registry.SoundEntityDrownedShoot, 1, ev.Pitch)
	case entity.MobShotArrow:
		t.shotArrow(fx, ev)
		if !ev.Crossbow { // the pillager's volley has its own sound
			fx.voiceID(w, ev.Mob, registry.SoundEntitySkeletonShoot, 1, ev.Pitch)
		}
	case entity.MobShulkerShot:
		t.shulkerFired(fx, ev)
	case entity.MobCastFangs:
		fx.fangCasts = append(fx.fangCasts, fangsCast{w: w, owner: ev.Mob.ID, x: ev.X, y: ev.Y, z: ev.Z, radians: ev.Radius, warmup: ev.Amount})
	case entity.MobSummonedVex:
		fx.vexCalls = append(fx.vexCalls, vexSummon{w: w, owner: ev.Mob.ID, x: ev.X, y: ev.Y, z: ev.Z, life: ev.Amount})
	case entity.MobShulkerCopied:
		fx.copies = append(fx.copies, shulkerShot{w: w, x: ev.X, y: ev.Y, z: ev.Z, color: uint8(ev.Status)})
	case entity.MobShulkerShell:
		t.shulkerShell(fx, ev)
	case entity.MobShulkerShoved:
		fx.shoves = append(fx.shoves, bodyShove{overlaps: slabOverlaps(ev.Slab), dx: ev.VX, dy: ev.VY, dz: ev.VZ, w: w})
	case entity.MobShulkerTeleported:
		t.shulkerTeleported(fx, ev)
	case entity.MobThrewPotion:
		t.witchThrewPotion(fx, ev)
	case entity.MobWitchDrank:
		fx.voiceID(w, ev.Mob, registry.SoundEntityWitchDrink, 1, ev.Pitch)
	case entity.MobWitchDrankUp:
		fx.gameEvent(w, rules.GameEventDrink, ev.Mob.X, ev.Mob.Y, ev.Mob.Z, rules.GameEventContext{Mob: t.h.MobUUID(ev.Mob)})
	case entity.MobWitchSparkled:
		fx.entityEvent(w, ev.Mob.ID, protocol.EntityStatusWitchParticles)
	case entity.MobBeganEating:
		// The sheep's eating animation.
		fx.entityEvent(w, ev.Mob.ID, protocol.EntityStatusEatGrass)
	case entity.MobAte, entity.MobMeltedSnow, entity.MobDousedCauldron,
		entity.MobSteppedOnSculk, entity.MobSteppedOnRedstoneOre, entity.MobTookBlock,
		entity.MobPlacedBlock, entity.MobLaidSnow, entity.MobWokeInfested,
		entity.MobMergedWithStone, entity.MobInfestedRelease, entity.MobPackLocated:
		t.worldTouchEvent(fx, ev)
	case entity.MobEffectChanged:
		t.effectChanged(fx, in, ev)
	case entity.MobLeashDropped:
		t.leashDropped(fx, ev)
	}
}

// worldTouchEvent queues the world change a mob's touch makes, after the locks.
func (t *mobDimTick) worldTouchEvent(fx *mobEffects, ev entity.MobEvent) {
	w := t.w
	switch ev.Kind {
	case entity.MobAte:
		// The bitten block changes outside the locks, where mob_griefing is read
		// and the world event plays.
		fx.grazes = append(fx.grazes, t.blockAt(ev))
	case entity.MobMeltedSnow:
		// The powder snow melts outside the locks, where mob_griefing is read.
		fx.snowMelts = append(fx.snowMelts, t.blockAt(ev))
	case entity.MobDousedCauldron:
		fx.cauldrons = append(fx.cauldrons, t.blockAt(ev))
	case entity.MobPackLocated:
		fx.located = append(fx.located, mobLocated{w: w, mob: ev.Mob, x: ev.X, y: ev.Y, z: ev.Z, changed: ev.Level})
	case entity.MobSteppedOnSculk:
		t.sculkStep(fx, ev)
	case entity.MobSteppedOnRedstoneOre:
		fx.oreTouches = append(fx.oreTouches, t.blockAt(ev))
	case entity.MobTookBlock:
		fx.blockTakes = append(fx.blockTakes, mobBlockMove{mob: ev.Mob, w: w, x: int32(ev.X), y: int32(ev.Y), z: int32(ev.Z), state: ev.BlockState, took: true})
	case entity.MobPlacedBlock:
		fx.blockTakes = append(fx.blockTakes, mobBlockMove{mob: ev.Mob, w: w, x: int32(ev.X), y: int32(ev.Y), z: int32(ev.Z), state: ev.BlockState})
	case entity.MobLaidSnow:
		fx.snowTrail = append(fx.snowTrail, t.blockAt(ev))
	case entity.MobWokeInfested, entity.MobMergedWithStone:
		fx.infests = append(fx.infests, mobInfest{w: w, x: int32(ev.X), y: int32(ev.Y), z: int32(ev.Z),
			merge: ev.Kind == entity.MobMergedWithStone})
		if ev.Kind == entity.MobMergedWithStone {
			fx.entityEvent(w, ev.Mob.ID, entityStatusSpawnAnim)
		}
	case entity.MobInfestedRelease:
		fx.infests = append(fx.infests, mobInfest{w: w, release: true,
			px: ev.X, py: ev.Y, pz: ev.Z, vx: ev.VX, vy: ev.VY, vz: ev.VZ})
	}
}

// died starts the corpse countdown and collects kill effects only for MobDied.
func (t *mobDimTick) died(fx *mobEffects, in *mobTickInputs, ev entity.MobEvent) {
	s, h, w := t.s, t.h, t.w
	if ev.Mob.Type == entity.TypeEnderDragon {
		end := ev
		end.Kind = entity.MobDragonFlameEnded
		fx.dragonShots = append(fx.dragonShots, ghastShotOf(w, end, h.book[ev.Mob]))
	}
	// The core already removed the dead from the herd.
	ent := h.book[ev.Mob]
	h.forget(ev.Mob)
	if ent == nil {
		return
	}
	// A villager a zombie kills becomes a zombie villager: no corpse or drops.
	// vanilla: Zombie.killedEntity
	if z := ev.Infection; z != nil && s.askTransformEnt(ent, entity.TypeZombieVillager) {
		fx.convert(mobConversion{w: w, old: ent, into: entity.TypeZombieVillager,
			infectionSilent: z.Silent, infectionX: rules.BlockOf(z.X), infectionY: rules.BlockOf(z.Y), infectionZ: rules.BlockOf(z.Z)})
		return
	}
	fx.release(ent)
	ent.dying = true
	body := dyingMob{id: ev.Mob.ID, remains: corpseTicks - ev.Mob.DeathTime, ent: ent}
	if ev.Kind == entity.MobDying {
		h.dying = append(h.dying, body)
		fx.sends = append(fx.sends, mobSend{w: w, id: protocol.IDPlayEntityMetadata, data: mobHealthPayload(ent)})
		return
	}
	if ent.raider != nil {
		var killer uuid.UUID
		if k, ok := in.clientByEntityID[ev.Mob.AttackerID()]; ok && ev.Mob.HurtByPlayer() {
			killer = k.player.UUID
		}
		fx.raiderDeaths = append(fx.raiderDeaths, raiderDeath{ent.raider, killer})
	}
	if ev.Mob.CreakingTearingDown() {
		// vanilla: Creaking.tickDeath
		body.remains = entity.CreakingTearDownTicks + 1
		fx.sends = append(fx.sends, mobSend{w: w, id: protocol.IDPlayEntityMetadata,
			data: creakingTearingDownPayload(ev.Mob.ID)})
	}
	if killer := s.mobByEntityIDLocked(w, ev.Mob.BlowAttackerID()); killer != nil && killer.mob.Type == entity.TypeFrog {
		// vanilla: ShootTongue.eatEntity removes killed prey immediately.
		fx.remove(w, ev.Mob.ID)
		fx.scoreDrops = append(fx.scoreDrops, goneMob{ent.uuid, plugin.RemoveDeath})
	} else if ev.AfterTickDeath {
		t.corpses = append(t.corpses, body)
	} else {
		h.dying = append(h.dying, body)
	}
	// vanilla: TamableAnimal.die
	if tame := ev.Mob.Tamable; tame.Tame && tame.Owner != (uuid.UUID{}) && w.BoolRule(world.RuleShowDeathMsg) {
		killerOf := func(id int32) *rules.Killer {
			if k, ok := in.clientByEntityID[id]; ok {
				return &rules.Killer{Name: k.DisplayName(), Weapon: k.heldItem(protocol.HandMain)}
			}
			if other := s.mobByEntityIDLocked(w, id); other != nil {
				return s.mobKiller(other)
			}
			return nil
		}
		var credit *rules.Killer
		if ev.Mob.PlayerKillCredit() {
			credit = killerOf(ev.Mob.PlayerCreditID())
		}
		fx.petDeaths = append(fx.petDeaths, petDeath{owner: tame.Owner,
			message: ev.Mob.CombatDeathMessage(s.mobKillerName(ent), killerOf, credit)})
	}
	// The loot killer is whoever last hurt the body, with their current weapon
	// and whether the killing blow was a melee swing.
	killer := rules.LootKiller{Melee: ev.Mob.HurtByMelee(), ThisNBT: func() nbt.Compound {
		// vanilla: Entity.saveWithoutId
		return nbt.PlainCopy(mobDataTag(ent)).(nbt.Compound)
	}}
	killer.ThisFill = func(out *rules.LootEntity) { playerLootRelations(out, ev.Mob, in.clientByEntityID) }
	if p, ok := in.byEntityID[ev.Mob.BlowAttackerID()]; ok {
		killer.Player = true
		killer.Weapon = rules.ToolOf(p.HeldItem)
		killer.AttackerPieces = []rules.EnchPiece{{Stack: p.HeldItem, Slot: "mainhand"}}
		if k, ok := in.clientByEntityID[ev.Mob.BlowAttackerID()]; ok {
			killer.Profile = k.lootProfile()
			killer.PlayerNBT = k.lootNBT
			killer.AttackerPieces = k.enchPieces()
			killer.AttackerFill = k.playerLootFill
		}
	} else if other := s.mobByEntityIDLocked(w, ev.Mob.BlowAttackerID()); other != nil {
		killer.AttackerType = other.mob.Type
		killer.AttackerVariant = other.mob.Variant
		killer.AttackerFill = func(out *rules.LootEntity) { *out = mobLootEntity(other.mob) }
		killer.AttackerPieces = other.mob.EnchEquipment()
	}
	if ev.Mob.PlayerKillCredit() {
		if k, ok := in.clientByEntityID[ev.Mob.PlayerCreditID()]; ok {
			killer.CreditedPlayer = rules.LootEntity{Type: "minecraft:player",
				Mainhand: rules.ToolOf(k.heldItem(protocol.HandMain)), ScoreHolder: k.Name(),
				Profile: k.lootProfile(), NBT: k.lootNBT}
			k.playerLootFill(&killer.CreditedPlayer)
			killer.Luck = float32(k.luckAttribute())
			killer.CreditedPieces = k.enchPieces()
		}
	}
	// Kill credit prefers the remembered player; sourceless damage retains
	// the mob attacker even though it clears the loot attacker.
	// vanilla: LivingEntity.getKillCredit, LivingEntity.createWitherRose
	credit := ev.Mob.AttackerID()
	if ev.Mob.PlayerKillCredit() {
		credit = ev.Mob.PlayerCreditID()
	}
	if other := s.mobByEntityIDLocked(w, credit); other != nil && other.mob.IsWither() {
		fx.roses = append(fx.roses, witherRose{w: w, x: ev.Mob.X, y: ev.Mob.Y, z: ev.Mob.Z})
	}
	t.killCredit(fx, in, ev, ent)

	xpTaken, spread := s.physicsFor(w).SculkCatalystDeath(ev.Mob.X, ev.Mob.Y, ev.Mob.Z, func() int { return ev.Mob.RollExperience(killer) })
	if spread && ev.Mob.LastHurtByPlayer() {
		if k, ok := in.clientByEntityID[ev.Mob.PlayerCreditID()]; ok {
			k.killedNearSculkCatalystAdvancements()
		}
	}
	// vanilla: LivingEntity.dropAllDeathLoot drops the chest outside shouldDropLoot.
	if chest := ev.Mob.TakeChest(); len(chest) > 0 {
		fx.deaths = append(fx.deaths, mobDrops{w: w, stacks: chest, x: ev.Mob.X, y: ev.Mob.Y, z: ev.Mob.Z})
	}
	if eq := ev.Mob.TakeDeathEquipment(); len(eq) > 0 {
		fx.deaths = append(fx.deaths, mobDrops{w: w, stacks: eq, x: ev.Mob.X, y: ev.Mob.Y, z: ev.Mob.Z})
	}
	// vanilla: LivingEntity.shouldDropLoot and dropExperience read MOB_DROPS:
	// off skips the loot table, the other mobs' equipment and experience.
	note := mobDeathNote{uuid: ent.uuid, w: w, x: ev.Mob.X, y: ev.Mob.Y, z: ev.Mob.Z,
		damageType: ev.Mob.LastDamageType(), causeID: ev.Mob.AttackerID(), drops: -1, xp: -1}
	if w.BoolRule(world.RuleMobDrops) {
		killer.ThisHasRaid = mobHasLootRaid(ent, t.raidCenters)
		drops := mobDrops{
			w:      w,
			stacks: ev.Mob.RollDrops(killer, s.lootLevelAt(w, ev.Mob.X, ev.Mob.Y, ev.Mob.Z)),
			x:      ev.Mob.X, y: ev.Mob.Y, z: ev.Mob.Z,
		}
		// vanilla: WitherBoss.dropCustomDeathLoot
		if ev.Mob.IsWither() {
			drops.lasting = []protocol.ItemStack{{ItemID: int32(registry.ItemNetherStar), Count: 1}}
		}
		note.drops = len(fx.deaths)
		fx.deaths = append(fx.deaths, drops)
		// vanilla: LivingEntity.dropExperience, only for a mob a player last hurt
		// within lastHurtByMemory ticks.
		if !xpTaken && ev.Mob.PlayerKillCredit() {
			if xp := ev.Mob.RollExperience(killer); xp > 0 {
				note.xp = len(fx.xpDrops)
				fx.xpDrops = append(fx.xpDrops, mobXpDrop{
					w: w, amount: xp,
					x: ev.Mob.X, y: ev.Mob.Y, z: ev.Mob.Z,
				})
			}
		}
	}
	if s.plugins != nil && s.plugins.Has(pluginruntime.EntityDeath) {
		fx.deathNotes = append(fx.deathNotes, note)
	}
	// The sound goes before the event, then the dying pose and zero health.
	// A body the heart kills is silent until its twitch ends.
	// vanilla: LivingEntity.hurtServer, LivingEntity.die, CreakingHeartBlockEntity.removeProtector
	if !ev.Mob.DeathSoftened() && !ev.Mob.CreakingTearingDown() {
		fx.voiceHolder(w, ev.Mob, mobSound(ev.Mob, false), ev.Mob.SoundVolume(), ev.Mob.VoicePitch())
	}
	fx.sends = append(fx.sends,
		mobSend{w: w, id: protocol.IDPlayEntityEvent,
			data: entityEventPayload(ev.Mob.ID, protocol.EntityStatusDeath)},
		mobSend{w: w, id: protocol.IDPlayEntityMetadata,
			data: dyingMetadataPayload(ev.Mob.ID)})
}

// killCredit settles the killing blow independently of corpse removal.
// vanilla: LivingEntity.die
func (t *mobDimTick) killCredit(fx *mobEffects, in *mobTickInputs, ev entity.MobEvent, ent *mobEntity) {
	s, w := t.s, t.w
	// mob_kills, the kill-count objectives and player_killed_entity go to the
	// kill credit; killed/<type> only to the player behind the killing blow,
	// which a later /kill or fall takes away.
	// vanilla: LivingEntity.die, ServerPlayer.awardKillScore, Player.killedEntity
	if ev.Mob.PlayerKillCredit() {
		if k, ok := in.clientByEntityID[ev.Mob.PlayerCreditID()]; ok {
			typeName, id, custom := ev.Mob.Type.Name(), ent.uuid, customNameText(ent.name)
			k.scoreKill(scoreboard.Holder{Name: ent.uuidText, Display: func() nbt.Tag {
				return protocol.TextTag(entityDisplayName(s.scoreboard, typeName, id, custom))
			}}, false)
			victim := s.advancementEntity(ev.Mob.Type.Name(), w)
			if ev.Mob.HurtByPlayer() {
				victim.Direct = ev.Mob.BlowDirectType()
			}
			victim.Captain = ev.Mob.Captain()
			at := k.Position()
			victim.Distance = math.Hypot(at.X-ev.Mob.X, at.Z-ev.Mob.Z)
			loot := frozenMobLoot(ev.Mob)
			victim.Loot = func() rules.LootEntity { return *loot }
			victim.Blow = killingBlow(k, ev.Mob)
			k.killedEntityAdvancements(victim)
		}
	}
	if ev.Mob.HurtByPlayer() {
		if k, ok := in.clientByEntityID[ev.Mob.AttackerID()]; ok {
			k.awardStat(player.Stat{Type: registry.StatTypeKilled, Key: int32(ev.Mob.Type)}, 1)
		}
	}
	// vanilla: LivingEntity.die
	fx.gameEvent(w, rules.GameEventEntityDie, ev.Mob.X, ev.Mob.Y, ev.Mob.Z, rules.GameEventContext{})
}

// attackedPlayer queues a monster's blow on a player, killer and dice read
// under the mob lock.
func (t *mobDimTick) attackedPlayer(fx *mobEffects, ev entity.MobEvent) {
	h, w := t.h, t.w
	// The swing is sent to the dimension; the damage pipeline runs outside the
	// locks.
	killer := &rules.Killer{Name: mobTypeName(ev.Mob)}
	if ent, ok := h.book[ev.Mob]; ok {
		killer = t.s.mobKiller(ent)
	}
	var igniteRoll float32
	ignites := ev.Mob.IgnitesBlow()
	if ignites {
		igniteRoll = ev.Mob.Rand.Float32()
	}
	var bunnyPitch float32
	bunny := ev.Mob.KillerBunny()
	if bunny {
		// vanilla: Rabbit.playAttackSound
		bunnyPitch = (ev.Mob.Rand.Float32()-ev.Mob.Rand.Float32())*0.2 + 1
	}
	var attackerUUID uuid.UUID
	var attackerText string
	if ent := h.book[ev.Mob]; ent != nil {
		attackerUUID, attackerText = ent.uuid, ent.uuidText
	}
	fx.strikes = append(fx.strikes, mobStrike{
		owner: ev.Mob.CreditOwner(),
		uuid:  attackerUUID, uuidText: attackerText,
		bunny: bunny, bunnyPitch: bunnyPitch,
		ignites: ignites, igniteRoll: igniteRoll,
		w:        w,
		mob:      ev.Mob,
		victimID: ev.Player.EntityID,
		damage:   ev.Damage,
		x:        ev.Mob.X, y: ev.Mob.Y, z: ev.Mob.Z,
		killer:        killer,
		touch:         ev.Touch,
		puff:          ev.Puff,
		dragonContact: ev.DragonContact, contactPush: ev.ContactPush,
		pitch:  ev.Pitch,
		beam:   ev.Magic,
		hoglin: ev.Hoglin, stab: ev.Stab,
		roar: ev.Roar, sonic: ev.Sonic,
		ram: ev.Ram, ramYaw: ev.RamYaw, ramPush: ev.RamPush,
	})
}

// burned tells a causeless hurt: fire, wall, drowning, lava or an effect.
func (t *mobDimTick) burned(fx *mobEffects, ev entity.MobEvent) {
	s, w := t.s, t.w
	// The damage event (no cause) and hurt sound go only with a whole hurt; a
	// window-softened one is told by health alone. The drowning bubbles are
	// outside that gate and go first, as for a player. No hurt animation: that
	// packet is for players. Lava also plays the burn (vanilla: Entity.lavaHurt).
	if ev.DamageType == rules.DamageTypeDrown && !ev.NoBubble {
		fx.entityEvent(w, ev.Mob.ID, protocol.EntityEventDrown)
	}
	if !ev.Softened {
		fx.sends = append(fx.sends, mobHurtTold(w, ev.Mob, ev.DamageType)...)
	}
	if ev.DamageType == rules.DamageTypeLava {
		fx.voiceID(w, ev.Mob, registry.SoundEntityGenericBurn, 0.4, s.burnPitch())
	}
}

// landed tells a landing's fall hurt.
func (t *mobDimTick) landed(fx *mobEffects, ev entity.MobEvent) {
	s, w := t.s, t.w
	if ev.Kind == entity.MobLandingDust {
		fx.sends = append(fx.sends, landingDust(w, ev))
		return
	}
	if entity.IsEquine(ev.Mob.Type) {
		// vanilla: AbstractHorse.causeFallDamage plays no generic fall sound.
		fx.sends = append(fx.sends, s.horseLandedSends(w, ev.Mob, ev.Fall, ev.Damage, ev.Damage > 0, ev.Softened, ev.DamageType)...)
		return
	}
	// Fall hurt: getFallDamageSound's sound by damage, and playBlockFallSound
	// beneath, both ungated. A killing fall's hurt sound goes beside MobDied's
	// death sound; the damage event and hurt sound go only with a whole fall,
	// a softened one told by health alone, like MobBurned.
	kind := rules.FallGeneric
	if entity.Enemy(ev.Mob.Type) {
		kind = rules.FallHostile
	}
	fx.voiceID(w, ev.Mob, rules.FallSound(ev.Damage, kind), 1.0, 1.0)
	if fall, volume, pitch, ok := blockFallSound(w, ev.Mob.X, ev.Mob.Y, ev.Mob.Z); ok {
		fx.voiceID(w, ev.Mob, fall, volume, pitch)
	}
	if !ev.Softened {
		fx.sends = append(fx.sends, mobHurtTold(w, ev.Mob, ev.DamageType)...)
	}
}

// bodyGameEvent posts the splash (with its sound), flap or bounce game event
// of a mob.
func (t *mobDimTick) bodyGameEvent(fx *mobEffects, ev entity.MobEvent) {
	m := ev.Mob
	event := rules.GameEventFlap
	switch ev.Kind {
	case entity.MobSplashed:
		event = rules.GameEventSplash
		fx.voiceID(t.w, m, ev.SoundID, ev.Volume, ev.Pitch)
	case entity.MobBouncedOff:
		event = rules.GameEventBounce
	}
	t.gameEvent(fx, m, event, rules.GameEventContext{})
}

// gameEvent posts a game event at m with m as its source, unless an earlier
// event this tick already took m off the books (a bee into its hive, a parrot
// onto a shoulder).
func (t *mobDimTick) gameEvent(fx *mobEffects, m *entity.Mob, event rules.GameEvent, ctx rules.GameEventContext) {
	ent, ok := t.h.book[m]
	if !ok {
		return
	}
	ctx.Mob = ent.uuid
	fx.gameEvent(t.w, event, m.X, m.Y, m.Z, ctx)
}

// sounded plays a MobSounded in the mob's voice, or a MobBounced's block
// sound at the block's centre, which no silent flag mutes.
func (t *mobDimTick) sounded(fx *mobEffects, ev entity.MobEvent) {
	if ev.Kind == entity.MobSounded {
		if ev.Unmuted {
			sound := ev.SoundID.Holder()
			if ev.SoundHolder != nil {
				sound = *ev.SoundHolder
			}
			m := ev.Mob
			x, y, z := m.X, m.Y, m.Z
			if ev.Centred {
				bx, by, bz := blockOf(x, y, z)
				x, y, z = float64(bx)+0.5, float64(by)+0.5, float64(bz)+0.5
			}
			send := mobVoicePacket(t.w, x, y, z,
				soundEffectHolderAt(sound, mobSoundCategory(m.Type), x, y, z, ev.Volume, ev.Pitch), ev.Volume)
			send.radius = soundHolderRange(sound, ev.Volume)
			fx.sends = append(fx.sends, send)
		} else if ev.SoundHolder != nil {
			fx.voiceHolder(t.w, ev.Mob, *ev.SoundHolder, ev.Volume, ev.Pitch)
		} else {
			fx.voiceID(t.w, ev.Mob, ev.SoundID, ev.Volume, ev.Pitch)
		}
		return
	}
	x, y, z := ev.X+0.5, ev.Y+0.5, ev.Z+0.5
	fx.sends = append(fx.sends, mobSend{w: t.w, id: protocol.IDPlaySoundEffect,
		data: soundEffectIDAt(ev.SoundID, protocol.SoundCategoryBlock, x, y, z, 1, 1),
		near: true, x: x, y: y, z: z, radius: soundRange(1)})
}

// endermanTeleported tells an enderman's teleport.
func (t *mobDimTick) endermanTeleported(fx *mobEffects, ev entity.MobEvent) {
	w := t.w
	// Sound at the start, then at the destination, and the portal particles.
	// vanilla: EnderMan.teleport
	if !ev.Mob.Silent {
		x, y, z := blockOf(ev.X, ev.Y, ev.Z)
		tx, ty, tz := blockOf(ev.VX, ev.VY, ev.VZ)
		fx.levelEvent(w, protocol.WorldEventEndermanTeleport, x, y, z, protocol.ClampedPackPosDiff(x, y, z, tx, ty, tz))
		const teleportSound = registry.SoundEntityEndermanTeleport
		fx.sends = append(fx.sends, mobSend{
			w: w, id: protocol.IDPlaySoundEffect,
			data: soundEffectIDAt(teleportSound, mobSoundCategory(ev.Mob.Type), ev.X, ev.Y, ev.Z, 1, 1),
			near: true, x: ev.X, y: ev.Y, z: ev.Z, radius: soundRange(1),
		})
	}
	fx.gameEvent(w, rules.GameEventTeleport, ev.X, ev.Y, ev.Z, rules.GameEventContext{Mob: t.h.MobUUID(ev.Mob)})
	fx.voiceID(w, ev.Mob, registry.SoundEntityEndermanTeleport, 1, 1)
	fx.entityEvent(w, ev.Mob.ID, protocol.EntityStatusTeleportParticles)
}

// bred queues a birth, the parents' love causes taken under the mob lock.
func (t *mobDimTick) bred(fx *mobEffects, ev entity.MobEvent) {
	h, w := t.h, t.w
	// The goal already did the parents' cooldown and love's end; place and
	// dice are read under the lock, as only the tick moves a mob meanwhile.
	var cause uuid.UUID
	var parentCauses [2]uuid.UUID
	for i, parent := range []*entity.Mob{ev.Mob, ev.Partner} {
		if ent := h.book[parent]; ent != nil {
			parentCauses[i] = ent.loveCause
			if cause == (uuid.UUID{}) {
				cause = ent.loveCause
			}
		}
	}
	fx.births = append(fx.births, mobBirth{
		w:            w,
		mother:       ev.Mob,
		partner:      ev.Partner,
		motherID:     t.h.MobUUID(ev.Mob),
		partnerID:    t.h.MobUUID(ev.Partner),
		cause:        cause,
		parentCauses: parentCauses,
		x:            ev.Mob.X, y: ev.Mob.Y, z: ev.Mob.Z,
		amount: ev.Amount,
	})
}

// sculkStep queues a body stepping on a sculk sensor, unless it is already
// off the books.
func (t *mobDimTick) sculkStep(fx *mobEffects, ev entity.MobEvent) {
	ent, ok := t.h.book[ev.Mob]
	if !ok {
		return
	}
	fx.sculkSteps = append(fx.sculkSteps, mobSculkStep{w: t.w, id: ent.uuid,
		x: int32(ev.X), y: int32(ev.Y), z: int32(ev.Z), at: [3]float64{ev.Mob.X, ev.Mob.Y, ev.Mob.Z}})
}

// chickenLaid rolls the hen's gift before queuing the drop and sound.
// vanilla: Chicken.aiStep
func (t *mobDimTick) chickenLaid(fx *mobEffects, m *entity.Mob) {
	ctx := rules.LootContext{This: mobLootEntity(m), Level: t.s.lootLevelAt(t.w, m.X, m.Y, m.Z)}
	if stacks := rules.RollLoot("minecraft:gameplay/chicken_lay", &ctx, t.s.lootRng); len(stacks) > 0 {
		fx.eggs = append(fx.eggs, mobEgg{w: t.w, x: m.X, y: m.Y, z: m.Z, stacks: stacks, silent: m.Silent, hen: t.h.book[m].uuid,
			pitch: (m.Rand.Float32()-m.Rand.Float32())*0.2 + 1.0})
	}
}

// turtleGrew drops the turtle_grow gift while MOB_DROPS allows it.
// vanilla: Turtle.ageBoundaryReached
func (t *mobDimTick) turtleGrew(fx *mobEffects, m *entity.Mob) {
	if !t.w.BoolRule(world.RuleMobDrops) {
		return
	}
	ctx := rules.LootContext{This: mobLootEntity(m), Level: t.s.lootLevelAt(t.w, m.X, m.Y, m.Z)}
	fx.deaths = append(fx.deaths, mobDrops{
		w:      t.w,
		stacks: rules.RollLoot("minecraft:gameplay/turtle_grow", &ctx, t.s.lootRng),
		by:     t.h.MobUUID(m),
		x:      m.X, y: m.Y, z: m.Z,
	})
}

// particles sends a MobBubbleSurface's splash and bubbles or a MobSparkled's
// one particle to the players in range.
func (t *mobDimTick) particles(fx *mobEffects, ev entity.MobEvent) {
	if ev.Kind == entity.MobBubbleSurface {
		for _, data := range t.s.bubbleColumnParticles(int32(ev.X), int32(ev.Y), int32(ev.Z)) {
			fx.sends = append(fx.sends, mobSend{w: t.w, id: protocol.IDPlayLevelParticles, data: data,
				near: true, x: ev.X + 0.5, y: ev.Y + 1, z: ev.Z + 0.5, radius: particleRange})
		}
		return
	}
	rnd := particleRandomDefault
	if ev.Alternative {
		rnd = particleRandomAlternative
	}
	fx.sends = append(fx.sends, mobSend{w: t.w, id: protocol.IDPlayLevelParticles,
		data: motionParticlesPayload(ev.Status, ev.X, ev.Y, ev.Z, 0, 0, 0,
			float32(ev.VX), float32(ev.VY), float32(ev.VZ), 1, rnd),
		near: true, x: ev.X, y: ev.Y, z: ev.Z, radius: particleRange})
}

// seatEvent queues the passenger list a seat change leaves: a mob that took a
// rider, or a rider thrown off by water.
func (t *mobDimTick) seatEvent(fx *mobEffects, ev entity.MobEvent) {
	if ev.Kind == entity.MobSeated {
		fx.mountNews = append(fx.mountNews, seatNews{w: t.w, vehicle: ev.Mob.ID, ids: ev.Mob.PassengerIDs()})
	} else if seat := dismountSeat(t.h.book[ev.Mob]); seat != nil {
		fx.mountNews = append(fx.mountNews, *seat)
	}
}

// axolotlSupported queues the player an axolotl is supporting, applied outside the mob lock.
func (t *mobDimTick) axolotlSupported(fx *mobEffects, in *mobTickInputs, ev entity.MobEvent) {
	if c := in.clientByEntityID[ev.Player.EntityID]; c != nil {
		fx.axolotlSupport = append(fx.axolotlSupport, c)
	}
}

// mountGameEvents resolves rider identities before removal events forget them.
func (t *mobDimTick) mountGameEvents(fx *mobEffects, events []entity.MobEvent) {
	for i := range events {
		ev := &events[i]
		if ev.Kind != entity.MobMounted && ev.Kind != entity.MobDismounted {
			continue
		}
		if ent := t.h.book[ev.Mob]; ent != nil {
			event := rules.GameEventEntityMount
			if ev.Kind == entity.MobDismounted {
				event = rules.GameEventEntityDismount
			}
			fx.gameEvent(t.w, event, ev.X, ev.Y, ev.Z, rules.GameEventContext{Mob: ent.uuid})
		}
	}
}

// landingDust is the block particle burst of a landing past the safe distance.
func landingDust(w *world.World, ev entity.MobEvent) mobSend {
	return mobSend{w: w, id: protocol.IDPlayLevelParticles,
		data: blockParticlesPayload(protocol.ParticleBlock, ev.BlockState, ev.X, ev.Y, ev.Z, 0, 0, 0, 0.15, ev.Count),
		near: true, x: ev.X, y: ev.Y, z: ev.Z, radius: particleRange}
}

// killingBlow is the blow of the player k that killed m, read under the mob
// lock: a projectile of the type the mob last took (an arrow, a trident or a
// thrown thing), else the player's swing. The swing's damage type is the
// player attack, as the weapon is no longer known.
// vanilla: LivingEntity.die, DamageSource
func killingBlow(k *client, m *entity.Mob) *advancement.Blow {
	player := k.lootEntityLocked()
	attacker := func() rules.LootEntity { return player }
	if direct := m.BlowDirectType(); m.HurtByPlayer() && direct != "" {
		typ := registry.DamageTypeArrow
		if id, _ := registry.EntityTypeID(direct); registry.EntityType(id) == registry.EntityTypeTrident {
			typ = registry.DamageTypeTrident
		}
		thrown := rules.LootEntity{Type: direct}
		return &advancement.Blow{Type: typ, Attacker: attacker, Direct: func() rules.LootEntity { return thrown }, Indirect: true}
	}
	return &advancement.Blow{Type: rules.DamageTypePlayerAttack, Attacker: attacker, Direct: attacker}
}
