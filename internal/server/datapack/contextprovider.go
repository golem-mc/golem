package datapack

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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
		decode := DecodeIntProvider
		if float {
			decode = DecodeFloatProvider
		}
		n, msg, err := decode(tag)
		if err != nil {
			return nil, err.Error()
		}
		return n, msg
	}
}

// partialMark prefixes an error whose codec result has a partial value.
// Codec.either forwards that error unwrapped.
// vanilla: EitherCodec.decode, ListCodec.decode
const partialMark = "\x00"

// WeightOverflowMsg is the codec error of a weighted list whose total weight
// overflows an int. Vanilla throws it from the list's constructor, so it
// escapes every enclosing codec and fails the whole load or command parse.
// vanilla: WeightedRandom.getTotalWeight
const WeightOverflowMsg = "Sum of weights must be <= 2147483647"

// ErrWeightOverflow is WeightOverflowMsg as an error.
var ErrWeightOverflow = errors.New(WeightOverflowMsg) //nolint:staticcheck // vanilla's wording

// eitherNumberOr wraps the typed branch's error of Codec.either(number, typed).
func eitherNumberOr(msg string) string {
	if rest, ok := strings.CutPrefix(msg, partialMark); ok {
		return rest
	}
	return "Failed to parse either. First: Not a number; Second: " + msg
}

// DecodeIntProvider reads a context int provider: a bare number or a
// compound with a type. The error is ErrWeightOverflow, which no codec
// message carries; the message is the codec's refusal otherwise.
// vanilla: ContextIntProviders.DIRECT_CODEC
func DecodeIntProvider(tag nbt.Tag) (registry.LootNumber, string, error) {
	return lootDecoder{}.decodeInt(tag)
}

func (d lootDecoder) decodeInt(tag nbt.Tag) (registry.LootNumber, string, error) {
	return decodeProvider(tag, func(tag nbt.Tag) (registry.LootNumber, string) {
		if v, ok := nbt.IntValue(tag); ok {
			return registry.LootConstant{Value: float64(v)}, ""
		}
		return d.decodeTyped(tag, "context_int_provider_type", intTypes)
	})
}

// DecodeFloatProvider reads a context float provider.
// vanilla: ContextFloatProviders.DIRECT_CODEC
func DecodeFloatProvider(tag nbt.Tag) (registry.LootNumber, string, error) {
	return lootDecoder{}.decodeFloat(tag)
}

func (d lootDecoder) decodeFloat(tag nbt.Tag) (registry.LootNumber, string, error) {
	return decodeProvider(tag, func(tag nbt.Tag) (registry.LootNumber, string) {
		if v, ok := tagFloat(tag); ok {
			return registry.LootConstant{Value: float64(v)}, ""
		}
		return d.decodeTyped(tag, "context_float_provider_type", floatTypes)
	})
}

// decodeProvider runs decode, whose typed branch's error is wrapped as
// Codec.either's. A weight overflow panics out of the nested decoders, as
// vanilla's constructor throws, and ends here as an error.
func decodeProvider(tag nbt.Tag, decode func(nbt.Tag) (registry.LootNumber, string)) (n registry.LootNumber, msg string, err error) {
	defer recoverLoot(&err)
	if n, msg = decode(tag); msg != "" {
		return nil, eitherNumberOr(msg), nil
	}
	return n, "", nil
}

// ProviderRef is the context_int_provider (or, when float,
// context_float_provider) entry the name gives. The entry is decoded when
// evaluated, so a pack's entries may refer to each other in any order.
// vanilla: ContextIntProviders.CODEC, ContextFloatProviders.CODEC
func ProviderRef(name string, float bool) (registry.LootNumber, string) {
	return lootDecoder{}.providerRef(name, float)
}

func (d lootDecoder) providerRef(name string, float bool) (registry.LootNumber, string) {
	key := registry.Qualify(name)
	if !d.layers.HasContextProvider(key, float) {
		return nil, "Unknown element: " + name
	}
	return registry.LootProviderRef{Name: key, Float: float}, ""
}

