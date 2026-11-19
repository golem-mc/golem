package protocol

import (
	"maps"
	"slices"

	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// ProfileToTag converts minecraft:profile wire bytes into a skull block
// entity's compound: "id" as an int array, "name", "properties", and the
// skin's "texture", "cape", "elytra" and "model". false for bytes the codec
// does not read.
// vanilla: ResolvableProfile.STREAM_CODEC, ExtraCodecs.STORED_GAME_PROFILE
func ProfileToTag(raw []byte) (nbt.Compound, bool) {
	tag := nbt.Compound{}
	if !writeProfileTag(raw, tag) {
		return nil, false
	}
	return tag, true
}

// OrderedProfileToTag retains the ResolvableProfile codec's compound insertion history.
func OrderedProfileToTag(raw []byte) (*nbt.OrderedCompound, bool) {
	tag := &nbt.OrderedCompound{}
	if !writeProfileTag(raw, tag) {
		return nil, false
	}
	return tag, true
}

// vanilla: ResolvableProfile.CODEC, ExtraCodecs.STORED_GAME_PROFILE, PlayerSkin.Patch.MAP_CODEC
func writeProfileTag(raw []byte, tag nbt.CompoundTag) bool {
	r := NewReader(raw)
	var id uuid.UUID
	var name string
	hasID, hasName := false, false
	complete := r.ReadBool()
	if complete {
		id = r.ReadUUID()
		name = r.ReadString(16)
		hasID, hasName = true, true
	} else {
		if r.ReadBool() {
			name = r.ReadString(16)
			hasName = true
		}
		if r.ReadBool() {
			id = r.ReadUUID()
			hasID = true
		}
	}
	count := r.ReadVarInt()
	if r.Err() != nil || count < 0 || count > 16 {
		return false
	}
	properties := make([]nbt.Tag, 0, count)
	for range count {
		var property nbt.CompoundTag = nbt.Compound{}
		if _, ordered := tag.(*nbt.OrderedCompound); ordered {
			property = &nbt.OrderedCompound{}
		}
		property.Set("name", nbt.String(r.ReadString(64)))
		property.Set("value", nbt.String(r.ReadString(32767)))
		if r.ReadBool() {
			property.Set("signature", nbt.String(r.ReadString(1024)))
		}
		if r.Err() != nil {
			return false
		}
		properties = append(properties, property)
	}
	skin, ok := skinPatchValue(r)
	if !ok {
		return false
	}
	// Stored GameProfile writes id before name; Partial writes name before id.
	if !complete && hasName {
		tag.Set("name", nbt.String(name))
	}
	if hasID {
		tag.Set("id", nbt.UUIDToIntArray(id))
	}
	if complete {
		tag.Set("name", nbt.String(name))
	}
	if len(properties) > 0 {
		tag.Set("properties", nbt.List{Elem: nbt.TypeCompound, Items: properties})
	}
	if skin.Body != "" {
		tag.Set("texture", nbt.String(skin.Body))
	}
	if skin.Cape != "" {
		tag.Set("cape", nbt.String(skin.Cape))
	}
	if skin.Elytra != "" {
		tag.Set("elytra", nbt.String(skin.Elytra))
	}
	if skin.Model != "" {
		tag.Set("model", nbt.String(skin.Model))
	}
	return true
}

// ProfileFromTag is ProfileToTag's inverse: complete shape with both id and
// name, partial otherwise. A property without name or value is dropped;
// anything past the wire codec's limits refuses the whole, since the client
// would drop the packet carrying it.
// vanilla: ResolvableProfile.STREAM_CODEC
func ProfileFromTag(tag nbt.Compound) ([]byte, bool) {
	var id uuid.UUID
	hasID := false
	if words, ok := UUIDWords(tag["id"]); ok {
		id, hasID = nbt.UUIDFromIntArray(words)
	}
	name, hasName := nbt.Get[nbt.String](tag.Entries(), "name")
	if hasName && (utf16Len(string(name)) > 16 || HasDisallowedNameChar(string(name))) {
		return nil, false
	}

	type property struct {
		name, value string
		signature   string
		hasSig      bool
	}
	var properties []property
	switch props := tag["properties"].(type) {
	case nbt.List:
		if len(props.Items) > 16 {
			return nil, false
		}
		for _, item := range props.Items {
			compound, ok := nbt.CompoundView(item)
			if !ok {
				continue
			}
			propertyName, nameOK := nbt.Get[nbt.String](compound, "name")
			value, valueOK := nbt.Get[nbt.String](compound, "value")
			if !nameOK || !valueOK {
				continue
			}
			property := property{name: string(propertyName), value: string(value)}
			if utf16Len(property.name) > 64 || utf16Len(property.value) > 32767 {
				return nil, false
			}
			if signature, ok := nbt.Get[nbt.String](compound, "signature"); ok {
				if utf16Len(string(signature)) > 1024 {
					return nil, false
				}
				property.signature, property.hasSig = string(signature), true
			}
			properties = append(properties, property)
		}
	case nbt.CompoundTag:
		// The map shape groups a property's values under its name.
		if len(props.Entries()) > 16 {
			return nil, false
		}
		for _, name := range slices.Sorted(maps.Keys(props.Entries())) {
			list, ok := props.Entries()[name].(nbt.List)
			if !ok {
				return nil, false
			}
			for _, item := range list.Items {
				value, ok := item.(nbt.String)
				if !ok {
					return nil, false
				}
				property := property{name: name, value: string(value)}
				if utf16Len(property.name) > 64 || utf16Len(property.value) > 32767 {
					return nil, false
				}
				properties = append(properties, property)
			}
		}
		if len(properties) > 16 {
			return nil, false
		}
	}

	var skin SkinPatch
	for _, texture := range []struct {
		key string
		dst *string
	}{
		{"texture", &skin.Body},
		{"cape", &skin.Cape},
		{"elytra", &skin.Elytra},
	} {
		value, ok := nbt.Get[nbt.String](tag, texture.key)
		if !ok {
			continue
		}
		if !ValidIdentifier(string(value)) {
			return nil, false
		}
		*texture.dst = string(value)
	}
	if value, ok := nbt.Get[nbt.String](tag, "model"); ok {
		switch string(value) {
		case "slim", "wide":
			skin.Model = string(value)
		default:
			return nil, false
		}
	}

	var b Buffer
	if hasID && hasName {
		b.PutBool(true)
		b.PutUUID(id)
		b.PutString(string(name))
	} else {
		b.PutBool(false)
		b.PutBool(hasName)
		if hasName {
			b.PutString(string(name))
		}
		b.PutBool(hasID)
		if hasID {
			b.PutUUID(id)
		}
	}
	b.PutVarInt(int32(len(properties)))
	for _, property := range properties {
		b.PutString(property.name)
		b.PutString(property.value)
		b.PutBool(property.hasSig)
		if property.hasSig {
			b.PutString(property.signature)
		}
	}
	for _, texture := range []string{skin.Body, skin.Cape, skin.Elytra} {
		b.PutBool(texture != "")
		if texture != "" {
			b.PutString(texture)
		}
	}
	b.PutBool(skin.Model != "")
	if skin.Model != "" {
		b.PutBool(skin.Model == "slim")
	}
	return b.Bytes(), true
}

// UUIDWords is nbt.UUIDWords.
func UUIDWords(tag nbt.Tag) ([]int32, bool) {
	return nbt.UUIDWords(tag)
}

// HasDisallowedNameChar reports whether a player name holds a character the
// name codec allows none of: everything outside printable ASCII.
// vanilla: StringUtil.isValidPlayerName
func HasDisallowedNameChar(name string) bool {
	for _, c := range name {
		if c <= 32 || c >= 127 {
			return true
		}
	}
	return false
}

// ProfileTag is the stack's minecraft:profile component as the compound
// OrderedProfileToTag writes, reporting false for a stack without one or whose
// value does not read.
func (s ItemStack) ProfileTag() (*nbt.OrderedCompound, bool) {
	for _, raw := range s.Raw {
		if raw.Type == DataComponentProfile {
			return OrderedProfileToTag(raw.Data)
		}
	}
	return nil, false
}

// ProfileName is the player name the stack's minecraft:profile component
// carries, which a named player head's display name is built from. ok is
// false when the stack carries no profile or the profile names no player.
func (s ItemStack) ProfileName() (string, bool) {
	tag, ok := s.ProfileTag()
	if !ok {
		return "", false
	}
	name, ok := nbt.Get[nbt.String](tag.Entries(), "name")
	return string(name), ok
}

// SkipProfile reads one minecraft:profile value off r and reports whether it
// was whole.
func SkipProfile(r *Reader) bool {
	_, ok := profileValue(r)
	return ok
}
