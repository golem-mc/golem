package server

import (
	"math"
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

// The firework rockets: the flight is entity.Firework and the burst's
// numbers are rules'; this file spawns rockets, tracks them, and carries out
// the burst's hurt. The tick takes the rocket lock, then the mob lock; the
// players' hurt and the sends run after both are down.
// vanilla: FireworkRocketEntity, FireworkRocketItem

// fireworkEntity is one rocket in a world. The body itself is
// entity.Firework; the server holds the dimension, the UUID, the stack the
// metadata says it is, and the tracking.
type fireworkEntity struct {
	rocket *entity.Firework
	w      *world.World
	uuid   uuid.UUID
	stack  protocol.ItemStack
	bodyTrack[struct{}]
	shotMark

	portal rules.EntityPortal

	// ownerID is the entity ID of the player whose use the rocket came
	// from: the hurt's cause, the death message's shooter. ownerUUID is
	// the player's own identity, outliving the session the ID belongs to.
	ownerID   int32
	ownerUUID uuid.UUID

	// tags are the scoreboard tags. Guarded by the firework lock.
	tags nbt.StringSet
	// custom is the custom name, guarded like tags.
	custom customName
}

// fireworkTracker tracks the rockets. A rocket's flight is never on the
// ground, and its Teleport Entity carries no velocity: the motion packets do.
var fireworkTracker = trackedKind[*fireworkEntity, struct{}]{
	rangeChunks: entity.FireworkTrackingRange,
	interval:    entity.FireworkUpdateInterval,
	lock:        func(s *Server) *sync.Mutex { return &s.fireworksMu },
	list:        func(s *Server) *[]*fireworkEntity { return &s.fireworks },
	chunks:      func(s *Server) *entity.ChunkIndex[*fireworkEntity] { return &s.fireworkChunks },
	filing:      func(it *fireworkEntity) *entity.Filing { return &it.rocket.Filing },
	uuid:        func(it *fireworkEntity) uuid.UUID { return it.uuid },
	track:       func(it *fireworkEntity) *bodyTrack[struct{}] { return &it.bodyTrack },
	world:       func(it *fireworkEntity) *world.World { return it.w },
	id:          func(it *fireworkEntity) int32 { return it.rocket.ID },
	dead:        func(it *fireworkEntity) bool { return it.rocket.Dead },
	pose: func(it *fireworkEntity) bodyPose {
		r := it.rocket
		return bodyPose{x: r.X, y: r.Y, z: r.Z, yaw: r.Yaw, pitch: r.Pitch}
	},
	pairing: fireworkPairing,
}

// handleUseFirework launches a gliding player's rocket attached to them for
// its life. It reports whether the hand held a rocket a gliding player
// used; a player on their feet passes and spends nothing.
// vanilla: FireworkRocketItem.use
func (c *client) handleUseFirework(hand int32) bool {
	if !c.fallFlying.Load() {
		return false
	}
	if c.player.Vitals().Dead {
		return false
	}
	mode := c.player.GameMode()
	if mode == protocol.GameModeSpectator {
		return false
	}
	held := c.heldItem(hand)
	if registry.Item(held.ItemID) != registry.ItemFireworkRocket {
		return false
	}
	pos := c.player.Position()
	if c.dropAllLeashConnections() {
		c.srv.broadcastSoundID(c.theWorld(), knotSoundBreak, protocol.SoundCategoryNeutral,
			pos.X, pos.Y, pos.Z, 1, 1)
	}
	c.srv.spawnFirework(c.theWorld(), held, pos.X, pos.Y, pos.Z,
		c.player.EntityID, c.player.UUID, c.player.EntityID)
	// vanilla: FireworkRocketItem.use
	c.awardUsedHeld(hand)
	// vanilla: ItemStack.consume, which creative's infinite materials skip
	if mode != protocol.GameModeCreative {
		if err := c.consumeHeld(hand); err != nil {
			return false
		}
	}
	return true
}

// launchFireworkOnFace launches a free rocket from a block click the block
// itself did not answer, 0.15 of a block off the clicked face. A gliding
// player passes; only survival spends the rocket.
// vanilla: FireworkRocketItem.useOn, ServerPlayerGameMode.useItemOn
func (c *client) launchFireworkOnFace(hand, x, y, z, face int32, cursorX, cursorY, cursorZ float32) (handled bool, err error) {
	if c.fallFlying.Load() {
		return false, nil
	}
	held := c.heldItem(hand)
	if registry.Item(held.ItemID) != registry.ItemFireworkRocket {
		return false, nil
	}
	if face < 0 || int(face) >= len(protocol.FaceOffset) {
		return false, nil
	}
	// vanilla: UseOnContext.getClickLocation,
	// FireworkRocketItem.ROCKET_PLACEMENT_OFFSET
	nx, ny, nz := protocol.FaceOffset[face][0], protocol.FaceOffset[face][1], protocol.FaceOffset[face][2]
	it := c.srv.spawnFirework(c.theWorld(), held,
		float64(x)+float64(cursorX)+float64(nx)*entity.FireworkPlaceOffset,
		float64(y)+float64(cursorY)+float64(ny)*entity.FireworkPlaceOffset,
		float64(z)+float64(cursorZ)+float64(nz)*entity.FireworkPlaceOffset,
		c.player.EntityID, c.player.UUID, 0)
	if it == nil {
		return false, nil
	}
	c.awardUsedHeld(hand)
	if c.player.GameMode() != protocol.GameModeCreative {
		if err := c.consumeHeld(hand); err != nil {
			return true, err
		}
	}
	return true, nil
}

// spawnFirework puts a rocket made from stack into the world. attached is
// the entity ID of the gliding body it rides, zero for a free rocket.
func (s *Server) spawnFirework(w *world.World, stack protocol.ItemStack, x, y, z float64,
	ownerID int32, ownerUUID uuid.UUID, attached int32) *fireworkEntity {
	it := s.newFirework(w, stack, x, y, z, ownerID, ownerUUID, attached)
	s.registerFirework(it)
	return it
}

// fireworkFlight is a rocket stack's flight duration and star count.
// vanilla: FireworkRocketEntity.<init>
func fireworkFlight(stack protocol.ItemStack) (flight, explosions int32) {
	if stack.Removes(protocol.DataComponentFireworks) {
		return 0, 0
	}
	// No fireworks component is the item's default of one flight.
	// vanilla: the item report's firework_rocket components
	flight = 1
	if fw, ok := stack.FireworksOf(); ok {
		flight, explosions = fw.FlightDuration, fw.Explosions
	}
	return flight, explosions
}

// newFirework builds the rocket spawnFirework lays, not yet in the world.
func (s *Server) newFirework(w *world.World, stack protocol.ItemStack, x, y, z float64,
	ownerID int32, ownerUUID uuid.UUID, attached int32) *fireworkEntity {
	flight, explosions := fireworkFlight(stack)
	id := s.nextEntity()
	rocket := entity.NewFirework(id, x, y, z, ownerID, flight, explosions, attached,
		s.entitySeed(id))
	carried := stack
	carried.Count = 1
	it := &fireworkEntity{
		rocket:    rocket,
		w:         w,
		uuid:      uuid.New(),
		stack:     carried,
		ownerID:   ownerID,
		ownerUUID: ownerUUID,
	}
	return it
}

// fireworkOfTag builds the rocket a saved compound describes, or nil for one
// with no place or identity. A boost rocket's glider is not saved, so
// the loaded rocket flies free.
// vanilla: FireworkRocketEntity.readAdditionalSaveData
func (s *Server) fireworkOfTag(w *world.World, compound nbt.Compound) *fireworkEntity {
	x, y, z, ok := entityPositionOfTag(compound)
	if !ok {
		return nil
	}
	identity, ok := entityUUIDOfTag(compound)
	if !ok {
		return nil
	}
	id := s.nextEntity()
	it := &fireworkEntity{rocket: entity.NewFirework(id, x, y, z, 0, 0, 0, 0, s.entitySeed(id)), w: w, uuid: identity}
	applyFireworkTag(it, compound)
	return it
}

// applyFireworkTag preserves the live rocket's rider and collision state.
// vanilla: FireworkRocketEntity.readAdditionalSaveData, Projectile.readAdditionalSaveData
func applyFireworkTag(it *fireworkEntity, compound nbt.Compound) {
	if x, y, z, ok := entityPositionOfTag(compound); ok {
		it.rocket.SetPos(x, y, z)
	}
	// A missing or empty FireworksItem loads the default rocket.
	// vanilla: FireworkRocketEntity.getDefaultItem
	var carried protocol.ItemStack
	if stack, ok := nbt.Get[nbt.Compound](compound, "FireworksItem"); ok {
		carried, _ = registry.StoredStack(stack)
	}
	if carried.Empty() {
		carried = protocol.ItemStack{ItemID: int32(registry.ItemFireworkRocket), Count: 1}
	}
	explosions := int32(0)
	if fw, ok := carried.FireworksOf(); ok {
		explosions = fw.Explosions
	}

	rocket := it.rocket
	rocket.VX, rocket.VY, rocket.VZ = entityMotionOfTag(compound)
	rocket.Yaw, rocket.Pitch = entityRotationOfTag(compound)
	rocket.Restore(intFieldOf(compound, "Life"), intFieldOf(compound, "LifeTime"), nbt.BoolField(compound, "ShotAtAngle"), nbt.BoolField(compound, "LeftOwner"))
	rocket.Explosions = int(explosions)
	it.stack = carried
	it.ownerUUID = entityUUIDFieldOf(compound, "Owner")
	it.shot = nbt.BoolField(compound, "HasBeenShot")
	it.portal.Cooldown = portalCooldownOf(compound)
	it.tags.Reset(tagsOfTag(compound))
	it.custom = customNameOfTag(compound)
}

// registerFirework puts a built rocket into the world and shows it to every
// player in range: the one door every arrival but a wake walks.
func (s *Server) registerFirework(it *fireworkEntity) { fireworkTracker.register(s, it) }

// fireworkPairing is what shows a rocket: the spawn, then the stack its
// metadata says it is, with the body a boost rocket rides where there is
// one.
func fireworkPairing(it *fireworkEntity) []outbound {
	return spawnAndMetadata(fireworkSpawnPayload(it), fireworkMetadataPayload(it))
}

// killFirework takes a rocket out of the world with no burst.
// vanilla: Entity.kill
func (s *Server) killFirework(it *fireworkEntity) {
	s.fireworksMu.Lock()
	fireworkTracker.unfile(it)
	s.fireworks = slices.DeleteFunc(s.fireworks, func(o *fireworkEntity) bool { return o == it })
	s.fireworksMu.Unlock()
	s.entityDestroyed(it.uuid)
	s.broadcastWorld(it.w, protocol.IDPlayRemoveEntities,
		removeEntitiesPayload([]int32{it.rocket.ID}))
}

// teleportFirework moves a rocket to the position given, its flight
// stopped; its next movement tick tells the watchers.
// vanilla: Entity.teleportTo
func (s *Server) teleportFirework(it *fireworkEntity, to player.Position, by string) {
	fireworkTracker.teleport(s, it, func() {
		it.rocket.SetPos(to.X, to.Y, to.Z)
		it.rocket.VX, it.rocket.VY, it.rocket.VZ = 0, 0, 0
	})
	logTeleported("firework", to, by)
}

// fireworkTwinAt builds the rocket that continues this one in another
// dimension at pos: a copy with a new ID, watched by no one yet. The caller
// holds the fireworks' lock or has taken the rocket out of the list.
// vanilla: Entity.teleportCrossDimension
func (s *Server) fireworkTwinAt(it *fireworkEntity, to *dimension, pos player.Position) *fireworkEntity {
	body := *it.rocket
	body.ID = s.nextEntity()
	body.SetPos(pos.X, pos.Y, pos.Z)
	twin := *it
	twin.rocket, twin.w = &body, to.w
	twin.tracked = nil
	twin.tags = nbt.NewStringSet(it.tags.Values())
	return &twin
}

// fireworkSpawnPayload builds a rocket's Spawn Entity; the data field names
// the owner, the launcher or the gliding body the rocket rides.
// vanilla: Projectile.getAddEntityPacket
func fireworkSpawnPayload(it *fireworkEntity) []byte {
	r := it.rocket
	return spawnEntityPayload(spawnEntity{
		id: r.ID, uuid: it.uuid, typ: protocol.EntityTypeFireworkRocket,
		x: r.X, y: r.Y, z: r.Z,
		vx: r.VX, vy: r.VY, vz: r.VZ,
		pitch: encodeAngle(r.Pitch),
		yaw:   encodeAngle(r.Yaw),
		data:  r.OwnerID,
	})
}

// fireworkMetadataPayload builds a rocket's metadata: the stack the client
// draws the burst's stars from, and the body a boost rocket rides.
func fireworkMetadataPayload(it *fireworkEntity) []byte {
	return fireworkMetadataTable(it, nil)
}

// fireworkMetadataState is the typed baseline for a command's metadata delta.
type fireworkMetadataState struct {
	stack       protocol.ItemStack
	name        customName
	attached    int32
	shotAtAngle bool
	fire        bool
}

func fireworkMetadataStateOf(it *fireworkEntity) fireworkMetadataState {
	return fireworkMetadataState{stack: it.stack, name: it.custom,
		attached: it.rocket.AttachedTo, shotAtAngle: it.rocket.ShotAtAngle, fire: it.rocket.FireTicks > 0}
}

// fireworkMetadataTable writes spawn fields (was == nil) or fields changed by a command.
// vanilla: SynchedEntityData.getNonDefaultValues, packDirty
func fireworkMetadataTable(it *fireworkEntity, was *fireworkMetadataState) []byte {
	now := fireworkMetadataStateOf(it)
	spawn := was == nil
	if spawn {
		was = &fireworkMetadataState{}
	}
	var b protocol.Buffer
	b.PutVarInt(it.rocket.ID)
	head := b.Len()
	if now.fire != was.fire {
		b.PutMetadata(protocol.MetadataIndexEntityFlags, protocol.MetadataByte)
		var flags uint8
		if now.fire {
			flags = protocol.EntityFlagOnFire
		}
		b.PutU8(flags)
	}
	// Pairing always includes the carried item, even an empty stack.
	if spawn || !now.stack.Equal(was.stack) {
		b.PutMetadata(protocol.MetadataIndexItemSlot, protocol.MetadataSlot)
		b.PutSlot(now.stack)
	}
	if now.attached != was.attached {
		b.PutMetadata(protocol.MetadataIndexFireworkAttachedTo, protocol.MetadataOptionalVarInt)
		if now.attached != 0 {
			b.PutVarInt(now.attached + 1)
		} else {
			b.PutVarInt(0)
		}
	}
	if now.shotAtAngle != was.shotAtAngle {
		b.PutMetadata(protocol.MetadataIndexFireworkShotAtAngle, protocol.MetadataBoolean)
		b.PutBool(now.shotAtAngle)
	}
	now.name.putMetadata(&b, was.name)
	if b.Len() == head {
		return nil
	}
	b.EndMetadata()
	return b.Bytes()
}

// fireworkBurst is one burst a rocket's tick ended in, carried out of the
// locks; attached is the player a boost rocket rode.
type fireworkBurst struct {
	it         *fireworkEntity
	x, y, z    float64
	attached   *client
	explosions int
	// watchers are the players shown the rocket, who alone see the bang.
	watchers []uuid.UUID
}

// fireworkLaunchVolume is the launch sound's volume, which sets its earshot.
// vanilla: FireworkRocketEntity.tick
const fireworkLaunchVolume = 3.0

// fireworkWatcher is one player as the firework tick sees them.
type fireworkWatcher struct {
	c           *client
	pos         player.Position
	movement    player.Velocity
	chunkRadius int32
}

// fireworkWatchers groups the players by world, read before any lock.
func fireworkWatchers(clients []*client) map[*world.World][]fireworkWatcher {
	watchers := make(map[*world.World][]fireworkWatcher)
	for _, c := range clients {
		if c.player == nil {
			continue
		}
		pos := c.player.Position()
		watchers[c.theWorld()] = append(watchers[c.theWorld()], fireworkWatcher{
			c: c, pos: pos, movement: c.player.KnownMovement(), chunkRadius: c.chunkRadius(),
		})
	}
	return watchers
}

// tickFireworks runs one game tick of every rocket. The players are read
// with nothing held; the flight and the mobs' hurt run under the rocket and
// mob locks; the players' hurt and the sends run after the locks are down.
func (s *Server) tickFireworks() {
	s.lockTick(&s.fireworksMu)
	none := len(s.fireworks) == 0
	s.unlockTick(&s.fireworksMu)
	if none {
		return
	}
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()

	watchers := fireworkWatchers(clients)
	near := nearbyWorlds(watchers, func(watch fireworkWatcher) trackingWatcher {
		return trackingWatcher{c: watch.c, x: watch.pos.X, z: watch.pos.Z, chunkRadius: watch.chunkRadius}
	})
	defer releaseNearbyWorlds(near)

	var (
		out             trackSends
		motions, sounds sendList
		removals        = make(map[*world.World][]int32)
		bursts          []fireworkBurst
		mobHits         []thrownMobHit
		gates           []gatewayTouch
		targetHits      []targetBlockHit
		projectileHits  []projectileBlockHit
		splashes        []projectileSplash
		landings        []projectileLanding
		shots           []projectileShot
		deflects        []breezeDeflect
	)

	boatsByWorld, _, boatRoots := s.boatSnapshots()
	fixtures := s.readProjectileTargetSnapshots()
	gate := s.newEntityGate()
	var held []*fireworkEntity
	defer func() { fireworkTracker.dormantPass(s, held) }()
	s.lockTick(&s.fireworksMu)
	s.lockTick(&s.mobsMu)
	alive := s.fireworks[:0]
	for _, it := range s.fireworks {
		if it.rocket.Dead {
			fireworkTracker.unfile(it)
			continue // the life ran out before this tick
		}
		if !gate.ticks(it.w, it.rocket.X, it.rocket.Z) {
			held = append(held, it)
			alive = append(alive, it)
			continue
		}

		// A boost rocket whose rider has left the world stands where the
		// last follow put it until its life ends.
		var carrier *entity.FireworkCarrier
		if it.rocket.AttachedTo != 0 {
			for _, watch := range watchers[it.w] {
				if watch.c.player.EntityID == it.rocket.AttachedTo {
					pos := watch.pos
					carrier = &entity.FireworkCarrier{
						X: pos.X, Y: pos.Y, Z: pos.Z,
						VX: watch.movement.X, VY: watch.movement.Y, VZ: watch.movement.Z,
						Yaw: pos.Yaw,
					}
				}
			}
		}

		// The bodies a free flight may meet. The launcher's own is pointed
		// out: the flight cannot hit it until it has left it.
		var targets []entity.ArrowTarget
		dim := s.dimensionOfWorld(it.w)
		targets = append(targets, boatsByWorld[it.w]...)
		for _, m := range dim.mobs.herd {
			if m.ID == it.rocket.AttachedTo {
				continue // never the body the rocket rides
			}
			targets = entity.MobProjectileTargets(targets, m, mobEntityType(m.Type), boatRoots[m.ID])
		}
		for _, watch := range watchers[it.w] {
			if watch.c.player.EntityID == it.rocket.AttachedTo {
				continue
			}
			target := watch.c.projectileTarget(watch.pos, projectileUnhittable(watch.c))
			target.RootVehicle = boatRoots[watch.c.player.EntityID]
			targets = append(targets, target)
		}
		targets = fixtures.appendHangings(targets, it.w)
		targets = append(targets, fixtures.cushions[it.w]...)
		targets = append(targets, fixtures.stands[it.w]...)
		targets = append(targets, fixtures.crystals[it.w]...)
		targets = append(targets, fixtures.projectiles[it.w]...)
		var owner *entity.ArrowTarget
		for i := range targets {
			if it.rocket.OwnerID != 0 && targets[i].EntityID == it.rocket.OwnerID {
				owner = &targets[i]
				break
			}
		}

		if it.portal.Cooldown > 0 {
			it.portal.Cooldown--
		}
		from := it.rocket.Box()
		first := it.rocket.Life == 0
		it.queue(&shots, it.w, it.rocket.X, it.rocket.Y, it.rocket.Z, it.ownerUUID)
		for _, ev := range it.rocket.Step(it.w, owner, carrier, targets) {
			if ev.Kind == entity.FireworkHitTarget {
				landings = append(landings, projectileLanding{w: it.w, x: ev.X, y: ev.Y, z: ev.Z, ownerUUID: it.ownerUUID})
			}
			if ev.Kind == entity.FireworkDeflected {
				deflects = appendDeflectBy(deflects, it.w, dim.mobs.herd, ev.Target.EntityID)
			}
			if ev.Kind == entity.FireworkSplashed {
				splashes = append(splashes, projectileSplash{w: it.w, x: ev.X, y: ev.Y, z: ev.Z,
					volume: ev.Volume, pitch: ev.Pitch, owner: it.ownerUUID})
			}
			if ev.Kind != entity.FireworkHitBlock {
				continue
			}
			landings = append(landings, blockProjectileLanding(it.w, ev.BlockHit, it.ownerUUID))
			if !ev.BlockHit.Border && isTargetBlock(ev.BlockHit.State) {
				targetHits = append(targetHits, targetBlockHit{w: it.w, block: ev.BlockHit, ownerID: it.rocket.OwnerID})
			}
			if !ev.BlockHit.Border && isProjectileHitBlock(ev.BlockHit.State) {
				projectileHits = append(projectileHits, projectileBlockHit{w: it.w, block: ev.BlockHit,
					entityType: protocol.EntityTypeFireworkRocket, ownerID: it.rocket.OwnerID, ownerUUID: it.ownerUUID})
			}
		}

		// The launch sound plays from where the first tick's rise took the
		// rocket, to the players within its volume's earshot.
		// vanilla: FireworkRocketEntity.tick's life == 0, ServerLevel.playSeededSound
		if first {
			earshot := soundRange(fireworkLaunchVolume)
			launch := soundEffectIDAt(registry.SoundEntityFireworkRocketLaunch,
				protocol.SoundCategoryAmbient,
				it.rocket.X, it.rocket.Y, it.rocket.Z, fireworkLaunchVolume, 1.0)
			for _, watch := range watchers[it.w] {
				if entity.Dist2(it.rocket.X, it.rocket.Y, it.rocket.Z, watch.pos.X, watch.pos.Y, watch.pos.Z) < earshot*earshot {
					sounds.add(watch.c, outbound{id: protocol.IDPlaySoundEffect, data: launch})
				}
			}
		}

		if it.rocket.Dead {
			burst := fireworkBurst{it: it, x: it.rocket.X, y: it.rocket.Y, z: it.rocket.Z,
				explosions: it.rocket.Explosions, watchers: trackedWatchers(it.tracked)}
			for _, watch := range watchers[it.w] {
				if watch.c.player.EntityID == it.rocket.AttachedTo {
					burst.attached = watch.c
				}
			}
			bursts = append(bursts, burst)
			fireworkTracker.unfile(it)
			removals[it.w] = append(removals[it.w], it.rocket.ID)
			s.entityDestroyed(it.uuid)
			continue
		}

		// Attached rockets only follow their carrier; they do not apply block effects.
		// vanilla: FireworkRocketEntity.tick
		if it.rocket.AttachedTo == 0 {
			if gx, gy, gz, ok := entity.EndGatewayInside(it.w, from, it.rocket.Box()); ok {
				gates = append(gates, gatewayTouch{
					w: it.w, x: gx, y: gy, z: gz, mu: &s.fireworksMu,
					portal: &it.portal, cooldown: projectileCooldownTicks,
					move: func(to player.Position) { s.gatewayMoveFirework(it, to) },
				})
			}
		}
		// The velocity goes out on a movement tick only, to the watchers
		// alone, the newly paired included.
		// vanilla: ServerEntity.sendChanges, sendToTrackingPlayers
		body := fireworkTracker.body(it)
		mt := body.schedule(nil)
		var motion *outbound
		if mt.gate && it.motionDue(it.rocket.VX, it.rocket.VY, it.rocket.VZ, false) {
			motion = &outbound{id: protocol.IDPlaySetEntityMotion,
				data: entityVelocityPayload(it.rocket.ID, it.rocket.VX, it.rocket.VY, it.rocket.VZ)}
		}

		for _, i := range body.watchers(near[it.w]) {
			watch := watchers[it.w][i]
			if watch.c.theWorld() != it.w {
				continue // crossed to another dimension since the snapshot
			}
			was, seen := body.settle(watch.c, watch.pos.X, watch.pos.Z, watch.chunkRadius, &out)
			if !seen || !mt.gate {
				continue
			}
			if motion != nil {
				motions.add(watch.c, *motion)
			}
			body.moveTold(was, watch.c, mt, &out)
		}
		alive = append(alive, it)
	}
	s.fireworks = alive

	// The bursts' hurt of the mobs, under the mob lock: the falloff by the
	// distance to the mob's feet, behind the burst's line-of-sight check.
	// vanilla: FireworkRocketEntity.dealExplosionDamage
	for _, burst := range bursts {
		amount := rules.FireworkBurstDamage(burst.explosions)
		if amount <= 0 {
			continue
		}
		dim := s.dimensionOfWorld(burst.it.w)
		for _, m := range dim.mobs.herd {
			d := math.Sqrt(entity.Dist2(burst.x, burst.y, burst.z, m.X, m.Y, m.Z))
			if !rules.FireworkBurstInBurstRange(d) {
				continue
			}
			minX, minY, minZ, maxX, maxY, maxZ := m.BlastBox()
			if !rules.FireworkBurstVisible(burst.it.w,
				burst.x, burst.y, burst.z, minX, minY, minZ, maxX, maxY, maxZ) {
				continue
			}
			owner := s.entityClient(burst.it.ownerID)
			from := ownerHitSource(m, owner, func() *entity.Mob {
				for _, source := range dim.mobs.herd {
					if source.ID == burst.it.ownerID {
						return source
					}
				}
				return nil
			})
			landed, softened := m.HurtByFirework(rules.FireworkBurstFalloffDamage(amount, d),
				rules.DamageTypeFireworks, burst.it.ownerID,
				owner != nil, creativeClient(owner),
				burst.x, burst.z, from)
			if !landed {
				continue
			}
			hit := thrownMobHit{
				damageType: rules.DamageTypeFireworks,
				w:          burst.it.w,
				victimID:   m.ID,
				kind:       m.Type,
				sound:      mobSound(m, true),
				volume:     m.SoundVolume(),
				pitch:      m.VoicePitch(),
				silent:     m.Silent,
				causeID:    causeEntityID(&damageCause{entityID: burst.it.ownerID}),
				directID:   causeEntityID(&damageCause{entityID: burst.it.rocket.ID}),
				x:          m.X, y: m.Y, z: m.Z,
				vx: m.VX, vy: m.VY, vz: m.VZ,
				softened: softened,
			}
			if ent := dim.mobs.book[m]; ent != nil {
				hit.health = ent.syncHealth()
				hit.watchers = trackedWatchers(ent.tracked)
			}
			mobHits = append(mobHits, hit)
		}
	}
	s.unlockTick(&s.mobsMu)
	s.unlockTick(&s.fireworksMu)
	applyEach(s, splashes, (*Server).splashProjectile)
	applyEach(s, deflects, (*Server).playBreezeDeflect)
	s.touchGateways(gates)
	s.hitTargetBlocks(targetHits)
	s.hitProjectileBlocks(projectileHits)

	s.announceBursts(bursts)
	applyEach(s, shots, (*Server).projectileShootEvent)
	applyEach(s, landings, (*Server).projectileLandEvent)

	s.burstArmorStands(bursts)

	// The rider of a boost rocket takes the whole amount, every other
	// player in reach the falloff.
	for _, burst := range bursts {
		amount := rules.FireworkBurstDamage(burst.explosions)
		if amount <= 0 {
			continue
		}
		cause := &damageCause{
			entityID: burst.it.ownerID,
			directID: causeEntityID(&damageCause{entityID: burst.it.rocket.ID}),
			x:        burst.x, y: burst.y, z: burst.z,
		}
		if burst.attached != nil {
			burst.attached.hurt(amount, rules.DamageTypeFireworks, cause)
		}
		for _, watch := range watchers[burst.it.w] {
			c := watch.c
			if c.player.EntityID == burst.it.rocket.AttachedTo {
				continue // the riding player's hurt is the attached one's
			}
			pos := watch.pos
			d := math.Sqrt(entity.Dist2(burst.x, burst.y, burst.z, pos.X, pos.Y, pos.Z))
			if !rules.FireworkBurstInBurstRange(d) {
				continue
			}
			dims := c.dims(c.player.Appearance().Pose)
			hw := dims.Width / 2
			if !rules.FireworkBurstVisible(burst.it.w,
				burst.x, burst.y, burst.z,
				pos.X-hw, pos.Y, pos.Z-hw,
				pos.X+hw, pos.Y+dims.Height, pos.Z+hw) {
				continue
			}
			c.hurt(rules.FireworkBurstFalloffDamage(amount, d), rules.DamageTypeFireworks, cause)
		}
	}

	// The sends, in the order the packets were gathered.
	for w, ids := range removals {
		s.broadcastWorld(w, protocol.IDPlayRemoveEntities, removeEntitiesPayload(ids))
	}
	out.send()
	motions.send()
	sounds.send()
	// The mob hits' telling, the hurts having been taken under the mob lock.
	// vanilla: LivingEntity.hurtServer
	for _, hit := range mobHits {
		s.tellThrownMobHit(hit)
	}
}

// tryToStartFallFlying answers the fall-flying player command: the wings
// open off the ground, out of water and lava, with a glider on the chest. A refused
// client is put straight back down, which corrects one that predicted its
// own glide.
// vanilla: ServerGamePacketListenerImpl.handlePlayerCommand,
// Player.tryToStartFallFlying
func (c *client) tryToStartFallFlying() {
	if c.fallFlying.Load() || !c.canGlide() {
		c.stopFallFlying()
		return
	}
	pos := c.player.Position()
	if pos.OnGround || c.inLiquidForGlide() {
		c.stopFallFlying()
		return
	}
	if !c.glideAllowed(true) {
		c.stopFallFlying()
		return
	}
	c.fallFlying.Store(true)
	c.updateAppearance(func(*player.Appearance) {})
}

// stopFallFlying takes the wings in, telling a body that was gliding.
// vanilla: Player.stopFallFlying
func (c *client) stopFallFlying() {
	if !c.fallFlying.Load() || !c.glideAllowed(false) || !c.fallFlying.Swap(false) {
		return
	}
	c.updateAppearance(func(*player.Appearance) {})
}

// updateFallFlying is the per-tick half of the glide: a body no longer able
// to glide (landed, glider gone, levitating) is taken out of it, and a
// gliding one wears its glider by 1 every 20 ticks.
// vanilla: LivingEntity.updateFallFlying, canGlide
func (c *client) updateFallFlying() {
	if !c.fallFlying.Load() {
		c.fallFlyTicks = 0
		return
	}
	// vanilla: Entity.checkFallDistanceAccumulation
	if c.player.Velocity().Y > -0.5 {
		c.player.UpdateVitals(func(v *player.Vitals) {
			if v.FallDistance > 1 {
				v.FallDistance = 1
			}
		})
	}
	if c.player.Position().OnGround || !c.canGlide() {
		c.stopFallFlying()
		return
	}
	// The counter goes up at the end of the tick, so the event lands on glide
	// ticks 10, 20, ... and the wear on every second one.
	// vanilla: LivingEntity.updateFallFlying
	if (c.fallFlyTicks+1)%10 == 0 {
		if (c.fallFlyTicks+1)%20 == 0 && c.player.GameMode() != protocol.GameModeCreative {
			c.wearGlider()
		}
		pos := c.player.Position()
		c.srv.gameEvent(c.theWorld(), rules.GameEventElytraGlide, pos.X, pos.Y, pos.Z, c.gameEventContext(0))
	}
	c.fallFlyTicks++
}

// wearGlider puts 1 wear on one usable glider, picked with the player's
// dice among the worn ones; none when the gliders were taken off since the
// glide check.
// vanilla: LivingEntity.updateFallFlying, ItemStack.hurtAndBreak
func (c *client) wearGlider() {
	var w slotWear
	slot, n := -1, int32(1)
	c.player.Inventory(func(inv *player.Inventory) {
		var usable []int
		for i, slot := range inv.EquipmentWindowSlots() {
			if rules.GliderUsable(inv.Slots[slot], player.EquipmentSlots[i]) {
				usable = append(usable, slot)
			}
		}
		if len(usable) == 0 {
			return
		}
		slot = usable[c.ownDice().Intn(len(usable))]
	})
	if slot < 0 {
		return
	}
	if asked := c.askWear(n, nil, slot); asked != nil {
		n = asked[0]
	}
	c.player.Inventory(func(inv *player.Inventory) {
		w = c.wearUnder(inv, slot, rules.ProcessDurabilityChange(inv.Slots[slot], n, c.ownDice()))
	})
	c.tellWear(w)
}

// canGlide reports whether this body may glide: not riding, a usable glider
// worn in any equipment slot, no Levitation, and not flying, which every
// spectator is.
// vanilla: Player.canGlide, LivingEntity.canGlide
func (c *client) canGlide() bool {
	if c.isPassenger() || c.flying.Load() || rules.Levitating(c.player.Effects()) {
		return false
	}
	usable := false
	c.player.Inventory(func(inv *player.Inventory) {
		for i, slot := range inv.EquipmentWindowSlots() {
			if rules.GliderUsable(inv.Slots[slot], player.EquipmentSlots[i]) {
				usable = true
				return
			}
		}
	})
	return usable
}

// inLiquidForGlide reports whether the player stands in water or lava, which
// the start of a glide refuses even while the glide itself continues into it.
// vanilla: Player.tryToStartFallFlying, Entity.isInLiquid
func (c *client) inLiquidForGlide() bool {
	pos := c.player.Position()
	dims := c.dims(c.player.Appearance().Pose)
	inWater, _, _ := entity.BoxInWater(c.theWorld(), pos.X, pos.Y, pos.Z,
		dims.Width, dims.Height, pos.Y+dims.EyeHeight)
	return inWater || entity.BoxInLava(c.theWorld(), pos.X, pos.Y, pos.Z, dims.Width, dims.Height)
}

// announceBursts tells the watchers of each burst, then posts its explode
// event. The entity event the client draws the bang from goes out ahead of
// the hurt. vanilla: FireworkRocketEntity.explode
func (s *Server) announceBursts(bursts []fireworkBurst) {
	for _, burst := range bursts {
		s.tellWatchers(burst.it.w, burst.watchers, []outbound{{id: protocol.IDPlayEntityEvent,
			data: entityEventPayload(burst.it.rocket.ID, protocol.EntityStatusFireworkBurst)}})
	}
	for _, burst := range bursts {
		s.gameEvent(burst.it.w, rules.GameEventExplode, burst.x, burst.y, burst.z, rules.GameEventContext{Player: burst.it.ownerUUID})
	}
}
