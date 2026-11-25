package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// mobEffects is what a mob tick leaves to do after the mob lock: collected
// under it and applied by apply. A new behaviour adds a kind here and a
// place in apply.
type mobEffects struct {
	// moves are the mobs that moved this tick while a plugin listens.
	moves        []mobMove
	dragonDeaths []dragonDeathEffect
	// gateways are the mobs found in an end gateway.
	gateways []gatewayTouch
	// raiderDeaths are raiders that died this tick, removed from their raids
	// after the mob lock.
	raiderDeaths []raiderDeath
	// releases are mobs whose boat seat ended this tick (dead, despawned or
	// converted), for the boat lock's phase after the herd's.
	releases []mobSeatRelease
	// The four kinds' arrivals from woken chunks and departures from unloading
	// ones, gathered before the mob lock (each sweep under its own kind's lock,
	// in turn) and handled after.
	loaded     wakeResult
	departures []departure
	travels    []mobPortalTravel
	// written is the chunks the unload write covered: a leaver there is gone,
	// any other stays.
	written map[*world.World]map[world.ChunkPos]struct{}
	// births are the breedings due, spawned after the mob lock.
	births []mobBirth
	sends  []mobSend
	// horseEvents are the tamings and the throws of the tick.
	horseEvents []horseNews
	inks        []mobInk
	turtleEggs  []turtleEgg
	stompedEggs []turtleEgg
	frogspawns  []frogspawn
	eggs        []mobEgg
	// leashDrops are leads the tick removed, told and dropped after the mob
	// lock.
	leashDrops []mobLeashDrop
	deaths     []mobDrops
	// deathNotes are the deaths EntityDeathEvent fires for, indexing deaths and xpDrops.
	deathNotes []mobDeathNote
	// scoreDrops are the scoreboard names (UUID strings) of mobs removed this
	// tick (a killed one when its corpse goes), reset after the removal packets.
	scoreDrops []goneMob
	// petDeaths are the death messages owed to a dead tame mob's owner.
	petDeaths   []petDeath
	xpDrops     []mobXpDrop
	strikes     []mobStrike
	contacts    []mobContact
	blazeShots  []blazeShot
	breezeShots []blazeShot
	spits       []mobProjectileShot
	snowballs   []mobProjectileShot
	ghastShots  []ghastShot
	standRams   []standRam
	dragonShots []ghastShot
	// witherSpawns are the summonings' global sounds.
	witherSpawns []witherSpawnSound
	witherShots  []witherShot
	witherBars   []witherBarView
	darkness     []wardenDarkness
	vibrations   []wardenVibrationSend
	gameEvents   []mobGameEvent
	curses       []elderCurse
	horns        []goatHorn

	// axolotlSupport is applied after combat, outside the mob lock.
	axolotlSupport []*client
	dolphinGrace   []*client
	blinded        []*client

	// tradeDraws can load destination chunks and run before offer resends.
	tradeDraws []mobTradeDraw
	// resends are the mobs whose open trade window needs fresh offers.
	resends    []*mobEntity
	throws     []piglinThrow
	tramples   []mobTrample
	grazes     []mobGraze
	raids      []mobGraze
	berryPicks []mobBerryPick
	snowMelts  []mobGraze
	// blockTakes are the blocks endermen picked up and set down.
	blockTakes []mobBlockMove
	farming    []mobFarm
	// breaks are the blocks mobs broke with their bodies.
	breaks        []mobBlockBreak
	smashes       []witherSmash
	roses         []witherRose
	acts          []mobAct
	golems        []mobGolemRequest
	shoulders     []mobShoulder
	doorOps       []mobDoorOp
	bedOps        []mobBedOp
	cauldrons     []mobGraze
	located       []mobLocated
	sculkSteps    []mobSculkStep
	oreTouches    []mobGraze
	snowTrail     []mobGraze
	infests       []mobInfest
	blasts        []blast
	creeperClouds []creeperCloud
	shots         []mobShot
	bullets       []shulkerShot
	copies        []shulkerShot
	shoves        []bodyShove
	potions       []mobPotionThrow
	// removals are the entity IDs each world's players take out of view.
	removals map[*world.World][]int32
	// mountNews are mob vehicles a crossing body left, told after the mob lock.
	mountNews  []seatNews
	splits     []cubeSplit
	converts   []mobConversion
	traps      []skeletonTrap
	statues    []copperStatue
	chestLids  []chestLid
	chestSyncs []chestLid
	hiveStays  []hiveStay
	cropGrowth []cropGrow
	calls      []reinforcementCall
	fangCasts  []fangsCast
	vexCalls   []vexSummon
	triggers   []mobEffectTrigger

	// villagerSignals ring bells and launch celebration rockets after the mob lock.
	villagerSignals []villagerSignal

	// creakingHurts are the blows on heart-bound creakings, answered by their
	// hearts.
	creakingHurts []creakingHurtCall
}

