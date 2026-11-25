package server

import (
	"maps"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Mob persistence: entity file compounds and the overworld generator's
// animals. As in playerdata.go, saves use vanilla's keys and loads read what
// they can, defaulting the rest.

// mobTag adapts the native save for map-facing loaders, shoulders and rule predicates.
func mobTag(ent *mobEntity) nbt.Compound {
	return nbt.PlainCopy(mobTagIn(ent, nil)).(nbt.Compound)
}

func mobDataTag(ent *mobEntity) *nbt.OrderedCompound { return mobTagAs(ent, nil, "") }

// mobTagIn writes a native entity save with the herd's passenger wrappers; nil omits riders.
func mobTagIn(ent *mobEntity, book map[*entity.Mob]*mobEntity) *nbt.OrderedCompound {
	return mobTagAs(ent, book, ent.mob.Type.Name())
}

func mobTagAs(ent *mobEntity, book map[*entity.Mob]*mobEntity, id string) *nbt.OrderedCompound {
	m := ent.mob
	tag := nbt.NewOrderedCompound(28)
	x, z := m.X, m.Z
	if v := m.Vehicle; v != nil {
		// vanilla: Entity.saveWithoutId stores the vehicle's X/Z and the rider's Y.
		x, z = v.X, v.Z
	}
	writeEntityBaseTag(tag, id,
		x, m.Y, z, m.VX, m.VY, m.VZ,
		m.Yaw, m.Pitch, ent.uuid, m.OnGround, ent.portal.Cooldown, max(m.FireTicks, restingFire), 0, false)
	if ent.name != nil {
		tag.Set("CustomName", ent.name)
	}
	if ent.nameVisible {
		tag.Set("CustomNameVisible", boolTag(true))
	}
	m.SaveEntityFields(tag)
	putTags(tag, ent.tags.Values())
	if ent.locatorIcon.HasData() {
		tag.Set("locator_bar_icon", ent.locatorIcon.Tag())
	}
	putSpawnReason(tag, id, ent.spawnReason)
	putOrigin(tag, id, ent.origin)
	// Unmodelled fields have no native writer; known fields below replace them.
	for _, key := range nbt.HashOrderedKeys(ent.saved) {
		if key != "attributes" {
			tag.Set(key, nbt.Copy(ent.saved[key]))
		}
	}
	m.SaveWith(tag, func() int64 { return mobGameTime(ent) }, ent.saved["attributes"], func() { ent.saveAttributes(tag) }, func() { putLeash(tag, ent.savedLeash()) }, func() { ent.saveRaid(tag) })
	if len(ent.carriedPassengers) > 0 || (book != nil && len(m.Passengers) > 0) {
		// vanilla: Entity.addSaveData nests passengers whole, so a riding pair
		// reloads linked; an unmodelled rider goes back as read.
		list := nbt.List{Elem: nbt.TypeCompound}
		if book != nil {
			for _, p := range m.Passengers {
				if pe, ok := book[p]; ok {
					list.Items = append(list.Items, mobTagIn(pe, book))
				}
			}
		}
		list.Items = append(list.Items, ent.carriedPassengers...)
		if len(list.Items) > 0 {
			tag.Set("Passengers", list)
		}
	}
	return tag
}

func (ent *mobEntity) saveRaid(tag nbt.CompoundTag) {
	m := ent.mob
	if entity.IsRaiderType(m.Type) {
		// vanilla: Raider.addAdditionalSaveData
		wave, canJoin := 0, false
		if ent.raider != nil {
			wave, canJoin = ent.raider.wave, ent.raider.canJoin
		}
		tag.Set("Wave", nbt.Int(wave))
		tag.Set("CanJoinRaid", boolTag(canJoin))
		if ent.raider != nil {
			if ent.raider.raid != nil {
				tag.Set("RaidId", nbt.Int(ent.raider.raid.ID()))
			} else if ent.raider.savedRaid > 0 {
				tag.Set("RaidId", nbt.Int(ent.raider.savedRaid))
			}
		}
	}
}

// saveAttributes writes every instantiated attribute over the attributes
// list, defaults included; a body with no map yet saves the instances its kind
// creates natively, and after its first tick its first-tick instances too. The
// instances the map makes for pairing and equipment are vanilla's first-tick
// ones, so a body that has not ticked leaves them out unless something
// instantiated them.
// vanilla: LivingEntity.addAdditionalSaveData, AttributeMap.pack,
// LivingEntity.removeFrost
func (ent *mobEntity) saveAttributes(tag nbt.CompoundTag) {
	m := ent.mob
	a := m.Attributes()
	if a == nil {
		if !m.NativeAttributeInstances() && !m.Ticked() {
			return
		}
		a = m.BuildAttributes(savedAttributes(ent.saved))
	}
	loaded, _ := nbt.Get[nbt.List](tag.Entries(), "attributes")
	if out := a.SaveInstances(&loaded, m.Ticked()); out != nil {
		tag.Set("attributes", *out)
	}
}

// mobOfTag reads an entity compound as a live mob, or nil for a kind this
// server does not simulate, which the caller carries through untouched.
// Missing or unreadable fields keep a fresh spawn's values.
func mobOfTag(s *Server, w *world.World, compound nbt.Compound) *mobEntity {
	id, _ := nbt.Get[nbt.String](compound, "id")
	kind := entity.MobTypeOf(string(id))
	if !entity.KnownMobKind(kind) {
		return nil
	}

	// Unlike other kinds, a mob with no usable Pos stands at the origin.
	x, y, z, ok := entityPositionOfTag(compound)
	if !ok {
		x, y, z = 0, 0, 0
	}

	// The saved UUID is kept so references survive a reload; a lost one is
	// replaced by a random one.
	identity, ok := entityUUIDOfTag(compound)
	if !ok {
		identity = uuid.New()
	}

	eid := s.nextMobEntity(kind)
	ent := &mobEntity{
		mob:  entity.NewMob(kind, eid, x, y, z, s.entitySeed(eid)),
		w:    w,
		uuid: identity,
	}
	m := ent.mob
	m.SetParrotSoundRandom(s.envRng)
	structure := w != nil && nbt.BoolField(compound, world.StructureFinalizeKey)
	if structure {
		delete(compound, world.StructureFinalizeKey)
	}
	// A drowned finalized at generation rides the zombie nautilus it rolled.
	if nbt.BoolField(compound, entity.StructureNautilusKey) {
		m.Jockey = entity.JockeyStructureNautilus
	}
	delete(compound, entity.StructureNautilusKey)
	applyMobTag(m, ent, compound)
	if structure || kind == entity.TypeVillager && !m.Villager.Finalized && nbt.BoolField(compound, "AssignProfessionWhenSpawned") && w != nil {
		s.finalizeMobSpawn(w, ent, nil, true)
	}
	return ent
}

// mobGameTime is the mob's world's game time, which persistent anger ends
// are counted on; 0 with no world.
func mobGameTime(ent *mobEntity) int64 {
	if ent == nil || ent.w == nil {
		return 0
	}
	gameTime, _ := ent.w.Time()
	return gameTime
}

// applyMobTag reads a compound's fields onto a live mob, missing or
// unreadable fields left alone; shared by a load and /data.
func applyMobTag(m *entity.Mob, ent *mobEntity, compound nbt.Compound) {
	if x, y, z, ok := entityPositionOfTag(compound); ok {
		m.SetPos(x, y, z)
	}
	m.VX, m.VY, m.VZ = entityMotionOfTag(compound)
	yaw, pitch := entityRotationOfTag(compound)
	m.Pitch = pitch
	m.SetLoadedYaw(yaw)
	if onGround, ok := nbt.ByteField(compound, "OnGround"); ok {
		m.OnGround = onGround != 0
	}
	// A nil *world.World must remain nil through the entity.World interface.
	var w entity.World
	if ent.w != nil {
		w = ent.w
	}
	size := m.PhantomSize()
	m.LoadFrom(compound, w, func() int64 { return mobGameTime(ent) })
	m.SetMetadataNoGravity(nbt.BoolField(compound, "NoGravity"))
	m.SetMetadataFireTicks(fireOfTag(compound))
	ent.portal.Cooldown = portalCooldownOf(compound)
	if entity.IsRaiderType(m.Type) {
		// vanilla: Raider.readAdditionalSaveData
		wave, _ := nbt.IntField(compound, "Wave")
		raidID, _ := nbt.IntField(compound, "RaidId")
		ent.raider = &raiderRef{ent: ent, wave: int(wave), canJoin: nbt.BoolField(compound, "CanJoinRaid"), savedRaid: int(raidID)}
	}
	// A name the component codec refuses leaves the mob unnamed.
	// vanilla: Entity.readAdditionalSaveData
	if name, ok := compound["CustomName"]; ok && protocol.IsTextComponent(name) {
		ent.name = name
	} else {
		ent.name = nil
	}
	ent.mob.Named = ent.name != nil
	ent.nameVisible = nbt.BoolField(compound, "CustomNameVisible")
	// vanilla: LivingEntity.readAdditionalSaveData's locator_bar_icon
	ent.locatorIcon = player.LocatorIcon{}
	if icon, ok := nbt.Get[nbt.Compound](compound, "locator_bar_icon"); ok {
		ent.locatorIcon = player.LocatorIconFromTag(icon)
	}
	ent.tags.Reset(tagsOfTag(compound))
	readSpawnReason(&ent.spawnReason, compound)
	if ent.w != nil {
		readOrigin(&ent.origin, ent.w.DimensionName(), compound)
	}
	// vanilla: Mob.readAdditionalSaveData
	ent.readLeash(leashOfTag(compound))
	ent.saved = maps.Clone(compound)
	for _, key := range mobEntityTags {
		delete(ent.saved, key)
	}
	for _, key := range entity.MobSavedKeys {
		delete(ent.saved, key)
	}
	// A merged list the wake hook above did not install (a traits-only
	// merge onto a mapped mob) still folds into the installed map, or the
	// map would overwrite the merge on the next save.
	if list, ok := nbt.Get[nbt.List](compound, "attributes"); ok {
		if a := m.Attributes(); a != nil {
			a.Load(list)
		}
	}
	// vanilla reads the attributes before a phantom's size, so a size the
	// load changed sets the base over the list's.
	// vanilla: Phantom.readAdditionalSaveData
	if m.PhantomSize() != size {
		m.SyncPhantomSize()
	}
}

// mobEntityTags are the keys mobTag writes beside entity.MobSavedKeys,
// excluded from what a load keeps for the same reason.
var mobEntityTags = []string{
	"id", "Pos", "Motion", "Rotation", "Fire", "OnGround", "NoGravity", "PortalCooldown", "UUID",
	"Passengers", "CustomName", "CustomNameVisible", "Tags", "Wave", "CanJoinRaid", "RaidId", "locator_bar_icon", leashKey, spawnReasonNBTKey, originNBTKey,
}

// resolveVexOwners points loaded vexes at the evoker their saved owner names,
// by identity, since a reload renumbers every body; a name no live mob
// answers to resolves to 0 and the UUID stays on the vex for the evoker's
// next arrival to retry. Callers hold neither the player nor the mob lock;
// the IDs are written under the mob lock, which the readers of the IDs hold.
// vanilla: Vex.readAdditionalSaveData, OwnableEntity.getOwner
func (s *Server) resolveVexOwners(mobs []*mobEntity) {
	var vexes []*mobEntity
	for _, ent := range mobs {
		if ent.mob.Type == entity.TypeVex {
			vexes = append(vexes, ent)
		}
	}
	if len(vexes) == 0 {
		return
	}
	// resolveOwner takes the player and mob locks, so it runs before the write.
	ids := make([]int32, len(vexes))
	for i, ent := range vexes {
		ids[i] = s.resolveOwner(ent.mob.VexOwnerUUID())
	}
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for i, ent := range vexes {
		ent.mob.SetVexOwner(ids[i])
	}
}

// relinkVexOwners re-resolves the owner of every live vex: a wake or a
// summon may have brought the evoker in after its vex, and a reload that
// renumbered it left the ID on a live vex stale, so the saved identity is
// the only name that still holds. vanilla: EntityReference.getEntity,
// resolved at the owner's arrival into the herd. Callers hold neither the
// player nor the mob lock.
func (s *Server) relinkVexOwners() {
	var vexes []*mobEntity
	s.mobsMu.Lock()
	for _, dim := range s.allDims() {
		for _, ent := range s.herdOf(dim.w).book {
			if ent.mob.Type == entity.TypeVex {
				vexes = append(vexes, ent)
			}
		}
	}
	s.mobsMu.Unlock()
	s.resolveVexOwners(vexes)
}

// putMobNameMetadata writes a non-default custom name and visibility. The
// name goes out as the file's tag in network NBT, if the component codec
// accepts it.
func putMobNameMetadata(b *protocol.Buffer, ent *mobEntity) {
	if protocol.IsTextComponent(ent.name) {
		if data, err := nbt.EncodeNetwork(ent.name); err == nil {
			b.PutMetadata(protocol.MetadataIndexCustomName, protocol.MetadataOptionalText)
			b.PutBool(true)
			b.PutRaw(data)
		}
	}
	if ent.nameVisible {
		b.PutMetadata(protocol.MetadataIndexCustomNameVisible, protocol.MetadataBoolean)
		b.PutBool(true)
	}
}

// putMobAnimalMetadata writes non-default age lock, variant and sound
// variant.
func putMobAnimalMetadata(b *protocol.Buffer, m *entity.Mob) {
	if m.AgeLocked && entity.Ageable(m.Type) {
		b.PutMetadata(protocol.MetadataIndexAgeLocked, protocol.MetadataBoolean)
		b.PutBool(true)
	}
	variants, sounds, ok := entity.VariantRegistries(m.Type)
	if !ok {
		return
	}
	variantIndex, soundIndex, variantType, soundType := mobVariantSlots(m.Type)
	defaultVariant, defaultSound := entity.DefaultVariants(m.Type)
	if m.Variant != defaultVariant {
		if id := registryEntryID(variants, m.Variant); id >= 0 {
			b.PutMetadata(variantIndex, variantType)
			b.PutVarInt(id)
		}
	}
	if m.SoundVariant != defaultSound {
		if id := registryEntryID(sounds, m.SoundVariant); id >= 0 {
			b.PutMetadata(soundIndex, soundType)
			b.PutVarInt(id)
		}
	}
}

// registryEntryID is an entry's ID in a synchronized registry, -1 for none.
func registryEntryID(registryName, entry string) int32 {
	r, ok := registry.Get(registryName)
	if !ok {
		return -1
	}
	return r.ID(entry)
}

// nameMob applies a name tag and makes the mob persistent, using the tag
// outside creative; an unnamed tag does nothing.
// vanilla: NameTagItem.interactLivingEntity, Player.interactOn
func (c *client) nameMob(ent *mobEntity, hand int32) error {
	held := c.heldItem(hand)
	if held.CustomName == nil {
		return nil
	}
	s := c.srv
	s.mobsMu.Lock()
	if ent.mob.Dead {
		s.mobsMu.Unlock()
		return nil
	}
	ent.name = protocol.TextTag(*held.CustomName)
	ent.mob.Persistence = true
	var b protocol.Buffer
	b.PutVarInt(ent.mob.ID)
	putMobNameMetadata(&b, ent)
	b.EndMetadata()
	s.mobsMu.Unlock()

	s.broadcastWorld(ent.w, protocol.IDPlayEntityMetadata, b.Bytes())
	if c.player.GameMode() == protocol.GameModeCreative {
		return nil
	}
	return c.consumeHeld(hand)
}

// mobCustomName is a mob's custom name as customNameTag reads it.
func mobCustomName(ent *mobEntity) (protocol.Text, string, bool) {
	return customNameTag(ent.name)
}

// customNameTag is a CustomName tag as a component and as the plain string a
// selector matches; false if tag is nil. A name protocol.Text cannot carry
// falls back to its plain text.
func customNameTag(tag nbt.Tag) (protocol.Text, string, bool) {
	if tag == nil {
		return protocol.Text{}, "", false
	}
	if text, ok := protocol.StyledTextOf(tag); ok {
		return text, protocol.PlainTextOf(tag), true
	}
	plain := protocol.PlainTextOf(tag)
	return protocol.Text{Text: plain}, plain, true
}

// mobDropChancesTag adapts the native drop_chances compound for map-facing readers.
func mobDropChancesTag(m *entity.Mob) nbt.Compound { return m.DropChancesTag().Entries() }

// readDropChances reads a drop_chances compound (entity.ReadDropChances).
func readDropChances(compound nbt.Compound) (chances [6]float32, body, saddle float32) {
	return entity.ReadDropChances(compound)
}

// dyeOfColour is entity.DyeOfColour, the dye ID a colour word is.
func dyeOfColour(colour string) uint8 { return entity.DyeOfColour(colour) }

// generateNaturalMobs grows a chunk's first-generation animals (entity.OriginalMobs).
// Only the non-flat overworld and the Nether grow any (a flat world with
// structure sets is a flat-mode OverworldGenerator), and spawn_mobs off
// stops them before any roll.
// vanilla: NoiseBasedChunkGenerator.spawnOriginalMobs; FlatLevelSource's own is empty
func generateNaturalMobs(s *Server, w *world.World, pos world.ChunkPos) []*mobEntity {
	d := s.dimensionOfWorld(w)
	switch g := d.gen.(type) {
	case *world.OverworldGenerator:
		if g.Flat() != nil {
			return nil
		}
	case *world.NetherGenerator:
	default:
		return nil
	}
	if !w.BoolRule(world.RuleSpawnMobs) {
		return nil
	}

	w.Acquire(pos)
	defer w.Release(pos)
	mobs := entity.OriginalMobs(w, w.SeedValue(), entity.ChunkPos{X: pos.X, Z: pos.Z}, s.nextEntity,
		func(x, z float64) rules.LocalDifficulty {
			return rules.NewLocalDifficulty(w.Difficulty(), s.effectiveDifficultyAt(w, x, z))
		})
	return generatedHerd(s, w, mobs)
}

// generatedHerd wraps a chunk's first-generation mobs, each strider's rider
// seated behind its strider; nobody views the chunk yet.
func generatedHerd(s *Server, w *world.World, mobs []*entity.Mob) []*mobEntity {
	herd := make([]*mobEntity, 0, len(mobs))
	for _, m := range mobs {
		m.SetParrotSoundRandom(s.envRng)
		herd = append(herd, &mobEntity{mob: m, w: w, uuid: uuid.New()})
		if m.Jockey == entity.JockeyBuiltRider {
			rider := m.Rider
			m.Jockey, m.Rider = entity.JockeyNone, nil
			rider.ID = s.nextEntity()
			entity.Ride(m, rider)
			herd = append(herd, &mobEntity{mob: rider, w: w, uuid: uuid.New()})
		}
	}
	return herd
}

// mobTagFields is the synched values mobMetadataState leaves out, defaults
// included: name, visibility, silence, age lock and variants.
func mobTagFields(ent *mobEntity) metaFields {
	var m metaFields
	m.add(protocol.MetadataIndexCustomName, protocol.MetadataOptionalText, func(b *protocol.Buffer) {
		if protocol.IsTextComponent(ent.name) {
			if data, err := nbt.EncodeNetwork(ent.name); err == nil {
				b.PutBool(true)
				b.PutRaw(data)
				return
			}
		}
		b.PutBool(false)
	})
	m.add(protocol.MetadataIndexCustomNameVisible, protocol.MetadataBoolean, func(b *protocol.Buffer) { b.PutBool(ent.nameVisible) })
	m.add(protocol.MetadataIndexSilent, protocol.MetadataBoolean, func(b *protocol.Buffer) { b.PutBool(ent.mob.Silent) })
	if entity.Ageable(ent.mob.Type) {
		m.add(protocol.MetadataIndexAgeLocked, protocol.MetadataBoolean, func(b *protocol.Buffer) { b.PutBool(ent.mob.AgeLocked) })
	}
	if variants, sounds, ok := entity.VariantRegistries(ent.mob.Type); ok {
		variantIndex, soundIndex, variantType, soundType := mobVariantSlots(ent.mob.Type)
		if id := registryEntryID(variants, ent.mob.Variant); id >= 0 {
			m.add(variantIndex, variantType, func(b *protocol.Buffer) { b.PutVarInt(id) })
		}
		if id := registryEntryID(sounds, ent.mob.SoundVariant); id >= 0 {
			m.add(soundIndex, soundType, func(b *protocol.Buffer) { b.PutVarInt(id) })
		}
	}
	return m
}

// mobVariantSlots is where a farm animal's variant and sound variant sit.
func mobVariantSlots(t entity.MobType) (variantIndex, soundIndex uint8, variantType, soundType int32) {
	switch t {
	case entity.TypeZombieNautilus:
		return protocol.MetadataIndexZombieNautilusVariant, 0, protocol.MetadataZombieNautilusVariant, 0
	case entity.TypeFrog:
		return protocol.MetadataIndexFrogVariant, 0, protocol.MetadataFrogVariant, 0
	case entity.TypeCow:
		return protocol.MetadataIndexCowVariant, protocol.MetadataIndexCowSoundVariant,
			protocol.MetadataCowVariant, protocol.MetadataCowSoundVariant
	case entity.TypePig:
		return protocol.MetadataIndexPigVariant, protocol.MetadataIndexPigSoundVariant,
			protocol.MetadataPigVariant, protocol.MetadataPigSoundVariant
	case entity.TypeWolf:
		return protocol.MetadataIndexWolfVariant, protocol.MetadataIndexWolfSoundVariant,
			protocol.MetadataWolfVariant, protocol.MetadataWolfSoundVariant
	case entity.TypeCat:
		return protocol.MetadataIndexCatVariant, protocol.MetadataIndexCatSoundVariant,
			protocol.MetadataCatVariant, protocol.MetadataCatSoundVariant
	}
	return protocol.MetadataIndexChickenVariant, protocol.MetadataIndexChickenSoundVariant,
		protocol.MetadataChickenVariant, protocol.MetadataChickenSoundVariant
}
