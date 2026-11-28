package server

import (
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

const (
	// eyeLocateRadius is the search radius in chunks.
	// vanilla: EnderEyeItem.use
	eyeLocateRadius = 100
	// eyeLaunchPitchMin and eyeLaunchPitchMax bound the launch sound's
	// pitch. vanilla: EnderEyeItem.use, Mth.lerp
	eyeLaunchPitchMin = 0.33
	eyeLaunchPitchMax = 0.5
	// levelEventEyeShatter is the particle burst of a shattered eye.
	// vanilla: LevelEvent.PARTICLES_EYE_OF_ENDER_DEATH
	levelEventEyeShatter = 2003
)

// eyeEntity is one flying eye of ender in a world. Guarded by the eyes
// lock, taken beneath no other lock.
type eyeEntity struct {
	eye   *entity.EyeOfEnder
	w     *world.World
	uuid  uuid.UUID
	stack protocol.ItemStack
	tags  nbt.StringSet
	effectData
	bodyTrack[struct{}]
}

// eyeTracker tracks the eyes of ender. Their movement is tickEyes' own: an
// absolute teleport with the velocity every EyeInterval ticks of the flight.
var eyeTracker = trackedKind[*eyeEntity, struct{}]{
	admitsType:  func(typ string) bool { return typ == eyeEntityID },
	rangeChunks: entity.EyeTrackingRange,
	lock:        func(s *Server) *sync.Mutex { return &s.eyesMu },
	list:        func(s *Server) *[]*eyeEntity { return &s.eyes },
	chunks:      func(s *Server) *entity.ChunkIndex[*eyeEntity] { return &s.eyeChunks },
	filing:      func(it *eyeEntity) *entity.Filing { return &it.eye.Filing },
	uuid:        func(it *eyeEntity) uuid.UUID { return it.uuid },
	track:       func(ent *eyeEntity) *bodyTrack[struct{}] { return &ent.bodyTrack },
	world:       func(ent *eyeEntity) *world.World { return ent.w },
	id:          func(ent *eyeEntity) int32 { return ent.eye.ID },
	dead:        func(ent *eyeEntity) bool { return ent.eye.Dead },
	pose: func(ent *eyeEntity) bodyPose {
		return ent.pose(ent.eye.X, ent.eye.Y, ent.eye.Z)
	},
	pairing: eyePairing,
}

// eyeTag writes an eye of ender's compound: the shared saved fields and the
// stack it flies as. The target and the flight's age are not saved, so a
// reloaded eye keeps its motion and nothing steers it.
// vanilla: EyeOfEnder.addAdditionalSaveData
func eyeTag(ent *eyeEntity) *nbt.OrderedCompound {
	return eyeTagAs(ent, eyeEntityID)
}

func eyeTagAs(ent *eyeEntity, id string) *nbt.OrderedCompound {
	e := ent.eye
	tag := orderedEntityBaseTag(id, e.X, e.Y, e.Z, e.VX, e.VY, e.VZ,
		ent.yaw, ent.pitch, ent.uuid, ent.onGround, ent.cooldown, ent.fire, 0)
	ent.put(tag)
	putTags(tag, ent.tags.Values())
	tag.Set("Item", registry.StackTag(ent.stack))
	return tag
}

// eyeOfTag builds the eye a saved compound describes, or nil for one with no
// place or identity.
// vanilla: EyeOfEnder.readAdditionalSaveData
func (s *Server) eyeOfTag(w *world.World, compound nbt.Compound) *eyeEntity {
	x, y, z, ok := entityPositionOfTag(compound)
	if !ok {
		return nil
	}
	identity, ok := entityUUIDOfTag(compound)
	if !ok {
		return nil
	}
	it := &eyeEntity{eye: entity.NewEyeOfEnder(s.nextEntity(), x, y, z), w: w, uuid: identity}
	applyEyeTag(it, compound)
	return it
}

// eyePairing is what shows an eye: the spawn, then the stack it is drawn as.
func eyePairing(ent *eyeEntity) []outbound {
	var spawn protocol.Buffer
	e := ent.eye
	spawn.PutVarInt(e.ID)
	spawn.PutUUID(ent.uuid)
	spawn.PutVarInt(protocol.EntityTypeEyeOfEnder)
	spawn.PutF64(e.X)
	spawn.PutF64(e.Y)
	spawn.PutF64(e.Z)
	spawn.PutLpVec3(e.VX, e.VY, e.VZ)
	spawn.PutAngle(ent.pitch)
	spawn.PutAngle(ent.yaw)
	spawn.PutAngle(ent.yaw)
	spawn.PutVarInt(0)
	return spawnAndMetadata(spawn.Bytes(), metadataChanges(e.ID, effectData{}.fields(), eyeFields(ent)))
}

// eyeTeleportPayload puts an eye where its flight has carried it.
func eyeTeleportPayload(it *eyeEntity) []byte {
	e := it.eye
	var b protocol.Buffer
	b.Grow(teleportEntityLen)
	protocol.PutEntityPositionSync(&b, e.ID, e.X, e.Y, e.Z, it.yaw, it.pitch, it.onGround)
	return b.Bytes()
}

// worldEyeTargets locates eyes in the dimension the player is using them in.
type worldEyeTargets struct{ w *world.World }

// NearestEyeTarget returns the structure placement's lower corner.
// vanilla: EnderEyeItem.use, StructurePlacement.getLocatePos
func (t worldEyeTargets) NearestEyeTarget(x, y, z int32, radius int) (int32, int32, int32, bool) {
	ids, _ := registry.StructureTagMembers("minecraft:eye_of_ender_located")
	tx, tz, _, ok := t.w.NearestStructure(ids, x, y, z, radius)
	return tx, 0, tz, ok
}

// handleUseEye launches the eye of ender in the hand toward the nearest
// stronghold. Where none is found nothing flies and nothing is spent.
// vanilla: EnderEyeItem.use
func (c *client) handleUseEye(hand int32) bool {
	s := c.srv
	if c.player.Vitals().Dead {
		return false
	}
	mode := c.player.GameMode()
	if mode == protocol.GameModeSpectator {
		return false
	}
	held := c.heldItem(hand)
	if held.Empty() || held.ItemID != int32(registry.ItemEnderEye) {
		return false
	}
	w := c.theWorld()
	pos := c.player.Position()
	pose := c.player.Appearance().Pose

	// An eye aimed at an end portal frame is left to the frame.
	fx, fy, fz := pos.X, pos.Y+c.dims(pose).EyeHeight, pos.Z
	dx, dy, dz := entity.LookVector(pos.Yaw, pos.Pitch)
	reach := c.blockRange()
	if hit, ok := w.ClipBlocks(fx, fy, fz, dx*reach, dy*reach, dz*reach, nil); ok && isEndPortalFrameState(hit.State) {
		return false
	}

	targets := s.eyeTargets
	if targets == nil {
		targets = worldEyeTargets{w}
	}
	tx, ty, tz, found := targets.NearestEyeTarget(rules.BlockOf(pos.X), rules.BlockOf(pos.Y), rules.BlockOf(pos.Z), eyeLocateRadius)
	if !found {
		c.useSwing = swingNone
		// The zero-duration use stays raised until release: decrementing it
		// never reaches zero. vanilla: LivingEntity.updateUsingItem
		c.bowMu.Lock()
		if c.bow.ticks > 0 || c.player.Appearance().Eating {
			c.bowMu.Unlock()
			return true
		}
		c.bow = bowSession{hand: hand, item: held.ItemID, ticks: 1}
		c.bowMu.Unlock()
		c.updateAppearance(func(a *player.Appearance) { a.Eating = true; a.EatingHand = hand })
		return true
	}
	rng := entity.NewDice(s.levelRng.Int63())
	stack := protocol.ItemStack{ItemID: held.ItemID, Count: 1, Raw: held.Raw}
	height := c.dims(pose).Height * entity.EyeSpawnHeightFraction
	ent := &eyeEntity{
		eye:   entity.NewEyeOfEnder(s.nextEntity(), pos.X, pos.Y+height, pos.Z),
		w:     w,
		uuid:  uuid.New(),
		stack: stack,
	}
	ent.eye.SignalTo(float64(tx), float64(ty), float64(tz), rng)
	s.gameEvent(w, rules.GameEventProjectileShoot, ent.eye.X, ent.eye.Y, ent.eye.Z, c.gameEventContext(0))
	s.registerEye(ent)
	c.usedEnderEyeAdvancements(pos.X-float64(tx), pos.Z-float64(tz))
	s.broadcastSoundID(w, registry.SoundEntityEnderEyeLaunch, protocol.SoundCategoryNeutral,
		pos.X, pos.Y, pos.Z, 1,
		float32(eyeLaunchPitchMin+rng.Float64()*(eyeLaunchPitchMax-eyeLaunchPitchMin)))
	c.awardUsedHeld(hand)
	if mode != protocol.GameModeCreative {
		if err := c.consumeHeld(hand); err != nil {
			return false
		}
	}
	// vanilla: InteractionResult.SUCCESS_SERVER — the use swings the arm.
	c.useSwing = swingServer
	return true
}

// registerEye puts a built eye into the world and shows it to every player
// in range.
func (s *Server) registerEye(ent *eyeEntity) { eyeTracker.register(s, ent) }

// tickEyes flies every eye one tick and ends the ones whose flight is over;
// the watchers are told of the move before the tracking pass settles who
// sees which eye.
// vanilla: EyeOfEnder.tick
func (s *Server) tickEyes() {
	watchers := s.trackingWatchers()
	type landing struct {
		ent     *eyeEntity
		x, y, z float64
	}
	var ended []landing
	var splashes []projectileSplash
	var moves []outbound
	var moveTos []showTo
	s.lockTick(&s.eyesMu)
	for _, ent := range s.eyes {
		if ent.eye.Dead {
			continue
		}
		over, splash := ent.eye.Step(ent.w)
		if splash != nil {
			splashes = append(splashes, projectileSplash{w: ent.w, x: splash.X, y: splash.Y, z: splash.Z,
				volume: splash.Volume, pitch: splash.Pitch})
		}
		ent.tickCount++
		if over {
			ended = append(ended, landing{ent, ent.eye.X, ent.eye.Y, ent.eye.Z})
			continue
		}
		if ent.tickCount%entity.EyeInterval != 0 {
			continue
		}
		for _, wh := range watchers {
			if _, held := ent.tracked.get(wh.c); !held || wh.c.theWorld() != ent.w {
				continue
			}
			moves = append(moves,
				outbound{id: protocol.IDPlaySetEntityMotion, data: entityVelocityPayload(ent.eye.ID, ent.eye.VX, ent.eye.VY, ent.eye.VZ)},
				outbound{id: protocol.IDPlayTeleportEntity, data: eyeTeleportPayload(ent)})
			moveTos = append(moveTos, showTo{wh.c, ent.w}, showTo{wh.c, ent.w})
		}
	}
	s.eyes = slices.DeleteFunc(s.eyes, func(ent *eyeEntity) bool {
		if ent.eye.Dead {
			s.entityDestroyed(ent.uuid)
			eyeTracker.unfile(ent)
		}
		return ent.eye.Dead
	})
	s.unlockTick(&s.eyesMu)
	applyEach(s, splashes, (*Server).splashProjectile)
	sendShows(moves, moveTos)
	eyeTracker.frozenPass(s, watchers)

	for _, l := range ended {
		s.finishEye(l.ent, l.x, l.y, l.z)
	}
}

// finishEye ends an eye's flight: it is removed, its sound plays, and it
// drops as an item or shatters.
// vanilla: EyeOfEnder.tick
func (s *Server) finishEye(ent *eyeEntity, x, y, z float64) {
	s.broadcastWorld(ent.w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{ent.eye.ID}))
	s.broadcastSoundID(ent.w, registry.SoundEntityEnderEyeDeath, protocol.SoundCategoryNeutral, x, y, z, 1, 1)
	if ent.eye.Survives() {
		vx, vy, vz := entity.PopVelocity(s.popRng)
		s.spawnItem(ent.w, ent.stack, x, y, z, vx, vy, vz, 0)
		return
	}
	s.broadcastLevelEvent(ent.w, levelEventEyeShatter, rules.BlockOf(x), rules.BlockOf(y), rules.BlockOf(z), 0)
}

