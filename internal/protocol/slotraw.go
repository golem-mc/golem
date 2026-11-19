package protocol

import (
	"maps"
	"slices"
	"strings"

	"github.com/golem-mc/golem/nbt"
)

// The unmodelled components held as raw bytes, and their wire shapes: the
// client's stream codecs, read off the server jar's bytecode. A value is checked
// before it is kept, because one the reading client would refuse takes the
// client down with the stack; the bytes the check read go back on the wire.

// readRawComponent reads an unmodelled component with its type's codec and
// keeps the bytes read on Raw (bare in the trusted shape, inside the length
// prefix in the untrusted one) for PutComponentData. A refused value refuses
// the stack.
func readRawComponent(r *Reader, stack *ItemStack, component int32, depth int) bool {
	start := r.pos
	if !readRawValue(r, component, depth) || r.Err() != nil {
		return false
	}
	stack.putRaw(RawComponent{Type: component,
		Data: append([]byte(nil), r.data[start:r.pos]...)})
	return true
}

// RawComponentHeld reports whether a data component is one of the unmodelled
// ones this server holds as raw bytes.
func RawComponentHeld(component int32) bool {
	switch component {
	case DataComponentCustomData, DataComponentBundleContents, DataComponentChargedProjectiles, DataComponentPotionContents,
		DataComponentPotionDurationScale, DataComponentWritableBookContent, DataComponentWrittenBookContent,
		DataComponentProfile, DataComponentBannerPatterns, DataComponentBaseColor, DataComponentPaintingVariant, DataComponentRabbitVariant,
		DataComponentTrim, DataComponentEntityData, DataComponentFireworks,
		DataComponentInstrument, DataComponentJukeboxPlayable, DataComponentBlockTransformer, DataComponentSuspiciousStew, DataComponentOminousBottleAmplifier,
		DataComponentFireworkExplosion, DataComponentCustomModelData,
		DataComponentLodestoneTracker, DataComponentBlockState,
		DataComponentMapID,
		DataComponentMapPostProcessing, DataComponentBlockEntityData,
		DataComponentNoteBlockSound, DataComponentPotDecorations, DataComponentBees,
		DataComponentSignTextFront, DataComponentSignTextBack, DataComponentWaxed,
		DataComponentCanPlaceOn, DataComponentCanBreak, DataComponentDyedColor, DataComponentTool, DataComponentEquippable,
		DataComponentWeapon, DataComponentBlocksAttacks, DataComponentAttackRange,
		DataComponentDamageResistant, DataComponentGlider, DataComponentDeathProtection,
		DataComponentFood, DataComponentConsumable, DataComponentUseRemainder, DataComponentUseCooldown, DataComponentCompostable, DataComponentCookingFuel, DataComponentBrewingFuel, DataComponentMaxStackSize,
		DataComponentMaxDamage, DataComponentMinimumAttackCharge, DataComponentDamageType, DataComponentEnchantable, DataComponentRepairable,
		DataComponentDye, DataComponentProvidesTrimMaterial, DataComponentProvidesBannerPatterns, DataComponentProvidesPotteryPattern,
		DataComponentTropicalFishPattern, DataComponentTropicalFishBaseColor, DataComponentTropicalFishPatternColor,
		DataComponentBucketEntityData, DataComponentSalmonSize, DataComponentSulfurCubeContent, DataComponentAxolotlVariant,
		DataComponentVillagerVariant, DataComponentWolfVariant, DataComponentWolfSoundVariant, DataComponentWolfCollar,
		DataComponentFoxVariant, DataComponentParrotVariant, DataComponentMooshroomVariant, DataComponentPigVariant,
		DataComponentPigSoundVariant, DataComponentCowVariant, DataComponentCowSoundVariant, DataComponentChickenVariant,
		DataComponentChickenSoundVariant, DataComponentZombieNautilusVariant, DataComponentFrogVariant, DataComponentHorseVariant,
		DataComponentLlamaVariant, DataComponentCatVariant, DataComponentCatSoundVariant, DataComponentCatCollar,
		DataComponentSheepColor, DataComponentShulkerColor, DataComponentCushionColor,
		DataComponentItemName, DataComponentRarity, DataComponentTooltipDisplay,
		DataComponentItemModel, DataComponentTooltipStyle, DataComponentBreakSound,
		DataComponentLore, DataComponentAttributeModifiers:
		return true
	default:
		_, record := RecordComponents[component]
		return record
	}
}

