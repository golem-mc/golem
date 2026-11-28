package datapack

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/golem-mc/golem/nbt"
)

// codecField is one key of a registry entry's codec: its check, and whether the
// codec lets the key be absent.
type codecField struct {
	check    func(nbt.Tag) error
	optional bool
}

func identifierCheck(t nbt.Tag) error {
	s, ok := t.(nbt.String)
	if !ok {
		return fmt.Errorf("not a string")
	}
	ns, path, found := strings.Cut(string(s), ":")
	if !found {
		ns, path = "minecraft", string(s)
	}
	// vanilla: Identifier.bySeparator, isValidNamespace (rejects "..")
	if ns == ".." || !validNamespace(ns) || !validPath(path) {
		return fmt.Errorf("not a valid resource location: %s", s)
	}
	return nil
}

// enumField accepts one of the listed strings, as a closed enum codec does.
func enumField(allowed ...string) func(nbt.Tag) error {
	return func(t nbt.Tag) error {
		s, ok := t.(nbt.String)
		if !ok {
			return fmt.Errorf("not a string")
		}
		if !slices.Contains(allowed, string(s)) {
			return fmt.Errorf("unknown value: %s", s)
		}
		return nil
	}
}

// textField accepts what ComponentSerialization.CODEC reads: a string, a
// list or a compound. Their contents are not checked.
func textField(t nbt.Tag) error {
	switch t.(type) {
	case nbt.String, nbt.List, nbt.Compound:
		return nil
	}
	return fmt.Errorf("not a text component")
}

func stringField(t nbt.Tag) error {
	if _, ok := t.(nbt.String); !ok {
		return fmt.Errorf("not a string")
	}
	return nil
}

func boolField(t nbt.Tag) error {
	// JSON true and false become bytes; the numeric forms are not accepted by
	// Codec.BOOL.
	if _, ok := t.(nbt.Byte); ok {
		return nil
	}
	return fmt.Errorf("not a boolean")
}

func compoundField(t nbt.Tag) error {
	if _, ok := t.(nbt.Compound); !ok {
		return fmt.Errorf("not a map")
	}
	return nil
}

// soundField accepts SoundEvent.CODEC's two forms: an id, or a compound with
// a sound_id.
func soundField(t nbt.Tag) error {
	if c, ok := t.(nbt.Compound); ok {
		id, ok := c["sound_id"]
		if !ok {
			return fmt.Errorf("missing sound_id")
		}
		return identifierCheck(id)
	}
	return identifierCheck(t)
}

func numberValue(t nbt.Tag) (float64, bool) {
	switch v := t.(type) {
	case nbt.Short:
		return float64(v), true
	case nbt.Int:
		return float64(v), true
	case nbt.Long:
		return float64(v), true
	case nbt.Float:
		return float64(v), true
	case nbt.Double:
		return float64(v), true
	}
	return 0, false
}

func intField(t nbt.Tag) (float64, error) {
	v, ok := numberValue(t)
	if !ok {
		return 0, fmt.Errorf("not a number")
	}
	if v != math.Trunc(v) {
		return 0, fmt.Errorf("not an integer: %v", v)
	}
	return v, nil
}

func rangedInt(lo, hi int) func(nbt.Tag) error {
	return func(t nbt.Tag) error {
		v, err := intField(t)
		if err != nil {
			return err
		}
		if v < float64(lo) || v > float64(hi) {
			return fmt.Errorf("value must be within range [%d;%d]: %v", lo, hi, v)
		}
		return nil
	}
}

func anyInt(t nbt.Tag) error {
	_, err := intField(t)
	return err
}

func floatAbove(lo float64, inclusive bool) func(nbt.Tag) error {
	return func(t nbt.Tag) error {
		v, ok := numberValue(t)
		if !ok {
			return fmt.Errorf("not a number")
		}
		if v < lo || v == lo && !inclusive {
			if inclusive {
				return fmt.Errorf("value must be non-negative: %v", v)
			}
			return fmt.Errorf("value must be positive: %v", v)
		}
		return nil
	}
}

func listField(item func(nbt.Tag) error) func(nbt.Tag) error {
	return func(t nbt.Tag) error {
		l, ok := t.(nbt.List)
		if !ok {
			return fmt.Errorf("not a list")
		}
		for i, e := range l.Items {
			if err := item(e); err != nil {
				return fmt.Errorf("[%d]: %w", i, err)
			}
		}
		return nil
	}
}

