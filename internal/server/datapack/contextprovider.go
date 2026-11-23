package datapack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The context int and float provider codec. A decoder answers the codec's
// error message, empty on success; the message text is what /compute shows.

func init() {
	registry.DecodeContextProvider = func(data []byte, float bool) (registry.LootNumber, string) {
		tag, err := JSONTag(data)
		if err != nil {
			return nil, err.Error()
		}
		if float {
			return DecodeFloatProvider(tag)
		}
		return DecodeIntProvider(tag)
	}
}

// eitherNumberOr wraps the typed branch's error of Codec.either(number, typed).
func eitherNumberOr(msg string) string {
	return "Failed to parse either. First: Not a number; Second: " + msg
}

// DecodeIntProvider reads a context int provider: a bare number or a
// compound with a type.
// vanilla: ContextIntProviders.DIRECT_CODEC
func DecodeIntProvider(tag nbt.Tag) (registry.LootNumber, string) {
	if v, ok := nbt.IntValue(tag); ok {
		return registry.LootConstant{Value: float64(v)}, ""
	}
	p, msg := decodeTyped(tag, "context_int_provider_type", intTypes)
	if msg != "" {
		return nil, eitherNumberOr(msg)
	}
	return p, ""
}

// DecodeFloatProvider reads a context float provider.
// vanilla: ContextFloatProviders.DIRECT_CODEC
func DecodeFloatProvider(tag nbt.Tag) (registry.LootNumber, string) {
	if v, ok := tagFloat(tag); ok {
		return registry.LootConstant{Value: float64(v)}, ""
	}
	p, msg := decodeTyped(tag, "context_float_provider_type", floatTypes)
	if msg != "" {
		return nil, eitherNumberOr(msg)
	}
	return p, ""
}

// ProviderRef is the context_int_provider (or, when float,
// context_float_provider) entry the name gives. The entry is decoded when
// evaluated, so a pack's entries may refer to each other in any order.
// vanilla: ContextIntProviders.CODEC, ContextFloatProviders.CODEC
func ProviderRef(name string, float bool) (registry.LootNumber, string) {
	key := registry.Qualify(name)
	lookup := registry.ContextIntProviderJSON
	if float {
		lookup = registry.ContextFloatProviderJSON
	}
	if _, ok := lookup(key); !ok {
		return nil, "Unknown element: " + name
	}
	return registry.LootProviderRef{Name: key, Float: float}, ""
}

// decodeCondition reads a condition holder: an identifier names a pack
// predicate, a compound is an inline condition. A name is resolved when
// evaluated, since pack predicates are applied after the loot tables decode.
// vanilla: LootItemCondition.CODEC
func decodeCondition(tag nbt.Tag) (registry.LootCondition, string) {
	if s, ok := tag.(nbt.String); ok {
		return registry.LootReferenceCondition{Name: registry.Qualify(string(s))}, ""
	}
	data, err := json.Marshal(TagJSON(tag))
	if err != nil {
		return nil, err.Error()
	}
	cond, err := DecodePredicate(data)
	if err != nil {
		return nil, err.Error()
	}
	return cond, ""
}

