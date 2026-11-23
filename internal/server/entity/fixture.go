package entity

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The fixtures never move: the three displays, the interaction, the marker,
// the cushion, and the ominous item spawner. Their state is the saved
// compound, and what clients see is read from it.
const (
	FixtureOminousItemSpawner = "minecraft:ominous_item_spawner"
	FixtureCushion            = "minecraft:cushion"

	FixtureBlockDisplay = "minecraft:block_display"
	FixtureItemDisplay  = "minecraft:item_display"
	FixtureTextDisplay  = "minecraft:text_display"
	FixtureInteraction  = "minecraft:interaction"
	FixtureMarker       = "minecraft:marker"
)

// Entity NBT keys decoded through BlockState.CODEC.
// vanilla: BlockDisplay, EnderMan, AbstractArrow, FallingBlockEntity, PrimedTnt, AbstractMinecart
const (
	BlockStateKeyDisplay  = "block_state"
	BlockStateKeyCarried  = "carriedBlockState"
	BlockStateKeyInBlock  = "inBlockState"
	BlockStateKeyFalling  = "BlockState"
	BlockStateKeyMinecart = "DisplayState"
)

// BlockStateNBTKeys lists every entity NBT key decoded with BlockState.CODEC.
var BlockStateNBTKeys = []string{
	BlockStateKeyDisplay,
	BlockStateKeyCarried,
	BlockStateKeyInBlock,
	BlockStateKeyFalling,
	BlockStateKeyMinecart,
}

// The metadata indexes of Display and its subtypes.
// vanilla: Display.defineSynchedData and the subtypes' own
const (
	displayIndexStartDelay    = 8
	displayIndexInterpolation = 9
	displayIndexTeleport      = 10
	displayIndexTranslation   = 11
	displayIndexScale         = 12
	displayIndexLeftRotation  = 13
	displayIndexRightRotation = 14
	displayIndexBillboard     = 15
	displayIndexBrightness    = 16
	displayIndexViewRange     = 17
	displayIndexShadowRadius  = 18
	displayIndexShadowPower   = 19
	displayIndexWidth         = 20
	displayIndexHeight        = 21
	displayIndexGlow          = 22
	displayIndexBlock         = 23
	displayIndexItem          = 23
	displayIndexItemContext   = 24
	displayIndexText          = 23
	displayIndexLineWidth     = 24
	displayIndexBackground    = 25
	displayIndexTextOpacity   = 26
	displayIndexTextFlags     = 27
	interactionIndexWidth     = 8
	interactionIndexHeight    = 9
	interactionIndexResponse  = 10
	ominousItemIndex          = 8
	cushionIndexColor         = 8
)

// The entity metadata field types the fixtures use beyond protocol's.
const (
	metadataText       = 5
	metadataVector3    = 39
	metadataQuaternion = 40
	metadataDyeColor   = 43
)

// FixtureKind reports whether the entity type name is a fixture.
func FixtureKind(name string) bool {
	switch name {
	case FixtureBlockDisplay, FixtureItemDisplay, FixtureTextDisplay, FixtureInteraction, FixtureMarker,
		FixtureOminousItemSpawner, FixtureCushion:
		return true
	}
	return false
}

// FixtureTrackingRange is the clientTrackingRange of the fixture's type in
// chunks; the marker's zero means no client is ever shown one.
// vanilla: EntityTypes.TEXT_DISPLAY, INTERACTION, MARKER, OMINOUS_ITEM_SPAWNER, CUSHION
func FixtureTrackingRange(kind string) int {
	switch kind {
	case FixtureMarker:
		return 0
	case FixtureOminousItemSpawner:
		return 8
	}
	return 10
}

// FixtureTypeID is the protocol entity type ID of the fixture kind.
func FixtureTypeID(kind string) int32 {
	id, _ := registry.EntityTypeID(kind)
	return id
}

// The cushion's box in blocks. vanilla: EntityTypes.CUSHION
const (
	CushionWidth  = 1.0
	CushionHeight = 0.25
)

// FixtureBox is the hit box of a fixture at (x, y, z): the interaction's
// width and height from its tag, the cushion's fixed size, nothing for the
// rest.
// vanilla: Interaction.getDimensions
func FixtureBox(kind string, tag nbt.Compound, x, y, z float64) (minX, minY, minZ, maxX, maxY, maxZ float64) {
	if kind == FixtureCushion {
		return x - CushionWidth/2, y, z - CushionWidth/2, x + CushionWidth/2, y + CushionHeight, z + CushionWidth/2
	}
	if kind != FixtureInteraction {
		return x, y, z, x, y, z
	}
	w := floatField(tag, "width", 1)
	h := floatField(tag, "height", 1)
	half := float64(w) / 2
	return x - half, y, z - half, x + half, y + float64(h), z + half
}

