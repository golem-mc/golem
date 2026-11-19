package registry

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// equippableOf reads a minecraft:equippable value in the shape its codec
// stores and returns it in its wire shape.
// vanilla: Equippable.CODEC
func equippableOf(value nbt.Tag) (protocol.RawComponent, bool) {
	entry, ok := nbt.CompoundView(value)
	if !ok {
		return protocol.RawComponent{}, false
	}
	slotName, ok := entry["slot"].(nbt.String)
	if !ok {
		return protocol.RawComponent{}, false
	}
	slot, ok := protocol.EquipmentSlotID(string(slotName))
	if !ok {
		return protocol.RawComponent{}, false
	}
	var out protocol.Buffer
	out.PutVarInt(slot)
	equip, ok := soundOr(entry, "equip_sound", "minecraft:item.armor.equip_generic")
	if !ok {
		return protocol.RawComponent{}, false
	}
	putSoundHolder(&out, equip)
	for _, key := range []string{"asset_id", "camera_overlay"} {
		field, has := entry[key]
		out.PutBool(has)
		if !has {
			continue
		}
		name, ok := field.(nbt.String)
		if !ok || !protocol.ValidIdentifier(string(name)) {
			return protocol.RawComponent{}, false
		}
		out.PutString(Qualify(string(name)))
	}
	entities, has := entry["allowed_entities"]
	out.PutBool(has)
	if has {
		set, ok := entitySetOf(entities)
		if !ok {
			return protocol.RawComponent{}, false
		}
		set.Put(&out)
	}
	for _, flag := range []struct {
		key     string
		missing bool
	}{{"dispensable", true}, {"swappable", true}, {"damage_on_hurt", true}, {"equip_on_interact", false}, {"can_be_sheared", false}} {
		set := flag.missing
		if field, has := entry[flag.key]; has {
			if set, ok = boolValue(field); !ok {
				return protocol.RawComponent{}, false
			}
		}
		out.PutBool(set)
	}
	shearing, ok := soundOr(entry, "shearing_sound", "minecraft:item.shears.snip")
	if !ok {
		return protocol.RawComponent{}, false
	}
	putSoundHolder(&out, shearing)
	return protocol.RawComponent{Type: protocol.DataComponentEquippable, Data: out.Bytes()}, true
}

// soundOr reads the sound holder an entry names under key, or the
// registered sound def when the key is absent.
func soundOr(entry nbt.Compound, key, def string) (protocol.SoundHolder, bool) {
	value, has := entry[key]
	if !has {
		id, ok := SoundEventID(def)
		return protocol.SoundHolder{ID: id}, ok
	}
	return soundHolderOf(value)
}

// soundHolderOf reads a Holder<SoundEvent>: a registered sound's name, or an
// inline {sound_id, range} whose malformed range is dropped.
// vanilla: SoundEvent.CODEC, RegistryFileCodec.decode (an unregistered name
// is refused), SoundEvent.DIRECT_CODEC (range is lenientOptionalFieldOf)
func soundHolderOf(value nbt.Tag) (protocol.SoundHolder, bool) {
	switch v := value.(type) {
	case nbt.String:
		if !protocol.ValidIdentifier(string(v)) {
			return protocol.SoundHolder{}, false
		}
		id, ok := SoundEventID(Qualify(string(v)))
		return protocol.SoundHolder{ID: id}, ok
	case nbt.CompoundTag:
		name, ok := v.Entries()["sound_id"].(nbt.String)
		if !ok || !protocol.ValidIdentifier(string(name)) {
			return protocol.SoundHolder{}, false
		}
		h := protocol.SoundHolder{Name: Qualify(string(name))}
		if rng, present := v.Entries()["range"]; present {
			h.Range, h.HasRange = floatOf(rng)
		}
		return h, true
	}
	return protocol.SoundHolder{}, false
}

// soundHolderTag writes a sound holder in the shape soundHolderOf reads: a
// registry entry's name, or an inline sound as {sound_id, range}; false for
// an ID outside the registry.
// vanilla: RegistryFileCodec.encode
func soundHolderTag(h protocol.SoundHolder) (nbt.Tag, bool) {
	if h.Name == "" {
		if h.ID < 0 || int(h.ID) >= len(SoundEvents) {
			return nil, false
		}
		return nbt.String(SoundEvents[h.ID]), true
	}
	tag := nbt.NewOrderedCompound(1)
	tag.Set("sound_id", nbt.String(h.Name))
	if h.HasRange {
		tag.Set("range", nbt.Float(h.Range))
	}
	return tag, true
}