// conditionalOf builds conditional and number_dispatcher over one nested
// decoder. An absent on_false or default is the constant zero.
// vanilla: ConditionalProvider.mapCodec, DispatcherProvider.mapCodec
func conditionalOf(nested func(nbt.Tag) (registry.LootNumber, string)) (conditional, dispatcher func(nbt.Compound) (registry.LootNumber, string)) {
	optional := func(c nbt.Compound, key string) (registry.LootNumber, string) {
		if t, ok := c[key]; ok {
			return nested(t)
		}
		return nil, ""
	}
	condField := func(c nbt.Compound) (registry.LootCondition, string) {
		t, msg := fieldOf(c, "condition")
		if msg != "" {
			return nil, msg
		}
		return decodeCondition(t)
	}
	required := func(c nbt.Compound, key string) (registry.LootNumber, string) {
		t, msg := fieldOf(c, key)
		if msg != "" {
			return nil, msg
		}
		return nested(t)
	}
	conditional = func(c nbt.Compound) (registry.LootNumber, string) {
		test, msg := condField(c)
		if msg != "" {
			return nil, msg
		}
		onTrue, msg := required(c, "on_true")
		if msg != "" {
			return nil, msg
		}
		onFalse, msg := optional(c, "on_false")
		if msg != "" {
			return nil, msg
		}
		return registry.LootConditional{Condition: test, OnTrue: onTrue, OnFalse: onFalse}, ""
	}
	dispatcher = func(c nbt.Compound) (registry.LootNumber, string) {
		casesTag, msg := fieldOf(c, "cases")
		if msg != "" {
			return nil, msg
		}
		items, ok := listItems(casesTag)
		if !ok {
			return nil, "Not a list: " + nbt.Stringify(casesTag)
		}
		var cases []registry.LootDispatchCase
		for _, e := range items {
			cc, ok := nbt.ListElement(e).(nbt.Compound)
			if !ok {
				return nil, "Not a map: " + nbt.Stringify(e)
			}
			test, msg := condField(cc)
			if msg != "" {
				return nil, msg
			}
			v, msg := required(cc, "value")
			if msg != "" {
				return nil, msg
			}
			cases = append(cases, registry.LootDispatchCase{Condition: test, Value: v})
		}
		def, msg := optional(c, "default")
		if msg != "" {
			return nil, msg
		}
		return registry.LootDispatcher{Cases: cases, Default: def}, ""
	}
	return conditional, dispatcher
}

type providerTypes map[string]func(nbt.Compound) (registry.LootNumber, string)

func decodeTyped(tag nbt.Tag, registryName string, types providerTypes) (registry.LootNumber, string) {
	c, ok := tag.(nbt.Compound)
	if !ok {
		return nil, "Not a map: " + nbt.Stringify(tag)
	}
	typeTag, ok := c["type"]
	if !ok {
		return nil, "No key type in " + mapLike(c)
	}
	s, ok := typeTag.(nbt.String)
	if !ok {
		return nil, "Not a string: " + nbt.Stringify(typeTag)
	}
	name := registry.Qualify(string(s))
	if decode, ok := types[strings.TrimPrefix(name, "minecraft:")]; ok && strings.HasPrefix(name, "minecraft:") {
		return decode(c)
	}
	return nil, "Unknown registry key in ResourceKey[minecraft:root / minecraft:" + registryName + "]: " + name
}

// mapLike renders a compound as the codec's MapLike message does.
func mapLike(c nbt.Compound) string { return "MapLike[" + nbt.Stringify(c) + "]" }

// fieldOf reads a required field.
func fieldOf(c nbt.Compound, key string) (nbt.Tag, string) {
	t, ok := c[key]
	if !ok {
		return nil, "No key " + key + " in " + mapLike(c)
	}
	return t, ""
}

// missingKeys reports every absent required key, in the order given (the
// order the record codec lists them in).
func missingKeys(c nbt.Compound, keys ...string) string {
	var msgs []string
	for _, k := range keys {
		if _, ok := c[k]; !ok {
			msgs = append(msgs, "No key "+k+" in "+mapLike(c))
		}
	}
	return strings.Join(msgs, "; ")
}

// nestedInt reads a holder field: an identifier names a registry entry,
// anything else is an inline provider.
func nestedInt(c nbt.Compound, key string) (registry.LootNumber, string) {
	t, msg := fieldOf(c, key)
	if msg != "" {
		return nil, msg
	}
	return nestedIntTag(t)
}

func nestedIntTag(t nbt.Tag) (registry.LootNumber, string) {
	if s, ok := t.(nbt.String); ok {
		return ProviderRef(string(s), false)
	}
	return DecodeIntProvider(t)
}

func nestedFloat(c nbt.Compound, key string) (registry.LootNumber, string) {
	t, msg := fieldOf(c, key)
	if msg != "" {
		return nil, msg
	}
	return nestedFloatTag(t)
}