// newMobEffects is an empty collector for a tick over s's dimensions.
func newMobEffects(s *Server) mobEffects {
	return mobEffects{
		removals: make(map[*world.World][]int32),
		written:  make(map[*world.World]map[world.ChunkPos]struct{}, len(s.allDims())),
	}
}

// mobGameEvent is a game event posted after the mob lock, since the warden
// listener pass takes that lock.
type mobGameEvent struct {
	w       *world.World
	event   rules.GameEvent
	x, y, z float64
	context rules.GameEventContext
}

// gameEvent queues an event for after the mob lock, since the warden listener
// pass takes that lock.
func (fx *mobEffects) gameEvent(w *world.World, event rules.GameEvent, x, y, z float64, context rules.GameEventContext) {
	fx.gameEvents = append(fx.gameEvents, mobGameEvent{w: w, event: event, x: x, y: y, z: z, context: context})
}

// voiceHolder queues a resolved voice after the mob lock.
func (fx *mobEffects) voiceHolder(w *world.World, m *entity.Mob, sound protocol.SoundHolder, volume, pitch float32) {
	if send, ok := mobVoiceSend(w, m, sound, volume, pitch); ok {
		fx.sends = append(fx.sends, send)
	}
}

func (fx *mobEffects) voiceID(w *world.World, m *entity.Mob, sound registry.Sound, volume, pitch float32) {
	if send, ok := mobVoiceIDSend(w, m, sound, volume, pitch); ok {
		fx.sends = append(fx.sends, send)
	}
}

// entityEvent queues an entity event to the whole of w.
func (fx *mobEffects) entityEvent(w *world.World, id int32, status uint8) {
	fx.sends = append(fx.sends, mobSend{w: w, id: protocol.IDPlayEntityEvent,
		data: entityEventPayload(id, status)})
}

// levelEvent queues a World Event to the players within levelEventRange.
func (fx *mobEffects) levelEvent(w *world.World, event, x, y, z, data int32) {
	fx.sends = append(fx.sends, mobSend{w: w, id: protocol.IDPlayWorldEvent,
		data: worldEvent(event, x, y, z, data),
		near: true, x: float64(x), y: float64(y), z: float64(z), radius: levelEventRange})
}

// wardenVibrationSend is one vibration particle addressed to a moving warden.
type wardenVibrationSend struct {
	w        *world.World
	id       int32
	particle *entity.WardenVibrationParticle
	eye      float64
}

// wardenVibrationQueue queues the particle, whose destination follows the
// warden by entity ID rather than a fixed block.
func (fx *mobEffects) wardenVibrationQueue(send wardenVibrationSend) {
	const id = protocol.ParticleVibration
	var data protocol.Buffer
	// The destination is an entity position source (type 1), the entity ID and
	// the eye-height offset its constructor chooses.
	data.PutVarInt(1)
	data.PutVarInt(send.id)
	data.PutF32(float32(send.eye))
	data.PutVarInt(int32(send.particle.ArrivalTicks))
	fx.sends = append(fx.sends, mobSend{w: send.w, id: protocol.IDPlayLevelParticles, data: particleDataPayload(
		id, data.Bytes(), false, send.particle.X, send.particle.Y, send.particle.Z, 0, 0, 0, 0, 1),
		near: true, x: send.particle.X, y: send.particle.Y, z: send.particle.Z, radius: particleRange})
}

// mobSeatRelease ends a vehicle link after the mob lock is released.
type mobSeatRelease struct {
	ent          *mobEntity
	keepPosition bool
}

func (s *Server) releaseMobSeat(release mobSeatRelease) {
	s.removeMobFromBoat(release.ent, !release.keepPosition)
	s.removeMobFromMinecart(release.ent, !release.keepPosition)
}

// remove queues id's removal from the view of w's players.
func (fx *mobEffects) remove(w *world.World, id int32) {
	fx.removals[w] = append(fx.removals[w], id)
}

// release queues the end of a leaving body's boat or cart seat, if any.
func (fx *mobEffects) release(ent *mobEntity) {
	if ent.vehicle != nil || ent.cart != nil {
		fx.releases = append(fx.releases, mobSeatRelease{ent: ent})
	}
}

// convert defers removal's seat release until the SINGLE replacement takes over.
// Caller holds mobsMu and has removed the old body from the herd's books.
func (fx *mobEffects) convert(cv mobConversion) {
	cv.old.converting = true
	fx.remove(cv.w, cv.old.mob.ID)
	fx.scoreDrops = append(fx.scoreDrops, goneMob{cv.old.uuid, plugin.RemoveTransform})
	fx.converts = append(fx.converts, cv)
}