// readRawValue reads one unmodelled value, reporting whether it is whole.
// depth counts the recursion through the item templates a value holds; a
// potion effect's hidden twin chain counts its own.
func readRawValue(r *Reader, component int32, depth int) bool {
	if !RawComponentHeld(component) {
		return false
	}
	switch component {
	case DataComponentAttackRange:
		_, ok := readAttackRange(r)
		return ok
	case DataComponentBundleContents, DataComponentChargedProjectiles:
		// The projectiles' codec is the bundle's: a list of item templates.
		_, ok := readBundleContents(r, depth)
		return ok
	case DataComponentSulfurCubeContent, DataComponentUseRemainder:
		_, ok := readItemTemplate(r, depth)
		return ok
	case DataComponentPotionContents:
		_, ok := readPotionContents(r)
		return ok
	case DataComponentSuspiciousStew:
		return readSuspiciousStew(r)
	case DataComponentRabbitVariant:
		// vanilla: Rabbit.Variant.STREAM_CODEC; an unknown id reads as brown
		r.ReadVarInt()
		return r.Err() == nil
	case DataComponentOminousBottleAmplifier:
		// vanilla: OminousBottleAmplifier.STREAM_CODEC
		r.ReadVarInt()
		return r.Err() == nil
	case DataComponentWritableBookContent:
		return readWritableBookContent(r)
	case DataComponentWrittenBookContent:
		return readWrittenBookContent(r)
	case DataComponentProfile:
		return readProfile(r)
	case DataComponentCustomModelData:
		_, ok := readCustomModelData(r)
		return ok
	case DataComponentBannerPatterns:
		return readBannerPatterns(r)
	case DataComponentDyedColor:
		return readDyedColor(r)
	case DataComponentPotionDurationScale:
		r.ReadF32()
		return r.Err() == nil
	case DataComponentBaseColor:
		return readBaseColor(r)
	case DataComponentTrim:
		return readTrim(r)
	case DataComponentInstrument:
		return readInstrument(r)
	case DataComponentBlockTransformer:
		// vanilla: BlockTransformer.STREAM_CODEC uses holderRegistry, without an inline marker.
		id := r.ReadVarInt()
		return r.Err() == nil && id >= 0 && id < RegistrySizeBlockTransformer
	case DataComponentJukeboxPlayable:
		// vanilla: JukeboxSong.STREAM_CODEC, DIRECT_STREAM_CODEC
		switch id := r.ReadVarInt(); {
		case id == 0:
			if _, ok := readSoundHolder(r); !ok {
				return false
			}
			if _, ok := ReadText(r); !ok {
				return false
			}
			r.ReadF32()
			r.ReadVarInt()
		case id < 0 || id > sizeJukeboxSong():
			return false
		}
		return r.Err() == nil
	case DataComponentEntityData:
		return readEntityData(r)
	case DataComponentBlockEntityData:
		return readBlockEntityData(r)
	case DataComponentLore:
		return readLore(r)
	case DataComponentAttributeModifiers:
		return readAttributeModifiers(r)
	case DataComponentCustomData, DataComponentBucketEntityData:
		// vanilla: CustomData.STREAM_CODEC, the bare network NBT compound
		return readNetworkNBT(r)
	case DataComponentItemName:
		_, ok := ReadText(r)
		return ok
	case DataComponentRarity:
		// vanilla: Rarity.STREAM_CODEC
		r.ReadVarInt()
		return r.Err() == nil
	case DataComponentTooltipDisplay:
		return readTooltipDisplay(r)
	case DataComponentItemModel, DataComponentTooltipStyle:
		// vanilla: Identifier.STREAM_CODEC
		return readIdentifier(r)
	case DataComponentBreakSound:
		_, ok := readSoundHolder(r)
		return ok
	case DataComponentAxolotlVariant:
		// vanilla: Axolotl.Variant.STREAM_CODEC, byId falls back to lucy
		r.ReadVarInt()
		return r.Err() == nil
	case DataComponentWolfCollar, DataComponentFoxVariant, DataComponentParrotVariant, DataComponentMooshroomVariant,
		DataComponentHorseVariant, DataComponentLlamaVariant, DataComponentCatCollar, DataComponentSheepColor,
		DataComponentShulkerColor, DataComponentCushionColor:
		// vanilla: ByteBufCodecs.idMapper over the enum; an unknown id maps by the enum's ByIdMap strategy
		r.ReadVarInt()
		return r.Err() == nil
	case DataComponentVillagerVariant, DataComponentWolfVariant, DataComponentWolfSoundVariant, DataComponentPigVariant,
		DataComponentPigSoundVariant, DataComponentCowVariant, DataComponentCowSoundVariant, DataComponentChickenVariant,
		DataComponentChickenSoundVariant, DataComponentZombieNautilusVariant, DataComponentFrogVariant, DataComponentCatVariant,
		DataComponentCatSoundVariant:
		// vanilla: ByteBufCodecs.holderRegistry, which throws on an unknown id
		name, builtin := holderVariantRegistry(component)
		id := r.ReadVarInt()
		return r.Err() == nil && id >= 0 && id < registrySize(name, builtin)
	case DataComponentSalmonSize, DataComponentTropicalFishPattern, DataComponentTropicalFishBaseColor, DataComponentTropicalFishPatternColor, DataComponentDye:
		// vanilla: Salmon.Variant, TropicalFish.Pattern and DyeColor.STREAM_CODEC
		r.ReadVarInt()
		return r.Err() == nil
	case DataComponentNoteBlockSound:
		return readNoteBlockSound(r)
	case DataComponentPotDecorations:
		return readPotDecorations(r, depth)
	case DataComponentBees:
		return readBees(r)
	case DataComponentSignTextFront, DataComponentSignTextBack:
		return readSignText(r)
	case DataComponentWaxed, DataComponentGlider:
		// vanilla: Unit.STREAM_CODEC, no bytes
		return true
	case DataComponentDamageResistant:
		_, ok := readDamageTypes(r)
		return ok
	case DataComponentDeathProtection:
		_, ok := readDeathProtection(r)
		return ok
	case DataComponentFireworks:
		return readFireworks(r)
	case DataComponentFireworkExplosion:
		_, ok := readFireworkExplosion(r)
		return ok
	case DataComponentLodestoneTracker:
		return readLodestoneTracker(r)
	case DataComponentBlockState:
		return readBlockStateProperties(r)
	case DataComponentMapID:
		return readMapID(r)
	case DataComponentMapPostProcessing:
		return readMapPostProcessing(r)
	case DataComponentCanPlaceOn, DataComponentCanBreak:
		_, ok := readBlockPredicates(r)
		return ok
	case DataComponentEquippable:
		_, ok := readEquippable(r)
		return ok
	case DataComponentTool:
		_, ok := readTool(r)
		return ok
	case DataComponentWeapon:
		_, ok := readWeapon(r)
		return ok
	case DataComponentBlocksAttacks:
		_, ok := readBlocksAttacks(r)
		return ok
	case DataComponentMaxStackSize:
		_, ok := readMaxStackSize(r)
		return ok
	case DataComponentCookingFuel, DataComponentBrewingFuel:
		_, ok := readFuel(r)
		return ok
	case DataComponentMaxDamage, DataComponentEnchantable:
		return readPositiveVarInt(r)
	case DataComponentMinimumAttackCharge:
		// vanilla: ByteBufCodecs.FLOAT; floatRange(0, 1) bounds only the persistent codec
		r.ReadF32()
		return r.Err() == nil
	case DataComponentDamageType:
		return readRegistryRef(r, "minecraft:damage_type")
	case DataComponentProvidesPotteryPattern:
		return readRegistryRef(r, "minecraft:decorated_pot_pattern")
	case DataComponentRepairable:
		return readHolderSet(r, "minecraft:item")
	case DataComponentProvidesBannerPatterns:
		return readHolderSet(r, "minecraft:banner_pattern")
	case DataComponentProvidesTrimMaterial:
		return readTrimMaterial(r)
	case DataComponentCompostable:
		_, ok := readCompostable(r)
		return ok
	case DataComponentFood:
		_, ok := readFood(r)
		return ok
	case DataComponentConsumable:
		_, ok := readConsumable(r)
		return ok
	case DataComponentUseCooldown:
		_, ok := readUseCooldown(r)
		return ok
	default:
		if fields, ok := RecordComponents[component]; ok {
			_, ok = ReadRecord(r, fields)
			return ok
		}
		return readPaintingVariant(r)
	}
}