func nestedFloatTag(t nbt.Tag) (registry.LootNumber, string) {
	if s, ok := t.(nbt.String); ok {
		return ProviderRef(string(s), true)
	}
	return DecodeFloatProvider(t)
}

// listItems is the elements of a list, or of an int array: a JSON list of
// ints converts to one.
// vanilla: NbtOps.getStream
func listItems(t nbt.Tag) ([]nbt.Tag, bool) {
	switch t := t.(type) {
	case nbt.List:
		return t.Items, true
	case nbt.IntArray:
		items := make([]nbt.Tag, len(t))
		for i, v := range t {
			items[i] = nbt.Int(v)
		}
		return items, true
	}
	return nil, false
}

// listOf decodes the elements of a list field with one decoder.
func listOf[T any](c nbt.Compound, key string, one func(nbt.Tag) (T, string)) ([]T, string) {
	t, msg := fieldOf(c, key)
	if msg != "" {
		return nil, msg
	}
	items, ok := listItems(t)
	if !ok {
		return nil, "Not a list: " + nbt.Stringify(t)
	}
	out := make([]T, 0, len(items))
	for _, item := range items {
		v, msg := one(nbt.ListElement(item))
		if msg != "" {
			return nil, msg
		}
		out = append(out, v)
	}
	return out, ""
}

// tagFloat reads a number the way Codec.FLOAT does.
func tagFloat(tag nbt.Tag) (float32, bool) {
	if v, ok := nbt.NumberValue(tag); ok {
		return float32(v), true
	}
	return 0, false
}

func unary(nested func(nbt.Compound, string) (registry.LootNumber, string), op string) func(nbt.Compound) (registry.LootNumber, string) {
	return func(c nbt.Compound) (registry.LootNumber, string) {
		in, msg := nested(c, "input")
		if msg != "" {
			return nil, msg
		}
		return registry.LootUnary{Op: op, Input: in}, ""
	}
}

func binary(nested func(nbt.Compound, string) (registry.LootNumber, string), op, leftKey, rightKey string) func(nbt.Compound) (registry.LootNumber, string) {
	return func(c nbt.Compound) (registry.LootNumber, string) {
		l, msg := nested(c, leftKey)
		if msg != "" {
			return nil, msg
		}
		r, msg := nested(c, rightKey)
		if msg != "" {
			return nil, msg
		}
		return registry.LootBinary{Op: op, Left: l, Right: r}, ""
	}
}

func aggregate(nested func(nbt.Tag) (registry.LootNumber, string), op string) func(nbt.Compound) (registry.LootNumber, string) {
	return func(c nbt.Compound) (registry.LootNumber, string) {
		ins, msg := listOf(c, "inputs", nested)
		if msg != "" {
			return nil, msg
		}
		if op == "add" {
			return registry.LootSum{Summands: ins}, ""
		}
		return registry.LootAggregate{Op: op, Inputs: ins}, ""
	}
}

// weightedChoice reads a distribution list of data and weight entries.
// vanilla: WeightedList.nonEmptyCodec
func weightedChoice(nested func(nbt.Tag) (registry.LootNumber, string)) func(nbt.Compound) (registry.LootNumber, string) {
	return func(c nbt.Compound) (registry.LootNumber, string) {
		t, msg := fieldOf(c, "distribution")
		if msg != "" {
			return nil, msg
		}
		items, ok := listItems(t)
		if !ok {
			return nil, "Not a list: " + nbt.Stringify(t)
		}
		if len(items) == 0 {
			return nil, "List must have contents"
		}
		var out registry.LootWeightedList
		for _, item := range items {
			entry, ok := nbt.ListElement(item).(nbt.Compound)
			if !ok {
				return nil, "Not a map: " + nbt.Stringify(item)
			}
			data, msg := fieldOf(entry, "data")
			if msg != "" {
				return nil, msg
			}
			v, msg := nested(data)
			if msg != "" {
				return nil, msg
			}
			weight := int32(1)
			if w, present := entry["weight"]; present {
				if weight, ok = nbt.IntValue(w); !ok {
					return nil, "Not a number: " + nbt.Stringify(w)
				}
				if weight < 1 {
					return nil, fmt.Sprintf("Value %d outside of range [1:2147483647]", weight)
				}
			}
			out.Values = append(out.Values, v)
			out.Weights = append(out.Weights, int64(weight))
		}
		return out, ""
	}
}