// MetaField is one synched value of a fixture: index, serializer, and the
// encoded value.
type MetaField struct {
	Index uint8
	Type  int32
	Value []byte
}

// fieldSet collects the fields of one pass over the tag. All names every
// field, defaults included; without it the ones at their default are left out,
// which is what a client that has just been spawned one already holds.
type fieldSet struct {
	all bool
	out []MetaField
}

func (f *fieldSet) add(index uint8, typ int32, put func(b *protocol.Buffer)) {
	var b protocol.Buffer
	put(&b)
	f.out = append(f.out, MetaField{Index: index, Type: typ, Value: b.Bytes()})
}

// FixtureFields is the synched values of the fixture read off its saved
// compound, in index order; all includes the ones at their default, so a write
// that puts a value back is told as a change.
// vanilla: Entity.defineSynchedData, Display.defineSynchedData and the
// subtypes', Interaction.defineSynchedData
func FixtureFields(kind string, tag nbt.Compound, all bool) []MetaField {
	f := &fieldSet{all: all}
	f.putEntity(tag)
	switch kind {
	case FixtureOminousItemSpawner:
		if item, ok := nbt.Get[nbt.Compound](tag, "item"); ok {
			if stack, ok := registry.StoredStack(item); ok && !stack.Empty() {
				f.add(ominousItemIndex, protocol.MetadataSlot, func(b *protocol.Buffer) { b.PutSlot(stack) })
			}
		}
		if all && len(f.out) == 0 {
			f.add(ominousItemIndex, protocol.MetadataSlot, func(b *protocol.Buffer) { b.PutSlot(protocol.ItemStack{}) })
		}
	case FixtureCushion:
		if color := CushionColor(tag); color != 0 || f.all {
			f.add(cushionIndexColor, metadataDyeColor, func(b *protocol.Buffer) { b.PutVarInt(color) })
		}
	case FixtureInteraction:
		f.float(interactionIndexWidth, floatField(tag, "width", 1), 1)
		f.float(interactionIndexHeight, floatField(tag, "height", 1), 1)
		f.boolean(interactionIndexResponse, nbt.BoolField(tag, "response"))
	case FixtureBlockDisplay, FixtureItemDisplay, FixtureTextDisplay:
		f.putDisplay(tag)
		switch kind {
		case FixtureBlockDisplay:
			f.putBlockState(tag)
		case FixtureItemDisplay:
			f.putItem(tag)
		default:
			f.putText(tag)
		}
	}
	slices.SortStableFunc(f.out, func(a, b MetaField) int { return int(a.Index) - int(b.Index) })
	return f.out
}

// FixtureMetadata is the Set Entity Data payload for the fixture: every
// field whose value differs from its default, nil when none does.
// vanilla: ServerEntity.sendPairingData
func FixtureMetadata(id int32, kind string, tag nbt.Compound) []byte {
	fields := FixtureFields(kind, tag, false)
	if len(fields) == 0 {
		return nil
	}
	var b protocol.Buffer
	b.PutVarInt(id)
	for _, f := range fields {
		b.PutMetadata(f.Index, f.Type)
		b.PutRaw(f.Value)
	}
	b.EndMetadata()
	return b.Bytes()
}

// putEntity is the fields every entity has, as its save form sets them.
// vanilla: Entity.load
func (f *fieldSet) putEntity(tag nbt.Compound) {
	var flags int32
	if nbt.BoolField(tag, "Glowing") {
		flags |= protocol.EntityFlagGlowing
	}
	f.byteV(protocol.MetadataIndexEntityFlags, flags, 0)
	name, named := protocol.StyledTextOf(tag["CustomName"])
	if named {
		if data, err := nbt.EncodeNetwork(protocol.TextTag(name)); err == nil {
			f.add(protocol.MetadataIndexCustomName, protocol.MetadataOptionalText, func(b *protocol.Buffer) {
				b.PutBool(true)
				b.PutRaw(data)
			})
		} else {
			named = false
		}
	}
	if !named && f.all {
		f.add(protocol.MetadataIndexCustomName, protocol.MetadataOptionalText, func(b *protocol.Buffer) { b.PutBool(false) })
	}
	f.boolean(protocol.MetadataIndexCustomNameVisible, nbt.BoolField(tag, "CustomNameVisible"))
	f.boolean(protocol.MetadataIndexSilent, nbt.BoolField(tag, "Silent"))
	f.boolean(protocol.MetadataIndexNoGravity, nbt.BoolField(tag, "NoGravity"))
}

