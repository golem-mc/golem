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
		return (lootDecoder{ResourceDecoder: liveResourceDecoder()}).decodeProviderJSON(data, float)
	}
}

// ValidatePackContextProviders decodes both provider registries against the staged layers.
// vanilla: ResourceManagerRegistryLoadTask.load, RegistryDataLoader.RELOADABLE_REGISTRIES
func ValidatePackContextProviders(layers *registry.PackLayers, ints, floats map[string][]byte) error {
	d := lootDecoder{ResourceDecoder: NewResourceDecoder(layers)}
	for _, entries := range []struct {
		kind  string
		float bool
		files map[string][]byte
	}{
		{"context_int_provider", false, ints},
		{"context_float_provider", true, floats},
	} {
		for _, name := range sortedKeys(entries.files) {
			if _, msg := d.decodeProviderJSON(entries.files[name], entries.float); msg != "" {
				return fmt.Errorf("%s %s: %s", entries.kind, name, msg)
			}
		}
	}
	return nil
}

func (d lootDecoder) decodeProviderJSON(data []byte, float bool) (registry.LootNumber, string) {
	tag, err := JSONTag(data)
	if err != nil {
		return nil, err.Error()
	}
	// JSONTag reads the first value; a registry file must consume the document.
	// vanilla: StrictJsonParser.parse
	if !json.Valid(data) {
		return nil, "trailing data after the JSON value"
	}
	decode := d.decodeInt
	if float {
		decode = d.decodeFloat
	}
	n, msg, err := decode(tag)
	if err != nil {
		return nil, err.Error()
	}
	return n, msg
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

// settled drops a partial result, keeping its error.
func settled(p registry.LootNumber, msg string) (registry.LootNumber, string) {
	if rest, ok := strings.CutPrefix(msg, partialMark); ok {
		return nil, rest
	}
	return p, msg
}

// eitherNumberOr is Codec.either(number, typed) over the typed branch's
// result; a partial result passes through.
func eitherNumberOr(p registry.LootNumber, msg string) (registry.LootNumber, string) {
	if msg == "" || strings.HasPrefix(msg, partialMark) {
		return p, msg
	}
	return nil, "Failed to parse either. First: Not a number; Second: " + msg
}

// DecodeIntProvider reads a context int provider: a bare number or a
// compound with a type, read from a command's inline SNBT. The error is
// ErrWeightOverflow, which no codec message carries; the message is the
// codec's refusal otherwise.
// vanilla: ContextIntProviders.DIRECT_CODEC
func DecodeIntProvider(tag nbt.Tag) (registry.LootNumber, string, error) {
	return (lootDecoder{ResourceDecoder: liveResourceDecoder(), nbtInput: true}).decodeInt(tag)
}

func (d lootDecoder) decodeInt(tag nbt.Tag) (registry.LootNumber, string, error) {
	return decodeProvider(tag, d.intOrPartial)
}

// intOrPartial is decodeInt keeping a partial result.
func (d lootDecoder) intOrPartial(tag nbt.Tag) (registry.LootNumber, string) {
	if v, ok := nbt.IntValue(tag); ok {
		return registry.LootConstant{Value: float64(v)}, ""
	}
	return eitherNumberOr(d.decodeTyped(tag, "context_int_provider_type", intTypes))
}

// DecodeFloatProvider reads a context float provider.
// vanilla: ContextFloatProviders.DIRECT_CODEC
func DecodeFloatProvider(tag nbt.Tag) (registry.LootNumber, string, error) {
	return (lootDecoder{ResourceDecoder: liveResourceDecoder(), nbtInput: true}).decodeFloat(tag)
}

func (d lootDecoder) decodeFloat(tag nbt.Tag) (registry.LootNumber, string, error) {
	return decodeProvider(tag, d.floatOrPartial)
}

func (d lootDecoder) floatOrPartial(tag nbt.Tag) (registry.LootNumber, string) {
	if v, ok := nbt.FloatValue(tag); ok {
		return registry.LootConstant{Value: float64(v)}, ""
	}
	return eitherNumberOr(d.decodeTyped(tag, "context_float_provider_type", floatTypes))
}

// decodeProvider runs decode and drops a partial result. A weight overflow
// panics out of the nested decoders, as vanilla's constructor throws, and
// ends here as an error.
func decodeProvider(tag nbt.Tag, decode func(nbt.Tag) (registry.LootNumber, string)) (n registry.LootNumber, msg string, err error) {
	defer recoverLoot(&err)
	n, msg = settled(decode(tag))
	return n, msg, nil
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
	cond, err := d.decodePredicate(data)
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
		cases, caseMsgs := decodeEach(items, func(e nbt.Tag) (registry.LootDispatchCase, string) {
			cc, ok := e.(nbt.Compound)
			if !ok {
				return registry.LootDispatchCase{}, "Not a map: " + nbt.Stringify(e)
			}
			test, testMsg := condField(d, cc)
			v, valueMsg := required(d, cc, "value")
			if msg := registry.JoinFieldErrors(valueMsg, testMsg); msg != "" {
				return registry.LootDispatchCase{}, msg
			}
			return registry.LootDispatchCase{Condition: test, Value: v}, ""
		})
		if msg := registry.JoinFieldErrors(caseMsgs...); msg != "" {
			return cases, partialMark + msg
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
				return registry.LootDispatcher{Cases: cases, Default: def}, partialMark + msg
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
	return settled(d.nestedIntOrPartial(t))
}

// nestedIntOrPartial is nestedIntTag keeping a partial result.
func (d lootDecoder) nestedIntOrPartial(t nbt.Tag) (registry.LootNumber, string) {
	if s, ok := t.(nbt.String); ok {
		return d.providerRef(string(s), false)
	}
	return d.intOrPartial(t)
}

func (d lootDecoder) nestedFloat(c nbt.Compound, key string) (registry.LootNumber, string) {
	t, msg := fieldOf(c, key)
	if msg != "" {
		return nil, msg
	}
	return d.nestedFloatTag(t)
}

func (d lootDecoder) nestedFloatTag(t nbt.Tag) (registry.LootNumber, string) {
	return settled(d.nestedFloatOrPartial(t))
}

func (d lootDecoder) nestedFloatOrPartial(t nbt.Tag) (registry.LootNumber, string) {
	if s, ok := t.(nbt.String); ok {
		return d.providerRef(string(s), true)
	}
	return d.floatOrPartial(t)
}

// listItems is the elements of a list or typed array.
// vanilla: NbtOps.getStream
func listItems(t nbt.Tag) ([]nbt.Tag, bool) {
	// A list keeps its entries as written: a literal {"": x} compound is not
	// a mixed-list wrapper, so nbt.ListElement must not strip it.
	if l, ok := t.(nbt.List); ok {
		return l.Items, true
	}
	seq := nbt.CollectionValues(t)
	if seq == nil {
		return nil, false
	}
	return slices.Collect(seq), true
}

// decodeEach decodes every element, keeping the successes and partial
// results in order and every failure's message, newest first.
// vanilla: ListCodec.decode, DataResult.apply2stable
func decodeEach[T any](items []nbt.Tag, one func(nbt.Tag) (T, string)) ([]T, []string) {
	var out []T
	var msgs []string
	for _, item := range items {
		v, msg := one(item)
		rest, partial := strings.CutPrefix(msg, partialMark)
		if msg == "" || partial {
			out = append(out, v)
		}
		if rest != "" {
			msgs = append(msgs, rest)
		}
	}
	slices.Reverse(msgs)
	return out, msgs
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
		v, msg := one(item)
		if msg != "" {
			return nil, msg
		}
		out = append(out, v)
	}
	return out, ""
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
		type entry struct {
			value  registry.LootNumber
			weight int32
		}
		entries, msgs := decodeEach(items, func(item nbt.Tag) (entry, string) {
			v, weight, msg := weightedEntry(d, item, nested)
			return entry{v, weight}, msg
		})
		var out registry.LootWeightedList
		var total int64
		for _, e := range entries {
			out.Values = append(out.Values, e.value)
			out.Weights = append(out.Weights, int64(e.weight))
			total += int64(e.weight)
		}
		// The constructor runs on the entries the list codec kept, so an
		// element's error does not hide the overflow.
		// vanilla: ListCodec.decode, WeightedList.entryToListCodec
		if total > math.MaxInt32 {
			panic(lootFailure{msg: WeightOverflowMsg, overflow: true})
		}
		// vanilla: WeightedList's non-zero check runs on the partial list.
		if total == 0 {
			msg := registry.JoinFieldErrors(append(msgs, "Weighted list must contain at least one entry with non-zero weight")...)
			return nil, msg
		}
		if msg := registry.JoinFieldErrors(msgs...); msg != "" {
			return out, partialMark + msg
		}
		return out, ""
	}
}

