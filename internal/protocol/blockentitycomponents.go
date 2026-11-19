package protocol

import (
	"slices"

	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// Components a block entity's item carries (banner patterns, a head's
// profile and note block sound, bees, pot sherds). Reading them is
// readRawValue's check; they are written here too because a ctrl-pick turns
// a block entity's tag into them, in the shape the reader checks.

// dyeColorCount is the size of DyeColor, which a banner layer's dye ID
// indexes; the registry is client-built-in and never sent.
const dyeColorCount = 16

// BannerPatternLayer is one banner layer: a banner_pattern registry ID and a
// DyeColor ID. The wire carries the pattern as ID plus one, zero for an
// inline pattern, whose asset and translation key the layer holds with
// Pattern at -1.
// vanilla: BannerPatternLayers.Layer
type BannerPatternLayer struct {
	Pattern        int32
	Dye            int32
	AssetID        string
	TranslationKey string
}

// SetBannerPatterns sets minecraft:banner_patterns, false for a pattern or
// dye out of range, which the reading client would refuse. A layer without
// a registry pattern writes its inline asset and key.
// vanilla: BannerPatternLayers.STREAM_CODEC
func (s *ItemStack) SetBannerPatterns(layers []BannerPatternLayer) bool {
	for _, layer := range layers {
		if layer.Dye < 0 || layer.Dye >= dyeColorCount {
			return false
		}
		if layer.Pattern < 0 {
			if !ValidIdentifier(layer.AssetID) {
				return false
			}
			continue
		}
		if layer.Pattern >= sizeBannerPattern() {
			return false
		}
	}
	var value Buffer
	value.PutVarInt(int32(len(layers)))
	for _, layer := range layers {
		if layer.Pattern < 0 {
			value.PutVarInt(0)
			value.PutString(layer.AssetID)
			value.PutString(layer.TranslationKey)
		} else {
			value.PutVarInt(layer.Pattern + 1)
		}
		value.PutVarInt(layer.Dye)
	}
	// A copy (a creative middle-click clone) shares Raw's backing array, so
	// the write below must not reach the original.
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentBannerPatterns, Data: value.Bytes()})
	return true
}

// BannerPatternsOf returns the stack's banner layers; false when it has
// none or the value does not read.
func (s ItemStack) BannerPatternsOf() ([]BannerPatternLayer, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentBannerPatterns {
			continue
		}
		r := NewReader(raw.Data)
		layers, ok := bannerPatternsValue(r)
		if !ok || r.Remaining() != 0 {
			return nil, false
		}
		return layers, true
	}
	return nil, false
}

// bannerPatternsValue decodes banner layers, reporting whether whole. A
// pattern is 0 and an inline asset and key (Pattern -1), or registry ID plus
// one. Any VarInt dye is accepted; out of range reads as the first colour.
func bannerPatternsValue(r *Reader) ([]BannerPatternLayer, bool) {
	count := r.ReadVarInt()
	if r.Err() != nil || count < 0 {
		return nil, false
	}
	// A layer is at least two bytes on the wire, the pattern and the dye.
	layers := make([]BannerPatternLayer, 0, min(count, int32(r.Remaining()/2)))
	for range count {
		layer := BannerPatternLayer{Pattern: -1}
		switch pattern := r.ReadVarInt(); {
		case pattern == 0:
			asset := r.ReadString(32767)
			if r.Err() != nil || !ValidIdentifier(asset) {
				return nil, false
			}
			layer.AssetID = asset
			layer.TranslationKey = r.ReadString(32767)
		case pattern > 0 && pattern <= sizeBannerPattern():
			layer.Pattern = pattern - 1
		default:
			return nil, false
		}
		layer.Dye = r.ReadVarInt()
		if r.Err() != nil {
			return nil, false
		}
		layers = append(layers, layer)
	}
	if r.Err() != nil {
		return nil, false
	}
	return layers, true
}

// ProfileProperty is one property of a game profile: its name and value and,
// when the game's own service signed it, the signature.
type ProfileProperty struct {
	Name      string
	Value     string
	Signature string
	Signed    bool
}

