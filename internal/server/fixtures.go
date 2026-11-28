package server

import (
	"maps"
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// fixtureEntity is one display, interaction, marker, cushion or ominous
// item spawner in a world, what clients are shown read off its saved
// compound. Guarded
// by the fixtures lock, which its registration takes inside s.mu and
// nothing takes inside another lock.
type fixtureEntity struct {
	entity.Filing
	id      int32
	w       *world.World
	uuid    uuid.UUID
	kind    string
	x, y, z float64
	yaw     float32
	pitch   float32
	// tag is the compound the body was made from; the base fields (id, Pos,
	// Rotation, UUID, Tags) are rebuilt from the struct on every save.
	tag  nbt.Compound
	tags nbt.StringSet
	dead bool
	// age is the ticks the body has lived, which an ominous item spawner
	// counts down by and a cushion times its survives poll by. It is not
	// saved.
	age int
	// riders are the players sitting on a cushion, in boarding order.
	riders []*client
	bodyTrack[struct{}]
}

// fixtureTracker tracks the fixtures, each at its kind's range. A fixture
// moves only when teleported, and a dimension arrival shows none.
var fixtureTracker = trackedKind[*fixtureEntity, struct{}]{
	invalidatePiston: func(dim *dimension) { dim.pistons.fixtures.built = false },

	rangeOf: func(ent *fixtureEntity) int { return entity.FixtureTrackingRange(ent.kind) },
	lock:    func(s *Server) *sync.Mutex { return &s.fixturesMu },
	list:    func(s *Server) *[]*fixtureEntity { return &s.fixtures },
	chunks:  func(s *Server) *entity.ChunkIndex[*fixtureEntity] { return &s.fixtureChunks },
	filing:  func(it *fixtureEntity) *entity.Filing { return &it.Filing },
	uuid:    func(it *fixtureEntity) uuid.UUID { return it.uuid },
	track:   func(ent *fixtureEntity) *bodyTrack[struct{}] { return &ent.bodyTrack },
	world:   func(ent *fixtureEntity) *world.World { return ent.w },
	id:      func(ent *fixtureEntity) int32 { return ent.id },
	// A range-0 kind is never shown, so it costs the passes nothing.
	dead: func(ent *fixtureEntity) bool { return ent.dead || entity.FixtureTrackingRange(ent.kind) == 0 },
	pose: func(ent *fixtureEntity) bodyPose {
		return bodyPose{x: ent.x, y: ent.y, z: ent.z, yaw: ent.yaw, pitch: ent.pitch}
	},
	pairing: fixturePairing,
}

// fixtureOfTag builds a fixture from its saved compound, nil for one that is
// not a fixture or has no position.
// vanilla: Display.readAdditionalSaveData, Interaction.readAdditionalSaveData
func fixtureOfTag(s *Server, w *world.World, compound nbt.Compound) *fixtureEntity {
	id, _ := nbt.Get[nbt.String](compound, "id")
	if !entity.FixtureKind(string(id)) {
		return nil
	}
	x, y, z, ok := entityPositionOfTag(compound)
	if !ok {
		return nil
	}
	identity, has := entityUUIDOfTag(compound)
	if !has {
		identity = uuid.New()
	}
	yaw, pitch := entityRotationOfTag(compound)
	if id == entity.FixtureCushion {
		s.cushionsMade.Store(true)
	}
	ent := &fixtureEntity{
		id: s.nextEntity(), w: w, uuid: identity, kind: string(id),
		x: x, y: y, z: z, yaw: yaw, pitch: pitch,
		tag: maps.Clone(compound), tags: nbt.NewStringSet(tagsOfTag(compound)),
	}
	s.resolveDisplayText(ent, ent.tag)
	return ent
}

// resolveDisplayText replaces a text display's text in tag with its resolved
// component, as the entity itself (@s) at its position. A text that refuses to
// resolve stays as given. Call it without fixturesMu held: a selector search
// takes entity locks.
// vanilla: Display.TextDisplay.readAdditionalSaveData, ComponentUtils.resolve
func (s *Server) resolveDisplayText(ent *fixtureEntity, tag nbt.Compound) {
	text, has := tag["text"]
	if ent.kind != entity.FixtureTextDisplay || !has {
		return
	}
	if cleaned, ok := protocol.ComponentTextOf(protocol.CleanStyle(text)); ok {
		text = protocol.TextTag(cleaned)
	}
	// Unbound to fixtureEntityOps: @s only reads, and binding would make the
	// package-level ops table depend on fixtureOfTag.
	pos := player.Position{X: ent.x, Y: ent.y, Z: ent.z, Yaw: ent.yaw, Pitch: ent.pitch}
	minX, minY, minZ, maxX, maxY, maxZ := entity.FixtureBox(ent.kind, ent.tag, ent.x, ent.y, ent.z)
	src := &commandBlockSource{name: "Text Display"}
	if self := s.snapshot(nil, ent.tags, ent, ent.id, ent.kind, fixtureEnglishName(ent.kind), ent.uuid,
		s.dimensionOfWorld(ent.w).name, pos, box6(minX, minY, minZ, maxX, maxY, maxZ)); self != nil {
		src.self = self
	}
	src.worldScope = worldScope{srv: s, in: func() *dimension { return s.dimensionOfWorld(ent.w) }, by: src.Profile}
	src.pos = pos
	tag["text"] = command.ResolveComponent(src, text)
}

// fixtureTag writes the compound a vanilla entity file holds for the body.
// Team was joined at the load and is never written back.
// vanilla: Entity.saveWithoutId
func fixtureTag(ent *fixtureEntity) *nbt.OrderedCompound {
	return fixtureTagAs(ent, ent.kind)
}

func fixtureTagAs(ent *fixtureEntity, id string) *nbt.OrderedCompound {
	invulnerableTime, _ := nbt.IntField(ent.tag, "invulnerable_time")
	tag := orderedEntityBaseTag(id, ent.x, ent.y, ent.z, 0, 0, 0, ent.yaw, ent.pitch,
		ent.uuid, false, 0, 0, int(invulnerableTime))
	// These fields live in the fixture's compound rather than a simulated body.
	for _, key := range []string{"Motion", "fall_distance", "Fire", "Air", "OnGround", "Invulnerable", "PortalCooldown", "invulnerable_time"} {
		if value, ok := ent.tag[key]; ok {
			tag.Set(key, value)
		}
	}
	customNameOfTag(ent.tag).put(tag)
	for _, key := range []string{"Silent", "NoGravity", "Glowing", "TicksFrozen", "HasVisualFire"} {
		if value, ok := ent.tag[key]; ok {
			tag.Set(key, value)
		}
	}
	putTags(tag, ent.tags.Values())
	if data, ok := ent.tag["data"]; ok {
		tag.Set("data", data)
	}
	entity.FixtureSaved(ent.kind, ent.tag, tag)
	// Keep unmodelled loaded data, after the native writer's fields. Map-only
	// callers cannot supply insertion history for those carried fields.
	for _, key := range nbt.HashOrderedKeys(ent.tag) {
		switch key {
		case "id", "Pos", "Rotation", "UUID", "Tags", "Team":
			continue
		// vanilla: Mob.addAdditionalSaveData; fixtures do not inherit Mob.
		case "NoAI", "CanPickUpLoot", "PersistenceRequired", "LeftHanded", "drop_chances",
			"leash", "home_pos", "home_radius", "DeathLootTable", "DeathLootTableSeed":
			continue
		}
		if _, written := tag.Entries()[key]; !written {
			tag.Set(key, ent.tag[key])
		}
	}
	return tag
}

// summonFixture stands up the fixture the summon's tag describes.
// vanilla: SummonCommand.createEntity
func (s *Server) summonFixture(w *world.World, name string, x, y, z float64, tag nbt.Compound) (*fixtureEntity, error) {
	compound := summonTag(tag, name, x, y, z)
	if woken, fresh := s.wakeChunkOnce(s.dimensionOfWorld(w),
		world.ChunkPos{X: world.ChunkOf(x), Z: world.ChunkOf(z)}); fresh {
		s.registerWake(woken)
	}
	ent := fixtureOfTag(s, w, compound)
	if ent == nil {
		return nil, errNotSimulated
	}
	if s.summonDuplicate(ent.uuid) {
		return nil, command.ErrDuplicateUUID
	}
	s.registerFixture(ent)
	return ent, nil
}

// fixturePairing is what shows a fixture: the spawn, then its metadata
// where any field differs from its default, then who sits on it.
// vanilla: ServerEntity.sendPairingData
func fixturePairing(ent *fixtureEntity) []outbound {
	var b protocol.Buffer
	b.PutVarInt(ent.id)
	b.PutUUID(ent.uuid)
	b.PutVarInt(entity.FixtureTypeID(ent.kind))
	b.PutF64(ent.x)
	b.PutF64(ent.y)
	b.PutF64(ent.z)
	b.PutLpVec3(0, 0, 0)
	b.PutAngle(ent.pitch)
	b.PutAngle(ent.yaw)
	b.PutAngle(0) // head yaw
	b.PutVarInt(0)
	packets := pairing(outbound{id: protocol.IDPlaySpawnEntity, data: b.Bytes()},
		outbound{id: protocol.IDPlayEntityMetadata, data: entity.FixtureMetadata(ent.id, ent.kind, ent.tag)})
	if len(ent.riders) > 0 {
		packets = append(packets, outbound{id: protocol.IDPlaySetPassengers, data: setPassengersPayload(ent.id, cushionRiderIDs(ent))})
	}
	return bundle(packets)
}

// registerFixture puts a built fixture into the world and shows it to every
// player in range.
func (s *Server) registerFixture(ent *fixtureEntity) { fixtureTracker.register(s, ent) }

// killFixture discards a fixture and tells its watchers.
func (s *Server) killFixture(ent *fixtureEntity) {
	s.fixturesMu.Lock()
	s.removeFixtureLocked(ent)
	riders := slices.Clone(ent.riders)
	s.fixturesMu.Unlock()
	ejectCushionRiders(riders)
	s.broadcastWorld(ent.w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{ent.id}))
}

