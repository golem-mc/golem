package server

import (
	"log/slog"
	"math"
	"math/rand"
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Server-side state for the primed TNT, kept and shown like the arrows and
// the orbs. The TNT itself (fall, bounce, fuse) is entity.TNT; the server
// holds the UUID, entity ID, dimension, per-observer tracking, and the fuse
// the metadata carries. The blast its fuse runs out into is the
// runExplosion every other blast goes through. The TNT lock sits beneath
// the player list's and alone of the entity ones — the TNT tick reads no
// other list — and the sends and blasts run after the locks are down.

// tntEntity is one primed TNT in a world. The TNT itself is entity.TNT; the
// server holds the dimension, UUID and tracking, each watcher keeping the
// fuse it was last told. rand is the source the blast's dice roll on, seeded
// from the identity. ownerUUID is the lighter's identity as the entity file
// keeps it — the reference the save writes and a loaded body resolves its
// owner's entity ID from, the ID itself being of the session and not of the
// world.
type tntEntity struct {
	tnt  *entity.TNT
	w    *world.World
	uuid uuid.UUID
	rand *rand.Rand

	bodyTrack[int]

	ownerUUID uuid.UUID

	// portal is the body's portal state, shared with every other body of
	// vanilla's Entity, the part its handlePortal carries one through.
	portal rules.EntityPortal

	// usedPortal is set once the body has been teleported (a portal, an end
	// gateway, /tp); its blast then spares nether portal blocks. Not saved.
	// vanilla: PrimedTnt.teleport
	usedPortal bool

	// tags are the scoreboard tags, what the tag selector option matches
	// and /tag edits. Guarded by the TNT lock, like the rest of a TNT's
	// own fields.
	tags nbt.StringSet
	// custom is the custom name, guarded like tags.
	custom customName
	look   entityLook
}

// tntTracker tracks the primed TNT; a watcher's own state is the fuse it was
// last told.
var tntTracker = trackedKind[*tntEntity, int]{
	invalidatePiston: func(dim *dimension) { dim.pistons.tnts.built = false },

	rangeChunks: entity.TNTTrackingRange,
	interval:    entity.TNTUpdateInterval,
	lock:        func(s *Server) *sync.Mutex { return &s.tntMu },
	list:        func(s *Server) *[]*tntEntity { return &s.tnts },
	chunks:      func(s *Server) *entity.ChunkIndex[*tntEntity] { return &s.tntChunks },
	filing:      func(it *tntEntity) *entity.Filing { return &it.tnt.Filing },
	uuid:        func(it *tntEntity) uuid.UUID { return it.uuid },
	track:       func(it *tntEntity) *bodyTrack[int] { return &it.bodyTrack },
	world:       func(it *tntEntity) *world.World { return it.w },
	id:          func(it *tntEntity) int32 { return it.tnt.ID },
	dead:        func(it *tntEntity) bool { return it.tnt.Dead },
	pose: func(it *tntEntity) bodyPose {
		return bodyPose{x: it.tnt.X, y: it.tnt.Y, z: it.tnt.Z, onGround: it.tnt.OnGround}
	},
	pairing: tntPairing,
	own:     func(it *tntEntity) int { return it.tnt.Fuse },
}

// tickTNT runs one game tick of every primed TNT: where everyone stands is
// read first, with nothing held; physics and movements run under the TNT
// lock alone; sends and blasts run after the locks are down.
func (s *Server) tickTNT() {
	s.lockTick(&s.tntMu)
	none := len(s.tnts) == 0
	s.unlockTick(&s.tntMu)
	if none {
		return
	}
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()

	watchers := make(map[*world.World][]trackingWatcher)
	for _, c := range clients {
		if c.player == nil {
			continue
		}
		pos := c.player.Position()
		watchers[c.theWorld()] = append(watchers[c.theWorld()], trackingWatcher{c: c, x: pos.X, z: pos.Z, chunkRadius: c.chunkRadius()})
	}
	near := nearbyWorlds(watchers, func(w trackingWatcher) trackingWatcher { return w })
	defer releaseNearbyWorlds(near)

	var (
		out            trackSends
		motions, metas sendList
		removals       = make(map[*world.World][]int32)
		blasts         []blast
		ores           []cellSpot
		bounces        []bodyBounce
		bodyLandings   []bodyLanding
	)

	colliders := s.collidersByWorld(tntCollisions)
	gate := s.newEntityGate()
	var held []*tntEntity
	defer func() { tntTracker.dormantPass(s, held) }()
	s.lockTick(&s.tntMu)
	// The portal pass runs ahead of the tick, as for items: a body the
	// portal is sending across leaves the list here.
	type tntPortalTravel struct {
		it   *tntEntity
		kind rules.PortalKind
	}
	var travels []tntPortalTravel
	var gates []gatewayTouch
	portalAlive := s.tnts[:0]
	for _, it := range s.tnts {
		if it.tnt.Dead {
			tntTracker.unfile(it)
			continue
		}
		state, kind, due := gate.stepPortal(it.w, it.tnt.X, it.tnt.Y, it.tnt.Z, &it.portal, portalCooldownTicks)
		s.notePortalTouch(nil, it.uuid, it.w, it.tnt.X, it.tnt.Y, it.tnt.Z, state)
		if !due {
			if state == rules.EndGatewayState {
				gatewayTouchAt(&gates, it.w, it.tnt.X, it.tnt.Y, it.tnt.Z, gatewayTouch{
					mu: &s.tntMu, portal: &it.portal, cooldown: portalCooldownTicks,
					move: func(to player.Position) { s.gatewayMoveTNT(it, to) },
				})
			}
			portalAlive = append(portalAlive, it)
			continue
		}
		tntTracker.unfile(it)
		travels = append(travels, tntPortalTravel{it: it, kind: kind})
	}
	s.tnts = portalAlive

	alive := s.tnts[:0]
	// ticked counts the TNT stepped per world against max-tnt-per-tick; a
	// TNT past it keeps its place in the list, so the earliest-primed go on
	// first and none starves.
	var ticked map[*world.World]int
	limit := s.Config.MaxTNTPerTick
	for _, it := range s.tnts {
		if it.tnt.Dead {
			tntTracker.unfile(it)
			continue // the fuse ran out before this tick
		}
		if limit > 0 {
			if ticked == nil {
				ticked = make(map[*world.World]int)
			}
			if ticked[it.w] >= limit {
				alive = append(alive, it)
				continue
			}
		}
		if !gate.ticks(it.w, it.tnt.X, it.tnt.Z) {
			held = append(held, it)
			alive = append(alive, it)
			continue
		}
		if limit > 0 {
			ticked[it.w]++
		}
		events := it.tnt.Step(colliders[it.w])
		if it.tnt.LandedFall > 0 {
			it.tnt.LandedFall = 0
			bodyLandings = append(bodyLandings, bodyLanding{w: it.w, x: it.tnt.X, y: it.tnt.Y, z: it.tnt.Z, halfWidth: entity.TNTHalfWidth})
		}
		for _, ev := range events {
			if ev.Kind == entity.TntSteppedOnRedstoneOre {
				ores = append(ores, cellSpot{it.w, int32(ev.X), int32(ev.Y), int32(ev.Z)})
			}
		}
		if it.tnt.TakeBounce() {
			bounces = append(bounces, bodyBounce{w: it.w, source: it.uuid, x: it.tnt.X, y: it.tnt.Y, z: it.tnt.Z, halfWidth: entity.TNTHalfWidth, quiet: true})
		}
		if it.tnt.Dead {
			// The fuse ran out under the step: the body is out of the
			// world before anything is told about it.
			for _, ev := range events {
				blasts = s.tntBlast(it, ev, blasts)
			}
			removals[it.w] = append(removals[it.w], it.tnt.ID)
			s.entityDestroyed(it.uuid)
			tntTracker.unfile(it)
			continue
		}

		body := tntTracker.body(it)
		mt := body.schedule(nil)

		// This TNT type tracks its own delta, so any velocity change goes
		// out at once, whatever the schedule says, to the watchers alone,
		// the newly paired included.
		// vanilla: ServerEntity.sendChanges, sendToTrackingPlayers
		var motion *outbound
		if it.motionDue(it.tnt.VX, it.tnt.VY, it.tnt.VZ, false) {
			motion = &outbound{id: protocol.IDPlaySetEntityMotion,
				data: entityVelocityPayload(it.tnt.ID, it.tnt.VX, it.tnt.VY, it.tnt.VZ)}
		}

		for _, i := range body.watchers(near[it.w]) {
			wh := watchers[it.w][i]
			c := wh.c
			if c.theWorld() != it.w {
				continue // crossed to another dimension since the snapshot
			}
			was, seen := body.settle(c, wh.x, wh.z, wh.chunkRadius, &out)
			if !seen {
				continue
			}
			if motion != nil {
				motions.add(c, *motion)
			}
			// The fuse a tick spent goes out the tick it was spent in,
			// whatever the movement schedule says.
			if was.own != it.tnt.Fuse {
				was.own = it.tnt.Fuse
				metas.add(c, outbound{id: protocol.IDPlayEntityMetadata, data: tntFusePayload(it)})
			}
			if mt.gate {
				body.moveTold(was, c, mt, &out)
			}
		}

		for _, ev := range events {
			blasts = s.tntBlast(it, ev, blasts)
		}
		if it.tnt.Dead {
			tntTracker.unfile(it)
		} else {
			alive = append(alive, it)
		}
	}
	s.tnts = alive
	if len(blasts) > 1 {
		s.tntBlasts.build(s.tnts)
	}
	s.unlockTick(&s.tntMu)

	// The bodies whose fuse ran out go out of every view ahead of the
	// blast they went up with — vanilla's discard takes the body out
	// while the explode is still to come.
	for w, ids := range removals {
		s.broadcastWorld(w, protocol.IDPlayRemoveEntities, removeEntitiesPayload(ids))
	}

	s.runBodyBounces(bounces)
	s.runBodyLandings(bodyLandings)

	// The blasts run ahead of the sends they may answer: a chain the blast
	// lit is in the world before the tick's packets go out.
	for _, o := range ores {
		s.lightRedstoneOre(o.w, o.x, o.y, o.z)
	}
	for _, b := range blasts {
		s.runExplosion(b)
	}
	s.lockTick(&s.tntMu)
	s.tntBlasts.drop()
	s.unlockTick(&s.tntMu)

	out.send()
	motions.send()
	metas.send()

	// Each body the portal pass caught arrives as a new primed TNT in the
	// dimension its portal goes to, the fuse it had left still burning; a
	// crossing with nowhere to arrive puts the body back in the list it
	// came out of.
	s.touchGateways(gates)
	for _, tr := range travels {
		s.finishTNTTravel(tr.it, tr.kind)
	}
}

// tntBlast turns one event out of a primed TNT's tick into the blast the
// tick is to run: the fuse that ran out into the explosion the event names,
// the body out of the world the way a despawn's is — no drops, no corpse.
// The owner is looked up now rather than carried: a lit block whose lighter
// has left the world blows up unowned, as the vanilla blast's damage source
// loses the reference it cannot resolve.
func (s *Server) tntBlast(it *tntEntity, ev entity.TNTEvent, blasts []blast) []blast {
	switch ev.Kind {
	case entity.TntExploded:
		// vanilla: PrimedTnt.explode
		if !it.w.TNTExplodes() {
			return blasts
		}
		ownerID, ownerName := s.tntOwnerName(it)
		killer := s.entityKiller(it.w, ownerID)
		damageType := rules.DamageTypePlayerExplosion
		if ownerID == 0 {
			damageType = rules.DamageTypeExplosion
		}
		var spared func(int32, int32, int32) bool
		var blockResist func(int32, int32, int32) (float64, bool)
		if it.usedPortal {
			spared, blockResist = portalSparing(it.w)
		}
		// PrimedTnt.tick discards before exploding, so Scoreboard.entityRemoved
		// has already cleared its team. Our scoreboard cleanup is deferred.
		return append(blasts, blast{
			blockResist: blockResist, spared: spared,
			w: it.w,
			x: ev.X, y: ev.Y, z: ev.Z,
			radius:      ev.Radius,
			causeID:     ownerID,
			causeName:   ownerName,
			causeKiller: killer,
			causeUUID:   it.ownerUUID,
			directID:    it.tnt.ID,
			directUUID:  it.uuid,
			directName:  s.directNameOf(killer, protocol.EntityTypeTnt, it.uuid, it.custom),
			silent:      it.tnt.Silent,
			directType:  "minecraft:tnt",
			directLoot:  &rules.LootEntity{Type: "minecraft:tnt", Tags: it.tags.Values()},
			// The primed TNT is the direct source; its feet cell holds
			// the water that spares a painting.
			directInWater: rules.IsWater(it.w.BlockAt(
				int32(math.Floor(it.tnt.X)), int32(math.Floor(it.tnt.Y)), int32(math.Floor(it.tnt.Z)))),
			damageType: damageType,
			rng:        it.rand,
			// A primed TNT's blast is a TNT's: the tnt decay rule scales
			// what the destroyed blocks drop.
			interaction: rules.TNTExplosionInteraction(s.world),
		})
	}
	return blasts
}

// tntOwnerName is who a primed TNT's blast is told by: the display name of
// the entity the body was lit behind — a player's own, a mob's kind's own —
// with the ID that names them on the wire. An owner gone from the world is
// nobody.
func (s *Server) tntOwnerName(it *tntEntity) (int32, string) {
	return s.ownerNameOf(it.w, it.tnt.OwnerID)
}

// ownerNameOf is tntOwnerName for any owner ID.
func (s *Server) ownerNameOf(w *world.World, owner int32) (int32, string) {
	if owner == 0 {
		return 0, ""
	}
	if c := s.entityClient(owner); c != nil {
		return owner, c.player.Name
	}
	if name, ok := s.mobNameByID(w, owner); ok {
		return owner, name
	}
	return 0, ""
}

// spawnTNT stands a primed TNT in the world at the position given, moving
// with the velocity given — the nudge of a lit one, the stillness of a
// summoned one — burning for the fuse given, drawn as the block state
// given, behind the owner named. The nudge says whether the birth was a
// lit one's or a summon's: the PrimedTnt constructor that takes an owner
// rolls its hop, the EntityType factory a summon goes through does not.
func (s *Server) spawnTNT(w *world.World, x, y, z float64, fuse int, ownerID int32, ownerName string, ownerUUID uuid.UUID, nudge bool) *tntEntity {
	it := s.newTNTEntity(w, x, y, z, fuse, ownerID, ownerUUID, nudge)
	if it == nil {
		return nil
	}
	slog.Debug("primed tnt lit", "fuse", fuse, "by", ownerName)
	s.registerTNT(it)
	return it
}

// newTNTEntity builds a primed TNT without putting it in the world, so a
// caller may set state the spawn metadata reads before registerTNT shows it.
func (s *Server) newTNTEntity(w *world.World, x, y, z float64, fuse int, ownerID int32, ownerUUID uuid.UUID, nudge bool) *tntEntity {
	state := registry.BlockTnt.DefaultState()
	id := s.nextEntity()
	seed := s.entitySeed(id)
	dice := rand.New(rand.NewSource(seed))
	var vx, vy, vz float64
	if nudge {
		// The nudge rolls the body's own stream from its start, which the
		// body then rolls again: re-seeded in place, not a second source.
		vx, vy, vz = entity.TNTNudgeVelocity(dice)
		dice.Seed(seed)
	}
	return &tntEntity{
		tnt: entity.NewTNT(id, x, y, z, vx, vy, vz,
			fuse, state, ownerID),
		uuid:      uuid.New(),
		w:         w,
		rand:      dice,
		ownerUUID: ownerUUID,
	}
}

// registerTNT puts a built primed TNT into the world and shows it to every
// player within the type's tracking range — the same both-locks moment
// registerItem explains, the one door every arrival but a wake walks.
func (s *Server) registerTNT(it *tntEntity) { tntTracker.register(s, it) }

// tntPairing is what shows a primed TNT: the spawn, then the fuse where it
// is not the default.
func tntPairing(it *tntEntity) []outbound {
	return spawnAndMetadata(tntSpawnPayload(it), tntMetadataPayload(it))
}

// tntMetadataState holds the values shared by spawn, tick and /data metadata.
type tntMetadataState struct {
	flags             uint8
	silent, noGravity bool
	fuse              int
	blockState        uint16
	custom            customName
}

func tntMetadataStateOf(it *tntEntity) tntMetadataState {
	t := it.tnt
	return tntMetadataState{it.look.glowFlags(), t.Silent, t.NoGravity, t.Fuse, t.BlockState, it.custom}
}

// tntMetadataPayload sends only values that differ from the client's defaults.
func tntMetadataPayload(it *tntEntity) []byte {
	defaults := tntMetadataState{fuse: entity.TNTDefaultFuse, blockState: registry.BlockTnt.DefaultState()}
	now := tntMetadataStateOf(it)
	return tntMetadataTable(it.tnt.ID, &now, &defaults, false)
}

// tntMetadataTable declares each field once for spawn, tick and /data updates.
// fuseOnly skips unchanged fields on the tick path; was may then be nil.
// vanilla: SynchedEntityData.getNonDefaultValues, SynchedEntityData.packDirty
func tntMetadataTable(id int32, now, was *tntMetadataState, fuseOnly bool) []byte {
	if !fuseOnly && *now == *was {
		return nil
	}
	var b protocol.Buffer
	b.PutVarInt(id)
	if !fuseOnly && now.flags != was.flags {
		b.PutMetadata(protocol.MetadataIndexEntityFlags, protocol.MetadataByte)
		b.PutU8(now.flags)
	}
	if !fuseOnly && now.silent != was.silent {
		b.PutMetadata(protocol.MetadataIndexSilent, protocol.MetadataBoolean)
		b.PutBool(now.silent)
	}
	if !fuseOnly && now.noGravity != was.noGravity {
		b.PutMetadata(protocol.MetadataIndexNoGravity, protocol.MetadataBoolean)
		b.PutBool(now.noGravity)
	}
	if fuseOnly || now.fuse != was.fuse {
		b.PutMetadata(protocol.MetadataIndexTNTFuse, protocol.MetadataVarInt)
		b.PutVarInt(int32(now.fuse))
	}
	if !fuseOnly && now.blockState != was.blockState {
		b.PutMetadata(protocol.MetadataIndexTNTBlockState, protocol.MetadataBlockState)
		b.PutVarInt(int32(now.blockState))
	}
	if !fuseOnly {
		now.custom.putMetadata(&b, was.custom)
	}
	b.EndMetadata()
	return b.Bytes()
}

// tntFusePayload sends the fuse alone, the only metadata changed by ticking.
func tntFusePayload(it *tntEntity) []byte {
	now := tntMetadataState{fuse: it.tnt.Fuse}
	return tntMetadataTable(it.tnt.ID, &now, nil, true)
}

// tntSpawnPayload builds the Spawn Entity packet that puts a primed TNT in
// the world for a client: entity ID and UUID, the minecraft:tnt type,
// position and the velocity of the nudge or the fall. There is no
// type-specific data.
func tntSpawnPayload(it *tntEntity) []byte {
	return spawnEntityPayload(spawnEntity{
		id: it.tnt.ID, uuid: it.uuid, typ: protocol.EntityTypeTnt,
		x: it.tnt.X, y: it.tnt.Y, z: it.tnt.Z,
		vx: it.tnt.VX, vy: it.tnt.VY, vz: it.tnt.VZ,
	})
}

// killTNT discards a primed TNT the way the /kill command and vanilla's
// Entity.kill do: out of the world with no blast behind it — a fuse cut
// short is no explosion.
func (s *Server) killTNT(it *tntEntity) {
	s.tntMu.Lock()
	it.tnt.Dead = true
	s.tntMu.Unlock()
	s.entityDestroyed(it.uuid)
	s.broadcastWorld(it.w, protocol.IDPlayRemoveEntities,
		removeEntitiesPayload([]int32{it.tnt.ID}))
}

// teleportTNT moves a primed TNT to the position given. The fall is
// stopped; the fuse keeps burning.
// vanilla: Entity.teleportTo
func (s *Server) teleportTNT(it *tntEntity, to player.Position, by string) {
	tntTracker.teleport(s, it, func() {
		it.tnt.SetPos(to.X, to.Y, to.Z)
		it.tnt.VX, it.tnt.VY, it.tnt.VZ = 0, 0, 0
		it.usedPortal = true
	})
	logTeleported("primed tnt", to, by)
}

// tntTwinAt builds the primed TNT that continues this one's burn in another
// dimension, standing at pos: a new body, new ID but the same UUID, raised
// from the old one's entity tag, which keeps the fuse still to burn, the
// block drawn as, the lighter. The caller holds the TNT lock or has taken
// the TNT out of the list.
// vanilla: Entity.teleportCrossDimension
func (s *Server) tntTwinAt(it *tntEntity, to *dimension, pos player.Position) *tntEntity {
	twin := tntOfTag(s, to.w, tntTag(it).Entries())
	if twin == nil {
		return nil
	}
	twin.tnt.SetPos(pos.X, pos.Y, pos.Z)
	twin.usedPortal = true
	return twin
}

// finishTNTTravel sends one primed TNT the portal pass caught across its
// portal (crossPortal), the lighter's reference resolved in the target world.
func (s *Server) finishTNTTravel(it *tntEntity, kind rules.PortalKind) {
	pos := player.Position{X: it.tnt.X, Y: it.tnt.Y, Z: it.tnt.Z}
	s.crossPortal(it.w, it.tnt.ID, pos, kind, func(to *dimension, spot world.SpawnPos, _ [2]int32) func() {
		twin := s.tntTwinAt(it, to, standingAt(spot))
		if twin == nil {
			slog.Error("could not carry a primed tnt across a portal")
			return nil
		}
		return func() {
			twin.tnt.OwnerID = s.resolveOwner(twin.ownerUUID)
			s.registerTNT(twin)
			slog.Debug("primed tnt travelled through a portal",
				"to", to.name, "x", twin.tnt.X, "y", twin.tnt.Y, "z", twin.tnt.Z)
		}
	}, func() {
		s.tntMu.Lock()
		s.tnts = append(s.tnts, it)
		tntTracker.file(s, it)
		s.tntMu.Unlock()
	})
}

// soundEffectHolderAt encodes a holder resolved before sound playback.
func soundEffectHolderAt(sound protocol.SoundHolder, category int32, x, y, z float64, volume, pitch float32) []byte {
	var b protocol.Buffer
	protocol.PutSoundHolder(&b, sound)
	return soundEffectTail(&b, category, x, y, z, volume, pitch)
}

// putSound writes a sound holder: the registry ID, or the name inline for a
// pack's sound the built-in registry lacks.
func putSound(b *protocol.Buffer, sound registry.Sound) {
	if sound.IsPack() {
		protocol.PutSoundHolder(b, sound.Holder())
		return
	}
	b.PutVarInt(int32(sound))
}

// soundEffectIDAt sends a registered holder without resolving a name.
func soundEffectIDAt(sound registry.Sound, category int32, x, y, z float64, volume, pitch float32) []byte {
	var b protocol.Buffer
	putSound(&b, sound)
	return soundEffectTail(&b, category, x, y, z, volume, pitch)
}

// soundEffectTail keeps the name and ID entry points on the same wire layout and seed draw.
func soundEffectTail(b *protocol.Buffer, category int32, x, y, z float64, volume, pitch float32) []byte {
	b.PutVarInt(category)
	b.PutI32(int32(x * 8))
	b.PutI32(int32(y * 8))
	b.PutI32(int32(z * 8))
	b.PutF32(volume)
	b.PutF32(pitch)
	putSoundSeed(b)
	return b.Bytes()
}

// entitySoundEffectID builds an entity sound using a registered holder.
func entitySoundEffectID(sound registry.Sound, category, entityID int32, volume, pitch float32) []byte {
	var b protocol.Buffer
	putSound(&b, sound)
	return entitySoundEffectTail(&b, category, entityID, volume, pitch)
}

func entitySoundEffectTail(b *protocol.Buffer, category, entityID int32, volume, pitch float32) []byte {
	b.PutVarInt(category)
	b.PutVarInt(entityID)
	b.PutF32(volume)
	b.PutF32(pitch)
	putSoundSeed(b)
	return b.Bytes()
}

// playSoundEffect builds a Sound Effect with a direct variable-range holder.
// The caller draws the seed so /playsound can reuse it for every target.
// vanilla: PlaySoundCommand.playSound, NoteBlock.triggerEvent
func playSoundEffect(name string, category int32, x, y, z float64, volume, pitch float32, seed int64) []byte {
	var b protocol.Buffer
	b.PutVarInt(0) // inline sound event, not a registry ID
	b.PutString(name)
	b.PutBool(false) // no fixed range
	b.PutVarInt(category)
	b.PutI32(int32(x * 8))
	b.PutI32(int32(y * 8))
	b.PutI32(int32(z * 8))
	b.PutF32(volume)
	b.PutF32(pitch)
	b.PutI64(seed)
	return b.Bytes()
}

// tntBlastIndex files the primed TNT by position for the blasts of one
// tickTNT, after the step has moved them. The TNT lock guards it.
type tntBlastIndex struct {
	grid   entity.BoxGrid
	bodies []*tntEntity
	hits   []int32
	out    []*tntEntity
	active bool
}

// build indexes list; a blast only changes velocities, so positions hold.
func (x *tntBlastIndex) build(list []*tntEntity) {
	x.grid.Cell = 8
	x.grid.Reset()
	clear(x.bodies)
	x.bodies = x.bodies[:0]
	for _, it := range list {
		i := int32(len(x.bodies))
		x.bodies = append(x.bodies, it)
		x.grid.Add(i, entity.NewAABB(it.tnt.X, it.tnt.Y, it.tnt.Z, it.tnt.X, it.tnt.Y, it.tnt.Z))
	}
	x.active = true
}

// drop ends the index's use and releases the bodies it holds.
func (x *tntBlastIndex) drop() {
	x.active = false
	x.grid.Reset()
	clear(x.bodies)
	x.bodies = x.bodies[:0]
	clear(x.out)
	x.out = x.out[:0]
}

// near returns the bodies of list within radius of the point, in list
// order, or list itself when no index applies. A body added since build is
// appended; a list that no longer starts with the indexed bodies is scanned
// whole. The slice is reused by the next call.
func (x *tntBlastIndex) near(list []*tntEntity, cx, cy, cz, radius float64) []*tntEntity {
	n := len(x.bodies)
	if !x.active || n == 0 || len(list) < n || list[n-1] != x.bodies[n-1] || list[0] != x.bodies[0] {
		return list
	}
	x.hits = x.hits[:0]
	x.grid.QueryPoints(entity.NewAABB(cx-radius, cy-radius, cz-radius, cx+radius, cy+radius, cz+radius),
		func(i int32) { x.hits = append(x.hits, i) })
	slices.Sort(x.hits)
	clear(x.out)
	x.out = x.out[:0]
	for _, i := range x.hits {
		x.out = append(x.out, x.bodies[i])
	}
	x.out = append(x.out, list[n:]...)
	return x.out
}