// SkinPatch is a profile's optional skin: body, cape and elytra texture
// locations and the arm model ("slim" or "wide"); empty is absent.
// vanilla: PlayerSkin.Patch
type SkinPatch struct {
	Body   string
	Cape   string
	Elytra string
	Model  string
}

// Profile is minecraft:profile: name, UUID, properties and skin patch. The
// full shape has both name and UUID; the partial one may omit either.
// vanilla: ResolvableProfile
type Profile struct {
	UUID       uuid.UUID
	Name       string
	HasUUID    bool
	HasName    bool
	Properties []ProfileProperty
	Skin       SkinPatch
}

// SetProfile sets minecraft:profile, full when both name and UUID are set,
// partial otherwise; false for what the reading client would refuse: an
// over-long name, property or signature, more than sixteen properties, or a
// texture or model the parser would refuse.
// vanilla: ResolvableProfile.STREAM_CODEC
func (s *ItemStack) SetProfile(p Profile) bool {
	var value Buffer
	if p.HasName && p.HasUUID {
		value.PutBool(true)
		value.PutUUID(p.UUID)
		value.PutString(p.Name)
	} else {
		value.PutBool(false)
		value.PutBool(p.HasName)
		if p.HasName {
			value.PutString(p.Name)
		}
		value.PutBool(p.HasUUID)
		if p.HasUUID {
			value.PutUUID(p.UUID)
		}
	}
	value.PutVarInt(int32(len(p.Properties)))
	for _, prop := range p.Properties {
		value.PutString(prop.Name)
		value.PutString(prop.Value)
		value.PutBool(prop.Signed)
		if prop.Signed {
			value.PutString(prop.Signature)
		}
	}
	// The skin patch: each texture and the model, present or absent.
	for _, tex := range []string{p.Skin.Body, p.Skin.Cape, p.Skin.Elytra} {
		value.PutBool(tex != "")
		if tex != "" {
			value.PutString(tex)
		}
	}
	value.PutBool(p.Skin.Model != "")
	if p.Skin.Model != "" {
		value.PutBool(p.Skin.Model == "slim")
	}
	// Reading the value back is what enforces the codec's string limits, in
	// the code units the client counts.
	if r := NewReader(value.Bytes()); !readProfile(r) || r.Remaining() != 0 {
		return false
	}
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentProfile, Data: value.Bytes()})
	return true
}

// ProfileOf returns the profile a stack's minecraft:profile component holds,
// reporting false for a stack without one or whose value does not read.
func (s ItemStack) ProfileOf() (Profile, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentProfile {
			continue
		}
		r := NewReader(raw.Data)
		p, ok := profileValue(r)
		if !ok || r.Remaining() != 0 {
			return Profile{}, false
		}
		return p, true
	}
	return Profile{}, false
}

// profileValue decodes a profile value: the name and UUID, the properties and
// the skin patch, and reports whether it is whole.
func profileValue(r *Reader) (Profile, bool) {
	var p Profile
	if r.ReadBool() {
		p.UUID = r.ReadUUID()
		p.HasUUID = true
		p.Name = r.ReadString(16)
		p.HasName = true
	} else {
		if r.ReadBool() {
			p.Name = r.ReadString(16)
			p.HasName = true
		}
		if r.ReadBool() {
			p.UUID = r.ReadUUID()
			p.HasUUID = true
		}
	}
	count := r.ReadVarInt()
	if r.Err() != nil || count < 0 || count > 16 {
		return Profile{}, false
	}
	for range count {
		var prop ProfileProperty
		prop.Name = r.ReadString(64)
		prop.Value = r.ReadString(32767)
		if r.ReadBool() {
			prop.Signature = r.ReadString(1024)
			prop.Signed = true
		}
		p.Properties = append(p.Properties, prop)
	}
	skin, ok := skinPatchValue(r)
	if !ok {
		return Profile{}, false
	}
	p.Skin = skin
	return p, true
}

