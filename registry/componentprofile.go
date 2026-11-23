package registry

import (
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// ProfileComponentMalformed validates bounded profile fields with the codec's error wording.
// vanilla: ResolvableProfile.CODEC, ExtraCodecs.STORED_GAME_PROFILE
func ProfileComponentMalformed(tag nbt.Tag) string {
	if name, isString := tag.(nbt.String); isString {
		if err := playerNameMalformed(string(name)); err != "" {
			return profileEitherMalformed("Not a map: "+nbt.Stringify(tag), err)
		}
		return ""
	}
	fields, isMap := nbt.CompoundView(tag)
	if !isMap {
		return profileEitherMalformed("Not a map: "+nbt.Stringify(tag), "Not a string")
	}
	name, hasName := fields["name"]
	nameErr := ""
	if hasName {
		nameErr = profileNameMalformed(name)
	}
	propertiesErr := profilePropertiesMalformed(fields["properties"])
	fieldErr := JoinFieldErrors(propertiesErr, nameErr)
	id, hasID := fields["id"]
	idErr := ""
	if hasID {
		idErr = profileUUIDMalformed(id)
	}
	missingName := ""
	if !hasName {
		missingName = "No key name in MapLike[" + nbt.Stringify(tag) + "]"
	}
	skinErr := profileSkinMalformed(fields)
	// The id's refusal replaces the name and property errors: vanilla's map
	// codec reports only it, before and after the missing name, while a bad
	// name or property repeats its own. The skin patch's refusal, read
	// first, precedes all of them.
	if idErr != "" {
		inner := idErr
		if missingName != "" {
			inner += "; " + missingName
		}
		return profileEitherMalformed(skinPrefix(skinErr)+inner+"; "+idErr, "Not a string")
	}
	if fieldErr == "" {
		return skinErr // game profile part decodes; only a skin refusal is left
	}
	inner := skinPrefix(skinErr) + fieldErr + "; " + fieldErr
	if missingName != "" {
		inner += "; " + missingName
	}
	if !hasID {
		inner += "; No key id in MapLike[" + nbt.Stringify(tag) + "]"
	}
	return profileEitherMalformed(inner, "Not a string")
}

// skinPrefix places skin errors before the game-profile errors.
func skinPrefix(skinErr string) string {
	if skinErr == "" {
		return ""
	}
	return skinErr + "; "
}

func profileEitherMalformed(inner, nameErr string) string {
	return "Failed to parse either. First: " + inner + "; Second: " + nameErr
}

// vanilla: ExtraCodecs.PLAYER_NAME
func profileNameMalformed(name nbt.Tag) string {
	s, isString := name.(nbt.String)
	if !isString {
		return "Not a string"
	}
	return playerNameMalformed(string(s))
}

// playerNameMalformed bounds UTF-16 length and printable ASCII characters.
// vanilla: ExtraCodecs.PLAYER_NAME, StringUtil.isValidPlayerName
func playerNameMalformed(name string) string {
	if err := tooLongStringMalformed(name, 16); err != "" {
		return err
	}
	if protocol.HasDisallowedNameChar(name) {
		return "Player name contained disallowed characters: '" + name + "'"
	}
	return ""
}

// vanilla: UUIDUtil.CODEC
func profileUUIDMalformed(id nbt.Tag) string {
	words, ok := protocol.UUIDWords(id)
	if !ok {
		return "Not a list"
	}
	if len(words) != 4 {
		return "Input is not a list of 4 ints"
	}
	return ""
}

// profileSkinMalformed checks fields back to front, matching the map codec's error order.
// vanilla: PlayerSkin$Patch.MAP_CODEC
func profileSkinMalformed(fields nbt.Compound) string {
	modelErr := ""
	if model, present := fields["model"]; present {
		modelErr = profileModelMalformed(model)
	}
	return JoinFieldErrors(
		modelErr,
		profileSkinLocationMalformed(fields["elytra"]),
		profileSkinLocationMalformed(fields["cape"]),
		profileSkinLocationMalformed(fields["texture"]),
	)
}

func profileSkinLocationMalformed(tag nbt.Tag) string {
	if tag == nil {
		return ""
	}
	return resourceLocationMalformed(tag)
}

// vanilla: PlayerModelType.CODEC
func profileModelMalformed(tag nbt.Tag) string {
	s, isString := tag.(nbt.String)
	if !isString {
		return "Not a string"
	}
	switch string(s) {
	case "slim", "wide":
		return ""
	}
	return "Unknown element name:" + string(s)
}

// profilePropertiesMalformed checks both property shapes and the wire codec's bounds.
// vanilla: ExtraCodecs.PROPERTY_MAP
func profilePropertiesMalformed(value nbt.Tag) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case nbt.CompoundTag:
		if err := profilePropertyMapMalformed(v.Entries()); err != "" {
			return profileEitherMalformed(err, "Not a list: "+nbt.Stringify(value))
		}
		return ""
	case nbt.List:
		if len(v.Items) > 16 {
			return "List is too long: " + strconv.Itoa(len(v.Items)) + ", expected range [0-16]"
		}
		for _, item := range v.Items {
			entry := nbt.ListElement(item)
			property, isMap := nbt.CompoundView(entry)
			if !isMap {
				return "Not a map: " + nbt.Stringify(entry)
			}
			if err := profilePropertyField(property, "name", 64); err != "" {
				return err
			}
			if err := profilePropertyField(property, "value", 32767); err != "" {
				return err
			}
			if err := profilePropertyField(property, "signature", 1024); err != "" {
				return err
			}
		}
		return ""
	default:
		// The codec reads either one property map or a list of them, so a
		// value that is neither names both shapes.
		text := nbt.Stringify(value)
		return profileEitherMalformed("Not a map: "+text, "Not a list: "+text)
	}
}

// vanilla: ExtraCodecs.PROPERTY_MAP
func profilePropertyMapMalformed(fields nbt.Compound) string {
	if len(fields) > 16 {
		return "Cannot have more than 16 properties, but was " + strconv.Itoa(len(fields))
	}
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		list, isList := fields[name].(nbt.List)
		if !isList {
			return "Not a list: " + nbt.Stringify(fields[name])
		}
		for _, item := range list.Items {
			if _, isString := nbt.ListElement(item).(nbt.String); !isString {
				return "Not a string"
			}
		}
	}
	return ""
}

func profilePropertyField(property nbt.Compound, key string, limit int) string {
	value, present := property[key]
	if !present {
		if key == "signature" {
			return ""
		}
		return "No key " + key + " in MapLike[" + nbt.Stringify(property) + "]"
	}
	s, isString := value.(nbt.String)
	if !isString {
		return "Not a string"
	}
	return tooLongStringMalformed(string(s), limit)
}

// tooLongStringMalformed bounds the UTF-16 length used by string codecs.
func tooLongStringMalformed(s string, limit int) string {
	n := len(utf16.Encode([]rune(s)))
	if n <= limit {
		return ""
	}
	return "String \"" + s + "\" is too long: " + strconv.Itoa(n) +
		", expected range [0-" + strconv.Itoa(limit) + "]"
}

// JoinFieldErrors preserves field order while omitting absent errors.
func JoinFieldErrors(errs ...string) string {
	var parts []string
	for _, err := range errs {
		if err != "" {
			parts = append(parts, err)
		}
	}
	return strings.Join(parts, "; ")
}