// IsDisplayFixture reports whether kind is a display entity.
func IsDisplayFixture(kind string) bool {
	return kind == FixtureBlockDisplay || kind == FixtureItemDisplay || kind == FixtureTextDisplay
}

// DisplayStartDelayIndex is the metadata index of a display's interpolation
// start delay, which vanilla marks dirty on every write.
const DisplayStartDelayIndex = displayIndexStartDelay

func (f *fieldSet) putDisplay(tag nbt.Compound) {
	f.varInt(displayIndexStartDelay, intField(tag, "start_interpolation", 0), 0)
	f.varInt(displayIndexInterpolation, intField(tag, "interpolation_duration", 0), 0)
	// vanilla: Display.readAdditionalSaveData clamps the teleport duration
	f.varInt(displayIndexTeleport, min(max(intField(tag, "teleport_duration", 0), 0), 59), 0)
	t := TransformationOf(tag["transformation"])
	f.vector(displayIndexTranslation, t.Translation, [3]float32{})
	f.vector(displayIndexScale, t.Scale, [3]float32{1, 1, 1})
	f.quaternion(displayIndexLeftRotation, t.LeftRotation)
	f.quaternion(displayIndexRightRotation, t.RightRotation)
	f.byteV(displayIndexBillboard, enumField(tag, "billboard", "fixed", "vertical", "horizontal", "center"), 0)
	// vanilla: Brightness.CODEC requires both block and sky, each 0 to 15
	brightness := int32(-1)
	if b, ok := nbt.Get[nbt.Compound](tag, "brightness"); ok {
		block, hasBlock := nbt.IntValue(b["block"])
		sky, hasSky := nbt.IntValue(b["sky"])
		if hasBlock && hasSky && block >= 0 && block <= 15 && sky >= 0 && sky <= 15 {
			brightness = block<<4 | sky<<20
		}
	}
	f.varInt(displayIndexBrightness, brightness, -1)
	f.float(displayIndexViewRange, floatField(tag, "view_range", 1), 1)
	f.float(displayIndexShadowRadius, floatField(tag, "shadow_radius", 0), 0)
	f.float(displayIndexShadowPower, floatField(tag, "shadow_strength", 1), 1)
	f.float(displayIndexWidth, floatField(tag, "width", 0), 0)
	f.float(displayIndexHeight, floatField(tag, "height", 0), 0)
	f.varInt(displayIndexGlow, intField(tag, "glow_color_override", -1), -1)
}

// putBlockState sends the displayed block when the tag names a state.
func (f *fieldSet) putBlockState(tag nbt.Compound) {
	var state int32
	if block, ok := tag[BlockStateKeyDisplay]; ok {
		name, props := registry.BlockStateOf(block)
		if s, ok := registry.FindState(registry.Qualify(name), props); ok {
			state = int32(s)
		}
	}
	if state == 0 && !f.all {
		return
	}
	f.add(displayIndexBlock, protocol.MetadataBlockState, func(b *protocol.Buffer) { b.PutVarInt(state) })
}

func (f *fieldSet) putItem(tag nbt.Compound) {
	var stack protocol.ItemStack
	if item, ok := nbt.Get[nbt.Compound](tag, "item"); ok {
		if s, ok := registry.StoredStack(item); ok && !s.Empty() {
			stack = s
		}
	}
	if !stack.Empty() || f.all {
		f.add(displayIndexItem, protocol.MetadataSlot, func(b *protocol.Buffer) { b.PutSlot(stack) })
	}
	// vanilla: ItemDisplayContext
	contexts := []string{"none", "thirdperson_lefthand", "thirdperson_righthand",
		"firstperson_lefthand", "firstperson_righthand", "head", "gui", "ground", "fixed", "on_shelf"}
	f.byteV(displayIndexItemContext, enumField(tag, "item_display", contexts...), 0)
}