// skinPatchValue decodes three optional texture identifiers and an optional
// model flag ("slim" set, "wide" clear).
// vanilla: PlayerSkin.Patch.STREAM_CODEC
func skinPatchValue(r *Reader) (SkinPatch, bool) {
	var p SkinPatch
	for _, dst := range []*string{&p.Body, &p.Cape, &p.Elytra} {
		if r.ReadBool() {
			id := r.ReadString(32767)
			if r.Err() != nil || !ValidIdentifier(id) {
				return SkinPatch{}, false
			}
			*dst = id
		}
	}
	if r.ReadBool() {
		p.Model = "wide"
		if r.ReadBool() {
			p.Model = "slim"
		}
	}
	if r.Err() != nil {
		return SkinPatch{}, false
	}
	return p, true
}

// SetNoteBlockSound sets minecraft:note_block_sound (what a note block or
// head plays); false for an identifier the parser would refuse.
// vanilla: DataComponents.NOTE_BLOCK_SOUND, Identifier.STREAM_CODEC
func (s *ItemStack) SetNoteBlockSound(id string) bool {
	if !ValidIdentifier(id) {
		return false
	}
	var value Buffer
	value.PutString(id)
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentNoteBlockSound, Data: value.Bytes()})
	return true
}

// NoteBlockSoundOf returns the resource location the stack's
// minecraft:note_block_sound names, reporting false for a stack without one.
func (s ItemStack) NoteBlockSoundOf() (string, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentNoteBlockSound {
			continue
		}
		r := NewReader(raw.Data)
		id := r.ReadString(32767)
		if r.Err() != nil || r.Remaining() != 0 || !ValidIdentifier(id) {
			return "", false
		}
		return id, true
	}
	return "", false
}

// JukeboxPlayableOf returns the jukebox_song registry ID, false without a
// component or for an inline song, whose wire value is only relayed.
func (s ItemStack) JukeboxPlayableOf() (int32, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentJukeboxPlayable {
			continue
		}
		r := NewReader(raw.Data)
		id := r.ReadVarInt()
		if r.Err() != nil || r.Remaining() != 0 || id < 1 || id > sizeJukeboxSong() {
			return 0, false
		}
		return id - 1, true
	}
	return 0, false
}

// SetJukeboxPlayable sets minecraft:jukebox_playable to a jukebox_song ID.
// vanilla: JukeboxPlayable.STREAM_CODEC
func (s *ItemStack) SetJukeboxPlayable(id int32) {
	var value Buffer
	value.PutVarInt(id + 1)
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentJukeboxPlayable, Data: value.Bytes()})
}

// SetPotDecorations sets minecraft:pot_decorations: back, left, right,
// front, each an item template or an empty stack for an absent side; false
// for a template that would not encode.
// vanilla: PotDecorations.STREAM_CODEC
func (s *ItemStack) SetPotDecorations(sides [4]ItemStack) bool {
	var value Buffer
	for _, side := range sides {
		if side.Empty() {
			value.PutBool(false)
			continue
		}
		if side.ItemID <= 0 || side.ItemID >= RegistrySizeItem || side.Count < 1 {
			return false
		}
		value.PutBool(true)
		value.PutItemTemplate(side)
	}
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentPotDecorations, Data: value.Bytes()})
	return true
}

// PotDecorationsOf returns the templates back, left, right, front, an empty
// stack for an absent side; false when the component is absent or unreadable.
func (s ItemStack) PotDecorationsOf() ([4]ItemStack, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentPotDecorations {
			continue
		}
		r := NewReader(raw.Data)
		sides, ok := potDecorationsValue(r, 0)
		if !ok || r.Remaining() != 0 {
			return [4]ItemStack{}, false
		}
		return sides, true
	}
	return [4]ItemStack{}, false
}

// potDecorationsValue decodes the four optional item templates, an empty
// stack for an absent side.
func potDecorationsValue(r *Reader, depth int) ([4]ItemStack, bool) {
	var sides [4]ItemStack
	for i := range sides {
		present := r.ReadBool()
		if r.Err() != nil {
			return [4]ItemStack{}, false
		}
		if !present {
			continue
		}
		side, ok := readItemTemplate(r, depth)
		if !ok {
			return [4]ItemStack{}, false
		}
		sides[i] = side
	}
	return sides, true
}