// decodeCondition reads a condition holder: an identifier names a pack
// predicate, a compound is an inline condition. A name is resolved when
// evaluated, since pack predicates are applied after the loot tables decode.
// vanilla: LootItemCondition.CODEC
func (d lootDecoder) decodeCondition(tag nbt.Tag) (registry.LootCondition, string) {
	if s, ok := tag.(nbt.String); ok {
		return registry.LootReferenceCondition{Name: registry.Qualify(string(s))}, ""
	}
	data, err := json.Marshal(TagJSON(tag))
	if err != nil {
		return nil, err.Error()
	}
	cond, err := DecodePredicate(d.layers, data)
	if errors.Is(err, ErrWeightOverflow) {
		panic(lootFailure{msg: WeightOverflowMsg, overflow: true})
	}
	if err != nil {
		return nil, err.Error()
	}
	return cond, ""
}

// conditionalOf builds conditional and number_dispatcher over one nested
// decoder. An absent on_false or default is the constant zero.
// vanilla: ConditionalProvider.mapCodec, DispatcherProvider.mapCodec
func conditionalOf(nested func(lootDecoder, nbt.Tag) (registry.LootNumber, string)) (conditional, dispatcher providerDecoder) {
	optional := func(d lootDecoder, c nbt.Compound, key string) (registry.LootNumber, string) {
		if t, ok := c[key]; ok {
			return nested(d, t)
		}
		return nil, ""
	}
	condField := func(d lootDecoder, c nbt.Compound) (registry.LootCondition, string) {
		t, msg := fieldOf(c, "condition")
		if msg != "" {
			return nil, msg
		}
		return d.decodeCondition(t)
	}
	required := func(d lootDecoder, c nbt.Compound, key string) (registry.LootNumber, string) {
		t, msg := fieldOf(c, key)
		if msg != "" {
			return nil, msg
		}
		return nested(d, t)
	}
	conditional = func(d lootDecoder, c nbt.Compound) (registry.LootNumber, string) {
		test, testMsg := condField(d, c)
		onTrue, onTrueMsg := required(d, c, "on_true")
		onFalse, onFalseMsg := optional(d, c, "on_false")
		if msg := registry.JoinFieldErrors(onFalseMsg, onTrueMsg, testMsg); msg != "" {
			return nil, msg
		}
		return registry.LootConditional{Condition: test, OnTrue: onTrue, OnFalse: onFalse}, ""
	}
	decodeCases := func(d lootDecoder, c nbt.Compound) ([]registry.LootDispatchCase, string) {
		casesTag, msg := fieldOf(c, "cases")
		if msg != "" {
			return nil, msg
		}
		items, ok := listItems(casesTag)
		if !ok {
			return nil, "Not a list: " + nbt.Stringify(casesTag)
		}
		var cases []registry.LootDispatchCase
		var caseMsgs []string
		for _, e := range items {
			cc, ok := nbt.ListElement(e).(nbt.Compound)
			if !ok {
				caseMsgs = append(caseMsgs, "Not a map: "+nbt.Stringify(e))
				continue
			}
			test, testMsg := condField(d, cc)
			v, valueMsg := required(d, cc, "value")
			if msg := registry.JoinFieldErrors(valueMsg, testMsg); msg != "" {
				caseMsgs = append(caseMsgs, msg)
				continue
			}
			cases = append(cases, registry.LootDispatchCase{Condition: test, Value: v})
		}
		// vanilla: ListCodec.decode keeps going and merges every element's error,
		// DataResult.apply2stable prepending the newest, so cases report last first.
		slices.Reverse(caseMsgs)
		if msg := registry.JoinFieldErrors(caseMsgs...); msg != "" {
			return nil, partialMark + msg
		}
		return cases, ""
	}
	dispatcher = func(d lootDecoder, c nbt.Compound) (registry.LootNumber, string) {
		cases, casesMsg := decodeCases(d, c)
		def, defaultMsg := optional(d, c, "default")
		casesErr, partial := strings.CutPrefix(casesMsg, partialMark)
		if msg := registry.JoinFieldErrors(defaultMsg, casesErr); msg != "" {
			// vanilla: OptionalFieldCodec.decode supplies a partial default on error,
			// so only missing or non-list cases leave the record without a value.
			if casesErr == "" || partial {
				return nil, partialMark + msg
			}
			return nil, msg
		}
		return registry.LootDispatcher{Cases: cases, Default: def}, ""
	}
	return conditional, dispatcher
}

// providerDecoder reads one provider type's compound.
type providerDecoder = func(lootDecoder, nbt.Compound) (registry.LootNumber, string)