// tickFixtureTracking settles who sees which fixture, frozen or not. It
// reads the players' places only while a fixture stands.
func (s *Server) tickFixtureTracking() {
	s.lockTick(&s.fixturesMu)
	empty := len(s.fixtures) == 0
	s.unlockTick(&s.fixturesMu)
	if !empty {
		fixtureTracker.frozenPass(s, s.trackingWatchers())
	}
}

func (s *Server) fixtureSnapshot(ent *fixtureEntity, q *entityQuery) *entitySnapshot {
	typ := ent.kind
	if rejectsEntityHead(q, typ, ent.uuid, ent.tags) {
		return nil
	}
	if ent.dead {
		return nil
	}
	pos := player.Position{X: ent.x, Y: ent.y, Z: ent.z, Yaw: ent.yaw, Pitch: ent.pitch}
	minX, minY, minZ, maxX, maxY, maxZ := entity.FixtureBox(ent.kind, ent.tag, ent.x, ent.y, ent.z)
	e := s.snapshot(q, ent.tags, ent, ent.id, typ, fixtureEnglishName(ent.kind), ent.uuid,
		s.dimensionOfWorld(ent.w).name, pos, box6(minX, minY, minZ, maxX, maxY, maxZ))
	if e == nil {
		return nil
	}
	e.setCustomName(ent.tag["CustomName"])
	e.bind(&s.fixturesMu, &ent.tags, &fixtureEntityOps)
	return e
}