// readNoteBlockSound reads minecraft:note_block_sound, a bare resource
// location.
// vanilla: Identifier.STREAM_CODEC
func readNoteBlockSound(r *Reader) bool {
	return readIdentifier(r)
}

// readPotDecorations reads minecraft:pot_decorations.
// vanilla: PotDecorations.STREAM_CODEC
func readPotDecorations(r *Reader, depth int) bool {
	_, ok := potDecorationsValue(r, depth)
	return ok
}

// Bee is one beehive occupant: entity type ID, network NBT (without its
// "id"), and ticks spent and required in the hive.
// vanilla: BeehiveBlockEntity.Occupant
type Bee struct {
	EntityType     int32
	Data           nbt.CompoundTag
	TicksInHive    int32
	MinTicksInHive int32
}

// SetBees sets minecraft:bees; false for an entity type out of range or
// entity data that would not encode.
// vanilla: BeehiveBlockEntity.Occupant.STREAM_CODEC
func (s *ItemStack) SetBees(bees []Bee) bool {
	var value Buffer
	value.PutVarInt(int32(len(bees)))
	for _, bee := range bees {
		if bee.EntityType < 0 || bee.EntityType >= RegistrySizeEntityType {
			return false
		}
		data, err := nbt.EncodeNetwork(withoutEntityID(bee.Data))
		if err != nil {
			return false
		}
		value.PutVarInt(bee.EntityType)
		value.PutRaw(data)
		value.PutVarInt(bee.TicksInHive)
		value.PutVarInt(bee.MinTicksInHive)
	}
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentBees, Data: value.Bytes()})
	return true
}

// BeesOf returns the occupants the stack's minecraft:bees component holds,
// reporting false for a stack without one or whose bees do not read.
func (s ItemStack) BeesOf() ([]Bee, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentBees {
			continue
		}
		r := NewReader(raw.Data)
		bees, ok := beesValue(r)
		if !ok || r.Remaining() != 0 {
			return nil, false
		}
		return bees, true
	}
	return nil, false
}

// readBees reads minecraft:bees off a stack's arrival.
// vanilla: BeehiveBlockEntity.Occupant.STREAM_CODEC
func readBees(r *Reader) bool {
	_, ok := beesValue(r)
	return ok
}

// beesValue decodes a count, then each bee's type, NBT and two tick counts.
func beesValue(r *Reader) ([]Bee, bool) {
	count := r.ReadVarInt()
	if r.Err() != nil || count < 0 {
		return nil, false
	}
	// A bee is at least five bytes on the wire: the type, an empty compound's
	// two and the two tick counts.
	bees := make([]Bee, 0, min(count, int32(r.Remaining()/5)))
	for range count {
		entityType := r.ReadVarInt()
		if r.Err() != nil || entityType < 0 || entityType >= RegistrySizeEntityType {
			return nil, false
		}
		tag, n, err := nbt.DecodeNetworkOrderedQuota(r.data[r.pos:], nbt.NetworkQuota)
		if err != nil {
			r.fail(err)
			return nil, false
		}
		data, ok := tag.(nbt.CompoundTag)
		if !ok {
			return nil, false
		}
		r.pos += n
		ticks := r.ReadVarInt()
		minTicks := r.ReadVarInt()
		if r.Err() != nil {
			return nil, false
		}
		bees = append(bees, Bee{
			EntityType:     entityType,
			Data:           withoutEntityID(data),
			TicksInHive:    ticks,
			MinTicksInHive: minTicks,
		})
	}
	if r.Err() != nil {
		return nil, false
	}
	return bees, true
}

// SetBaseColor sets minecraft:base_color, a shield's base dye; false out of
// range.
// vanilla: DyeColor.STREAM_CODEC
func (s *ItemStack) SetBaseColor(dye int32) bool {
	if dye < 0 || dye >= dyeColorCount {
		return false
	}
	var value Buffer
	value.PutVarInt(dye)
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentBaseColor, Data: value.Bytes()})
	return true
}