func (f *fieldSet) putText(tag nbt.Compound) {
	text, ok := protocol.StyledTextOf(tag["text"])
	if !ok {
		text, _ = protocol.StyledTextOf(nbt.String(""))
		ok = f.all
	}
	if ok {
		if data, err := nbt.EncodeNetwork(protocol.TextTag(text)); err == nil {
			f.add(displayIndexText, metadataText, func(b *protocol.Buffer) { b.PutRaw(data) })
		}
	}
	f.varInt(displayIndexLineWidth, intField(tag, "line_width", 200), 200)
	f.varInt(displayIndexBackground, intField(tag, "background", 0x40000000), 0x40000000)
	opacity := int32(-1)
	if v, ok := nbt.ByteValue(tag["text_opacity"]); ok {
		opacity = int32(v)
	}
	f.byteV(displayIndexTextOpacity, opacity, -1)
	var flags int32
	for key, mask := range map[string]int32{"shadow": 1, "see_through": 2, "default_background": 4} {
		if nbt.BoolField(tag, key) {
			flags |= mask
		}
	}
	switch enumField(tag, "alignment", "center", "left", "right") {
	case 1:
		flags |= 8
	case 2:
		flags |= 0x10
	}
	f.byteV(displayIndexTextFlags, flags, 0)
}

func (f *fieldSet) float(index uint8, v, def float32) {
	if v != def || f.all {
		f.add(index, protocol.MetadataFloat, func(b *protocol.Buffer) { b.PutF32(v) })
	}
}

func (f *fieldSet) varInt(index uint8, v, def int32) {
	if v != def || f.all {
		f.add(index, protocol.MetadataVarInt, func(b *protocol.Buffer) { b.PutVarInt(v) })
	}
}

func (f *fieldSet) byteV(index uint8, v, def int32) {
	if v != def || f.all {
		f.add(index, protocol.MetadataByte, func(b *protocol.Buffer) { b.PutU8(uint8(v)) })
	}
}

func (f *fieldSet) boolean(index uint8, v bool) {
	if v || f.all {
		f.add(index, protocol.MetadataBoolean, func(b *protocol.Buffer) { b.PutBool(v) })
	}
}

func (f *fieldSet) vector(index uint8, v [3]float32, def [3]float32) {
	if v != def || f.all {
		f.add(index, metadataVector3, func(b *protocol.Buffer) {
			for _, c := range v {
				b.PutF32(c)
			}
		})
	}
}

// quaternion sends a rotation normalized, as the game stores it, and omits
// the identity.
// vanilla: ExtraCodecs.QUATERNIONF_COMPONENTS
func (f *fieldSet) quaternion(index uint8, v [4]float32) {
	v = normalizedQuaternion(v)
	if v != [4]float32{0, 0, 0, 1} || f.all {
		f.add(index, metadataQuaternion, func(b *protocol.Buffer) {
			for _, c := range v {
				b.PutF32(c)
			}
		})
	}
}

// normalizedQuaternion is v scaled to unit length, the identity for a
// rotation that has no length.
func normalizedQuaternion(v [4]float32) [4]float32 {
	length := math.Sqrt(float64(v[0])*float64(v[0]) + float64(v[1])*float64(v[1]) +
		float64(v[2])*float64(v[2]) + float64(v[3])*float64(v[3]))
	if length == 0 || math.IsInf(length, 0) || math.IsNaN(length) {
		return [4]float32{0, 0, 0, 1}
	}
	return [4]float32{float32(float64(v[0]) / length), float32(float64(v[1]) / length),
		float32(float64(v[2]) / length), float32(float64(v[3]) / length)}
}

// floatsOf reads a list of exactly n numbers, nil for anything else.
func floatsOf(tag nbt.Tag, n int) []float32 {
	list, ok := tag.(nbt.List)
	if !ok || len(list.Items) != n {
		return nil
	}
	out := make([]float32, n)
	for i, item := range list.Items {
		v, ok := floatOf(item)
		if !ok {
			return nil
		}
		out[i] = v
	}
	return out
}

func floatOf(tag nbt.Tag) (float32, bool) {
	switch v := tag.(type) {
	case nbt.Float:
		return float32(v), true
	case nbt.Double:
		return float32(v), true
	}
	if v, ok := nbt.LongValue(tag); ok {
		return float32(v), true
	}
	return 0, false
}

func floatField(tag nbt.Compound, key string, def float32) float32 {
	if v, ok := floatOf(tag[key]); ok && !math.IsNaN(float64(v)) {
		return v
	}
	return def
}

func intField(tag nbt.Compound, key string, def int32) int32 {
	if v, ok := nbt.IntField(tag, key); ok {
		return v
	}
	return def
}