// selectors checks SpawnPrioritySelectors.CODEC's shape: a list of entries
// with an int priority and an optional condition map. The conditions'
// contents are not checked.
func selectors(t nbt.Tag) error {
	return listField(func(e nbt.Tag) error {
		return checkFields(e, map[string]codecField{
			"priority":  {check: anyInt},
			"condition": {check: compoundField, optional: true},
		})
	})(t)
}

func checkFields(t nbt.Tag, fields map[string]codecField) error {
	c, ok := t.(nbt.Compound)
	if !ok {
		return fmt.Errorf("not a map")
	}
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		f := fields[name]
		v, present := c[name]
		if !present {
			if f.optional {
				continue
			}
			return fmt.Errorf("%s: Missing field", name)
		}
		if err := f.check(v); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func decoration(t nbt.Tag) error {
	return checkFields(t, map[string]codecField{
		"translation_key": {check: stringField},
		"parameters":      {check: listField(enumField("sender", "target", "content"))},
		"style":           {check: compoundField, optional: true},
	})
}

func fieldsOf(pairs ...any) map[string]codecField {
	out := map[string]codecField{}
	for i := 0; i < len(pairs); i += 2 {
		switch v := pairs[i+1].(type) {
		case codecField:
			out[pairs[i].(string)] = v
		case func(nbt.Tag) error:
			out[pairs[i].(string)] = codecField{check: v}
		}
	}
	return out
}

func opt(check func(nbt.Tag) error) codecField { return codecField{check: check, optional: true} }

// modelVariant is a pig, cow or chicken variant: a ModelType name, an asset
// and the spawn rules.
func modelVariant(models ...string) map[string]codecField {
	return fieldsOf("asset_id", identifierCheck, "model", opt(enumField(models...)),
		"baby_asset_id", identifierCheck, "spawn_conditions", selectors)
}

// registryCodecs are the client-side codecs of the registries a data pack
// commonly fills, by registry name without the namespace. A registry absent
// here, and the contents of text components, sound events, spawn conditions
// and dialogs, are not checked beyond what is listed.
// vanilla: RegistryDataLoader.SYNCHRONIZED_REGISTRIES, each DIRECT_CODEC
var registryCodecs = map[string]map[string]codecField{
	"painting_variant": fieldsOf("width", rangedInt(1, 16), "height", rangedInt(1, 16),
		"asset_id", identifierCheck, "title", opt(textField), "author", opt(textField)),
	"jukebox_song": fieldsOf("sound_event", soundField, "description", textField,
		"length_in_seconds", floatAbove(0, false), "comparator_output", rangedInt(0, 15)),
	"instrument": fieldsOf("sound_event", soundField, "use_duration", floatAbove(0, true),
		"range", floatAbove(0, false), "durability_damage", opt(rangedInt(0, math.MaxInt32)),
		"description", textField),
	"trim_material": fieldsOf("palette_id", identifierCheck, "description", textField),
	"trim_pattern": fieldsOf("asset_id", identifierCheck, "description", textField,
		"decal", opt(boolField)),
	"banner_pattern": fieldsOf("asset_id", identifierCheck, "translation_key", stringField),
	"chat_type":      fieldsOf("chat", decoration, "narration", decoration),
	"wolf_variant": fieldsOf("assets", wolfAssets, "baby_assets", wolfAssets,
		"spawn_conditions", selectors),
	"cat_variant":             fieldsOf("asset_id", identifierCheck, "baby_asset_id", identifierCheck, "spawn_conditions", selectors),
	"frog_variant":            fieldsOf("asset_id", identifierCheck, "spawn_conditions", selectors),
	"pig_variant":             modelVariant("normal", "cold"),
	"cow_variant":             modelVariant("normal", "cold", "warm"),
	"chicken_variant":         modelVariant("normal", "cold"),
	"zombie_nautilus_variant": fieldsOf("asset_id", identifierCheck, "model", opt(enumField("normal", "warm")), "spawn_conditions", selectors),
	"dialog":                  fieldsOf("type", stringField),
}

func wolfAssets(t nbt.Tag) error {
	return checkFields(t, fieldsOf("wild", identifierCheck, "tame", identifierCheck, "angry", identifierCheck))
}

// ValidateRegistryEntry checks a pack entry of a synchronized registry
// (name as in registry.Synchronized) against the field types, required keys
// and ranges of the codec the client decodes it with. A registry it does not
// know passes. The error reads like the client's own.
// vanilla: RegistryDataLoader.load
func ValidateRegistryEntry(registryName string, tag nbt.Tag) error {
	fields, ok := registryCodecs[strings.TrimPrefix(registryName, "minecraft:")]
	if !ok {
		return nil
	}
	return checkFields(tag, fields)
}