// putSoundHolder writes a sound holder: a registry ID plus one, else 0 and
// the inline sound.
// vanilla: SoundEvent.STREAM_CODEC
func putSoundHolder(out *protocol.Buffer, h protocol.SoundHolder) {
	if h.Name == "" {
		out.PutVarInt(h.ID + 1)
		return
	}
	out.PutVarInt(0)
	out.PutString(h.Name)
	out.PutBool(h.HasRange)
	if h.HasRange {
		out.PutF32(h.Range)
	}
}

// equippableTag writes a minecraft:equippable in the shape equippableOf
// reads, leaving out the fields that hold the codec's defaults.
// vanilla: Equippable.CODEC
func equippableTag(e protocol.Equippable) *nbt.OrderedCompound {
	out := nbt.NewOrderedCompound(1)
	out.Set("slot", nbt.String(e.Slot))
	putSoundTag(out, "equip_sound", e.EquipSound, "minecraft:item.armor.equip_generic")
	if e.AssetID != "" {
		out.Set("asset_id", nbt.String(e.AssetID))
	}
	if e.CameraOverlay != "" {
		out.Set("camera_overlay", nbt.String(e.CameraOverlay))
	}
	if set := e.AllowedEntities; set.Present {
		out.Set("allowed_entities", entitySetTag(set))
	}
	for _, flag := range []struct {
		key      string
		set, def bool
	}{
		{"dispensable", e.Dispensable, true},
		{"swappable", e.Swappable, true},
		{"damage_on_hurt", e.DamageOnHurt, true},
		{"equip_on_interact", e.EquipOnInteract, false},
		{"can_be_sheared", e.CanBeSheared, false},
	} {
		if flag.set != flag.def {
			out.Set(flag.key, nbt.Byte(boolByte(flag.set)))
		}
	}
	putSoundTag(out, "shearing_sound", e.ShearingSound, "minecraft:item.shears.snip")
	return out
}

// putSoundTag writes a sound holder under key unless it is the registered
// sound def, the codec's default.
func putSoundTag(out nbt.CompoundTag, key string, h protocol.SoundHolder, def string) {
	if id, ok := SoundEventID(def); ok && h == (protocol.SoundHolder{ID: id}) {
		return
	}
	if tag, ok := soundHolderTag(h); ok {
		out.Set(key, tag)
	}
}

// entitySetOf reads allowed_entities: a "#tag", an entity type name or a
// list of names.
// vanilla: RegistryCodecs.homogeneousList, ByteBufCodecs.holderSet
func entitySetOf(value nbt.Tag) (protocol.EntityTypeSet, bool) {
	names := []nbt.Tag{value}
	if list, ok := value.(nbt.List); ok {
		names = list.Items
	}
	set := protocol.EntityTypeSet{Present: true}
	if s, ok := value.(nbt.String); ok && len(s) > 0 && s[0] == '#' {
		set.Tag = Qualify(string(s[1:]))
		_, known := EntityTypeTagMembers(set.Tag)
		return set, known
	}
	set.IDs = make([]int32, 0, len(names))
	for _, n := range names {
		s, ok := n.(nbt.String)
		if !ok {
			return set, false
		}
		id, ok := EntityTypeID(Qualify(string(s)))
		if !ok {
			return set, false
		}
		set.IDs = append(set.IDs, id)
	}
	return set, true
}

// entitySetTag writes a set in the shape entitySetOf reads.
func entitySetTag(set protocol.EntityTypeSet) nbt.Tag {
	switch {
	case set.Tag != "":
		return nbt.String("#" + set.Tag)
	case len(set.IDs) == 1:
		// vanilla: ExtraCodecs.compactListCodec
		return nbt.String(EntityTypes[set.IDs[0]])
	}
	names := nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{}}
	for _, id := range set.IDs {
		names.Items = append(names.Items, nbt.String(EntityTypes[id]))
	}
	return names
}