// enumField is the index of the string tag's value among names, 0 when the
// tag names none of them.
func enumField(tag nbt.Compound, key string, names ...string) int32 {
	s, _ := nbt.Get[nbt.String](tag, key)
	for i, name := range names {
		if string(s) == name {
			return int32(i)
		}
	}
	return 0
}

// FixtureSaved writes the kind's fields in addAdditionalSaveData order.
// vanilla: Display and its subtypes, Interaction, Marker, Cushion, OminousItemSpawner
func FixtureSaved(kind string, source nbt.Compound, tag nbt.CompoundTag) {
	set := func(key string, fallback nbt.Tag) {
		if value, has := source[key]; has {
			tag.Set(key, value)
		} else if fallback != nil {
			tag.Set(key, fallback)
		}
	}
	// Native stack fields are encoded from their value, rather than replaying
	// the field order of the compound that supplied them.
	setItem := func() {
		if item, ok := nbt.CompoundView(source["item"]); ok {
			if stack, ok := registry.StoredStack(item); ok && !stack.Empty() {
				tag.Set("item", registry.StackTag(stack))
				return
			}
		}
		set("item", nil)
	}
	vec := func(v ...float32) nbt.Tag {
		items := make([]nbt.Tag, len(v))
		for i, f := range v {
			items[i] = nbt.Float(f)
		}
		return nbt.List{Elem: nbt.TypeFloat, Items: items}
	}
	switch kind {
	case FixtureOminousItemSpawner:
		setItem()
		set("spawn_item_after_ticks", nbt.Long(0))
		return
	case FixtureCushion:
		// BlockAttachedEntity derives its block position from the saved position.
		if pos, ok := nbt.Get[nbt.List](tag.Entries(), "Pos"); ok && len(pos.Items) == 3 {
			var cell nbt.IntArray
			for _, v := range pos.Items {
				d, _ := v.(nbt.Double)
				cell = append(cell, int32(math.Floor(float64(d))))
			}
			tag.Set("block_pos", cell)
		}
		tag.Set("color", nbt.String(registry.DyeNames[CushionColor(source)]))
		return
	case FixtureInteraction:
		set("width", nbt.Float(1))
		set("height", nbt.Float(1))
		set("attack", nil)
		set("interaction", nil)
		set("response", nbt.Byte(0))
		return
	case FixtureBlockDisplay, FixtureItemDisplay, FixtureTextDisplay:
	default:
		return
	}
	t := TransformationOf(source["transformation"])
	// vanilla: Transformation.CODEC
	transform := &nbt.OrderedCompound{}
	transform.Set("translation", vec(t.Translation[:]...))
	transform.Set("left_rotation", vec(t.LeftRotation[:]...))
	transform.Set("scale", vec(t.Scale[:]...))
	transform.Set("right_rotation", vec(t.RightRotation[:]...))
	tag.Set("transformation", transform)
	set("billboard", nbt.String("fixed"))
	set("interpolation_duration", nbt.Int(0))
	set("teleport_duration", nbt.Int(0))
	set("view_range", nbt.Float(1))
	set("shadow_radius", nbt.Float(0))
	set("shadow_strength", nbt.Float(1))
	set("width", nbt.Float(0))
	set("height", nbt.Float(0))
	set("glow_color_override", nbt.Int(-1))
	set("brightness", nil)
	switch kind {
	case FixtureBlockDisplay:
		name, props := registry.BlockStateOf(source[BlockStateKeyDisplay])
		state, _ := registry.FindState(registry.Qualify(name), props)
		tag.Set(BlockStateKeyDisplay, registry.BlockStateCodecTag(state))
	case FixtureItemDisplay:
		setItem()
		set("item_display", nbt.String("none"))
	case FixtureTextDisplay:
		set("text", nbt.String(""))
		set("line_width", nbt.Int(200))
		set("background", nbt.Int(0x40000000))
		set("text_opacity", nbt.Byte(-1))
		set("shadow", nbt.Byte(0))
		set("see_through", nbt.Byte(0))
		set("default_background", nbt.Byte(0))
		set("alignment", nbt.String("center"))
	}
}

// CushionColor is the cushion's DyeColor ID read off its color key, white
// for one missing or unknown.
// vanilla: Cushion.readAdditionalSaveData
func CushionColor(tag nbt.Compound) int32 {
	name, _ := nbt.Get[nbt.String](tag, "color")
	return int32(DyeOfColour(string(name)))
}