// fixtureLoot builds a fixture's loot entity for a criterion's entity
// condition, taking the fixture lock when called.
func (s *Server) fixtureLoot(ent *fixtureEntity) func() rules.LootEntity {
	return func() rules.LootEntity {
		s.fixturesMu.Lock()
		snap := s.fixtureSnapshot(ent, nil)
		s.fixturesMu.Unlock()
		if snap == nil {
			return rules.LootEntity{Type: ent.kind}
		}
		return command.LootEntityOf(snap)
	}
}

// fixtureEnglishName is the name a selector's name option matches.
func fixtureEnglishName(kind string) string {
	switch kind {
	case entity.FixtureBlockDisplay:
		return "Block Display"
	case entity.FixtureItemDisplay:
		return "Item Display"
	case entity.FixtureTextDisplay:
		return "Text Display"
	case entity.FixtureInteraction:
		return "Interaction"
	case entity.FixtureOminousItemSpawner:
		return "Ominous Item Spawner"
	case entity.FixtureCushion:
		return "Cushion"
	}
	return "Marker"
}

// fixtureByEntityID is the live fixture of kind with the id in w, nil when
// there is none.
func (s *Server) fixtureByEntityID(w *world.World, id int32, kind string) *fixtureEntity {
	s.fixturesMu.Lock()
	defer s.fixturesMu.Unlock()
	for _, ent := range s.fixtures {
		if ent.id == id && ent.w == w && !ent.dead && ent.kind == kind {
			return ent
		}
	}
	return nil
}