// BaseColorOf returns minecraft:base_color's dye, out of range as white;
// false when absent or unreadable.
// vanilla: DyeColor.BY_ID (OutOfBoundsStrategy.ZERO)
func (s ItemStack) BaseColorOf() (int32, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentBaseColor {
			continue
		}
		r := NewReader(raw.Data)
		dye := r.ReadVarInt()
		if r.Err() != nil || r.Remaining() != 0 {
			return 0, false
		}
		if dye < 0 || dye >= dyeColorCount {
			dye = 0
		}
		return dye, true
	}
	return 0, false
}

// readBaseColor reads any VarInt; out of range is the first colour.
// vanilla: DyeColor.STREAM_CODEC
func readBaseColor(r *Reader) bool {
	r.ReadVarInt()
	return r.Err() == nil
}

// SignText is minecraft:sign_text_front or sign_text_back: four line
// components, the filtered lines when they differ (nil for none), a DyeColor
// ID and glow.
// vanilla: SignText.STREAM_CODEC
type SignText struct {
	Messages [4]nbt.Tag
	Filtered *[4]nbt.Tag
	Dye      int32
	Glowing  bool
}

// signTextComponent is the component type for a side.
func signTextComponent(front bool) int32 {
	if front {
		return DataComponentSignTextFront
	}
	return DataComponentSignTextBack
}

// SetSignText sets one side's sign text component.
func (s *ItemStack) SetSignText(front bool, text SignText) {
	var value Buffer
	for _, line := range text.Messages {
		value.PutText(Text{Raw: line})
	}
	value.PutBool(text.Filtered != nil)
	if text.Filtered != nil {
		for _, line := range text.Filtered {
			value.PutText(Text{Raw: line})
		}
	}
	value.PutVarInt(text.Dye)
	value.PutBool(text.Glowing)
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: signTextComponent(front), Data: value.Bytes()})
}

// SignTextOf returns one side's sign text component, false for a stack
// without it.
func (s ItemStack) SignTextOf(front bool) (SignText, bool) {
	component := signTextComponent(front)
	for _, raw := range s.Raw {
		if raw.Type != component {
			continue
		}
		r := NewReader(raw.Data)
		text, ok := signTextValue(r)
		return text, ok && r.Remaining() == 0
	}
	return SignText{}, false
}

// readSignText checks a sign text component off a stack's arrival.
func readSignText(r *Reader) bool {
	_, ok := signTextValue(r)
	return ok
}

// signTextValue decodes four components, an optional four filtered ones, a
// dye and glow. Any VarInt dye is accepted; the client reads one out of range
// as white.
// vanilla: SignText.STREAM_CODEC, DyeColor.STREAM_CODEC
func signTextValue(r *Reader) (SignText, bool) {
	var text SignText
	if !readSignLines(r, &text.Messages) {
		return SignText{}, false
	}
	if r.ReadBool() {
		var filtered [4]nbt.Tag
		if !readSignLines(r, &filtered) {
			return SignText{}, false
		}
		text.Filtered = &filtered
	}
	text.Dye = r.ReadVarInt()
	text.Glowing = r.ReadBool()
	return text, r.Err() == nil
}

// readSignLines reads four network NBT text components.
// vanilla: ComponentSerialization.STREAM_CODEC
func readSignLines(r *Reader, lines *[4]nbt.Tag) bool {
	for i := range lines {
		line, ok := ReadComponentText(r)
		if !ok {
			return false
		}
		lines[i] = TextTag(line)
	}
	return true
}

// SetWaxed sets minecraft:waxed, a unit: its presence is the value.
// vanilla: DataComponents.WAXED
func (s *ItemStack) SetWaxed() {
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentWaxed})
}

// Waxed reports whether the stack carries minecraft:waxed.
func (s ItemStack) Waxed() bool {
	for _, raw := range s.Raw {
		if raw.Type == DataComponentWaxed {
			return true
		}
	}
	return false
}