// weightedEntry decodes one data and weight entry of a weighted list; a
// partial data with a valid weight is a partial entry.
// vanilla: Weighted.codec, DataResult.apply2stable
func weightedEntry(d lootDecoder, item nbt.Tag, nested func(lootDecoder, nbt.Tag) (registry.LootNumber, string)) (registry.LootNumber, int32, string) {
	entry, ok := item.(nbt.Compound)
	if !ok {
		return nil, 0, "Not a map: " + nbt.Stringify(item)
	}
	var v registry.LootNumber
	data, dataMsg := fieldOf(entry, "data")
	if dataMsg == "" {
		v, dataMsg = nested(d, data)
	}
	var weight int32
	w, weightMsg := fieldOf(entry, "weight")
	if weightMsg == "" {
		var ok bool
		if weight, ok = nbt.IntValue(w); !ok {
			weightMsg = d.notANumber(w)
		} else if weight < 0 {
			weightMsg = fmt.Sprintf("Value must be non-negative: %d", weight)
		}
	}
	dataMsg, partial := strings.CutPrefix(dataMsg, partialMark)
	if partial && weightMsg == "" {
		return v, weight, partialMark + dataMsg
	}
	if msg := registry.JoinFieldErrors(weightMsg, dataMsg); msg != "" {
		return nil, 0, msg
	}
	return v, weight, ""
}

// notANumber is the codec error of a non-numeric value: NbtOps leaves the
// value out, JsonOps appends it.
// vanilla: NbtOps.getNumberValue, JsonOps.getNumberValue
func (d lootDecoder) notANumber(t nbt.Tag) string {
	if d.nbtInput {
		return "Not a number"
	}
	return "Not a number: " + nbt.Stringify(t)
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
		"weighted_list": weightedChoice(lootDecoder.nestedIntOrPartial),
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
			v, ok := nbt.FloatValue(t)
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
		"weighted_list": weightedChoice(lootDecoder.nestedFloatOrPartial),
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
			out[i] = TagJSON(item)
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