// fixtureBox is the fixture's box, read under the fixtures lock.
func (s *Server) fixtureBox(ent *fixtureEntity) (minX, minY, minZ, maxX, maxY, maxZ float64) {
	s.fixturesMu.Lock()
	defer s.fixturesMu.Unlock()
	return entity.FixtureBox(ent.kind, ent.tag, ent.x, ent.y, ent.z)
}

// recordInteraction stores the player and game time of the last attack or
// use under key ("attack" or "interaction"), which /data reads back and a
// save keeps. vanilla: Interaction.skipAttackInteraction, Interaction.interact
func (s *Server) recordInteraction(ent *fixtureEntity, key string, player uuid.UUID) {
	gameTime, _ := s.world.Time()
	s.fixturesMu.Lock()
	ent.tag[key] = nbt.Compound{
		"player":    nbt.UUIDToIntArray(player),
		"timestamp": nbt.Long(gameTime),
	}
	s.fixturesMu.Unlock()
}

// attackInteraction records a player's hit on an interaction entity; true when
// the target is one. Nothing is hurt, and the blow spends no cooldown.
// vanilla: ServerGamePacketListenerImpl.handleAttack, Player.cannotAttack
func (c *client) attackInteraction(target int32) bool {
	s := c.srv
	ent := s.fixtureByEntityID(c.theWorld(), target, entity.FixtureInteraction)
	if ent == nil {
		return false
	}
	minX, minY, minZ, maxX, maxY, maxZ := s.fixtureBox(ent)
	if c.withinAttackRange(minX, minY, minZ, maxX, maxY, maxZ) {
		s.recordInteraction(ent, "attack", c.player.UUID)
		// vanilla: Interaction.skipAttackInteraction, a generic 1-damage blow
		c.hurtEntityAdvancements(advancement.Entity{Type: "minecraft:interaction", Loot: s.fixtureLoot(ent), Blow: &advancement.Blow{
			Dealt: 1, Taken: 1, Type: rules.DamageTypeGeneric, Attacker: c.LootEntity, Direct: c.LootEntity}})
	}
	return true
}

// useInteraction records a player's use of an interaction entity; true when
// the target is one. vanilla: Interaction.interact, ServerGamePacketListenerImpl.handleInteract
func (c *client) useInteraction(target, hand int32) bool {
	s := c.srv
	ent := s.fixtureByEntityID(c.theWorld(), target, entity.FixtureInteraction)
	if ent == nil {
		return false
	}
	held := c.heldItem(hand)
	pos := c.player.Position()
	minX, minY, minZ, maxX, maxY, maxZ := s.fixtureBox(ent)
	if rules.WithinEntityInteractionRange(c.entityRange(),
		pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z,
		minX, minY, minZ, maxX, maxY, maxZ) {
		s.recordInteraction(ent, "interaction", c.player.UUID)
		c.interactedWithEntityAdvancements(advancement.EntityInteraction{Type: "minecraft:interaction", Loot: s.fixtureLoot(ent)}, held)
	}
	return true
}

