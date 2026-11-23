package server

import (
	"math"
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Area effect clouds, left by lingering potions, kept like thrown
// projectiles in one list and tracked by cloudTracker.
// The pass and shrink are entity.Cloud's; effects on mobs and players apply
// like a splash's, tells gathered under the locks and sent outside.

// cloudEntity is one cloud: entity.Cloud plus dimension, UUID and
// tracking.
type cloudEntity struct {
	flame bool
	// ownerDragon caches the resolved owner type, as AreaEffectCloud.getOwner does.
	ownerDragon bool
	cloud       *entity.Cloud
	w           *world.World
	uuid        uuid.UUID
	// colour is the particle's ARGB, the potion's colour as its world event
	// played.
	colour int32
	// contents is the shattered stack's potion contents, kept whole for the
	// potion_contents NBT /data get reads.
	contents protocol.PotionContents
	// durationScale is the lingering potion's potion_duration_scale, already
	// applied to the instances.
	durationScale float32
	// customParticle overrides the potion-colour particle and is saved as custom_particle.
	customParticle nbt.Tag
	// ownerUUID is the potion's thrower, the saved Owner.
	ownerUUID uuid.UUID
	bodyTrack[cloudShown]
	// fresh marks a cloud spawned since the last cloud pass: vanilla ticks an
	// entity added during the entity loop only next tick, so a new cloud skips
	// the pass after its throw.
	fresh bool

	// tags are the scoreboard tags (tag selector, /tag). Guarded by the cloud
	// lock.
	tags nbt.StringSet
	// custom is the custom name, guarded like tags.
	custom customName
	look   entityLook
}

// cloudShown is a cloud as one client was last told it; the wire's
// defaults (radius 3, not waiting) stand for never told.
type cloudShown struct {
	radius    float32
	waiting   bool
	silent    bool
	flags     uint8
	noGravity bool
}

// cloudUnshown is an untold client's state: the metadata defaults a first
// tell may omit.
var cloudUnshown = cloudShown{radius: entity.CloudStartRadius}

// cloudTracker tracks the clouds, which never move: a watcher's own state is
// the radius and waiting flag it was last told.
var cloudTracker = trackedKind[*cloudEntity, cloudShown]{
	rangeChunks: entity.CloudTracking,
	lock:        func(s *Server) *sync.Mutex { return &s.arrowsMu },
	list:        func(s *Server) *[]*cloudEntity { return &s.clouds },
	chunks:      func(s *Server) *entity.ChunkIndex[*cloudEntity] { return &s.cloudChunks },
	filing:      func(it *cloudEntity) *entity.Filing { return &it.cloud.Filing },
	uuid:        func(it *cloudEntity) uuid.UUID { return it.uuid },
	track:       func(it *cloudEntity) *bodyTrack[cloudShown] { return &it.bodyTrack },
	world:       func(it *cloudEntity) *world.World { return it.w },
	id:          func(it *cloudEntity) int32 { return it.cloud.ID },
	dead:        func(it *cloudEntity) bool { return it.cloud.Dead },
	pose:        func(it *cloudEntity) bodyPose { return bodyPose{x: it.cloud.X, y: it.cloud.Y, z: it.cloud.Z} },
	pairing:     cloudPairing,
	own:         (*cloudEntity).cloudState,
}

// cloudPairing is what shows a cloud: spawn, then non-default metadata
// (always the particle, and radius and waiting once they changed).
func cloudPairing(it *cloudEntity) []outbound {
	return spawnAndMetadata(cloudSpawnPayload(it), cloudSpill(it, cloudUnshown, true))
}

// cloudSpawnPayload builds a cloud's Spawn Entity; it never moves and has
// no owner, so velocity and data are zero.
func cloudSpawnPayload(it *cloudEntity) []byte {
	c := it.cloud
	return spawnEntityPayload(spawnEntity{
		id: c.ID, uuid: it.uuid, typ: protocol.EntityTypeAreaEffectCloud,
		x: c.X, y: c.Y, z: c.Z,
	})
}

// cloudSpill builds a cloud's metadata: radius and waiting where they
// differ from told, and the particle when particle is set; nil
// when nothing differs.
func cloudSpill(it *cloudEntity, told cloudShown, particle bool) []byte {
	return cloudMetadataTable(it, told, particle, customName{})
}

// cloudMetadataTable serves pairing, ticking and data writes from one field list.
func cloudMetadataTable(it *cloudEntity, told cloudShown, particle bool, name customName) []byte {
	c := it.cloud
	var b protocol.Buffer
	b.PutVarInt(c.ID)
	wrote := it.look.silent != told.silent || it.look.noGravity != told.noGravity
	if flags := it.look.flags(false); flags != told.flags {
		b.PutMetadata(protocol.MetadataIndexEntityFlags, protocol.MetadataByte)
		b.PutU8(flags)
		wrote = true
	}
	it.look.putSilent(&b, told.silent)
	it.look.putNoGravity(&b, told.noGravity)
	if c.Radius() != told.radius || particle && it.dragonParticle() {
		b.PutMetadata(protocol.MetadataIndexCloudRadius, protocol.MetadataFloat)
		b.PutF32(c.Radius())
		wrote = true
	}
	if c.Waiting() != told.waiting {
		b.PutMetadata(protocol.MetadataIndexCloudWaiting, protocol.MetadataBoolean)
		b.PutBool(c.Waiting())
		wrote = true
	}
	if particle {
		it.custom.putMetadata(&b, name)
		b.PutMetadata(protocol.MetadataIndexCloudParticle, protocol.MetadataParticle)
		if id, data, ok := registry.ParticleOptions(it.customParticle); ok {
			b.PutVarInt(id)
			b.PutRaw(data)
		} else {
			b.PutVarInt(protocol.ParticleEntityEffect)
			b.PutI32(it.colour)
		}
		wrote = true
	}
	b.EndMetadata()
	if !wrote {
		return nil
	}
	return b.Bytes()
}

// cloudState is the record of what a tell left the client holding.
func (it *cloudEntity) cloudState() cloudShown {
	return cloudShown{radius: it.cloud.Radius(), waiting: it.cloud.Waiting(), silent: it.look.silent, flags: it.look.flags(false), noGravity: it.look.noGravity}
}

// spawnCloud spawns a lingering potion's cloud raining its effects, in the
// world event's colour, keeping the stack's contents for its NBT.
func (s *Server) spawnCloud(w *world.World, x, y, z float64, effects []effect.Instance, colour int32, contents protocol.PotionContents, ownerUUID uuid.UUID) {
	it := &cloudEntity{
		cloud:         entity.NewCloud(s.nextEntity(), x, y, z, effects),
		w:             w,
		uuid:          uuid.New(),
		colour:        colour,
		contents:      contents,
		durationScale: rules.LingeringDurationScale,
		ownerUUID:     ownerUUID,
		fresh:         true,
	}
	s.registerCloud(it)
}

// The cloud a creeper's blast leaves.
// vanilla: Creeper.spawnLingeringCloud
const (
	creeperCloudRadius        = 2.5
	creeperCloudRadiusOnUse   = -0.5
	creeperCloudWaitTime      = 10
	creeperCloudDuration      = 300
	creeperCloudDurationScale = 0.25
)

// creeperCloud is a creeper's effects at its blast, read under the mob lock.
type creeperCloud struct {
	w       *world.World
	x, y, z float64
	effects []effect.Instance
}

// spawnCreeperCloud leaves the cloud carrying the creeper's effects as its
// custom effects, with no potion and no owner.
// vanilla: Creeper.spawnLingeringCloud
func (s *Server) spawnCreeperCloud(c creeperCloud) {
	contents := protocol.PotionContents{Potion: protocol.NoPotion}
	for _, in := range c.effects {
		contents.CustomEffects = append(contents.CustomEffects, effect.WireSpec(in))
	}
	var instances []effect.Instance
	for _, in := range rules.PotionInstances(contents) {
		instances = append(instances, rules.LingeringInstance(effect.Clone(in), creeperCloudDurationScale))
	}
	cloud := entity.NewCloud(s.nextEntity(), c.x, c.y, c.z, instances)
	cloud.WaitTime, cloud.Duration, cloud.RadiusOnUse = creeperCloudWaitTime, creeperCloudDuration, creeperCloudRadiusOnUse
	cloud.Restore(0, creeperCloudRadius, -creeperCloudRadius/creeperCloudDuration)
	s.registerCloud(&cloudEntity{cloud: cloud, w: c.w, uuid: uuid.New(), fresh: true,
		colour: rules.PotionColour(contents), contents: contents, durationScale: creeperCloudDurationScale})
}

// cloudOfTag builds a cloud from a saved compound, nil without a place or
// identity. Effects come from the saved potion contents at the saved
// duration scale; timing fields take vanilla's defaults.
// vanilla: AreaEffectCloud.readAdditionalSaveData
func (s *Server) cloudOfTag(w *world.World, compound nbt.Compound) *cloudEntity {
	x, y, z, ok := entityPositionOfTag(compound)
	if !ok {
		return nil
	}
	identity, ok := entityUUIDOfTag(compound)
	if !ok {
		return nil
	}
	it := &cloudEntity{cloud: entity.NewCloud(s.nextEntity(), x, y, z, nil), w: w, uuid: identity}
	applyCloudTag(it, compound)
	it.cloud.Restore(it.cloud.TickCount, it.cloud.Radius(), it.cloud.RadiusPerTick())
	return it
}

// applyCloudTag reloads saved fields without replacing the cloud's victim cooldowns.
// vanilla: AreaEffectCloud.readAdditionalSaveData
func applyCloudTag(it *cloudEntity, compound nbt.Compound) {
	it.look = entityLookOf(compound)
	if x, y, z, ok := entityPositionOfTag(compound); ok {
		it.cloud.SetPos(x, y, z)
	}
	contents := protocol.PotionContents{Potion: protocol.NoPotion}
	if saved, ok := compound["potion_contents"]; ok {
		if read, ok := registry.PotionContentsOf(saved); ok {
			contents = read
		}
	}
	scale := floatFieldOr(compound, "potion_duration_scale", 1)
	var effects []effect.Instance
	for _, in := range rules.PotionInstances(contents) {
		effects = append(effects, rules.LingeringInstance(effect.Clone(in), scale))
	}
	cloud := it.cloud
	cloud.Effects = effects
	cloud.Duration = intFieldOr(compound, "Duration", -1)
	cloud.WaitTime = intFieldOr(compound, "WaitTime", 20)
	cloud.Reapplication = intFieldOr(compound, "ReapplicationDelay", 20)
	cloud.DurationOnUse = intFieldOr(compound, "DurationOnUse", 0)
	cloud.RadiusOnUse = float32(floatFieldOr(compound, "RadiusOnUse", 0))
	cloud.Reload(intFieldOf(compound, "Age"), float32(math.Max(0, math.Min(32, floatFieldOr(compound, "Radius", 3)))), float32(floatFieldOr(compound, "RadiusPerTick", 0)))
	it.contents, it.durationScale, it.colour = contents, float32(scale), rules.PotionColour(contents)
	if contents.CustomColor != nil {
		// vanilla: AreaEffectCloud.updateParticle uses ARGB.opaque.
		it.colour = *contents.CustomColor | -0x1000000
	}
	owner := entityUUIDFieldOf(compound, "Owner")
	if owner != it.ownerUUID {
		it.ownerDragon = false
	}
	it.ownerUUID = owner
	it.tags.Reset(tagsOfTag(compound))
	it.custom = customNameOfTag(compound)
	it.customParticle = nil
	if tag := compound["custom_particle"]; tag != nil {
		if _, _, ok := registry.ParticleOptions(tag); ok {
			it.customParticle = tag
		}
	}
}

// registerCloud adds a built cloud and shows it to players in range.
func (s *Server) registerCloud(it *cloudEntity) { cloudTracker.register(s, it) }

// killCloud discards a cloud. vanilla: Entity.kill
func (s *Server) killCloud(it *cloudEntity) {
	s.arrowsMu.Lock()
	it.cloud.Dead = true
	cloudTracker.unfile(it)
	s.clouds = slices.DeleteFunc(s.clouds, func(o *cloudEntity) bool { return o == it })
	s.arrowsMu.Unlock()
	s.entityDestroyed(it.uuid)
	s.broadcastWorld(it.w, protocol.IDPlayRemoveEntities,
		removeEntitiesPayload([]int32{it.cloud.ID}))
}

// teleportCloud moves a cloud; the tick tells its viewers.
// vanilla: Entity.teleportTo, ServerEntity.sendChanges
func (s *Server) teleportCloud(it *cloudEntity, to player.Position, by string) {
	cloudTracker.teleport(s, it, func() { it.cloud.SetPos(to.X, to.Y, to.Z) })
}

// cloudTwinAt builds the cloud continuing this one in another dimension at
// pos: a copy with a new ID and same UUID, nothing touched yet. The caller
// holds the clouds lock or has taken it out of the list.
// vanilla: Entity.teleportCrossDimension
func (s *Server) cloudTwinAt(it *cloudEntity, to *dimension, pos player.Position) *cloudEntity {
	body := *it.cloud
	body.ID = s.nextEntity()
	body.SetPos(pos.X, pos.Y, pos.Z)
	twin := *it
	twin.cloud, twin.w = &body, to.w
	twin.bodyTrack = bodyTrack[cloudShown]{}
	twin.tags = nbt.NewStringSet(it.tags.Values())
	return &twin
}

// tickClouds ticks every cloud against its dimension's bodies, applying
// its effects, and gathers sends, effect tells and removals for after the
// locks, like the thrown projectiles' tick.
func (s *Server) tickClouds() {
	s.lockTick(&s.arrowsMu)
	none := len(s.clouds) == 0
	s.unlockTick(&s.arrowsMu)
	if none {
		return
	}

	// Where everyone is, read before any lock the tick takes.
	s.mu.RLock()
	clients := make([]*client, 0, len(s.clients))
	byUUID := make(map[uuid.UUID]*client, len(s.clients))
	for id, c := range s.clients {
		clients = append(clients, c)
		byUUID[id] = c
	}
	s.mu.RUnlock()

	type watcher struct {
		c           *client
		pos         player.Position
		chunkRadius int32
	}
	watchers := make(map[*world.World][]watcher)
	for _, c := range clients {
		if c.player == nil {
			continue
		}
		pos := c.player.Position()
		watchers[c.theWorld()] = append(watchers[c.theWorld()], watcher{
			c: c, pos: pos, chunkRadius: c.chunkRadius(),
		})
	}
	near := nearbyWorlds(watchers, func(w watcher) trackingWatcher {
		return trackingWatcher{c: w.c, x: w.pos.X, z: w.pos.Z, chunkRadius: w.chunkRadius}
	})
	defer releaseNearbyWorlds(near)

	mannequins := s.mannequinTargets()
	lnd := &cloudLanding{mannequins: mannequins, byUUID: byUUID}
	var (
		// out's moves are the metadata deltas, sent after the pairings.
		out   trackSends
		drops []cloudDrop
		// held are the clouds whose reach waits for AreaEffectCloudApplyEvent.
		held []heldCloudApply
	)
	worldTargets := make(map[*world.World]*cloudWorldTargets)
	s.lockTick(&s.arrowsMu)
	s.lockTick(&s.mobsMu)
	alive := s.clouds[:0]
	for _, it := range s.clouds {
		if it.cloud.Dead {
			cloudTracker.unfile(it)
			continue
		}
		if it.fresh {
			// A cloud spawned this tick skips this pass: vanilla ticks a body added
			// mid-loop next tick.
			it.fresh = false
			alive = append(alive, it)
			continue
		}
		dim := s.dimensionOfWorld(it.w)
		wt := worldTargets[it.w]
		if wt == nil {
			wt = &cloudWorldTargets{
				mobsByID:    make(map[int32]*entity.Mob, len(dim.mobs.herd)),
				clientsByID: make(map[int32]*client, len(watchers[it.w])),
				index:       make(map[int32]int, len(dim.mobs.herd)),
			}
			// Skip the dying: vanilla: isAffectedByPotions, !isDeadOrDying
			for _, m := range dim.mobs.herd {
				if !m.Alive() {
					continue
				}
				wt.index[m.ID] = len(wt.targets)
				wt.targets = append(wt.targets, entity.CloudTarget{
					ArrowTarget: entity.MobTarget(m.ID, mobEntityType(m.Type), m),
					FeetX:       m.X, FeetZ: m.Z,
				})
				wt.mobsByID[m.ID] = m
			}
			for _, target := range mannequins.byWorld[it.w] {
				wt.targets = append(wt.targets, entity.CloudTarget{ArrowTarget: target, FeetX: (target.MinX + target.MaxX) / 2, FeetZ: (target.MinZ + target.MaxZ) / 2})
			}
			for _, w := range watchers[it.w] {
				if w.c.player.Vitals().Dead {
					continue
				}
				wt.targets = append(wt.targets, entity.CloudTarget{
					ArrowTarget: w.c.projectileTarget(w.pos, projectileUnhittable(w.c)),
					FeetX:       w.pos.X, FeetZ: w.pos.Z,
				})
				wt.clientsByID[w.c.player.EntityID] = w.c
			}
			worldTargets[it.w] = wt
		}

		reached := it.cloud.Step(wt.targets)
		if s.plugins != nil && s.plugins.Has(pluginruntime.AreaEffectCloudApply) && len(reached) > 0 {
			held = append(held, s.holdCloudApply(it, dim, wt, reached))
		} else {
			lnd.land(it, dim, wt, reached)
		}

		body := cloudTracker.body(it)
		mt := body.teleportTick()
		for _, i := range body.watchers(near[it.w]) {
			w := watchers[it.w][i]
			if w.c.theWorld() != it.w {
				continue // crossed to another dimension since the snapshot
			}
			shown, told := it.tracked.get(w.c)
			// A dead cloud is never paired; its watchers get the removal.
			if !told && it.cloud.Dead {
				continue
			}
			if _, seen := body.settle(w.c, w.pos.X, w.pos.Z, w.chunkRadius, &out); !seen || !told || it.cloud.Dead {
				continue
			}
			if mt.teleported {
				body.moveTold(shown, w.c, mt, &out)
			}
			if data := cloudSpill(it, shown.own, false); data != nil {
				*shown = body.record()
				out.moves.add(w.c, outbound{id: protocol.IDPlayEntityMetadata, data: data})
			}
		}
		if it.cloud.Dead {
			// Only players still tracking the cloud are told of its removal; one that
			// left range already had it removed and must not get a second removal.
			var paired []*client
			for _, i := range body.watchers(near[it.w]) {
				w := watchers[it.w][i]
				if _, told := it.tracked.get(w.c); told {
					paired = append(paired, w.c)
				}
			}
			drops = append(drops, cloudDrop{it: it, paired: paired})
			s.entityDestroyed(it.uuid)
			cloudTracker.unfile(it)
			continue
		}
		alive = append(alive, it)
	}
	s.clouds = alive
	s.unlockTick(&s.mobsMu)
	s.unlockTick(&s.arrowsMu)

	out.send()
	s.applyHeldClouds(held, lnd)
	applyEach(s, lnd.mannequinTells, (*Server).landMannequinEffects)
	for i, tell := range lnd.mobTells {
		s.tellSplashMob(lnd.tellWorlds[i], tell)
	}
	for _, hit := range lnd.instantHurts {
		s.tellThrownMobHit(hit)
	}
	for _, tell := range lnd.playerTells {
		s.landPlayerTell(tell)
	}
	for _, drop := range drops {
		for _, c := range drop.paired {
			c.sendIn(drop.it.w, protocol.IDPlayRemoveEntities,
				removeEntitiesPayload([]int32{drop.it.cloud.ID}))
		}
	}
}

// cloudWorldTargets is one world's living mobs and players, built when a
// cloud there first steps; index is a mob's entry by entity ID.
type cloudWorldTargets struct {
	targets     []entity.CloudTarget
	index       map[int32]int
	mobsByID    map[int32]*entity.Mob
	clientsByID map[int32]*client
}

// cloudLanding collects what the clouds' effects did, to tell once the locks
// are released; byUUID maps each player.
type cloudLanding struct {
	mannequins     mannequinTargetSnapshot
	byUUID         map[uuid.UUID]*client
	mannequinTells []mannequinEffectTell
	mobTells       []splashMobTell
	tellWorlds     []*world.World
	playerTells    []splashPlayerTell
	// instantHurts are the blows the clouds' instant harm landed on mobs.
	instantHurts []thrownMobHit
}

// land gives each body the cloud reached its own copy of the instances, with
// separate countdowns. The mob lock is held.
func (l *cloudLanding) land(it *cloudEntity, dim *dimension, wt *cloudWorldTargets, reached []entity.CloudTarget) {
	mobsByID, clientsByID := wt.mobsByID, wt.clientsByID
	var src instantSource
	for i, target := range reached {
		var landing []effect.Instance
		for _, in := range it.cloud.Effects {
			if registry.MobEffectOf(in.ID).Instant {
				landing = append(landing, in)
			} else {
				landing = append(landing, effect.Clone(in))
			}
		}
		if i == 0 {
			src = cloudInstantSource(it, dim, l.byUUID[it.ownerUUID])
		}
		if target.Type == protocol.EntityTypeMannequin {
			l.mannequinTells = append(l.mannequinTells, mannequinEffectTell{w: it.w, ent: l.mannequins.byID[target.EntityID], effects: landing, scale: rules.CloudInstantScale, src: src})
			continue
		}
		if m := mobsByID[target.EntityID]; m != nil {
			hits, changes := landMobEffects(dim, it.w, m, landing, rules.CloudInstantScale, it.cloud.X, it.cloud.Z, src)
			l.instantHurts = append(l.instantHurts, hits...)
			// An ownerless instant harm can teleport an enderman, and the
			// next cloud passes a body this one killed by.
			// vanilla: Enderman.hurtServer
			entry := entity.CloudTarget{ArrowTarget: entity.MobTarget(m.ID, mobEntityType(m.Type), m), FeetX: m.X, FeetZ: m.Z}
			entry.Unhittable = entry.Unhittable || !m.Alive()
			wt.targets[wt.index[m.ID]] = entry
			if tell, ok := mobEffectTell(dim, m, changes); ok {
				l.mobTells = append(l.mobTells, tell)
				l.tellWorlds = append(l.tellWorlds, it.w)
			}
			continue
		}
		if c := clientsByID[target.EntityID]; c != nil {
			l.playerTells = append(l.playerTells, splashPlayerTell{c: c, effects: landing, scale: rules.CloudInstantScale, src: src})
		}
	}
}

// cloudInstantSource is where a cloud's instant effects come from: the cloud
// and its owner, a player (ownerClient) or a mob of the dimension. The mob
// lock is held.
// vanilla: AreaEffectCloud.getOwner
func cloudInstantSource(it *cloudEntity, dim *dimension, ownerClient *client) instantSource {
	src := instantSource{bodyID: it.cloud.ID, bodyName: "entity.minecraft.area_effect_cloud", bodyX: it.cloud.X, bodyZ: it.cloud.Z}
	if ownerClient != nil {
		src.ownerID, src.ownerIsPlayer, src.ownerCreative = ownerClient.player.EntityID, true, creativeClient(ownerClient)
	} else if owner := dim.mobs.byUUID[it.ownerUUID]; owner != nil {
		src.ownerID, src.ownerMob = owner.ID, true
	}
	return src
}

// cloudDrop is a cloud whose life ended and the clients still holding its
// pairing.
type cloudDrop struct {
	it     *cloudEntity
	paired []*client
}