// readSuspiciousStew reads minecraft:suspicious_stew_effects: a prefixed list
// of a mob effect id and a VarInt duration.
// vanilla: SuspiciousStewEffects.STREAM_CODEC
func readSuspiciousStew(r *Reader) bool {
	n := r.ReadVarInt()
	if r.Err() != nil || n < 0 {
		return false
	}
	for range n {
		id := r.ReadVarInt()
		r.ReadVarInt()
		if r.Err() != nil || id < 0 || id >= RegistrySizeMobEffect {
			return false
		}
	}
	return true
}

// readFireworks reads a VarInt flight duration (the stream codec does not
// cap it at a byte as the datapack codec does) and up to 256 explosions:
// shape, two lists of int colours, trail and twinkle. Any VarInt shape is
// accepted; the client reads an unknown one as the small ball.
// vanilla: Fireworks.STREAM_CODEC, FireworkExplosion.STREAM_CODEC
func readFireworks(r *Reader) bool {
	_, _, ok := readFireworksValue(r)
	return ok
}

// readFireworksValue reads the value readFireworks checks.
func readFireworksValue(r *Reader) (int32, []FireworkExplosion, bool) {
	flight := r.ReadVarInt()
	count := r.ReadVarInt()
	if r.Err() != nil || count < 0 || count > 256 {
		return 0, nil, false
	}
	explosions := make([]FireworkExplosion, 0, count)
	for range count {
		e, ok := readFireworkExplosion(r)
		if !ok {
			return 0, nil, false
		}
		explosions = append(explosions, e)
	}
	return flight, explosions, r.Err() == nil
}