type providerTypes map[string]providerDecoder

func (d lootDecoder) decodeTyped(tag nbt.Tag, registryName string, types providerTypes) (registry.LootNumber, string) {
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
		return decode(d, c)
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

// nestedInt reads a holder field: an identifier names a registry entry,
// anything else is an inline provider.
func (d lootDecoder) nestedInt(c nbt.Compound, key string) (registry.LootNumber, string) {
	t, msg := fieldOf(c, key)
	if msg != "" {
		return nil, msg
	}
	return d.nestedIntTag(t)
}

func (d lootDecoder) nestedIntTag(t nbt.Tag) (registry.LootNumber, string) {
	if s, ok := t.(nbt.String); ok {
		return d.providerRef(string(s), false)
	}
	return decodeNested(t, d.decodeInt)
}

func (d lootDecoder) nestedFloat(c nbt.Compound, key string) (registry.LootNumber, string) {
	t, msg := fieldOf(c, key)
	if msg != "" {
		return nil, msg
	}
	return d.nestedFloatTag(t)
}

func (d lootDecoder) nestedFloatTag(t nbt.Tag) (registry.LootNumber, string) {
	if s, ok := t.(nbt.String); ok {
		return d.providerRef(string(s), true)
	}
	return decodeNested(t, d.decodeFloat)
}

// decodeNested reads an inline provider inside another, re-raising an
// overflow for the outermost entry point.
func decodeNested(t nbt.Tag, decode func(nbt.Tag) (registry.LootNumber, string, error)) (registry.LootNumber, string) {
	n, msg, err := decode(t)
	if err != nil {
		panic(lootFailure{msg: err.Error(), overflow: true})
	}
	return n, msg
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

func unary(nested func(lootDecoder, nbt.Compound, string) (registry.LootNumber, string), op string) providerDecoder {
	return func(d lootDecoder, c nbt.Compound) (registry.LootNumber, string) {
		in, msg := nested(d, c, "input")
		if msg != "" {
			return nil, msg
		}
		return registry.LootUnary{Op: op, Input: in}, ""
	}
}

func binary(nested func(lootDecoder, nbt.Compound, string) (registry.LootNumber, string), op, leftKey, rightKey string) providerDecoder {
	return func(d lootDecoder, c nbt.Compound) (registry.LootNumber, string) {
		l, lMsg := nested(d, c, leftKey)
		r, rMsg := nested(d, c, rightKey)
		if msg := registry.JoinFieldErrors(rMsg, lMsg); msg != "" {
			return nil, msg
		}
		return registry.LootBinary{Op: op, Left: l, Right: r}, ""
	}
}

func aggregate(nested func(lootDecoder, nbt.Tag) (registry.LootNumber, string), op string) providerDecoder {
	return func(d lootDecoder, c nbt.Compound) (registry.LootNumber, string) {
		ins, msg := listOf(c, "inputs", func(t nbt.Tag) (registry.LootNumber, string) { return nested(d, t) })
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
func weightedChoice(nested func(lootDecoder, nbt.Tag) (registry.LootNumber, string)) providerDecoder {
	return func(d lootDecoder, c nbt.Compound) (registry.LootNumber, string) {
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
		var total int64
		var firstMsg string
		for _, item := range items {
			v, weight, msg := weightedEntry(d, item, nested)
			if msg != "" {
				if firstMsg == "" {
					firstMsg = msg
				}
				continue
			}
			out.Values = append(out.Values, v)
			out.Weights = append(out.Weights, int64(weight))
			total += int64(weight)
		}
		// The constructor runs on the entries the list codec kept, so an
		// element's error does not hide the overflow.
		// vanilla: ListCodec.decode, WeightedList.entryToListCodec
		if total > math.MaxInt32 {
			panic(lootFailure{msg: WeightOverflowMsg, overflow: true})
		}
		if firstMsg != "" {
			return nil, firstMsg
		}
		return out, ""
	}
}

// weightedEntry reads one data and weight entry of a distribution list.
func weightedEntry(d lootDecoder, item nbt.Tag, nested func(lootDecoder, nbt.Tag) (registry.LootNumber, string)) (registry.LootNumber, int32, string) {
	entry, ok := nbt.ListElement(item).(nbt.Compound)
	if !ok {
		return nil, 0, "Not a map: " + nbt.Stringify(item)
	}
	data, msg := fieldOf(entry, "data")
	if msg != "" {
		return nil, 0, msg
	}
	v, msg := nested(d, data)
	if msg != "" {
		return nil, 0, msg
	}
	weight := int32(1)
	if w, present := entry["weight"]; present {
		if weight, ok = nbt.IntValue(w); !ok {
			return nil, 0, "Not a number: " + nbt.Stringify(w)
		}
		if weight < 1 {
			return nil, 0, fmt.Sprintf("Value %d outside of range [1:2147483647]", weight)
		}
	}
	return v, weight, ""
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
func fallbackOf(d lootDecoder, c nbt.Compound, nested func(lootDecoder, nbt.Tag) (registry.LootNumber, string)) (registry.LootNumber, string) {
	if f, present := c["fallback"]; present {
		return nested(d, f)
	}
	return nil, ""
}

// storage reads a storage provider: the storage, the path (kept as text and
// parsed when evaluated) and the fallback.
// vanilla: StoredNumberAccess
func storage(nested func(lootDecoder, nbt.Tag) (registry.LootNumber, string)) providerDecoder {
	return func(d lootDecoder, c nbt.Compound) (registry.LootNumber, string) {
		id, idMsg := identifierField(c, "storage")
		var path nbt.String
		pathTag, pathMsg := fieldOf(c, "path")
		if pathMsg == "" {
			var ok bool
			if path, ok = pathTag.(nbt.String); !ok {
				pathMsg = "Not a string: " + nbt.Stringify(pathTag)
			}
		}
		fallback, fallbackMsg := fallbackOf(d, c, nested)
		if msg := registry.JoinFieldErrors(fallbackMsg, pathMsg, idMsg); msg != "" {
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
	intConditional, intDispatcher := conditionalOf(lootDecoder.nestedIntTag)
	intTypes = providerTypes{
		// No attribute converts to an integer (tools/gen registrytables
		// stops if one does), so every attribute it names is refused.
		// vanilla: ints.EnvironmentAttributeValue
		"environment_attribute": func(_ lootDecoder, c nbt.Compound) (registry.LootNumber, string) {
			name, _, msg := envAttributeField(c)
			if msg != "" {
				return nil, msg
			}
			return nil, name + " cannot be converted to an integer"
		},
		"conditional":       intConditional,
		"number_dispatcher": intDispatcher,
		"abs":               unary(lootDecoder.nestedInt, "abs"),
		"negate":            unary(lootDecoder.nestedInt, "negate"),
		"sub":               binary(lootDecoder.nestedInt, "sub", "left", "right"),
		"mod":               binary(lootDecoder.nestedInt, "mod", "left", "right"),
		"div":               binary(lootDecoder.nestedInt, "div", "left", "right"),
		"floor_div":         binary(lootDecoder.nestedInt, "floor_div", "left", "right"),
		"floor_mod":         binary(lootDecoder.nestedInt, "floor_mod", "left", "right"),
		"pow":               binary(lootDecoder.nestedInt, "pow", "base", "exponent"),
		"add":               aggregate(lootDecoder.nestedIntTag, "add"),
		"mul":               aggregate(lootDecoder.nestedIntTag, "mul"),
		"max":               aggregate(lootDecoder.nestedIntTag, "max"),
		"min":               aggregate(lootDecoder.nestedIntTag, "min"),
		"avg":               aggregate(lootDecoder.nestedIntTag, "avg"),
		"constant": func(_ lootDecoder, c nbt.Compound) (registry.LootNumber, string) {
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
		"uniform": func(d lootDecoder, c nbt.Compound) (registry.LootNumber, string) {
			lo, loMsg := d.nestedInt(c, "min")
			hi, hiMsg := d.nestedInt(c, "max")
			if msg := registry.JoinFieldErrors(hiMsg, loMsg); msg != "" {
				return nil, msg
			}
			return registry.LootUniform{Min: lo, Max: hi}, ""
		},
		"binomial": func(d lootDecoder, c nbt.Compound) (registry.LootNumber, string) {
			n, nMsg := d.nestedInt(c, "n")
			p, pMsg := d.nestedFloat(c, "p")
			if msg := registry.JoinFieldErrors(pMsg, nMsg); msg != "" {
				return nil, msg
			}
			return registry.LootBinomial{N: n, P: p}, ""
		},
		"from_float": func(d lootDecoder, c nbt.Compound) (registry.LootNumber, string) {
			in, msg := d.nestedFloat(c, "input")
			if msg != "" {
				return nil, msg
			}
			return registry.LootConvert{Input: in}, ""
		},
		"weighted_list": weightedChoice(lootDecoder.nestedIntTag),
		"score": func(d lootDecoder, c nbt.Compound) (registry.LootNumber, string) {
			var out registry.LootScore
			holderTag, holderMsg := fieldOf(c, "target")
			if holderMsg == "" {
				holderMsg = scoreHolder(holderTag, &out)
			}
			var objMsg string
			if objTag, msg := fieldOf(c, "score"); msg != "" {
				objMsg = msg
			} else if obj, ok := objTag.(nbt.String); ok {
				out.Objective = string(obj)
			} else {
				objMsg = "Not a string: " + nbt.Stringify(objTag)
			}
			var fallbackMsg string
			out.Fallback, fallbackMsg = fallbackOf(d, c, lootDecoder.nestedIntTag)
			if msg := registry.JoinFieldErrors(fallbackMsg, objMsg, holderMsg); msg != "" {
				return nil, msg
			}
			return out, ""
		},
		"storage": storage(lootDecoder.nestedIntTag),
	}
}

func init() {
	floatConditional, floatDispatcher := conditionalOf(lootDecoder.nestedFloatTag)
	floatTypes = providerTypes{
		// vanilla: EnchantmentLevelProvider
		"enchantment_level": func(_ lootDecoder, c nbt.Compound) (registry.LootNumber, string) {
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
		"environment_attribute": func(_ lootDecoder, c nbt.Compound) (registry.LootNumber, string) {
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
		"abs":               unary(lootDecoder.nestedFloat, "abs"),
		"negate":            unary(lootDecoder.nestedFloat, "negate"),
		"ceil":              unary(lootDecoder.nestedFloat, "ceil"),
		"floor":             unary(lootDecoder.nestedFloat, "floor"),
		"round":             unary(lootDecoder.nestedFloat, "round"),
		"truncate":          unary(lootDecoder.nestedFloat, "truncate"),
		"sqrt":              unary(lootDecoder.nestedFloat, "sqrt"),
		"sin":               unary(lootDecoder.nestedFloat, "sin"),
		"cos":               unary(lootDecoder.nestedFloat, "cos"),
		"sub":               binary(lootDecoder.nestedFloat, "sub", "left", "right"),
		"div":               binary(lootDecoder.nestedFloat, "div", "left", "right"),
		"mod":               binary(lootDecoder.nestedFloat, "mod", "left", "right"),
		"pow":               binary(lootDecoder.nestedFloat, "pow", "base", "exponent"),
		"add":               aggregate(lootDecoder.nestedFloatTag, "add"),
		"mul":               aggregate(lootDecoder.nestedFloatTag, "mul"),
		"max":               aggregate(lootDecoder.nestedFloatTag, "max"),
		"min":               aggregate(lootDecoder.nestedFloatTag, "min"),
		"avg":               aggregate(lootDecoder.nestedFloatTag, "avg"),
		"length":            aggregate(lootDecoder.nestedFloatTag, "length"),
		"constant": func(_ lootDecoder, c nbt.Compound) (registry.LootNumber, string) {
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
		"uniform": func(d lootDecoder, c nbt.Compound) (registry.LootNumber, string) {
			lo, loMsg := d.nestedFloat(c, "min")
			hi, hiMsg := d.nestedFloat(c, "max")
			if msg := registry.JoinFieldErrors(hiMsg, loMsg); msg != "" {
				return nil, msg
			}
			return registry.LootUniform{Min: lo, Max: hi}, ""
		},
		"from_int": func(d lootDecoder, c nbt.Compound) (registry.LootNumber, string) {
			in, msg := d.nestedInt(c, "input")
			if msg != "" {
				return nil, msg
			}
			return registry.LootConvert{Input: in}, ""
		},
		"weighted_list": weightedChoice(lootDecoder.nestedFloatTag),
		"storage":       storage(lootDecoder.nestedFloatTag),
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