var entityTargetNames = []string{"this", "attacker", "direct_attacker", "attacking_player", "target_entity", "interacting_entity"}

// scoreHolder reads a scoreboard name provider into the score node: an
// entity target name, or a compound of type context or fixed.
// vanilla: ScoreboardNameProviders.CODEC
func scoreHolder(tag nbt.Tag, out *registry.LootScore) string {
	targetOf := func(t nbt.Tag) string {
		s, ok := t.(nbt.String)
		if !ok {
			return "Not a string: " + nbt.Stringify(t)
		}
		if !slices.Contains(entityTargetNames, string(s)) {
			return "Invalid entity target " + string(s)
		}
		out.Target = string(s)
		return ""
	}
	c, ok := tag.(nbt.Compound)
	if !ok {
		return targetOf(tag)
	}
	typeTag, msg := fieldOf(c, "type")
	if msg != "" {
		return msg
	}
	typeName, _ := typeTag.(nbt.String)
	switch registry.Qualify(string(typeName)) {
	case "minecraft:context":
		t, msg := fieldOf(c, "target")
		if msg != "" {
			return msg
		}
		return targetOf(t)
	case "minecraft:fixed":
		t, msg := fieldOf(c, "name")
		if msg != "" {
			return msg
		}
		s, ok := t.(nbt.String)
		if !ok {
			return "Not a string: " + nbt.Stringify(t)
		}
		out.Target, out.Fixed = string(s), true
		return ""
	}
	return "Unknown registry key in ResourceKey[minecraft:root / minecraft:loot_score_provider_type]: " + registry.Qualify(string(typeName))
}

// identifierField reads a resource location field.
func identifierField(c nbt.Compound, key string) (string, string) {
	t, msg := fieldOf(c, key)
	if msg != "" {
		return "", msg
	}
	s, ok := t.(nbt.String)
	if !ok {
		return "", "Not a string: " + nbt.Stringify(t)
	}
	if !protocol.ValidIdentifier(string(s)) {
		return "", "Not a valid resource location: " + string(s)
	}
	return registry.Qualify(string(s)), ""
}

// fallbackOf reads a provider's optional fallback; absent is nil, zero.
func fallbackOf(c nbt.Compound, nested func(nbt.Tag) (registry.LootNumber, string)) (registry.LootNumber, string) {
	if f, present := c["fallback"]; present {
		return nested(f)
	}
	return nil, ""
}

// storage reads a storage provider: the storage, the path (kept as text and
// parsed when evaluated) and the fallback.
// vanilla: StoredNumberAccess
func storage(nested func(nbt.Tag) (registry.LootNumber, string)) func(nbt.Compound) (registry.LootNumber, string) {
	return func(c nbt.Compound) (registry.LootNumber, string) {
		id, msg := identifierField(c, "storage")
		if msg != "" {
			return nil, msg
		}
		pathTag, msg := fieldOf(c, "path")
		if msg != "" {
			return nil, msg
		}
		path, ok := pathTag.(nbt.String)
		if !ok {
			return nil, "Not a string: " + nbt.Stringify(pathTag)
		}
		fallback, msg := fallbackOf(c, nested)
		if msg != "" {
			return nil, msg
		}
		return registry.LootStorage{Storage: id, Path: string(path), Fallback: fallback}, ""
	}
}

// envAttributeField reads an environment_attribute provider's attribute: an
// identifier naming an entry of the environment attribute registry.
// vanilla: EnvironmentAttributeProvider.mapCodec, EnvironmentAttributes.CODEC
func envAttributeField(c nbt.Compound) (string, registry.EnvAttribute, string) {
	name, msg := identifierField(c, "attribute")
	if msg != "" {
		return "", registry.EnvAttribute{}, msg
	}
	a, ok := registry.EnvAttributeByName(name)
	if !ok {
		return "", registry.EnvAttribute{}, "Unknown registry key in ResourceKey[minecraft:root / minecraft:environment_attribute]: " + name
	}
	return name, a, ""
}