// readBlockStateProperties reads minecraft:block_state, a map of property
// names to string values with no cap and no name check: which properties a
// block takes is the placement's business.
// vanilla: BlockItemStateProperties.STREAM_CODEC, ByteBufCodecs.map
func readBlockStateProperties(r *Reader) bool {
	_, ok := readBlockStateMap(r)
	return ok
}

// readBlockStateMap reads a VarInt count of name/value string pairs; false
// when not whole.
func readBlockStateMap(r *Reader) (map[string]string, bool) {
	count := r.ReadVarInt()
	if r.Err() != nil || count < 0 {
		return nil, false
	}
	// An entry is at least two bytes on the wire, two empty strings, so the
	// map is presized no further than the rest of the payload could back.
	props := make(map[string]string, min(count, int32(r.Remaining()/2)))
	for range count {
		key, value := r.ReadString(32767), r.ReadString(32767)
		if r.Err() != nil {
			return nil, false
		}
		props[key] = value
	}
	return props, true
}

// readEntityData reads minecraft:entity_data: an entity type ID, then the
// network NBT compound.
// vanilla: TypedEntityData.STREAM_CODEC
func readEntityData(r *Reader) bool {
	return readTypedEntityData(r, RegistrySizeEntityType)
}

// readBlockEntityData reads minecraft:block_entity_data, the same
// TypedEntityData shape naming a block entity type.
// vanilla: TypedEntityData.STREAM_CODEC
func readBlockEntityData(r *Reader) bool {
	return readTypedEntityData(r, RegistrySizeBlockEntityType)
}

// readNetworkNBT reads one network NBT compound off the unread payload and
// moves the reader over the bytes it took.
func readNetworkNBT(r *Reader) bool {
	if r.err != nil {
		return false
	}
	_, n, err := nbt.DecodeNetworkQuota(r.data[r.pos:], nbt.NetworkQuota)
	if err != nil {
		r.fail(err)
		return false
	}
	r.pos += n
	return true
}

// readTypedEntityData reads a registry ID bounded by registrySize, then a
// network NBT compound, decoded from the unread payload since it has no
// length on the wire.
func readTypedEntityData(r *Reader, registrySize int32) bool {
	if r.err != nil {
		return false
	}
	if id := r.ReadVarInt(); r.err != nil || id < 0 || id >= registrySize {
		return false
	}
	_, n, err := nbt.DecodeNetworkQuota(r.data[r.pos:], nbt.NetworkQuota)
	if err != nil {
		r.fail(err)
		return false
	}
	r.pos += n
	return true
}

// readPotionContents reads minecraft:potion_contents: an optional potion ID,
// optional custom colour, the effects and an optional custom name.
// vanilla: PotionContents.STREAM_CODEC
func readPotionContents(r *Reader) (PotionContents, bool) {
	var contents PotionContents
	contents.Potion = NoPotion
	if r.ReadBool() {
		id := r.ReadVarInt()
		if id < 0 || id >= RegistrySizePotion {
			return contents, false
		}
		contents.Potion = id
	}
	if r.ReadBool() {
		color := r.ReadI32()
		contents.CustomColor = &color
	}
	effects := r.ReadVarInt()
	if r.Err() != nil || effects < 0 {
		return contents, false
	}
	for range effects {
		id := r.ReadVarInt()
		if id < 0 || id >= RegistrySizeMobEffect {
			return contents, false
		}
		spec, ok := readPotionEffect(r, id, 0)
		if !ok {
			return contents, false
		}
		contents.CustomEffects = append(contents.CustomEffects, spec)
	}
	if r.ReadBool() {
		name := r.ReadString(32767)
		contents.CustomName = &name
	}
	return contents, r.Err() == nil
}