// apply runs the effects with the mob lock released, kind by kind and each
// kind in collection order. That is the order packets and world changes
// reach clients, which the black-box tests read and vanilla fixes, so a new
// kind goes where its packets belong, not at the end.
func (fx *mobEffects) apply(s *Server, in *mobTickInputs) {
	s.rollDeathLoot(fx.deaths)
	applyEach(s, fx.gameEvents, func(s *Server, event mobGameEvent) {
		s.gameEvent(event.w, event.event, event.x, event.y, event.z, event.context)
	})
	applyEach(s, fx.villagerSignals, (*Server).applyVillagerSignal)
	applyEach(s, fx.raiderDeaths, (*Server).finishRaiderDeath)
	// Bodies whose seat ended leave their hull's list, under the boat lock
	// alone.
	applyEach(s, fx.releases, (*Server).releaseMobSeat)
	// Arrivals first: registered, shown, and owner references resolved in
	// their new world so a skeleton and its saved arrow find each other.
	s.registerWake(fx.loaded)
	s.touchGateways(fx.gateways)
	// Portal crossings arrive as new bodies (new entity ID, same UUID).
	applyEach(s, fx.travels, (*Server).finishMobTravel)
	// The leavers last.
	s.settleDepartures(fx.departures, fx.written)
	applyEach(s, fx.births, (*Server).finishBreeding)
	applyEach(s, fx.vibrations, func(_ *Server, send wardenVibrationSend) {
		fx.wardenVibrationQueue(send)
	})
	s.sendMobSends(fx.sends)
	s.finishHorseEvents(fx.horseEvents)
	// Not an effect: the mounts pass, ending seats of mounts the step killed or
	// removed, belongs here.
	s.tickMounts()
	applyEach(s, fx.inks, (*Server).sendMobInk)
	applyEach(s, fx.turtleEggs, (*Server).layTurtleEgg)
	applyEach(s, fx.stompedEggs, (*Server).removeStompedEgg)
	applyEach(s, fx.frogspawns, (*Server).layFrogspawn)
	applyEach(s, fx.eggs, (*Server).spawnEgg)
	applyEach(s, fx.leashDrops, (*Server).finishMobLeashDrop)
	applyEach(s, fx.petDeaths, (*Server).tellPetDeath)
	s.fireMobDeaths(fx)
	s.fireMobMoves(fx.moves)
	applyEach(s, fx.deaths, (*Server).scatterDrops)
	// The board's listener broadcasts, so this runs after the mob lock.
	for _, gone := range fx.scoreDrops {
		name := gone.id.String()
		s.scoreboard.EntityRemoved(name)
		s.dropEntityData(name)
		s.fireEntityRemove(gone.id, gone.cause)
	}
	applyEach(s, fx.xpDrops, (*Server).spawnMobXp)
	applyEach(s, fx.dragonDeaths, (*Server).applyDragonDeath)
	applyEach(s, fx.strikes, func(s *Server, strike mobStrike) { s.landingBlow(strike, in.inWorld[strike.w]) })
	applyEach(s, fx.contacts, func(s *Server, hurt mobContact) {
		if c := in.clientByEntityID[hurt.victimID]; c != nil && c.theWorld() == hurt.w {
			s.sulfurBurnPlayer(c, hurt)
		}
	})
	applyEach(s, fx.blazeShots, (*Server).blazeShoot)
	applyEach(s, fx.breezeShots, (*Server).breezeShoot)
	applyEach(s, fx.ghastShots, (*Server).ghastShoot)
	applyEach(s, fx.standRams, (*Server).ramStand)
	applyEach(s, fx.dragonShots, (*Server).dragonEffect)
	applyEach(s, fx.witherShots, (*Server).witherShoot)
	applyEach(s, fx.witherSpawns, (*Server).tellWitherSpawn)
	applyEach(s, fx.spits, (*Server).spitShoot)
	applyEach(s, fx.snowballs, (*Server).snowballShoot)
	applyEach(s, fx.creakingHurts, (*Server).creakingHurtHeart)
	for _, c := range fx.dolphinGrace {
		c.applyDolphinGrace()
	}
	for _, c := range fx.blinded {
		c.applyBlindness()
	}
	for _, c := range fx.axolotlSupport {
		c.applyAxolotlSupport()
	}
	applyEach(s, fx.curses, func(s *Server, curse elderCurse) { s.elderGuardianCurse(curse, in.inWorld[curse.w]) })
	applyEach(s, fx.darkness, (*Server).applyWardenDarkness)
	applyEach(s, fx.horns, (*Server).dropGoatHorn)
	applyEach(s, fx.throws, (*Server).throwPiglinItem)
	applyEach(s, fx.tradeDraws, (*Server).finishTradeDraw)
	applyEach(s, fx.resends, (*Server).resendMerchantOffers)
	applyEach(s, fx.tramples, (*Server).mobTrampled)
	applyEach(s, fx.grazes, (*Server).mobGrazedGround)
	applyEach(s, fx.raids, (*Server).mobRaidedCrop)
	applyEach(s, fx.berryPicks, (*Server).mobPickedBerries)
	applyEach(s, fx.snowMelts, (*Server).mobMeltedSnow)
	applyEach(s, fx.blockTakes, (*Server).mobMovedBlock)
	applyEach(s, fx.farming, (*Server).mobFarmed)
	applyEach(s, fx.breaks, func(s *Server, b mobBlockBreak) { s.mobBrokeBlock(b) })
	applyEach(s, fx.smashes, (*Server).witherSmashed)
	applyEach(s, fx.roses, (*Server).witherRoseLeft)
	applyEach(s, fx.acts, (*Server).mobActed)
	applyEach(s, fx.golems, (*Server).spawnVillageGolem)
	applyEach(s, fx.shoulders, (*Server).perch)
	applyEach(s, fx.doorOps, (*Server).mobDoorWork)
	applyEach(s, fx.bedOps, (*Server).mobBedWork)
	applyEach(s, fx.cauldrons, (*Server).mobDousedCauldron)
	var disks []mobDisk
	for _, l := range fx.located {
		disks = append(disks, s.mobLocated(l)...)
	}
	s.applyDisks(disks)
	applyEach(s, fx.sculkSteps, func(s *Server, st mobSculkStep) { s.mobSteppedOnSculk(st) })
	applyEach(s, fx.oreTouches, func(s *Server, g mobGraze) { s.lightRedstoneOre(g.w, g.x, g.y, g.z) })
	applyEach(s, fx.snowTrail, (*Server).mobLaidSnow)
	applyEach(s, fx.infests, (*Server).finishInfest)
	applyEach(s, fx.blasts, (*Server).runExplosion)
	applyEach(s, fx.creeperClouds, (*Server).spawnCreeperCloud)
	applyEach(s, fx.shots, (*Server).spawnMobShot)
	applyEach(s, fx.bullets, (*Server).spawnShulkerBullet)
	applyEach(s, fx.copies, (*Server).spawnShulkerCopy)
	applyEach(s, fx.shoves, (*Server).shoveBodiesBy)
	applyEach(s, fx.potions, (*Server).spawnMobPotion)
	s.showWitherBars(fx.witherBars)
	fx.tellRemovals(s)
	s.sendSeatNews(fx.mountNews)
	applyEach(s, s.fireSlimeSplits(fx.splits), (*Server).spawnSplitCube)
	applyEach(s, fx.converts, (*Server).finishConversion)
	applyEach(s, fx.hiveStays, (*Server).storeHiveBee)
	applyEach(s, fx.cropGrowth, (*Server).growBeeCrop)
	applyEach(s, fx.calls, (*Server).finishReinforcement)
	applyEach(s, fx.traps, (*Server).springSkeletonTrap)
	applyEach(s, fx.statues, (*Server).placeCopperStatue)
	applyEach(s, fx.chestLids, (*Server).applyChestLid)
	applyEach(s, fx.chestSyncs, (*Server).syncGolemChest)
	applyEach(s, fx.fangCasts, (*Server).spawnFangs)
	applyEach(s, fx.vexCalls, (*Server).summonVex)
	// Death effects fire last, after the splits, so the cramming cap counts a
	// cube's smaller selves.
	applyEach(s, fx.triggers, (*Server).fireMobEffectTrigger)
}

// applyEach applies one kind of effect, in the order collected.
func applyEach[T any](s *Server, effects []T, apply func(*Server, T)) {
	for _, e := range effects {
		apply(s, e)
	}
}

// tellRemovals sends each world's players one removal packet, worlds in
// dimension order.
func (fx *mobEffects) tellRemovals(s *Server) {
	for _, dim := range s.allDims() {
		if ids, ok := fx.removals[dim.w]; ok {
			s.broadcastWorld(dim.w, protocol.IDPlayRemoveEntities, removeEntitiesPayload(ids))
		}
	}
}

// petDeath is a dead tame mob's death message and its owner.
type petDeath struct {
	owner   uuid.UUID
	message protocol.Text
}

// tellPetDeath sends the message to the owner if they are online.
// vanilla: TamableAnimal.die
func (s *Server) tellPetDeath(d petDeath) {
	if c := s.clientByUUID(d.owner); c != nil {
		c.send(protocol.IDPlaySystemChat, systemChatPayload(d.message, false))
	}
}