func isEndPortalFrameState(state uint16) bool {
	return registry.BlockOf(state) == registry.BlockEndPortalFrame
}

func (s *Server) eyeSnapshot(it *eyeEntity, q *entityQuery) *entitySnapshot {
	typ := eyeEntityID
	if rejectsEntityHead(q, typ, it.uuid, it.tags) {
		return nil
	}
	f := it.eye
	if f.Dead {
		return nil
	}
	pos := player.Position{X: f.X, Y: f.Y, Z: f.Z, Yaw: it.yaw, Pitch: it.pitch}
	e := s.snapshot(q, it.tags, it, f.ID, typ, "Eye of Ender", it.uuid,
		s.dimensionOfWorld(it.w).name, pos,
		box6(f.X-entity.EyeHalfWidth, f.Y, f.Z-entity.EyeHalfWidth,
			f.X+entity.EyeHalfWidth, f.Y+entity.EyeHeight, f.Z+entity.EyeHalfWidth))
	if e != nil {
		e.bind(&s.eyesMu, &it.tags, &eyeEntityOps)
		e.setCustomName(it.custom.tag())
	}
	return e
}

// killEye discards an eye without the end-of-flight sound or drop. vanilla: Entity.kill
func (s *Server) killEye(it *eyeEntity) {
	s.eyesMu.Lock()
	it.eye.Dead = true
	eyeTracker.unfile(it)
	s.eyes = slices.DeleteFunc(s.eyes, func(o *eyeEntity) bool { return o == it })
	s.eyesMu.Unlock()
	s.entityDestroyed(it.uuid)
	s.broadcastWorld(it.w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{it.eye.ID}))
}