// PutPotionContents writes minecraft:potion_contents as its stream codec
// reads it (absent optionals as a false flag), for a stack read from a file.
// vanilla: PotionContents.STREAM_CODEC
func (b *Buffer) PutPotionContents(contents PotionContents) {
	b.PutBool(contents.Potion != NoPotion)
	if contents.Potion != NoPotion {
		b.PutVarInt(contents.Potion)
	}
	b.PutBool(contents.CustomColor != nil)
	if contents.CustomColor != nil {
		b.PutI32(*contents.CustomColor)
	}
	b.PutVarInt(int32(len(contents.CustomEffects)))
	for i := range contents.CustomEffects {
		b.PutVarInt(contents.CustomEffects[i].ID)
		// The stream codec reads the copying accessor, which drops the hidden effect.
		// vanilla: PotionContents.customEffects
		visible := contents.CustomEffects[i]
		visible.Hidden = nil
		putPotionEffectChain(b, &visible)
	}
	b.PutBool(contents.CustomName != nil)
	if contents.CustomName != nil {
		b.PutString(*contents.CustomName)
	}
}

// putPotionEffectChain writes one effect's details (amplifier, duration,
// three flags, has-hidden flag) and its hidden twin's, which share its ID.
func putPotionEffectChain(b *Buffer, spec *PotionEffectSpec) {
	b.PutVarInt(spec.Amplifier)
	b.PutVarInt(spec.Duration)
	b.PutBool(spec.Ambient)
	b.PutBool(spec.ShowParticles)
	b.PutBool(spec.ShowIcon)
	b.PutBool(spec.Hidden != nil)
	if spec.Hidden != nil {
		putPotionEffectChain(b, spec.Hidden)
	}
}

// readPotionEffect reads an effect instance past its ID: amplifier, duration,
// three display flags and an optional hidden twin of the same fields. The
// chain counts its own depth, so a potion deep in a bundle keeps its whole
// budget, and past the limit fails the packet as component nesting does.
// vanilla: MobEffectInstance.Details.STREAM_CODEC
func readPotionEffect(r *Reader, id, chain int32) (PotionEffectSpec, bool) {
	if chain > maxComponentDepth {
		r.fail(ErrSlotTooDeep)
		return PotionEffectSpec{}, false
	}
	spec := PotionEffectSpec{
		ID:            id,
		Amplifier:     r.ReadVarInt(),
		Duration:      r.ReadVarInt(),
		Ambient:       r.ReadBool(),
		ShowParticles: r.ReadBool(),
		ShowIcon:      r.ReadBool(),
	}
	if r.ReadBool() {
		hidden, ok := readPotionEffect(r, id, chain+1)
		if !ok {
			return spec, false
		}
		spec.Hidden = &hidden
	}
	return spec, r.Err() == nil
}

// readBundleContents reads minecraft:bundle_contents: slot templates (item,
// count and a trusted-shape patch, values bare), recursing through their
// components.
// vanilla: BundleContents.STREAM_CODEC
func readBundleContents(r *Reader, depth int) ([]ItemStack, bool) {
	count := r.ReadVarInt()
	if r.Err() != nil || count < 0 {
		return nil, false
	}
	var items []ItemStack
	for range count {
		entry, ok := readItemTemplate(r, depth)
		if !ok {
			return nil, false
		}
		items = append(items, entry)
	}
	return items, r.Err() == nil
}

// readWritableBookContent reads minecraft:writable_book_content, the shape
// writableBookValue decodes.
// vanilla: WritableBookContent.STREAM_CODEC
func readWritableBookContent(r *Reader) bool {
	_, ok := writableBookValue(r)
	return ok
}

// readWrittenBookContent reads minecraft:written_book_content, the shape
// writtenBookValue decodes.
// vanilla: WrittenBookContent.STREAM_CODEC
func readWrittenBookContent(r *Reader) bool {
	_, ok := writtenBookValue(r, ReadText)
	return ok
}

// readProfile reads minecraft:profile, the shape profileValue decodes.
// vanilla: ResolvableProfile.STREAM_CODEC
func readProfile(r *Reader) bool {
	_, ok := profileValue(r)
	return ok
}

// readBannerPatterns reads minecraft:banner_patterns, the shape
// bannerPatternsValue decodes.
// vanilla: BannerPatternLayers.STREAM_CODEC
func readBannerPatterns(r *Reader) bool {
	_, ok := bannerPatternsValue(r)
	return ok
}

// readPaintingVariant reads 0 and an inline variant (width, height, asset,
// optional title and author), or the registry ID plus one.
// vanilla: PaintingVariant.STREAM_CODEC
func readPaintingVariant(r *Reader) bool {
	switch variant := r.ReadVarInt(); {
	case variant == 0:
		r.ReadVarInt() // width
		r.ReadVarInt() // height
		if !readIdentifier(r) {
			return false
		}
		for range 2 {
			if r.ReadBool() {
				if _, ok := ReadText(r); !ok {
					return false
				}
			}
		}
	case variant > 0 && variant <= sizePaintingVariant():
		// The registry ID is one behind what the wire carries.
	default:
		return false
	}
	return r.Err() == nil
}

