package entity

import (
	"math"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/merchant"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The mob's saved form: the entity compound fields the body carries. The
// shared Entity fields (id, Pos, Motion, Rotation, OnGround, Fire,
// NoGravity, PortalCooldown, UUID) and the server's own (name, tags, raid,
// passengers, attribute map) are the caller's.

// animalOwnerUUID accepts the legacy UUID string only for animal owners.
// vanilla: EntityReference.readWithOldOwnerConversion, OldUsersConverter.convertMobOwnerIfNecessary
func animalOwnerUUID(tag nbt.Compound) (uuid.UUID, bool) {
	if id, ok := nbt.UUIDField(tag, "Owner"); ok {
		return id, true
	}
	text, ok := nbt.Get[nbt.String](tag, "Owner")
	if !ok || len(text) != 36 {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(string(text))
	return id, err == nil
}

// SaveTo writes the mob's fields into tag, which already holds the shared
// Entity fields and the tags a load kept; a zombie's traits merge into its
// attributes list. gameTime is asked only of an angry mob, to count its
// anger's end.
func (m *Mob) SaveTo(tag nbt.Compound, gameTime func() int64) {
	m.SaveEntityFields(tag)
	m.SaveWith(tag, gameTime, tag["attributes"], nil, nil, nil)
	// Map-facing simulation and generation callers retain their loader's representation.
	for key, value := range tag {
		tag[key] = nbt.PlainCopy(value)
	}
}

// SaveWith inserts server-owned attributes and leash data in their superclass phases.
func (m *Mob) SaveWith(tag nbt.CompoundTag, gameTime func() int64, keptAttributes nbt.Tag, attributes, leash, raider func()) {
	tag.Set("Health", nbt.Float(m.Health))
	var sleepingPos nbt.IntArray
	if m.Type == TypeVillager && m.Sleeping {
		bed := m.vb.sleepPos
		sleepingPos = nbt.IntArray{bed.X, bed.Y, bed.Z}
	}
	m.SaveWithSleepingPos(tag, func() {
		if keptAttributes != nil {
			tag.Set("attributes", nbt.Copy(keptAttributes))
		} else {
			// vanilla: LivingEntity.addAdditionalSaveData always stores the list.
			tag.Set("attributes", nbt.List{Elem: nbt.TypeCompound})
		}
		if IsEquine(m.Type) {
			tag.Set("attributes", horseAttributesTag(m))
		}
		if m.Type == TypeWolf {
			tag.Set("attributes", wolfAttributesTag(m, tag.Entries()["attributes"]))
		}
		if Family(m.Type) == TypeZombie {
			tag.Set("attributes", zombieAttributesTag(m, tag.Entries()["attributes"]))
		} else if list, ok := spawnBonusAttributes(m, tag.Entries()["attributes"]); ok {
			tag.Set("attributes", list)
		}
		if m.IsShulker() {
			tag.Set("attributes", shulkerAttributesTag(m, tag.Entries()["attributes"]))
		}
		if attributes != nil {
			attributes()
		}
		if effects := effect.Encode(m.AllEffects()); effects != nil {
			tag.Set("active_effects", *effects)
		}
	}, sleepingPos)
	tag.Set("AbsorptionAmount", nbt.Float(m.Absorption))
	if equipment := equipmentTag(m); len(equipment.Entries()) > 0 {
		tag.Set("equipment", equipment)
	}
	m.mobFields(mobCodec{tag: tag.Entries(), out: tag}, leash)
	if savesAge(m.Type) {
		tag.Set("Age", nbt.Int(m.BabyAge))
		tag.Set("ForcedAge", nbt.Int(m.forcedAge))
		tag.Set("AgeLocked", boolTag(m.AgeLocked))
	}
	if IsAnimal(m.Type) {
		tag.Set("InLove", nbt.Int(m.InLove))
		if m.HasLoveCause {
			tag.Set("LoveCause", nbt.UUIDToIntArray(m.LoveCause))
		}
	}
	if m.Size > 0 {
		// vanilla: AbstractCubeMob.addAdditionalSaveData, the size less one
		tag.Set("Size", nbt.Int(m.Size-1))
		tag.Set("wasOnGround", boolTag(m.OnGround))
	}
	// Baby state as each kind's vanilla read keeps it: the zombie's IsBaby
	// boolean, the animals' Age int (negative while growing).
	if Family(m.Type) == TypeZombie {
		tag.Set("IsBaby", boolTag(m.IsBaby()))
		// vanilla: Zombie.addAdditionalSaveData
		tag.Set("CanBreakDoors", boolTag(m.CanBreakDoors))
		// vanilla: ConversionTracker.addAdditionalSaveData, afflicted (eyes in
		// water) ? afflictionTime : -1
		tag.Set("DrownedConversionTime", nbt.Int(-1))
		if m.DrownedConverting {
			tag.Set("DrownedConversionTime", nbt.Int(m.DrownedConversionTime))
		}
		tag.Set("InWaterTime", nbt.Int(-1))
		if m.waterFluid.eyesIn {
			tag.Set("InWaterTime", nbt.Int(m.InWaterTime))
		}
	} else if m.Type == TypeZoglin {
		// vanilla: Zoglin.addAdditionalSaveData
		tag.Set("IsBaby", boolTag(m.IsBaby()))
	}

	if m.Type == TypeWanderingTrader && m.Offers != nil {
		tag.Set("Offers", m.Offers)
	}
	m.kindFields(mobCodec{tag: tag.Entries(), out: tag}, raider)
	m.dragonFields(tag, false)
	m.pandaFields(tag, false)
	if m.Type == TypeTadpole {
		m.tadpoleBrainFields(tag, false)
	}
	if IsNautilus(m.Type) {
		m.nautilusFields(tag, false)
	}
	if m.Type == TypeWolf {
		tag.Set("CollarColor", nbt.Byte(DyeOfColour(m.Collar)))
	}
	if m.Variant != "" {
		tag.Set("variant", nbt.String(m.Variant))
		if m.SoundVariant != "" && m.Type != TypeWolf {
			tag.Set("sound_variant", nbt.String(m.SoundVariant))
		}
	}
	if m.Type == TypeCat {
		tag.Set("CollarColor", nbt.Byte(DyeOfColour(m.Collar)))
	}
	if m.Type == TypeSheep {
		tag.Set("Color", nbt.Byte(DyeOfColour(m.Colour)))
	}
	if m.Type == TypeParrot {
		tag.Set("Variant", nbt.Int(int32(m.parrot.variant)))
	}
	if m.Type == TypeSniffer {
		m.snifferFields(tag, false)
	}
	if m.Type == TypeAxolotl {
		tag.Set("Variant", nbt.Int(m.AxolotlVariant()))
		memories := m.newMemoryMap()
		if m.axolotl.huntingCooldown > 0 {
			memories.Set("minecraft:has_hunting_cooldown", memoryTag(nbt.Byte(1), int64(m.axolotl.huntingCooldown)))
		}
		if m.axolotl.playDead > 0 {
			memories.Set("minecraft:play_dead_ticks", memoryTag(nbt.Int(m.axolotl.playDead)))
		}
		if m.axolotl.temptationCooldown > 0 {
			memories.Set("minecraft:temptation_cooldown_ticks", memoryTag(nbt.Int(m.axolotl.temptationCooldown)))
		}
		tag.Set("Brain", brainTag(memories))
		tag.Set("FromBucket", boolTag(m.FromBucket))
	}
	if m.IsFish() || m.Type == TypeTadpole {
		// vanilla: AbstractFish.addAdditionalSaveData, Salmon.addAdditionalSaveData
		tag.Set("FromBucket", boolTag(m.FromBucket))
		if m.Type == TypeSalmon {
			tag.Set("type", nbt.String(SalmonSizeName(m.SalmonSize)))
		}
		if m.Type == TypeTropicalFish {
			tag.Set("Variant", nbt.Int(m.TropicalVariant))
		}
		if m.Type == TypePufferfish {
			tag.Set("PuffState", nbt.Int(m.puffer.state))
		}
	}
	if m.Type == TypeTadpole {
		m.tadpoleFields(tag, false)
	}
	if m.Type == TypeBee {
		// vanilla: Bee.addAdditionalSaveData
		mem := m.BeeMemory()
		if mem.Hive != nil {
			tag.Set("hive_pos", nbt.IntArray{mem.Hive[0], mem.Hive[1], mem.Hive[2]})
		}
		if mem.Flower != nil {
			tag.Set("flower_pos", nbt.IntArray{mem.Flower[0], mem.Flower[1], mem.Flower[2]})
		}
		tag.Set("HasNectar", boolTag(m.HasNectar()))
		tag.Set("HasStung", boolTag(m.HasStung()))
		tag.Set("TicksSincePollination", nbt.Int(mem.NoNectar))
		tag.Set("CannotEnterHiveTicks", nbt.Int(mem.StayOut))
		tag.Set("CropsGrownSincePollination", nbt.Int(mem.CropsGrown))
	}
	if m.Type == TypeMooshroom {
		// vanilla: MushroomCow.addAdditionalSaveData's Type
		tag.Set("Type", nbt.String(m.Mushroom))
		tag.Delete("stew_effects")
		if m.StewEffects != nil {
			list := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{}}
			for _, e := range m.StewEffects {
				// vanilla: SuspiciousStewEffects.Entry.CODEC
				entry := nbt.NewOrderedCompound(2)
				entry.Set("id", nbt.String(registry.MobEffectName(e.Effect)))
				entry.Set("duration", nbt.Int(e.Duration))
				list.Items = append(list.Items, entry)
			}
			tag.Set("stew_effects", list)
		}
	}
	if x, y, z, bound := m.CreakingHome(); bound {
		// vanilla: Creaking.addAdditionalSaveData; the heart is the whole
		// home, so there is no home_radius
		tag.Set("home_pos", nbt.IntArray{x, y, z})
	}
	if m.Type == TypeCreeper {
		// vanilla: Creeper.addAdditionalSaveData
		tag.Set("Fuse", nbt.Short(clampToShort(m.MaxSwell)))
		tag.Set("ExplosionRadius", nbt.Byte(clampToByte(m.ExplosionRadius)))
		tag.Set("ignited", boolTag(m.Ignited))
	}
	if m.IsGhast() {
		// vanilla: Ghast.addAdditionalSaveData
		tag.Set("ExplosionPower", nbt.Byte(clampToByte(m.GhastExplosionPower())))
	}
	if m.Type == TypeEnderman {
		// vanilla: EnderMan.addAdditionalSaveData, NeutralMob.addPersistentAngerSaveData
		if state := m.CarriedBlock(); state != 0 {
			tag.Set("carriedBlockState", registry.BlockStateCodecTag(state))
		}
	}
	if m.HasAnger() {
		// vanilla: NeutralMob.addPersistentAngerSaveData
		endTime := int64(-1)
		if ticks, _, _ := m.Anger(); ticks > 0 {
			endTime = gameTime() + int64(ticks)
		} else if m.anger.neverAngered {
			endTime = 0
		}
		tag.Set("anger_end_time", nbt.Long(endTime))
		if _, target, ok := m.Anger(); ok {
			tag.Set("angry_at", nbt.UUIDToIntArray(target))
		}
	}
	if m.Type == TypeWolf && m.SoundVariant != "" {
		tag.Set("sound_variant", nbt.String(m.SoundVariant))
	}
	if m.Type == TypePhantom {
		// vanilla: Phantom.addAdditionalSaveData
		if x, y, z, ok := m.AnchorPos(); ok {
			tag.Set("anchor_pos", nbt.IntArray{x, y, z})
		}
		tag.Set("size", nbt.Int(int32(m.PhantomSize())))
	}

	if m.Type == TypeVillager {
		// vanilla: Villager.addAdditionalSaveData
		m.saveBrain(tag)
		if m.Offers != nil {
			tag.Set("Offers", m.Offers)
		}
		m.putInventory(tag)
		if m.vb.assignProfessionWhenSpawned {
			tag.Set("AssignProfessionWhenSpawned", nbt.Byte(1))
		}
		tag.Set("VillagerData", villagerDataTag(m.Villager))
		tag.Set("VillagerDataFinalized", boolTag(m.Villager.Finalized))
		m.saveFood(tag)
		m.saveGossip(tag)
		tag.Set("LastRestock", nbt.Long(m.vb.lastRestock))
		tag.Set("LastGossipDecay", nbt.Long(m.vg.lastDecay))
		tag.Set("RestocksToday", nbt.Int(m.vb.restocksToday))
	}

	if m.Type == TypeEvoker || m.Type == TypeIllusioner {
		// vanilla: SpellcasterIllager.addAdditionalSaveData
		tag.Set("SpellTicks", nbt.Int(m.SpellTicks()))
	}
	if m.Type == TypeRavager {
		// vanilla: Ravager.addAdditionalSaveData
		attack, stun, roar := m.RavagerTicks()
		tag.Set("AttackTick", nbt.Int(attack))
		tag.Set("StunTick", nbt.Int(stun))
		tag.Set("RoarTick", nbt.Int(roar))
	}
	if m.Type == TypeVex {
		// vanilla: Vex.addAdditionalSaveData
		if x, y, z, ok := m.VexBound(); ok {
			tag.Set("bound_pos", nbt.IntArray{x, y, z})
		}
		if ticks, limited := m.VexLife(); limited {
			tag.Set("life_ticks", nbt.Int(ticks))
		}
		if owner := m.VexOwnerUUID(); owner != uuid.Nil {
			tag.Set("owner", nbt.UUIDToIntArray(owner))
		}
	}
	if m.Johnny {
		// vanilla: Vindicator.addAdditionalSaveData
		tag.Set("Johnny", boolTag(true))
	}
	if m.Type == TypeZombieVillager {
		tag.Set("VillagerData", villagerDataTag(m.Villager))
		tag.Set("VillagerDataFinalized", boolTag(m.Villager.Finalized))
		if m.Offers != nil {
			tag.Set("Offers", m.Offers)
		}
		m.saveCure(tag)
	}
	if m.Type == TypeSkeleton {
		// vanilla: Skeleton.addAdditionalSaveData
		tag.Set("StrayConversionTime", nbt.Int(-1))
		if m.StrayConverting {
			tag.Set("StrayConversionTime", nbt.Int(m.StrayConversionTime))
		}
		tag.Set("FreezingTime", nbt.Int(-1))
		if m.lastInPowderSnow {
			tag.Set("FreezingTime", nbt.Int(m.inPowderSnowTime))
		}
	}
	if m.Type == TypePiglin || m.Type == TypePillager || m.Type == TypeWanderingTrader {
		m.putInventory(tag)
	}
}

// LoadFrom reads SaveTo's fields from compound; an absent or unreadable
// field keeps the mob's value. The caller reads the position first. w is
// where a read-back growth moves the body clear; gameTime is as for SaveTo.
func (m *Mob) LoadFrom(compound nbt.Compound, w World, gameTime func() int64) {
	m.LivingState.LoadFrom(compound)
	m.dragonFields(compound, true)
	m.pandaFields(compound, true)
	m.loadDeathLootTable(compound)
	// The leader's boosted maximum is read before the health it caps.
	if Family(m.Type) == TypeZombie {
		if t, ok := zombieTraitsOfTag(compound); ok {
			m.SetTraits(t)
		}
	}
	m.readRolledAttributes(compound)
	if m.Size > 0 {
		// vanilla: AbstractCubeMob.readAdditionalSaveData; the size before
		// the maximum health it sizes.
		size, _ := nbt.IntField(compound, "Size")
		m.SetSize(int(size)+1, m.Type != TypeSulfurCube)
	}
	if m.Type == TypeWolf || m.Type == TypeCat || IsNautilus(m.Type) || m.Type == TypeParrot {
		// vanilla: TamableAnimal.readAdditionalSaveData; setTame raises the
		// wolf's maximum before the health it caps.
		if owner, ok := animalOwnerUUID(compound); ok {
			m.Tamable.Tame, m.Tamable.Owner = true, owner
			if m.Type == TypeWolf {
				m.MaxHealth = 40
				m.syncMaxHealth()
			}
		}
		// vanilla: Wolf.readAdditionalSaveData; an absent collar is red. A
		// codec reads it (DyeColor.LEGACY_ID_CODEC), so a float truncates.
		m.Collar = WolfDefaultCollar
		if dye, ok := nbt.IntValue(compound["CollarColor"]); ok {
			m.Collar = colourOfDye(uint8(dye))
		}
	}
	if v, ok := nbt.IntValue(compound["RabbitType"]); ok && m.Type == TypeRabbit && ValidRabbitType(v) {
		// vanilla: Rabbit.readAdditionalSaveData, by Rabbit.Variant.LEGACY_CODEC;
		// an unknown id is brown. Read before the attribute map, whose
		// variant modifier depends on it.
		m.RabbitType = v
	}
	if list, ok := nbt.Get[nbt.List](compound, "attributes"); ok && savedAttrEdits(list, m) {
		// A list the kind's own save cannot have written: a command's
		// edits, which the map owns. The hook runs after every read that
		// sizes the bases (traits, horse rolls, cube size, taming) and
		// before the health it caps; vanilla reads the same way.
		m.SetAttributes(m.BuildAttributes(list))
	}
	if health, ok := nbt.FloatField(compound, "Health"); ok {
		// vanilla: LivingEntity.readAdditionalSaveData, setHealth
		m.Health = min(max(float64(health), 0), m.healthCap())
	}
	if air, ok := nbt.IntField(compound, "Air"); ok {
		m.AirSupply = int(air)
	}
	// fall_distance, falling back to the legacy FallDistance float.
	m.FallDistance = max(0, nbt.DoubleFieldOr(compound, "fall_distance",
		nbt.DoubleFieldOr(compound, "FallDistance", 0)))
	if IsEquine(m.Type) {
		m.Horse.Owner, _ = animalOwnerUUID(compound)
	}
	if m.Type == TypeSheep {
		// vanilla: Sheep.readAdditionalSaveData, by DyeColor.LEGACY_ID_CODEC
		if colour, ok := nbt.IntValue(compound["Color"]); ok {
			m.Colour = colourOfDye(uint8(colour))
		}
	}
	if m.Type == TypeSulfurCube {
		m.FromBucket = nbt.BoolField(compound, "from_bucket")
	}
	if m.Type == TypeParrot {
		variant, _ := nbt.IntValue(compound["Variant"])
		m.SetParrotVariant(int(variant))
	}
	if m.Type == TypeSniffer {
		m.snifferFields(compound, true)
	}
	if m.Type == TypeAxolotl {
		variant, _ := nbt.IntValue(compound["Variant"])
		m.SetAxolotlVariant(int(variant))
		brain, _ := nbt.Get[nbt.Compound](compound, "Brain")
		memories, _ := nbt.Get[nbt.Compound](brain, "memories")
		cooldown, _ := nbt.Get[nbt.Compound](memories, "minecraft:has_hunting_cooldown")
		m.axolotl.huntingCooldown = 0
		if nbt.BoolField(cooldown, "value") {
			m.axolotl.huntingCooldown, _ = nbt.LongValue(cooldown["ttl"])
		}
		playDead, _ := nbt.Get[nbt.Compound](memories, "minecraft:play_dead_ticks")
		playTicks, _ := nbt.IntValue(playDead["value"])
		m.axolotl.playDead = int(playTicks)
		temptation, _ := nbt.Get[nbt.Compound](memories, "minecraft:temptation_cooldown_ticks")
		temptTicks, _ := nbt.IntValue(temptation["value"])
		m.axolotl.temptationCooldown = int(temptTicks)
		m.FromBucket = nbt.BoolField(compound, "FromBucket")
	}
	if m.IsFish() || m.Type == TypeTadpole {
		m.loadFish(compound)
	}
	if m.Type == TypeBee {
		// vanilla: Bee.readAdditionalSaveData
		m.SetBeeState(nbt.BoolField(compound, "HasStung"), nbt.BoolField(compound, "HasNectar"))
		var mem BeeMemory
		mem.Hive, mem.Flower = blockPosTag(compound, "hive_pos"), blockPosTag(compound, "flower_pos")
		ticks := func(key string) int { v, _ := nbt.IntField(compound, key); return int(v) }
		mem.NoNectar, mem.StayOut, mem.CropsGrown = ticks("TicksSincePollination"), ticks("CannotEnterHiveTicks"), ticks("CropsGrownSincePollination")
		m.SetBeeMemory(mem)
	}
	if m.Type == TypeFox {
		// vanilla: Fox.readAdditionalSaveData; an unknown type is red
		if name, ok := nbt.Get[nbt.String](compound, "Type"); ok {
			m.Fox.Variant = FoxVariantByName(string(name))
		}
		m.Fox.Trusted = nil
		if list, ok := nbt.Get[nbt.List](compound, "Trusted"); ok {
			for _, item := range list.Items {
				if words, ok := item.(nbt.IntArray); ok {
					if id, ok := nbt.UUIDFromIntArray(words); ok {
						m.FoxTrust(id)
					}
				}
			}
		}
	}
	if m.Type == TypeMooshroom {
		// vanilla: MushroomCow.readAdditionalSaveData's orElse(DEFAULT):
		// anything but brown is red.
		if kind, ok := nbt.Get[nbt.String](compound, "Type"); ok && kind == "brown" {
			m.Mushroom = "brown"
		}
		m.StewEffects = readMooshroomStewEffects(compound)
	}
	if m.Type == TypeCreeper {
		// vanilla: Creeper.readAdditionalSaveData, ignite() when set
		if nbt.BoolField(compound, "ignited") {
			m.Ignite()
		}
		if fuse, ok := nbt.ShortField(compound, "Fuse"); ok {
			m.MaxSwell = int(fuse)
		}
		if radius, ok := nbt.ByteField(compound, "ExplosionRadius"); ok {
			m.ExplosionRadius = int(radius)
		}
	}
	if m.IsGhast() {
		// vanilla: Ghast.readAdditionalSaveData
		power := int8(1)
		if v, ok := nbt.ByteField(compound, "ExplosionPower"); ok {
			power = v
		}
		m.SetGhastExplosionPower(int(power))
	}
	if m.Type == TypeEnderman {
		// vanilla: EnderMan.readAdditionalSaveData, NeutralMob.readPersistentAngerSaveData
		m.SetCarriedBlock(0)
		if block, ok := compound[BlockStateKeyCarried]; ok {
			name, props := registry.BlockStateOf(block)
			if state, found := registry.FindState(registry.Qualify(name), props); found && state != 0 {
				m.SetCarriedBlock(state)
			}
		}
	}
	if m.HasAnger() {
		// vanilla: NeutralMob.readPersistentAngerSaveData
		ticks, never := 0, false
		if end, ok := compound["anger_end_time"].(nbt.Long); ok {
			ticks, never = int(max(int64(end)-gameTime(), 0)), end == 0
		} else if legacy, ok := nbt.IntField(compound, "AngerTime"); ok {
			ticks = int(legacy)
		}
		target, hasTarget := nbt.UUIDField(compound, "angry_at")
		m.SetAnger(ticks, target, hasTarget)
		m.anger.neverAngered = never
	}
	if m.Type == TypePhantom {
		// vanilla: Phantom.readAdditionalSaveData
		size, _ := nbt.IntField(compound, "size")
		m.SetPhantomSize(int(size))
		if anchor, ok := nbt.Get[nbt.IntArray](compound, "anchor_pos"); ok && len(anchor) == 3 {
			m.SetAnchorPos(anchor[0], anchor[1], anchor[2])
		}
	}
	if m.Type == TypeEvoker || m.Type == TypeIllusioner {
		v, _ := nbt.IntField(compound, "SpellTicks")
		m.SetSpellTicks(int(v))
	}
	if m.Type == TypeRavager {
		// vanilla: Ravager.readAdditionalSaveData
		attack, _ := nbt.IntField(compound, "AttackTick")
		stun, _ := nbt.IntField(compound, "StunTick")
		roar, _ := nbt.IntField(compound, "RoarTick")
		m.SetRavagerTicks(int(attack), int(stun), int(roar))
	}
	if m.Type == TypeVex {
		// vanilla: Vex.readAdditionalSaveData
		if pos, ok := nbt.Get[nbt.IntArray](compound, "bound_pos"); ok && len(pos) == 3 {
			m.SetVexBound(pos[0], pos[1], pos[2])
		}
		if ticks, ok := nbt.IntField(compound, "life_ticks"); ok {
			m.SetVexLife(int(ticks))
		}
		// vanilla: Vex.readAdditionalSaveData, EntityReference.read; an absent
		// key keeps the owner, as with a /data merge of other fields.
		if owner, ok := nbt.UUIDField(compound, "owner"); ok {
			m.SetVexOwnerUUID(owner)
		}
	}
	if m.Type == TypeVindicator && nbt.BoolField(compound, "Johnny") {
		m.MarkJohnny()
	}
	if m.Type == TypeZombieVillager || m.Type == TypeVillager {
		// vanilla: ZombieVillager and Villager.readAdditionalSaveData; read data
		// counts as finalized
		if data, ok := nbt.Get[nbt.Compound](compound, "VillagerData"); ok {
			v := m.Villager
			if typ, ok := nbt.Get[nbt.String](data, "type"); ok {
				v.Type = registry.Qualify(string(typ))
			}
			if profession, ok := nbt.Get[nbt.String](data, "profession"); ok {
				v.Profession = registry.Qualify(string(profession))
			}
			// vanilla: VillagerData.CODEC, whose Codec.INT truncates a float
			if level, ok := nbt.IntValue(data["level"]); ok {
				v.Level = int(level)
			}
			if _, _, known := v.IDs(); known {
				m.Villager = v
			}
			m.Villager.Finalized = true
		}
		if nbt.BoolField(compound, "VillagerDataFinalized") {
			m.Villager.Finalized = true
		}
	}
	if m.Type == TypeVillager {
		m.vb.assignProfessionWhenSpawned = nbt.BoolField(compound, "AssignProfessionWhenSpawned")
		m.loadBrain(compound)
		m.loadSleepingPos(compound)
		m.loadFood(compound)
	}
	if m.Type == TypeVillager || m.Type == TypeZombieVillager || m.Type == TypeWanderingTrader {
		offers, _ := nbt.Get[nbt.CompoundTag](compound, "Offers")
		m.Offers = merchant.ReadOffers(offers)
	}
	if m.IsCreaking() {
		// vanilla: Creaking.readAdditionalSaveData
		if home, ok := nbt.Get[nbt.IntArray](compound, "home_pos"); ok && len(home) == 3 {
			m.CreakingBind(home[0], home[1], home[2])
		}
	} else if radius, ok := nbt.IntField(compound, "home_radius"); ok && radius >= 0 {
		// vanilla: Mob.readAdditionalSaveData, a negative radius meaning no home
		var x, y, z int32
		if home, ok := nbt.Get[nbt.IntArray](compound, "home_pos"); ok && len(home) == 3 {
			x, y, z = home[0], home[1], home[2]
		}
		m.RestrictTo(x, y, z, int32(radius))
	}
	if m.Type == TypeTurtle {
		// vanilla: Turtle.readAdditionalSaveData; the home falls back to the
		// turtle's block
		if home, ok := nbt.Get[nbt.IntArray](compound, "home_pos"); ok && len(home) == 3 {
			m.Home = Waypoint{X: home[0], Y: home[1], Z: home[2]}
		}
	}
	m.loadFreezeConversion(compound)
	// Baby state comes back as its kind kept it (IsBaby or Age), through the
	// same setAge vanilla's read runs.
	if Family(m.Type) == TypeZombie {
		if t, ok := nbt.IntField(compound, "InWaterTime"); ok {
			m.InWaterTime = int(t)
		}
		if t, ok := nbt.IntField(compound, "DrownedConversionTime"); ok && t != -1 {
			m.DrownedConverting, m.DrownedConversionTime = true, int(t)
		}
		// vanilla: Zombie.readAdditionalSaveData
		m.SetBaby(nbt.BoolField(compound, "IsBaby"))
		m.CanBreakDoors = nbt.BoolField(compound, "CanBreakDoors")
	} else if m.Type == TypeZoglin || m.Type == TypePiglin {
		m.SetBaby(nbt.BoolField(compound, "IsBaby"))
	} else if savesAge(m.Type) {
		if age, ok := nbt.IntField(compound, "Age"); ok {
			// A freshly loaded body starts grown, so a read-back age only shrinks
			// it; a /data merge giving a live baby a grown age grows it and moves
			// it clear.
			m.SetAge(w, int(age))
		}
		if forced, ok := nbt.IntField(compound, "ForcedAge"); ok {
			m.forcedAge = int(forced)
		}
		m.AgeLocked = nbt.BoolField(compound, "AgeLocked")
	}
	if IsAnimal(m.Type) {
		// vanilla: Animal.readAdditionalSaveData
		if love, ok := nbt.IntField(compound, "InLove"); ok {
			m.InLove = int(love)
		}
		// A missing or unreadable key clears the reference.
		m.LoveCause, m.HasLoveCause = nbt.UUIDField(compound, "LoveCause")
	}
	m.scalarFields(compound, true)
	if m.Type == TypeTadpole {
		m.tadpoleFields(compound, true)
		m.tadpoleBrainFields(compound, true)
	}
	if IsNautilus(m.Type) {
		m.nautilusFields(compound, true)
	}
	if IsLlama(m.Type) {
		// vanilla: Llama.setStrength, Variant.byId's clamp
		m.Horse.Strength = max(1, min(m.Horse.Strength, llamaMaxStrength))
		m.Horse.Variant = max(0, min(m.Horse.Variant, llamaVariants-1))
	}
	// vanilla: AbstractChestedHorse.readAdditionalSaveData
	if m.Horse.Chest {
		m.fitChest()
		registry.LoadSlottedItems(compound, m.Horse.Items)
	}
	// An unknown variant leaves the default.
	// vanilla: VariantUtils.readVariant
	if variants, sounds, ok := VariantRegistries(m.Type); ok {
		m.Variant = registryEntryOr(compound, "variant", variants, m.Variant)
		m.SoundVariant = registryEntryOr(compound, "sound_variant", sounds, m.SoundVariant)
	}
	m.loadEquipment(compound["equipment"])
	if m.Type == TypePiglin || m.Type == TypePillager || m.Type == TypeWanderingTrader {
		m.loadInventory(compound)
	}
	if chances, ok := nbt.Get[nbt.Compound](compound, "drop_chances"); ok {
		m.DropChances, m.BodyDropChance, m.SaddleDropChance = ReadDropChances(chances)
	}
	// The effects the file kept on the body, woken with it.
	if list, ok := nbt.Get[nbt.List](compound, "active_effects"); ok {
		m.RestoreEffects(effect.Decode(list))
	}
	if m.Type == TypeZombieVillager {
		difficulty := rules.DifficultyNormal
		if w != nil {
			difficulty = w.Difficulty()
		}
		m.loadCure(compound, difficulty)
	}
	// Set after the effects and not clamped, as vanilla's read does.
	if absorption, ok := nbt.FloatField(compound, "AbsorptionAmount"); ok {
		m.Absorption = max(float64(absorption), 0)
	}
	// Pointer-based codecs and embedded living state share this bulk boundary.
	m.InvalidateMetadata()
	m.equipmentRevision++
}

// vanilla: LivingEntity.readAdditionalSaveData, EntityEquipment.setAll
func (m *Mob) loadEquipment(tag nbt.Tag) {
	if saved, ok := tag.(nbt.CompoundTag); ok {
		equipment := saved.Entries()
		m.SetEquipment(protocol.EquipMainHand, StoredSlot(equipment, "mainhand"))
		m.SetEquipment(protocol.EquipOffHand, StoredSlot(equipment, "offhand"))
		for i, slot := range ArmorSlotNames {
			m.SetEquipment(protocol.EquipFeet+i, StoredSlot(equipment, slot))
		}
		m.loadSaddleSlot(equipment)
		m.SetEquipment(6, StoredSlot(equipment, "body"))
	}
}

func (m *Mob) loadFish(compound nbt.Compound) {
	m.SetMetadataFromBucket(m.Type == TypeTadpole || nbt.BoolField(compound, "FromBucket"))
	if name, ok := nbt.Get[nbt.String](compound, "type"); ok && m.Type == TypeSalmon {
		m.SalmonSize = SalmonSizeByName(string(name))
	}
	if m.Type == TypeTropicalFish {
		v, _ := nbt.IntValue(compound["Variant"])
		m.SetMetadataTropicalVariant(NormalizeTropicalVariant(v))
	}
	if m.Type == TypePufferfish {
		puff, _ := nbt.IntField(compound, "PuffState")
		m.SetPuffState(int(puff))
	}
}

// loadDeathLootTable reads the loot table override and its seed.
// vanilla: Mob.readAdditionalSaveData
func (m *Mob) loadDeathLootTable(compound nbt.Compound) {
	m.DeathLootTable = ""
	if name, ok := nbt.Get[nbt.String](compound, "DeathLootTable"); ok {
		m.DeathLootTable = string(name)
		if !strings.Contains(m.DeathLootTable, ":") {
			m.DeathLootTable = "minecraft:" + m.DeathLootTable
		}
	}
	m.DeathLootTableSeed = 0
	if v, ok := compound["DeathLootTableSeed"]; ok {
		m.DeathLootTableSeed, _ = nbt.LongValue(v)
	}
}

// MobSavedKeys are the keys SaveTo writes, which a load drops from its kept
// tags so stale values never return. One list serves every kind, since a
// key another kind owns never appears. The caller keeps attributes for its
// owning codec. FallDistance is the legacy float fall_distance replaced;
// AngerTime the legacy anger countdown.
var MobSavedKeys = []string{
	"HasVisualFire", "Invul", "BatFlags", "DarkTicksRemaining", "Inventory", "Items", "stew_effects",
	"ChestedHorse", "Strength", "DespawnDelay", "LastPoseTick", "IsImmuneToZombification",
	"AttachFace", "Peek", "state", "scute_time", "DuplicationCooldown", "listener", "anger",
	"MainGene", "HiddenGene", "DragonPhase", "DragonDeathTime", "sitting_damage_received",
	"HurtTime", "DeathTime", "FallFlying", "current_impulse_context_reset_grace_time", "Brain",
	"fall_distance", "FallDistance", "data",
	"Air", "NoGravity", "TicksFrozen", "Health", "AbsorptionAmount", "PersistenceRequired",
	"CanPickUpLoot", "DeathLootTable", "DeathLootTableSeed",
	"home_pos", "home_radius", "has_egg", "Color", "Sheared", "EggLayTime", "IsChickenJockey",
	"IsBaby", "CanBreakDoors", "Age", "ForcedAge", "AgeLocked", "InLove", "LoveCause", "variant", "sound_variant", "NoAI", "Silent", "Glowing", "LeftHanded",
	"RabbitType", "MoreCarrotTicks", "IsScreamingGoat", "HasLeftHorn", "HasRightHorn", "GotFish", "Moistness", "Trusting",
	"CollarColor", "PuffState", "anchor_pos", "size",
	"Invulnerable", "FromBucket", "from_bucket", "pickup_timer", "fuse", "type", "HasNectar", "HasStung",
	"sleeping_pos", "hive_pos", "flower_pos", "TicksSincePollination", "CannotEnterHiveTicks", "CropsGrownSincePollination",
	"weather_state", "next_weather_age", "equipment", "drop_chances", "sheared", "Offers", "FoodLevel", "LastRestock", "LastGossipDecay", "Gossips", "RestocksToday", "VillagerData", "VillagerDataFinalized", "AssignProfessionWhenSpawned", "Type",
	"PlayerCreated", "Pumpkin", "ConversionTime", "ConversionPlayer", "Xp", "Size", "wasOnGround", "InWaterTime", "DrownedConversionTime", "StrayConversionTime", "FreezingTime",
	"ignited", "powered", "Fuse", "ExplosionRadius", "ExplosionPower", "active_effects",
	"Tame", "Bred", "Temper", "Owner", "EatingHaystack", "Variant", "Lifetime", "SkeletonTrap", "SkeletonTrapTime",
	"SpellTicks", "AttackTick", "StunTick", "RoarTick", "bound_pos", "life_ticks", "owner",
	"patrol_target", "Patrolling", "PatrolLeader", "wander_target", "Johnny", "carriedBlockState", "anger_end_time", "angry_at", "AngerTime", "Sitting", "Sleeping", "Crouching", "Trusted",
	"TimeInOverworld", "CannotHunt", "CannotBeHunted", "still_timeout",
}

// Ageable reports vanilla's AgeableMob sorts (the animals and the happy
// ghast), whose baby state is the Age int.
func Ageable(t MobType) bool {
	switch t {
	case TypeChicken, TypeCow, TypeMooshroom, TypePig, TypeSheep, TypeTurtle, TypeHappyGhast, TypeCamel, TypeCamelHusk, TypeHorse, TypeDonkey, TypeMule, TypeLlama, TypeTraderLlama, TypeSkeletonHorse, TypeZombieHorse, TypeVillager, TypeRabbit, TypeWolf,
		TypeArmadillo, TypeSniffer, TypeCat, TypeOcelot, TypePanda, TypePolarBear, TypeGoat, TypeHoglin, TypeStrider, TypeBee, TypeSulfurCube, TypeAxolotl, TypeNautilus, TypeZombieNautilus, TypeDolphin, TypeFrog, TypeFox, TypeSquid, TypeGlowSquid, TypeWanderingTrader:
		return true
	}
	return false
}

// savesAge reports the AgeableMob kinds that write and read Age, ForcedAge and
// AgeLocked.
// vanilla: AgeableMob.canBeABaby
func savesAge(t MobType) bool {
	return Ageable(t) && kinds[t].babyable
}

// DyeOfColour gives a colour name's dye ID, white's 0 for an unknown name.
func DyeOfColour(colour string) uint8 {
	for dye, name := range registry.DyeNames {
		if colour == name {
			return uint8(dye)
		}
	}
	return 0
}

// colourOfDye is the inverse of DyeOfColour, white for unknown IDs.
func colourOfDye(dye uint8) string {
	if int(dye) < len(registry.DyeNames) {
		return registry.DyeNames[dye]
	}
	return "white"
}

// registryEntryOr reads a namespaced registry entry field, or fallback when
// absent or not in the registry.
func registryEntryOr(compound nbt.Compound, key, registryName, fallback string) string {
	name, ok := nbt.Get[nbt.String](compound, key)
	if !ok {
		return fallback
	}
	entry := string(name)
	if !strings.Contains(entry, ":") {
		entry = "minecraft:" + entry
	}
	if r, ok := registry.Get(registryName); !ok || r.ID(entry) < 0 {
		return fallback
	}
	return entry
}

// DropChancesTag encodes the nondefault slots in native DropChances map traversal order.
// vanilla: DropChances.CODEC, filterDefaultValues
func (m *Mob) DropChancesTag() *nbt.OrderedCompound {
	chances := &nbt.OrderedCompound{}
	order := m.dropChancesOrder
	if order == nil {
		order = defaultDropChancesOrder
	}
	for _, i := range order.slots {
		var name string
		var chance float32
		switch {
		case i < len(LootSlotNames):
			name, chance = LootSlotNames[i], m.DropChances[i]
		case i == 6:
			name, chance = "body", m.BodyDropChance
		default:
			name, chance = "saddle", m.SaddleDropChance
		}
		if chance != equipmentDropChance {
			chances.Set(name, nbt.Float(chance))
		}
	}
	return chances
}

// ReadDropChances reads a drop_chances compound over the default chance,
// returning the loot slots then body and saddle. A negative or non-finite chance
// leaves only its own slot at the default; the other entries are kept.
// vanilla: DropChances.CODEC, ExtraCodecs.NON_NEGATIVE_FLOAT, TagValueInput
func ReadDropChances(compound nbt.Compound) (chances [6]float32, body, saddle float32) {
	chances, body, saddle = DefaultDropChances(), equipmentDropChance, equipmentDropChance
	read := func(name string, to *float32) {
		if chance, ok := nbt.FloatField(compound, name); ok && chance >= 0 && !math.IsInf(float64(chance), 0) {
			*to = chance
		}
	}
	for i, slot := range LootSlotNames {
		read(slot, &chances[i])
	}
	read("body", &body)
	read("saddle", &saddle)
	return chances, body, saddle
}

// equipmentTag is Mob's equipment compound: the occupied slots, keyed by the
// EquipmentSlot names.
func equipmentTag(m *Mob) *nbt.OrderedCompound {
	equipment := &nbt.OrderedCompound{}
	if !m.MainHand.Empty() {
		equipment.Set("mainhand", registry.StackTag(m.MainHand))
	}
	if !m.OffHand.Empty() {
		equipment.Set("offhand", registry.StackTag(m.OffHand))
	}
	for i, slot := range ArmorSlotNames {
		if !m.Worn[i].Empty() {
			equipment.Set(slot, registry.StackTag(m.Worn[i]))
		}
	}
	if !m.Body.Empty() {
		equipment.Set("body", registry.StackTag(m.Body))
	}
	if !m.Horse.Saddle.Empty() {
		equipment.Set("saddle", registry.StackTag(m.Horse.Saddle))
	}
	if !m.Antenna.Empty() {
		equipment.Set("saddle", registry.StackTag(m.Antenna))
	}

	return equipment
}

// StoredSlot reads one equipment slot, empty when it holds nothing this
// server can read.
func StoredSlot(equipment nbt.Compound, slot string) protocol.ItemStack {
	tag, ok := nbt.Get[nbt.Compound](equipment, slot)
	if !ok {
		return protocol.ItemStack{}
	}
	stack, _ := registry.StoredStack(tag)
	return stack
}

// vanilla: VillagerData.CODEC
func villagerDataTag(v VillagerData) *nbt.OrderedCompound {
	tag := nbt.NewOrderedCompound(3)
	tag.Set("type", nbt.String(v.Type))
	tag.Set("profession", nbt.String(v.Profession))
	tag.Set("level", nbt.Int(v.Level))
	return tag
}

// blockPosTag reads a three-int BlockPos array, nil when absent or
// malformed.
func blockPosTag(compound nbt.Compound, key string) *[3]int32 {
	arr, ok := compound[key].(nbt.IntArray)
	if !ok || len(arr) != 3 {
		return nil
	}
	return &[3]int32{arr[0], arr[1], arr[2]}
}

// boolTag is a boolean as the byte a compound keeps it as.
func boolTag(v bool) nbt.Byte {
	if v {
		return 1
	}
	return 0
}

// clampToShort clamps a counter into the short the file stores it as.
func clampToShort(v int) int16 {
	return int16(min(max(v, -32768), 32767))
}

// clampToByte clamps a counter into 0..255.
func clampToByte(v int) uint8 {
	return uint8(min(max(v, 0), 255))
}

// loadSaddleSlot reads the "saddle" equipment slot into the field the kind keeps it in.
func (m *Mob) loadSaddleSlot(equipment nbt.Compound) {
	if m.Type == TypeCopperGolem {
		m.SetMetadataAntenna(StoredSlot(equipment, "saddle"))
	} else {
		m.SetMetadataHorseSaddle(StoredSlot(equipment, "saddle"))
	}
}

// loadFreezeConversion reads a skeleton's freeze conversion state.
// vanilla: Skeleton.readAdditionalSaveData
func (m *Mob) loadFreezeConversion(compound nbt.Compound) {
	if m.Type != TypeSkeleton {
		return
	}
	if t, ok := nbt.IntField(compound, "StrayConversionTime"); ok && t != -1 {
		m.SetMetadataStrayConverting(true)
		m.StrayConversionTime = int(t)
	}
	if t, ok := nbt.IntField(compound, "FreezingTime"); ok {
		m.inPowderSnowTime = int(t)
	}
}
