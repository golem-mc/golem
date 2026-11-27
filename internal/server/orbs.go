package server

import (
	"log/slog"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// Server-side state for the experience orbs, kept and shown like the item
// entities and the arrows. The orb itself (float, follow, merging, lifetime)
// is entity.Orb; the server holds the UUID, entity ID, dimension,
// per-observer tracking, and the experience a pickup hands its collector.
// The orb lock sits beneath the player list's and beside the mobs' — the orb
// tick reads the players' places before any lock — and the sends, pickups
// and experience run after the locks are down.

// orbEntity is one orb in a world. The orb itself is entity.Orb; the
// server holds the dimension, UUID and tracking.
type orbEntity struct {
	orb  *entity.Orb
	w    *world.World
	uuid uuid.UUID

	bodyTrack[struct{}]

	// portal is the orb's portal state, shared with every other body of
	// vanilla's Entity.
	portal rules.EntityPortal

	// tags are the scoreboard tags, what the tag selector option matches
	// and /tag edits. Guarded by the orb lock, like the rest of an orb's
	// own fields.
	tags nbt.StringSet
	// custom is the custom name, guarded like tags.
	custom customName
	look   entityLook
}

// orbTracker tracks the orbs. An orb is never told on the ground, and its
// Teleport Entity carries no velocity: the motion packets do.
var orbTracker = trackedKind[*orbEntity, struct{}]{
	invalidatePiston: func(dim *dimension) { dim.pistons.orbs.built = false },
	pistonBit:        pistonOrbs,

	rangeChunks: entity.OrbTracking,
	interval:    entity.OrbInterval,
	lock:        func(s *Server) *sync.Mutex { return &s.orbsMu },
	list:        func(s *Server) *[]*orbEntity { return &s.orbs },
	chunks:      func(s *Server) *entity.ChunkIndex[*orbEntity] { return &s.orbChunks },
	filing:      func(it *orbEntity) *entity.Filing { return &it.orb.Filing },
	uuid:        func(it *orbEntity) uuid.UUID { return it.uuid },
	track:       func(it *orbEntity) *bodyTrack[struct{}] { return &it.bodyTrack },
	world:       func(it *orbEntity) *world.World { return it.w },
	id:          func(it *orbEntity) int32 { return it.orb.ID },
	dead:        func(it *orbEntity) bool { return it.orb.Dead },
	pose:        func(it *orbEntity) bodyPose { return bodyPose{x: it.orb.X, y: it.orb.Y, z: it.orb.Z} },
	pairing:     orbPairing,
}

// orbWatcher is one player as the orb passes read them: the client and its
// orb's-eye view.
type orbWatcher struct {
	c    *client
	view entity.OrbPlayer
}

// orbWatchers reads where every player stands and what each may take this
// tick, grouped by the world each is in, before any orb lock is taken: a
// position belongs to the player's lock.
func orbWatchers(clients []*client) map[*world.World][]orbWatcher {
	watchers := make(map[*world.World][]orbWatcher)
	for _, c := range clients {
		if c.player == nil {
			continue
		}
		pos := c.player.Position()
		vitals := c.player.Vitals()
		spectator := vitals.GameMode == protocol.GameModeSpectator
		w := c.theWorld()
		watchers[w] = append(watchers[w], orbWatcher{
			c: c,
			view: entity.OrbPlayer{
				X: pos.X, Y: pos.Y, Z: pos.Z,
				EntityID:  c.player.EntityID,
				Spectator: spectator,
				Dead:      vitals.Dead,
				Reach:     c.pickupBox(),
				EyeHeight: c.dims(c.player.Appearance().Pose).EyeHeight,
				TakeReady: !spectator && !vitals.Dead && c.takeXpDelay.Load() == 0,
			},
		})
	}
	return watchers
}

// refuseFurtherTakes clears TakeReady on the collector's view: the touch sets
// takeXpDelay inside itself, so no later orb this tick may take from them.
// vanilla: ExperienceOrb.playerTouch
func refuseFurtherTakes(views []entity.OrbPlayer, collector int32) {
	for i := range views {
		if views[i].EntityID == collector {
			views[i].TakeReady = false
		}
	}
}

// orbViews projects the watcher read into the orb's-eye half, for the passes
// that need only that.
func orbViews(watchers map[*world.World][]orbWatcher) map[*world.World][]entity.OrbPlayer {
	views := make(map[*world.World][]entity.OrbPlayer, len(watchers))
	for w, ws := range watchers {
		for _, watcher := range ws {
			views[w] = append(views[w], watcher.view)
		}
	}
	return views
}

// tickOrbs runs one game tick of every orb: positions read first with
// nothing held; physics, follow and merges run under the orb lock alone;
// the pickups, which need a player's vitals, run last.
func (s *Server) tickOrbs() {
	s.lockTick(&s.orbsMu)
	none := len(s.orbs) == 0
	s.unlockTick(&s.orbsMu)
	if none {
		return
	}
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()

	// Where everyone is and what each may take this tick, read before the
	// orb lock: a position belongs to the player's lock.
	watchers := orbWatchers(clients)
	near := nearbyWorlds(watchers, func(w orbWatcher) trackingWatcher {
		return trackingWatcher{c: w.c, x: w.view.X, z: w.view.Z, chunkRadius: w.c.chunkRadius()}
	})
	defer releaseNearbyWorlds(near)

	// orbSound is one packet the fire's or the water's say-so produced,
	// collected under the orb lock with the world it belongs to.
	type orbSound struct {
		w *world.World
		outbound
	}

	var (
		out    trackSends
		sounds []orbSound
		flakes []snowflakeSpot
		ores   []cellSpot
		// removals is the orbs the tick itself ended, per world.
		removals = make(map[*world.World][]int32)
		takes    []orbTake
		// stepped is the orbs the tick ran; an orb a neighbour's step
		// absorbed this tick is dead without an event of its own.
		stepped []*orbEntity
		// announced is the orbs whose death a packet is already on its
		// way for, so the second pass does not send the removal twice.
		announced    = make(map[*orbEntity]bool)
		splashes     []projectileSplash
		bodyLandings []bodyLanding
	)

	// Read solid bodies before taking the orb lock.
	colliders := s.collidersByWorld(orbCollisions)
	gate := s.newEntityGate()
	var held []*orbEntity
	defer func() { orbTracker.dormantPass(s, held) }()
	s.lockTick(&s.orbsMu)
	// The portal pass runs ahead of the tick, as for items: a body the
	// portal is sending across leaves the list here.
	type orbPortalTravel struct {
		it   *orbEntity
		kind rules.PortalKind
	}
	var travels []orbPortalTravel
	var gates []gatewayTouch
	portalAlive := s.orbs[:0]
	for _, it := range s.orbs {
		if it.orb.Dead {
			orbTracker.unfile(it)
			continue
		}
		state, kind, due := gate.stepPortal(it.w, it.orb.X, it.orb.Y, it.orb.Z, &it.portal, portalCooldownTicks)
		s.notePortalTouch(nil, it.uuid, it.w, it.orb.X, it.orb.Y, it.orb.Z, state)
		if !due {
			if state == rules.EndGatewayState {
				gatewayTouchAt(&gates, it.w, it.orb.X, it.orb.Y, it.orb.Z, gatewayTouch{
					mu: &s.orbsMu, portal: &it.portal, cooldown: portalCooldownTicks,
					move: func(to player.Position) { s.gatewayMoveOrb(it, to) },
				})
			}
			portalAlive = append(portalAlive, it)
			continue
		}
		orbTracker.unfile(it)
		travels = append(travels, orbPortalTravel{it: it, kind: kind})
	}
	s.orbs = portalAlive

	// The orbs alive as the tick began, copied out before anything is
	// written back: a neighbour a scan absorbs this tick stays in the
	// list.
	// The orbs alive as the tick began, and each world's of them in a grid
	// for the merge scan (which skips the orb itself and the dead): a
	// neighbour a scan absorbs this tick stays filed. slot is each live
	// orb's index in its grid.
	live := make([]*orbEntity, 0, len(s.orbs))
	slot := make([]int, 0, len(s.orbs))
	orbsIn := make(map[*world.World]*bodyNear[*entity.Orb])
	for _, it := range s.orbs {
		if it.orb.Dead {
			continue
		}
		n := orbsIn[it.w]
		if n == nil {
			n = newBodyNear(entity.OrbMergeReach, func(o *entity.Orb) (float64, float64) { return o.X, o.Z })
			orbsIn[it.w] = n
		}
		live = append(live, it)
		slot = append(slot, n.add(it.orb))
	}
	defer func() {
		for _, n := range orbsIn {
			n.release()
		}
	}()
	views := orbViews(watchers)
	alive := s.orbs[:0]
	for k, it := range live {
		if !gate.ticks(it.w, it.orb.X, it.orb.Z) {
			held = append(held, it)
			alive = append(alive, it)
			continue
		}
		players, neighbours := views[it.w], orbsIn[it.w]

		events := it.orb.Step(colliders[it.w], players, neighbours.query)
		if it.orb.LandedFall > 0 {
			it.orb.LandedFall = 0
			bodyLandings = append(bodyLandings, bodyLanding{w: it.w, x: it.orb.X, y: it.orb.Y, z: it.orb.Z, halfWidth: entity.OrbHalfSize})
		}
		if sp := it.orb.Splash; sp != nil {
			splashes = append(splashes, projectileSplash{w: it.w, x: sp.X, y: sp.Y, z: sp.Z, volume: sp.Volume, pitch: sp.Pitch})
		}
		neighbours.moved(slot[k])
		stepped = append(stepped, it)
		// The motion packet goes on the movement schedule, ahead of each
		// watcher's move.
		body := orbTracker.body(it)
		mt := body.schedule(it.orb.TakeSync())
		var motion []byte
		if mt.gate && it.motionDue(it.orb.VX, it.orb.VY, it.orb.VZ, true) {
			motion = entityVelocityPayload(it.orb.ID, it.orb.VX, it.orb.VY, it.orb.VZ)
		}
		for _, i := range body.watchers(near[it.w]) {
			w := watchers[it.w][i]
			if w.c.theWorld() != it.w {
				continue // crossed to another dimension since the snapshot
			}
			was, seen := body.settle(w.c, w.view.X, w.view.Z, w.c.chunkRadius(), &out)
			if !seen || !mt.gate {
				continue
			}
			if motion != nil {
				out.moves.add(w.c, outbound{id: protocol.IDPlaySetEntityMotion, data: motion})
			}
			body.moveTold(was, w.c, mt, &out)
		}

		// What the step came to: the pickup, the ends (life run out here,
		// the lava and fire and merges having marked their own dead for
		// the passes below), and the sounds a burn or its quenching played.
		for _, ev := range events {
			switch ev.Kind {
			case entity.OrbTaken:
				takes = append(takes, orbTake{orb: it, collector: ev.Collector, value: it.orb.Value})
				refuseFurtherTakes(views[it.w], ev.Collector)
				if it.orb.Dead {
					// The pickup was the last the orb held, and the take
					// that gives it is what carries the removal out.
					announced[it] = true
				}
			case entity.OrbExpired:
				removals[it.w] = append(removals[it.w], it.orb.ID)
				s.entityDestroyed(it.uuid)
				announced[it] = true
			case entity.OrbBurned:
				sounds = append(sounds, orbSound{w: it.w, id: protocol.IDPlaySoundEffect,
					data: s.burnSound(protocol.SoundCategoryAmbient, it.orb.X, it.orb.Y, it.orb.Z)})
			case entity.OrbExtinguished:
				sounds = append(sounds, orbSound{w: it.w, id: protocol.IDPlaySoundEffect,
					data: s.extinguishSound(protocol.SoundCategoryAmbient, it.orb.X, it.orb.Y, it.orb.Z)})
			case entity.OrbSteppedOnRedstoneOre:
				ores = append(ores, cellSpot{it.w, ev.X, ev.Y, ev.Z})
			case entity.OrbSnowflake:
				flakes = append(flakes, snowflakeSpot{it.w, it.orb.X, float64(ev.Y + 1), it.orb.Z})
			}
		}
		if it.orb.Dead {
			orbTracker.unfile(it)
		} else {
			alive = append(alive, it)
		}
	}
	// The absorbed: an orb a later neighbour's scan took into its count is
	// dead without an event of its own, however their steps ordered.
	for _, it := range stepped {
		if it.orb.Dead && !announced[it] {
			removals[it.w] = append(removals[it.w], it.orb.ID)
			s.entityDestroyed(it.uuid)
		}
	}
	s.orbs = alive
	s.unlockTick(&s.orbsMu)
	s.runBodyLandings(bodyLandings)

	out.send()
	applyEach(s, splashes, (*Server).splashProjectile)
	// The removals ahead of the fire's sounds: vanilla takes the burned
	// body out of the world inside the hurt that ends it, and plays the
	// fizz after the hurt returns, so the removal is heard first.
	for w, ids := range removals {
		s.broadcastWorld(w, protocol.IDPlayRemoveEntities, removeEntitiesPayload(ids))
	}
	for _, sound := range sounds {
		s.broadcastWorld(sound.w, sound.id, sound.data)
	}
	for _, o := range ores {
		s.lightRedstoneOre(o.w, o.x, o.y, o.z)
	}
	s.rollSnowflakes(flakes)
	for _, take := range takes {
		s.orbTaken(take)
	}

	// Each orb the portal pass caught arrives as a new body in the
	// dimension its portal goes to; a crossing with nowhere to arrive
	// puts the orb back in the list, the cooldown spent.
	s.touchGateways(gates)
	for _, tr := range travels {
		s.finishOrbTravel(tr.it, tr.kind)
	}
}

// tickOrbPickups runs one pass of the orb pickups alone, the part of
// tickOrbs a frozen world still owes its players: vanilla's player keeps
// ticking and touching nearby entities while the orb's own tick is held.
// vanilla: Player.aiStep, ExperienceOrb.playerTouch
func (s *Server) tickOrbPickups() {
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()

	views := orbViews(orbWatchers(clients))

	var takes []orbTake
	s.lockTick(&s.orbsMu)
	for _, it := range s.orbs {
		if it.orb.Dead {
			continue
		}
		for _, ev := range it.orb.PlayerTouch(views[it.w]) {
			if ev.Kind == entity.OrbTaken {
				takes = append(takes, orbTake{orb: it, collector: ev.Collector, value: it.orb.Value})
				refuseFurtherTakes(views[it.w], ev.Collector)
			}
		}
	}
	s.unlockTick(&s.orbsMu)
	for _, take := range takes {
		s.orbTaken(take)
	}
}

// orbTake is one pickup waiting to be given: the orb, the player who walked
// into it and the worth the pickup carries.
type orbTake struct {
	orb       *orbEntity
	collector int32
	value     int
}

// orbTaken gives a taken orb's worth to its collector and tells the world:
// the repair a mending item takes out of the worth first, the experience
// the repairs leave onto their bar, the level-up sound where the level was
// crossed, the Take Item Entity packet that plays the orb flying to its
// taker, and the body going out of every view once the pickup was the last
// the orb held. The two ticks of takeXpDelay the vanilla touch sets keep
// the taker from taking a second orb straight away.
func (s *Server) orbTaken(take orbTake) {
	c := s.entityClient(take.collector)
	if c == nil {
		return
	}
	c.takeXpDelay.Store(2)

	// The repair the worth pays before the bar. The slot packets are built
	// under the inventory's lock with the repairs and sent outside it, like
	// the wear's; an orb worth nothing mends nothing either.
	// vanilla: ExperienceOrb.repairPlayerItems
	var mended []slotWear
	worth := take.value
	if worth > 0 {
		c.player.Inventory(func(inv *player.Inventory) {
			// equipment and windowSlots both walk EquipmentSlots order, so
			// repairs[i] names the slot pieces[i] was read from.
			equipment := inv.Equipment()
			windowSlots := inv.EquipmentWindowSlots()
			pieces := make([]rules.EnchPiece, len(equipment))
			for i := range equipment {
				pieces[i] = rules.EnchPiece{Stack: equipment[i], Slot: player.EquipmentSlots[i]}
			}
			repairs, leftover := rules.RepairFromXp(pieces, worth, c.ownDice())
			worth = leftover
			for i, n := range repairs {
				if n <= 0 {
					continue
				}
				slot := windowSlots[i]
				stack := inv.Slots[slot]
				stack.Damage -= n
				mend := slotWear{slot: slot}
				mend.payload, mend.visible = c.setSlotUnder(inv, slot, stack)
				mended = append(mended, mend)
			}
		})
	}

	// The experience that is left. The bar is written here on the tick's own
	// goroutine, ahead of the take the broadcast queues, so the two keep
	// one order; a write that fails is one to a client already gone, which
	// the reads on the other side will see to.
	if worth > 0 {
		c.GiveExperiencePoints(worth)
	}

	for _, mend := range mended {
		c.tellWear(mend)
	}

	s.orbsMu.Lock()
	watchers := trackedWatchers(take.orb.tracked)
	s.orbsMu.Unlock()
	s.tellPickup(c.theWorld(), watchers, c, pickupItemPayload(take.orb.orb.ID, c.player.EntityID, 1))
	if take.orb.orb.Dead {
		s.entityDestroyed(take.orb.uuid)
		s.broadcastWorld(c.theWorld(), protocol.IDPlayRemoveEntities,
			removeEntitiesPayload([]int32{take.orb.orb.ID}))
	}
}

// spawnOrb lays one orb of experience in the world at the position and
// velocity given, shown to every player within its tracking range — the
// same both-locks moment spawnItem keeps.
func (s *Server) spawnOrb(w *world.World, x, y, z, vx, vy, vz float64, value int) *orbEntity {
	it := s.newOrbEntity(w, x, y, z, vx, vy, vz, value)
	s.registerOrb(it)
	return it
}

// newOrbEntity builds an experience orb without putting it in the world, so
// a caller may set state the spawn metadata reads before registerOrb shows
// it.
func (s *Server) newOrbEntity(w *world.World, x, y, z, vx, vy, vz float64, value int) *orbEntity {
	id := s.nextEntity()
	return &orbEntity{
		orb:    entity.NewOrb(id, x, y, z, vx, vy, vz, value, s.entitySeed(id)),
		uuid:   uuid.New(),
		w:      w,
		sentVX: vx, sentVY: vy, sentVZ: vz,
	}
}

// registerOrb puts a built experience orb into the world and shows it to
// every player within the type's tracking range — the same both-locks
// moment spawnOrb keeps. It is the one door every arrival into the world
// but a chunk's own wake walks: a chunk's saved orbs take the wake and
// arrive here, and the awards and the summons take it through spawnOrb.
func (s *Server) registerOrb(it *orbEntity) { orbTracker.register(s, it) }

// spawnOrbs awards experience at a position, vanilla's ExperienceOrb.award
// read for the path it runs: the total cut into the sizes getExperienceValue
// allows, each size tried as a merge into an orb already lying near the spot
// (the award-time merge, whose one-in-forty id shift is what keeps most of
// the split's own orbs apart), the rest stood up as fresh orbs popping
// outward the little arc the constructor gives one. The merge shift is
// rolled here rather than in the entity package: it is the award's own
// dice, drawn from the level's random source before it looks for the orb
// to add to.
func (s *Server) spawnOrbs(w *world.World, x, y, z float64, total int) {
	s.spawnOrbsDirected(w, x, y, z, 0, 0, 0, total)
}

// spawnOrbsDirected is spawnOrbs with a rough direction the fresh orbs
// leave along, as a thrown bottle's burst gives them.
// vanilla: ExperienceOrb.awardWithDirection
func (s *Server) spawnOrbsDirected(w *world.World, x, y, z, dx, dy, dz float64, total int) {
	for _, value := range rules.OrbValueSplit(total) {
		if s.mergeOrbAt(w, x, y, z, value) {
			continue
		}
		// The fresh orb's push: the constructor's random start, a spin
		// about the vertical and a wander of a fifth of a block a tick
		// each way, doubled — the shape of the pop a vanilla award
		// leaves its orbs with. The value rides the metadata, which is
		// built from the orb as the spawn left it; nothing else to set
		// here.
		vx, vy, vz, ox, oy, oz := entity.DirectedOrbPop(s.popRng, dx, dy, dz)
		s.spawnOrb(w, x+ox, y+oy, z+oz, vx, vy, vz, value)
	}
}

// orbPopVector is entity.OrbPopVector rolled from the server's pop random.
func (s *Server) orbPopVector() (vx, vy, vz float64) {
	return entity.OrbPopVector(s.popRng)
}

// mergeOrbAt merges an award into the first orb of the world that
// entity.Orb.TakesAward accepts, which takes it into its count and starts its
// life over. The shift is rolled here, from the server's own random; it
// reports whether an orb took the award.
// vanilla: ExperienceOrb.tryMergeToExisting
func (s *Server) mergeOrbAt(w *world.World, x, y, z float64, value int) bool {
	shift := int32(s.popRng.Intn(40))
	s.orbsMu.Lock()
	defer s.orbsMu.Unlock()
	for _, it := range s.orbs {
		if it.w != w || !it.orb.TakesAward(x, y, z, value, shift) {
			continue
		}
		it.orb.Count++
		it.orb.Age = 0
		return true
	}
	return false
}

// orbPairing is what shows an orb: the spawn, then the worth the client
// sizes it by.
func orbPairing(it *orbEntity) []outbound {
	return spawnAndMetadata(orbSpawnPayload(it), orbMetadataPayload(it))
}

// orbSpawnPayload builds the Spawn Entity packet that puts an orb in the
// world for a client: entity ID and UUID, the minecraft:experience_orb
// type, position and velocity. The worth follows in the metadata packet.
func orbSpawnPayload(it *orbEntity) []byte {
	return spawnEntityPayload(spawnEntity{
		id: it.orb.ID, uuid: it.uuid, typ: protocol.EntityTypeExperienceOrb,
		x: it.orb.X, y: it.orb.Y, z: it.orb.Z,
		vx: it.orb.VX, vy: it.orb.VY, vz: it.orb.VZ,
	})
}

// orbMetadataPayload builds the Set Entity Metadata packet of what an orb is
// at its spawn: the orbFields that differ from a fresh orb's.
// vanilla: SynchedEntityData.getNonDefaultValues
func orbMetadataPayload(it *orbEntity) []byte {
	return metadataChanges(it.orb.ID, orbDefaultFields, orbFields(it))
}

// orbDefaultFields is what a client assumes of an orb it has been told
// nothing of.
var orbDefaultFields = orbFields(&orbEntity{orb: &entity.Orb{}})

// orbFields is every synched value of an experience orb, defaults included.
func orbFields(it *orbEntity) metaFields {
	var m metaFields
	m.add(protocol.MetadataIndexEntityFlags, protocol.MetadataByte, func(b *protocol.Buffer) { b.PutU8(it.look.flags(false)) })
	it.look.addSilentField(&m)
	m.add(protocol.MetadataIndexNoGravity, protocol.MetadataBoolean, func(b *protocol.Buffer) { b.PutBool(it.orb.NoGravity) })
	m.add(protocol.MetadataIndexOrbValue, protocol.MetadataVarInt, func(b *protocol.Buffer) { b.PutVarInt(int32(it.orb.Value)) })
	it.custom.addFields(&m)
	return m
}

// killOrb discards an experience orb the way the /kill command and vanilla's
// Entity.kill do: out of the world with nothing laid down behind it.
func (s *Server) killOrb(it *orbEntity) {
	s.orbsMu.Lock()
	it.orb.Dead = true
	s.orbsMu.Unlock()
	s.entityDestroyed(it.uuid)
	s.broadcastWorld(it.w, protocol.IDPlayRemoveEntities,
		removeEntitiesPayload([]int32{it.orb.ID}))
}

// teleportOrb moves an experience orb to the position given; the tick tells
// the watchers.
// vanilla: Entity.teleportTo
func (s *Server) teleportOrb(it *orbEntity, to player.Position, by string) {
	orbTracker.teleport(s, it, func() {
		it.orb.SetPos(to.X, to.Y, to.Z)
		it.orb.VX, it.orb.VY, it.orb.VZ = 0, 0, 0
		it.look.yaw, it.look.pitch = to.Yaw, to.Pitch
	})
	logTeleported("orb", to, by)
}

// orbTwinAt builds the experience orb that continues this one's worth in
// another dimension, standing at pos: a new entity, new ID but the same
// UUID, raised from the old one's entity tag, which keeps worth, the
// pickups still waiting in it, age, health. The caller holds the orb lock
// or has taken the orb out of the list.
// vanilla: Entity.teleportCrossDimension
func (s *Server) orbTwinAt(it *orbEntity, to *dimension, pos player.Position) *orbEntity {
	twin := orbOfTag(s, to.w, orbTag(it).Entries())
	if twin == nil {
		return nil
	}
	twin.orb.SetPos(pos.X, pos.Y, pos.Z)
	return twin
}

// finishOrbTravel sends one orb the portal pass caught across its portal
// (crossPortal).
func (s *Server) finishOrbTravel(it *orbEntity, kind rules.PortalKind) {
	pos := player.Position{X: it.orb.X, Y: it.orb.Y, Z: it.orb.Z}
	s.crossPortal(it.w, it.orb.ID, pos, kind, func(to *dimension, spot world.SpawnPos, _ [2]int32) func() {
		twin := s.orbTwinAt(it, to, standingAt(spot))
		if twin == nil {
			slog.Error("could not carry an orb across a portal", "value", it.orb.Value)
			return nil
		}
		return func() {
			s.registerOrb(twin)
			slog.Debug("orb travelled through a portal",
				"to", to.name, "x", twin.orb.X, "y", twin.orb.Y, "z", twin.orb.Z)
		}
	}, func() {
		s.orbsMu.Lock()
		s.orbs = append(s.orbs, it)
		orbTracker.file(s, it)
		s.orbsMu.Unlock()
	})
}