// PaintingVariantOf returns the painting_variant registry ID the stack's
// minecraft:painting/variant names; false with none or an inline variant,
// which is only relayed.
// vanilla: PaintingVariant.STREAM_CODEC
func (s ItemStack) PaintingVariantOf() (int32, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentPaintingVariant {
			continue
		}
		r := NewReader(raw.Data)
		v := r.ReadVarInt()
		if r.Err() != nil || v < 1 || v > sizePaintingVariant() {
			return 0, false
		}
		return v - 1, true
	}
	return 0, false
}

// SetPaintingVariant gives the stack minecraft:painting/variant naming the
// registry ID, written as the ID plus one.
func (s *ItemStack) SetPaintingVariant(id int32) {
	var value Buffer
	value.PutVarInt(id + 1)
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentPaintingVariant, Data: value.Bytes()})
}

// readInstrument reads a registry ID plus one; the inline form, which no
// goat horn of this version has, is refused.
// vanilla: InstrumentComponent.STREAM_CODEC
func readInstrument(r *Reader) bool {
	id := r.ReadVarInt()
	return r.Err() == nil && id >= 1 && id <= sizeInstrument()
}

// InstrumentOf returns the instrument ID the stack names; false when it
// carries none and a goat horn plays its default.
func (s ItemStack) InstrumentOf() (int32, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentInstrument {
			continue
		}
		r := NewReader(raw.Data)
		id := r.ReadVarInt()
		if r.Err() != nil || r.Remaining() != 0 || id < 1 || id > sizeInstrument() {
			return 0, false
		}
		return id - 1, true
	}
	return 0, false
}

// SetInstrument gives the stack the minecraft:instrument component naming
// the registry ID.
// vanilla: InstrumentComponent.STREAM_CODEC
func (s *ItemStack) SetInstrument(id int32) {
	var value Buffer
	value.PutVarInt(id + 1)
	s.putRaw(RawComponent{Type: DataComponentInstrument, Data: value.Bytes()})
}

// readTrim reads minecraft:trim's material and pattern, each a registry
// reference or an inline definition.
// vanilla: ArmorTrim.STREAM_CODEC
func readTrim(r *Reader) bool {
	if !readTrimMaterial(r) {
		return false
	}
	// The pattern: a registry reference, or the inline pattern.
	switch pattern := r.ReadVarInt(); {
	case pattern == 0:
		if !readIdentifier(r) {
			return false
		}
		if _, ok := ReadText(r); !ok {
			return false
		}
		r.ReadBool() // the decal flag
	case pattern < 0 || pattern > sizeTrimPattern():
		return false
	}
	return r.Err() == nil
}

// readTrimMaterial reads a registry reference, or an inline material: its
// palette and description.
// vanilla: TrimMaterial.STREAM_CODEC
func readTrimMaterial(r *Reader) bool {
	switch material := r.ReadVarInt(); {
	case material == 0:
		if !readIdentifier(r) {
			return false
		}
		if _, ok := ReadText(r); !ok {
			return false
		}
	case material < 0 || material > sizeTrimMaterial():
		return false
	}
	return r.Err() == nil
}

// SetTrim sets minecraft:trim by material and pattern registry IDs, written
// as ID plus one (zero would be inline, which is neither written nor read).
// vanilla: ArmorTrim.STREAM_CODEC
func (s *ItemStack) SetTrim(material, pattern int32) {
	var value Buffer
	value.PutVarInt(material + 1)
	value.PutVarInt(pattern + 1)
	s.putRaw(RawComponent{Type: DataComponentTrim, Data: value.Bytes()})
}

// TrimOf returns the stack's trim material and pattern IDs; false for none
// or an inline shape, which is only relayed: the smithing table compares
// references, and trims made here are always references.
func (s ItemStack) TrimOf() (material, pattern int32, ok bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentTrim {
			continue
		}
		r := NewReader(raw.Data)
		m, p := r.ReadVarInt(), r.ReadVarInt()
		if r.Err() != nil || r.Remaining() != 0 ||
			m < 1 || m > sizeTrimMaterial() ||
			p < 1 || p > sizeTrimPattern() {
			return 0, 0, false
		}
		return m - 1, p - 1, true
	}
	return 0, 0, false
}

// BlockStatePropertiesOf returns the stack's minecraft:block_state
// properties; false for none, since absent is the item default and an empty
// map carries nothing.
func (s ItemStack) BlockStatePropertiesOf() (map[string]string, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentBlockState {
			continue
		}
		r := NewReader(raw.Data)
		props, ok := readBlockStateMap(r)
		if !ok || r.Remaining() != 0 {
			return nil, false
		}
		return props, true
	}
	return nil, false
}