// fixtureFields is every synched value of the fixture, defaults included, for
// the delta a write sends.
func fixtureFields(ent *fixtureEntity) metaFields {
	fields := entity.FixtureFields(ent.kind, ent.tag, true)
	out := make(metaFields, len(fields))
	for i, f := range fields {
		out[i] = metaField{index: f.Index, typ: f.Type, value: f.Value}
	}
	return out
}

// setFixtureData loads a merged compound onto the fixture and tells its
// watchers the metadata that changed and, when it moved, where it now stands.
// The id and UUID stay.
// vanilla: EntityDataAccessor.setData, Entity.load
func (s *Server) setFixtureData(ent *fixtureEntity, data nbt.Compound) {
	data = maps.Clone(data)
	s.resolveDisplayText(ent, data)
	s.fixturesMu.Lock()
	if ent.dead {
		s.fixturesMu.Unlock()
		return
	}
	before := fixtureFields(ent)
	if entity.IsDisplayFixture(ent.kind) {
		// vanilla: Display.setTransformationInterpolationDelay forces the
		// write dirty, and the client restarts interpolation only on it.
		before = slices.DeleteFunc(before, func(f metaField) bool { return f.index == entity.DisplayStartDelayIndex })
	}
	ent.tag = maps.Clone(data)
	fixtureTracker.invalidatePistonGrid(s, ent)
	if x, y, z, ok := entityPositionOfTag(data); ok && (x != ent.x || y != ent.y || z != ent.z) {
		ent.x, ent.y, ent.z = x, y, z
		ent.Refile(entity.ChunkAt(x, z))
		ent.teleported = true
	}
	if _, has := data["Rotation"]; has {
		if yaw, pitch := entityRotationOfTag(data); yaw != ent.yaw || pitch != ent.pitch {
			ent.yaw, ent.pitch = yaw, pitch
			ent.teleported = true
		}
	}
	if _, has := data["Tags"]; has {
		ent.tags.Reset(tagsOfTag(data))
	}
	payload := metadataChanges(ent.id, before, fixtureFields(ent))
	watchers := trackedWatchers(ent.tracked)
	w := ent.w
	s.fixturesMu.Unlock()
	s.sendEntityMetadata(w, watchers, payload)
}

// teleportFixture moves a fixture, which the next tracking pass tells.
// vanilla: Entity.teleportTo
func (s *Server) teleportFixture(ent *fixtureEntity, to player.Position) {
	fixtureTracker.teleport(s, ent, func() {
		ent.x, ent.y, ent.z, ent.yaw, ent.pitch = to.X, to.Y, to.Z, to.Yaw, to.Pitch
		ent.Refile(entity.ChunkAt(to.X, to.Z))
	})
}

// fixtureTwinAt builds the fixture that continues this one in another
// dimension: a new id, the same UUID and compound. The caller holds the
// fixtures lock.
// vanilla: Entity.teleportCrossDimension
func (s *Server) fixtureTwinAt(ent *fixtureEntity, to *dimension, pos player.Position) *fixtureEntity {
	tag := fixtureTag(ent)
	tag.Set("Pos", nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(pos.X), nbt.Double(pos.Y), nbt.Double(pos.Z)}})
	tag.Set("Rotation", nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{nbt.Float(pos.Yaw), nbt.Float(pos.Pitch)}})
	return fixtureOfTag(s, to.w, tag.Entries())
}

// unlistFixtureLocked takes a fixture out of the list, keeping its scores for
// a twin that carries its UUID on; the fixtures lock is held.
func (s *Server) unlistFixtureLocked(ent *fixtureEntity) {
	ent.dead = true
	// Searched from the end: the newest fixture is the one usually removed, and
	// the list keeps spawn order for selector ties.
	for i, v := range slices.Backward(s.fixtures) {
		if v == ent {
			fixtureTracker.unfile(ent)
			s.fixtures = slices.Delete(s.fixtures, i, i+1)
			break
		}
	}
}

// removeFixtureLocked unlists a destroyed fixture and forgets its scores; the
// fixtures lock is held.
func (s *Server) removeFixtureLocked(ent *fixtureEntity) {
	s.unlistFixtureLocked(ent)
	s.entityDestroyed(ent.uuid)
}