var intTypes, floatTypes providerTypes

func init() {
	intConditional, intDispatcher := conditionalOf(nestedIntTag)
	intTypes = providerTypes{
		// No attribute converts to an integer (tools/gen registrytables
		// stops if one does), so every attribute it names is refused.
		// vanilla: ints.EnvironmentAttributeValue
		"environment_attribute": func(c nbt.Compound) (registry.LootNumber, string) {
			name, _, msg := envAttributeField(c)
			if msg != "" {
				return nil, msg
			}
			return nil, name + " cannot be converted to an integer"
		},
		"conditional":       intConditional,
		"number_dispatcher": intDispatcher,
		"abs":               unary(nestedInt, "abs"),
		"negate":            unary(nestedInt, "negate"),
		"sub":               binary(nestedInt, "sub", "left", "right"),
		"mod":               binary(nestedInt, "mod", "left", "right"),
		"div":               binary(nestedInt, "div", "left", "right"),
		"floor_div":         binary(nestedInt, "floor_div", "left", "right"),
		"floor_mod":         binary(nestedInt, "floor_mod", "left", "right"),
		"pow":               binary(nestedInt, "pow", "base", "exponent"),
		"add":               aggregate(nestedIntTag, "add"),
		"mul":               aggregate(nestedIntTag, "mul"),
		"max":               aggregate(nestedIntTag, "max"),
		"min":               aggregate(nestedIntTag, "min"),
		"avg":               aggregate(nestedIntTag, "avg"),
		"constant": func(c nbt.Compound) (registry.LootNumber, string) {
			t, msg := fieldOf(c, "value")
			if msg != "" {
				return nil, msg
			}
			v, ok := nbt.IntValue(t)
			if !ok {
				return nil, "Not a number: " + nbt.Stringify(t)
			}
			return registry.LootConstant{Value: float64(v)}, ""
		},
		"uniform": func(c nbt.Compound) (registry.LootNumber, string) {
			if msg := missingKeys(c, "max", "min"); msg != "" {
				return nil, msg
			}
			lo, msg := nestedInt(c, "min")
			if msg != "" {
				return nil, msg
			}
			hi, msg := nestedInt(c, "max")
			if msg != "" {
				return nil, msg
			}
			return registry.LootUniform{Min: lo, Max: hi}, ""
		},
		"binomial": func(c nbt.Compound) (registry.LootNumber, string) {
			n, msg := nestedInt(c, "n")
			if msg != "" {
				return nil, msg
			}
			p, msg := nestedFloat(c, "p")
			if msg != "" {
				return nil, msg
			}
			return registry.LootBinomial{N: n, P: p}, ""
		},
		"from_float": func(c nbt.Compound) (registry.LootNumber, string) {
			in, msg := nestedFloat(c, "input")
			if msg != "" {
				return nil, msg
			}
			return registry.LootConvert{Input: in}, ""
		},
		"weighted_list": weightedChoice(nestedIntTag),
		"score": func(c nbt.Compound) (registry.LootNumber, string) {
			holderTag, msg := fieldOf(c, "target")
			if msg != "" {
				return nil, msg
			}
			var out registry.LootScore
			if msg := scoreHolder(holderTag, &out); msg != "" {
				return nil, msg
			}
			objTag, msg := fieldOf(c, "score")
			if msg != "" {
				return nil, msg
			}
			obj, ok := objTag.(nbt.String)
			if !ok {
				return nil, "Not a string: " + nbt.Stringify(objTag)
			}
			out.Objective = string(obj)
			if out.Fallback, msg = fallbackOf(c, nestedIntTag); msg != "" {
				return nil, msg
			}
			return out, ""
		},
		"storage": storage(nestedIntTag),
	}
}