// BlockTagKnown reports whether the server sends a block tag in Update Tags;
// the registry package installs it at startup. A predicate's tag is checked
// because a client reading a relayed stack fails on an unknown tag; nil lets
// every name through.
// vanilla: ByteBufCodecs tag decode, MappedRegistry.get
var BlockTagKnown func(tag string) bool

// BlockPredicate is one can_place_on or can_break predicate as the wire
// carries it: a block set (inline or a tag), state property matchers and a
// block entity NBT guard. Matching is in the rules package.
// vanilla: BlockPredicate.STREAM_CODEC
type BlockPredicate struct {
	// Blocks is the predicate's optional block set. Absent, a predicate
	// tests no block in particular and every kind passes this half.
	Blocks BlockPredicateSet
	// Props are the state property matchers, every one of which the block
	// has to satisfy.
	Props []BlockPredicateProp
	// NBT is the block-entity NBT guard, nil when the predicate has none.
	// No matcher here tests it.
	NBT nbt.Compound
}

// BlockPredicateSet is an optional block set: inline block IDs or a tag
// name. A present, empty Inline matches nothing.
type BlockPredicateSet struct {
	Present bool
	Inline  []int32
	Tag     string
}

// BlockPredicateProp matches one property: an exact value, or the integer
// range Min to Max, an empty string leaving that end open.
type BlockPredicateProp struct {
	Name     string
	Value    string
	Ranged   bool
	Min, Max string
}

// CanPlaceOn returns minecraft:can_place_on's predicates and whether they
// read back; without the component, or with unreadable bytes, nothing
// matches.
// vanilla: ItemStack.canPlaceOnBlockInAdventureMode
func (s ItemStack) CanPlaceOn() ([]BlockPredicate, bool) {
	return s.blockPredicates(DataComponentCanPlaceOn)
}

// CanBreak is CanPlaceOn for minecraft:can_break.
// vanilla: ItemStack.canBreakBlockInAdventureMode
func (s ItemStack) CanBreak() ([]BlockPredicate, bool) {
	return s.blockPredicates(DataComponentCanBreak)
}

// blockPredicates decodes an adventure component off the stack's raw bytes;
// none when it is absent or unreadable.
func (s ItemStack) blockPredicates(component int32) ([]BlockPredicate, bool) {
	for _, raw := range s.Raw {
		if raw.Type == component {
			return readBlockPredicates(NewReader(raw.Data))
		}
	}
	return nil, false
}

// SetBlockPredicates lays can_place_on or can_break (component) on the stack
// in its wire shape, replacing any the stack had; false for a guard that
// does not encode.
// vanilla: AdventureModePredicate.STREAM_CODEC, BlockPredicate.STREAM_CODEC
func (s *ItemStack) SetBlockPredicates(component int32, predicates []BlockPredicate) bool {
	var value Buffer
	value.PutVarInt(int32(len(predicates)))
	for _, p := range predicates {
		value.PutBool(p.Blocks.Present)
		if p.Blocks.Present {
			putBlockSet(&value, p.Blocks)
		}
		value.PutBool(p.Props != nil)
		if p.Props != nil {
			value.PutVarInt(int32(len(p.Props)))
			for _, m := range p.Props {
				value.PutString(m.Name)
				value.PutBool(!m.Ranged)
				if !m.Ranged {
					value.PutString(m.Value)
					continue
				}
				for _, end := range [...]string{m.Min, m.Max} {
					value.PutBool(end != "")
					if end != "" {
						value.PutString(end)
					}
				}
			}
		}
		value.PutBool(p.NBT != nil)
		if p.NBT != nil {
			encoded, err := nbt.EncodeNetwork(p.NBT)
			if err != nil {
				return false
			}
			value.PutRaw(encoded)
		}
		// The exact and partial component matchers, which are never kept.
		value.PutVarInt(0)
		value.PutVarInt(0)
	}
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: component, Data: value.Bytes()})
	return true
}

// SetBlockStateProperties sets minecraft:block_state, encoded in its wire
// shape, replacing any the stack had.
func (s *ItemStack) SetBlockStateProperties(props map[string]string) {
	tag := nbt.NewOrderedCompound(len(props))
	for _, key := range slices.Sorted(maps.Keys(props)) {
		tag.Set(key, nbt.String(props[key]))
	}
	s.SetBlockStatePropertiesTag(tag)
}

// readBlockSet reads 0 and a tag identifier, or the inline block ID count
// plus one and the IDs.
// vanilla: ByteBufCodecs.holderSet
func readBlockSet(r *Reader) (BlockPredicateSet, bool) {
	set := BlockPredicateSet{Present: true}
	n := r.ReadVarInt()
	if r.Err() != nil || n < 0 {
		return set, false
	}
	if n == 0 {
		// A tag the server never sent is refused.
		tag := r.ReadString(32767)
		if r.Err() != nil || !ValidIdentifier(tag) ||
			(BlockTagKnown != nil && !BlockTagKnown(tag)) {
			return set, false
		}
		set.Tag = tag
		return set, true
	}
	// Each ID is indexed into the registry by the reading client.
	ids := make([]int32, 0, min(n-1, int32(r.Remaining())))
	for range n - 1 {
		id := r.ReadVarInt()
		if r.Err() != nil || id < 0 || id >= RegistrySizeBlock {
			return set, false
		}
		ids = append(ids, id)
	}
	set.Inline = ids
	return set, true
}

// readBlockPredicates reads a can_place_on or can_break value: predicates,
// each an optional block set, optional property matchers, optional NBT and
// two component matcher lists. Component matchers carry values this server
// cannot read, so a predicate with any is refused rather than relayed
// unread.
// vanilla: AdventureModePredicate.STREAM_CODEC, BlockPredicate.STREAM_CODEC
func readBlockPredicates(r *Reader) ([]BlockPredicate, bool) {
	count := r.ReadVarInt()
	if r.Err() != nil || count < 0 {
		return nil, false
	}
	// Each predicate is at least five bytes, so a count the bytes cannot
	// back fails on the reads; this only keeps the allocation honest.
	predicates := make([]BlockPredicate, 0, min(count, int32(r.Remaining()/5)))
	for range count {
		p := BlockPredicate{}
		if r.ReadBool() {
			set, ok := readBlockSet(r)
			if !ok {
				return nil, false
			}
			p.Blocks = set
		}
		if r.ReadBool() {
			props := r.ReadVarInt()
			if r.Err() != nil || props < 0 {
				return nil, false
			}
			// Non-nil even when empty: a present, empty matcher list is kept.
			p.Props = []BlockPredicateProp{}
			for range props {
				m := BlockPredicateProp{Name: r.ReadString(32767)}
				if r.Err() != nil {
					return nil, false
				}
				if r.ReadBool() {
					m.Value = r.ReadString(32767)
					if r.Err() != nil {
						return nil, false
					}
				} else {
					m.Ranged = true
					if r.ReadBool() {
						m.Min = r.ReadString(32767)
					}
					if r.ReadBool() {
						m.Max = r.ReadString(32767)
					}
					if r.Err() != nil {
						return nil, false
					}
				}
				p.Props = append(p.Props, m)
			}
		}
		if r.ReadBool() {
			// The NBT guard: the compound is decoded past — the guarantee a
			// relaying client would read it — but no matcher reads it.
			if r.Err() != nil {
				return nil, false
			}
			tag, n, err := nbt.DecodeNetworkQuota(r.data[r.pos:], nbt.NetworkQuota)
			if err != nil {
				r.fail(err)
				return nil, false
			}
			r.pos += n
			// vanilla: ByteBufCodecs.COMPOUND_TAG refuses any other tag
			compound, ok := tag.(nbt.Compound)
			if !ok {
				return nil, false
			}
			p.NBT = compound
		}
		if exact := r.ReadVarInt(); r.Err() != nil || exact != 0 {
			return nil, false
		}
		if partial := r.ReadVarInt(); r.Err() != nil || partial != 0 {
			return nil, false
		}
		predicates = append(predicates, p)
	}
	if r.Err() != nil {
		return nil, false
	}
	return predicates, true
}

// readIdentifier refuses a string the game's resource location parser would
// refuse, which would take the reading client down with the value.
// vanilla: Identifier.STREAM_CODEC
func readIdentifier(r *Reader) bool {
	id := r.ReadString(32767)
	return r.Err() == nil && ValidIdentifier(id)
}

// ValidIdentifier reports whether the game's parser would accept id: allowed
// characters only, and no ".." namespace.
// vanilla: Identifier.parse
func ValidIdentifier(id string) bool {
	namespace, path := "minecraft", id
	if i := strings.IndexByte(id, ':'); i >= 0 {
		path = id[i+1:]
		if i > 0 {
			namespace = id[:i]
		}
	}
	return validIdentifierPart(namespace, "_.-") &&
		namespace != ".." && validIdentifierPart(path, "_./-")
}

// validIdentifierPart reports whether s is only lowercase letters, digits
// and the given punctuation.
// vanilla: Identifier.validNamespaceChar, Identifier.validPathChar
func validIdentifierPart(s, punctuation string) bool {
	for _, c := range s {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			continue
		}
		if !strings.ContainsRune(punctuation, c) {
			return false
		}
	}
	return true
}