func init() {
	floatConditional, floatDispatcher := conditionalOf(nestedFloatTag)
	floatTypes = providerTypes{
		// vanilla: EnchantmentLevelProvider
		"enchantment_level": func(c nbt.Compound) (registry.LootNumber, string) {
			t, msg := fieldOf(c, "amount")
			if msg != "" {
				return nil, msg
			}
			v, msg := decodeLevelValue(t)
			if msg != "" {
				return nil, msg
			}
			return registry.LootEnchantmentLevel{Amount: v}, ""
		},
		// vanilla: floats.EnvironmentAttributeValue
		"environment_attribute": func(c nbt.Compound) (registry.LootNumber, string) {
			name, a, msg := envAttributeField(c)
			if msg != "" {
				return nil, msg
			}
			if !a.Float {
				return nil, name + " cannot be converted to a float"
			}
			return registry.LootEnvAttribute{Attribute: name}, ""
		},
		"conditional":       floatConditional,
		"number_dispatcher": floatDispatcher,
		"abs":               unary(nestedFloat, "abs"),
		"negate":            unary(nestedFloat, "negate"),
		"ceil":              unary(nestedFloat, "ceil"),
		"floor":             unary(nestedFloat, "floor"),
		"round":             unary(nestedFloat, "round"),
		"truncate":          unary(nestedFloat, "truncate"),
		"sqrt":              unary(nestedFloat, "sqrt"),
		"sin":               unary(nestedFloat, "sin"),
		"cos":               unary(nestedFloat, "cos"),
		"sub":               binary(nestedFloat, "sub", "left", "right"),
		"div":               binary(nestedFloat, "div", "left", "right"),
		"mod":               binary(nestedFloat, "mod", "left", "right"),
		"pow":               binary(nestedFloat, "pow", "base", "exponent"),
		"add":               aggregate(nestedFloatTag, "add"),
		"mul":               aggregate(nestedFloatTag, "mul"),
		"max":               aggregate(nestedFloatTag, "max"),
		"min":               aggregate(nestedFloatTag, "min"),
		"avg":               aggregate(nestedFloatTag, "avg"),
		"length":            aggregate(nestedFloatTag, "length"),
		"constant": func(c nbt.Compound) (registry.LootNumber, string) {
			t, msg := fieldOf(c, "value")
			if msg != "" {
				return nil, msg
			}
			v, ok := tagFloat(t)
			if !ok {
				return nil, "Not a number: " + nbt.Stringify(t)
			}
			return registry.LootConstant{Value: float64(v)}, ""
		},
		"uniform": func(c nbt.Compound) (registry.LootNumber, string) {
			if msg := missingKeys(c, "max", "min"); msg != "" {
				return nil, msg
			}
			lo, msg := nestedFloat(c, "min")
			if msg != "" {
				return nil, msg
			}
			hi, msg := nestedFloat(c, "max")
			if msg != "" {
				return nil, msg
			}
			return registry.LootUniform{Min: lo, Max: hi}, ""
		},
		"from_int": func(c nbt.Compound) (registry.LootNumber, string) {
			in, msg := nestedInt(c, "input")
			if msg != "" {
				return nil, msg
			}
			return registry.LootConvert{Input: in}, ""
		},
		"weighted_list": weightedChoice(nestedFloatTag),
		"storage":       storage(nestedFloatTag),
	}
}

// JSONTag reads a data pack's JSON as the tag the provider decoders take.
func JSONTag(raw []byte) (nbt.Tag, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return protocol.JSONValueTag(v)
}

// TagJSON is the JSON value of an SNBT tag.
func TagJSON(tag nbt.Tag) any {
	switch v := tag.(type) {
	case nbt.String:
		return string(v)
	case nbt.Byte:
		return int8(v)
	case nbt.Short:
		return int16(v)
	case nbt.Int:
		return int32(v)
	case nbt.Long:
		return int64(v)
	case nbt.Float:
		return float32(v)
	case nbt.Double:
		return float64(v)
	case nbt.List:
		out := make([]any, len(v.Items))
		for i, item := range v.Items {
			out[i] = TagJSON(nbt.ListElement(item))
		}
		return out
	case nbt.Compound:
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[k] = TagJSON(item)
		}
		return out
	}
	return nil
}
