package datapack

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The loot table and predicate codec reads data pack JSON into the registry
// shapes internal/server/rules rolls. It reads the subset tools/gen loot reads and
// refuses anything else rather than approximating, so a table is rolled as
// written or not loaded.
// vanilla: LootTable.DIRECT_CODEC, LootItemCondition.DIRECT_CODEC

// lootFailure is the panic a decoding step raises; the entry points recover
// it into an error.
type lootFailure struct {
	msg string
	// An optional field or list supplies a partial result to Codec.either.
	partial bool
}

func fail(format string, args ...any) { panic(lootFailure{msg: fmt.Sprintf(format, args...)}) }

func recoverLoot(err *error) {
	switch r := recover().(type) {
	case nil:
	case lootFailure:
		*err = errors.New(r.msg)
	default:
		panic(r)
	}
}

// partialLoot retains an optional field's default or a list's decoded elements.
func partialLoot() {
	r := recover()
	if failure, ok := r.(lootFailure); ok {
		failure.partial = true
		panic(failure)
	}
	if r != nil {
		panic(r)
	}
}

func parseJSON(data []byte) any {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		fail("%s", err)
	}
	if _, err := dec.Token(); err == nil {
		fail("trailing data after the JSON value")
	}
	return v
}

// DecodeLootTable reads one loot table file.
func DecodeLootTable(data []byte) (table registry.LootTable, err error) {
	defer recoverLoot(&err)
	return (lootDecoder{legacyFunctions: true}).lootTable(object(parseJSON(data), "loot table")), nil
}

// DecodeLootTableValue reads a loot table already parsed into the shapes
// encoding/json gives, as an inline /loot argument is. Functions require
// the current type discriminator; file decoders also accept function.
func DecodeLootTableValue(v any) (table registry.LootTable, err error) {
	defer recoverLoot(&err)
	return (lootDecoder{inlineLootTable: true}).lootTable(object(v, "loot table")), nil
}

func init() { registry.DecodeBuiltinPredicate = DecodePredicate }

// DecodePredicate reads a predicate file: a condition, or a list that must
// all hold.
// vanilla: LootDataType.PREDICATES
func DecodePredicate(data []byte) (cond registry.LootCondition, err error) {
	defer recoverLoot(&err)
	v := parseJSON(data)
	if list, ok := v.([]any); ok {
		terms := make([]registry.LootCondition, len(list))
		for i, t := range list {
			terms[i] = condition(object(t, "predicate"))
		}
		return registry.LootAllOf{Terms: terms}, nil
	}
	return condition(object(v, "predicate")), nil
}

// DecodeItemModifier reads an item modifier file: one function, or a list
// of them run in order.
// vanilla: LootDataType.MODIFIERS
func DecodeItemModifier(data []byte) (fn registry.LootFunction, err error) {
	defer recoverLoot(&err)
	return (lootDecoder{legacyFunctions: true}).itemModifier(parseJSON(data)), nil
}

// DecodeItemModifierValue reads an item modifier already parsed into the
// shapes encoding/json gives (objects, lists, strings, bools, json.Number),
// as an inline /item modify argument is. Functions require the current type
// discriminator; file decoders also accept the legacy function discriminator.
func DecodeItemModifierValue(v any) (fn registry.LootFunction, err error) {
	defer recoverLoot(&err)
	return (lootDecoder{}).itemModifier(v), nil
}

// lootDecoder keeps file compatibility and inline loot-table codec errors separate.
type lootDecoder struct {
	legacyFunctions bool
	inlineLootTable bool
}

// itemModifier reads a typed function, or else a holder set of modifiers run
// in order.
// vanilla: LootItemFunctions.DIRECT_CODEC
func (d lootDecoder) itemModifier(v any) registry.LootFunction {
	if o, ok := v.(obj); ok {
		return d.function(o)
	}
	return registry.LootFunctionSequence{Functions: d.modifierHolders(v)}
}

type obj = map[string]any

func object(v any, what string) obj {
	o, ok := v.(map[string]any)
	if !ok {
		fail("Not a map: %s", snbt(v))
	}
	return o
}

func list(v any, what string) []any {
	if v == nil {
		return nil
	}
	l, ok := v.([]any)
	if !ok {
		fail("%s is not a list", what)
	}
	return l
}

// holderList reads the direct form of a holder set: a list, or a single
// holder standing for a list of one.
// vanilla: HolderSetCodec.directCodec, ExtraCodecs.compactListCodec
func holderList(v any) []any {
	if v == nil {
		return nil
	}
	if l, ok := v.([]any); ok {
		return l
	}
	return []any{v}
}

func str(v any, what string) string {
	s, ok := v.(string)
	if !ok {
		fail("%s is not a string", what)
	}
	return s
}

func requireStr(o obj, key string) string {
	return str(required(o, key), key)
}

func num(v any) (float64, bool) {
	if b, isBool := v.(bool); isBool { // an SNBT 1b/0b arrives as a bool
		if b {
			return 1, true
		}
		return 0, true
	}
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	f, err := n.Float64()
	return f, err == nil
}

func requireNum(o obj, key string) float64 {
	f, ok := num(o[key])
	if !ok {
		fail("%q is not a number", key)
	}
	return f
}

func optNum(o obj, key string, def float64) float64 {
	if _, ok := o[key]; !ok {
		return def
	}
	return requireNum(o, key)
}

func requireFlag(o obj, key string) bool {
	required(o, key)
	return flag(o, key, false)
}

func flag(o obj, key string, def bool) bool {
	v, ok := o[key]
	if !ok {
		return def
	}
	if n, isNum := num(v); isNum && (n == 0 || n == 1) {
		return n == 1 // an SNBT byte: true and false are 1b and 0b
	}
	b, isBool := v.(bool)
	if !isBool {
		fail("%q is not a boolean", key)
	}
	return b
}

func unknownKeys(o obj, what string, known ...string) {
	for k := range o {
		if !slices.Contains(known, k) {
			fail("%s field %q is not supported by this server", what, k)
		}
	}
}

//go:fix inline

//go:fix inline

func (d lootDecoder) lootTable(o obj) registry.LootTable {
	var t registry.LootTable
	for _, p := range list(o["pools"], "pools") {
		t.Pools = append(t.Pools, d.lootPool(object(p, "pool")))
	}
	t.Functions = d.modifiers(o)
	return t
}

func (d lootDecoder) lootPool(o obj) registry.LootPool {
	p := registry.LootPool{Rolls: intAt(o, "rolls")}
	if _, ok := o["bonus_rolls"]; ok {
		p.BonusRolls = floatProvider(o["bonus_rolls"])
	} else {
		p.BonusRolls = registry.LootConstant{}
	}
	for _, e := range list(o["entries"], "entries") {
		p.Entries = append(p.Entries, d.entry(object(e, "entry")))
	}
	p.Conditions = conditions(o)
	p.Functions = d.modifiers(o)
	return p
}

func required(o obj, key string) any {
	v, ok := o[key]
	if !ok {
		fail("No key %s in MapLike[%s]", key, snbt(o))
	}
	return v
}

// intAt and floatAt read a required context int or float provider field.
func intAt(o obj, key string) registry.LootNumber   { return intProvider(required(o, key)) }
func floatAt(o obj, key string) registry.LootNumber { return floatProvider(required(o, key)) }

// intProvider reads a context int provider holder: a registry entry's name
// or an inline provider.
// vanilla: ContextIntProviders.CODEC
func intProvider(v any) registry.LootNumber { return providerOf(v, nestedIntTag) }

// floatProvider reads a context float provider holder.
// vanilla: ContextFloatProviders.CODEC
func floatProvider(v any) registry.LootNumber { return providerOf(v, nestedFloatTag) }

func providerOf(v any, decode func(nbt.Tag) (registry.LootNumber, string)) registry.LootNumber {
	tag, err := protocol.JSONValueTag(v)
	if err != nil {
		fail("%s", err)
	}
	n, msg := decode(tag)
	if msg != "" {
		fail("%s", msg)
	}
	return n
}

func valueOr(o obj, key string, def any) any {
	if v, ok := o[key]; ok {
		return v
	}
	return def
}

func (d lootDecoder) entry(o obj) registry.LootEntry {
	kind := registry.Qualify(str(o["type"], "entry type"))
	weight := int32(optNum(o, "weight", 1))
	quality := int32(optNum(o, "quality", 0))
	conds := conditions(o)
	switch kind {
	case "minecraft:item":
		item := registry.Qualify(requireStr(o, "name"))
		id, ok := registry.ItemByName(item)
		if !ok {
			fail("Unknown registry key in ResourceKey[minecraft:root / minecraft:item]: %s", item)
		}
		return registry.LootItemEntry{Item: id, Weight: weight, Quality: quality, Conditions: conds, Functions: d.modifiers(o)}
	case "minecraft:alternatives", "minecraft:group", "minecraft:sequence":
		var children []registry.LootEntry
		for _, c := range list(o["children"], "children") {
			children = append(children, d.entry(object(c, "entry")))
		}
		switch kind {
		case "minecraft:alternatives":
			return registry.LootAlternatives{Children: children, Conditions: conds}
		case "minecraft:group":
			return registry.LootGroup{Children: children, Conditions: conds}
		}
		return registry.LootSequence{Children: children, Conditions: conds}
	case "minecraft:empty":
		return registry.LootEmpty{Weight: weight, Quality: quality, Conditions: conds}
	case "minecraft:loot_table":
		ref, ok := o["value"].(string)
		if !ok {
			// vanilla: NestedLootTable.CODEC, an Either of a key and a table
			inline := d.lootTable(object(o["value"], "value"))
			return registry.LootReference{Inline: &inline, Weight: weight, Quality: quality, Conditions: conds, Functions: d.modifiers(o)}
		}
		return registry.LootReference{Table: registry.Qualify(ref), Weight: weight, Quality: quality, Conditions: conds, Functions: d.modifiers(o)}
	case "minecraft:tag":
		if items, ok := o["items"]; ok {
			// vanilla: TagEntry.MAP_CODEC, ExpandableContainerBase.expandableFields
			return registry.LootTag{Items: lootItems(holderSet(items, "item", registry.ItemTagMembers)), Expand: flag(o, "expand", false), Weight: weight, Conditions: conds, Functions: d.modifiers(o)}
		}
		// A pack in the earlier format names the tag and must say expand; a tag no pack
		// defines holds nothing.
		items, _ := registry.ItemTagMembers(registry.Qualify(strings.TrimPrefix(requireStr(o, "name"), "#")))
		items = slices.Clone(items)
		slices.Sort(items)
		return registry.LootTag{Items: lootItems(items), Expand: requireFlag(o, "expand"), Weight: weight, Quality: quality, Conditions: conds, Functions: d.modifiers(o)}
	case "minecraft:slots":
		return registry.LootSlots{Source: slotSource(required(o, "slot_source")), Weight: weight, Quality: quality, Conditions: conds, Functions: d.modifiers(o)}
	case "minecraft:dynamic":
		return registry.LootDynamic{Name: registry.Qualify(requireStr(o, "name")), Weight: weight, Quality: quality, Conditions: conds}
	}
	fail("loot entry %s is not supported by this server", kind)
	return nil
}

// tagMembers resolves a tag against the tags loaded now, sorted.
func tagMembers(kind, tag string, members func(string) ([]string, bool)) []string {
	names, ok := members(registry.Qualify(tag))
	if !ok {
		fail("unknown %s tag %s", kind, tag)
	}
	names = slices.Clone(names)
	slices.Sort(names)
	return names
}

// holderSet reads one name, a list of names or a #tag.
func holderSet(v any, kind string, members func(string) ([]string, bool)) []string {
	switch k := v.(type) {
	case []any:
		out := make([]string, len(k))
		for i, e := range k {
			out[i] = registry.Qualify(str(e, kind))
		}
		return out
	case string:
		if tag, ok := strings.CutPrefix(k, "#"); ok {
			return tagMembers(kind, tag, members)
		}
		return []string{registry.Qualify(k)}
	}
	fail("%s is not a name, a list or a tag", kind)
	return nil
}

// conditions reads the optional condition holder of a pool, an entry or a
// function. A pack in the earlier format lists them under conditions
// instead, which is still read.
// vanilla: LootItemCondition.CODEC.optionalFieldOf("condition")
func conditions(o obj) []registry.LootCondition {
	if v, ok := o["condition"]; ok {
		return []registry.LootCondition{conditionHolder(v)}
	}
	var out []registry.LootCondition
	for _, c := range list(o["conditions"], "conditions") {
		out = append(out, condition(object(c, "condition")))
	}
	return out
}

// conditionHolder reads a predicate's name, resolved when evaluated, or an
// inline condition.
// vanilla: LootItemCondition.CODEC
func conditionHolder(v any) registry.LootCondition {
	if name, ok := v.(string); ok {
		return registry.LootReferenceCondition{Name: holderName(name)}
	}
	return condition(object(v, "condition"))
}

func stateProperties(v any) []registry.LootStateProperty {
	props := []registry.LootStateProperty{}
	if v == nil {
		return props
	}
	o := object(v, "properties")
	for _, k := range sortedKeys(o) {
		s, ok := o[k].(string)
		if !ok {
			fail("block property %q is not an exact string value", k)
		}
		props = append(props, registry.LootStateProperty{Key: k, Value: s})
	}
	return props
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// condition reads a condition of tools/gen loot's set, plus reference and
// the weather, time, score, environment and value checks.
// vanilla: LootItemConditions
func condition(c obj) registry.LootCondition {
	kind := registry.Qualify(requireStr(c, "type"))
	switch kind {
	case "minecraft:survives_explosion":
		return registry.LootSurvivesExplosion{}
	case "minecraft:killed_by_player":
		return registry.LootKilledByPlayer{}
	case "minecraft:match_tool":
		return registry.LootMatchTool{Predicate: itemPredicate(object(valueOr(c, "predicate", obj{}), "predicate"))}
	case "minecraft:random_chance":
		return registry.LootRandomChance{Chance: floatAt(c, "chance")}
	case "minecraft:random_chance_with_enchanted_bonus":
		enchanted := object(c["enchanted_chance"], "enchanted_chance")
		out := registry.LootEnchantedChance{
			Enchantment: registry.Qualify(requireStr(c, "enchantment")), Unenchanted: requireNum(c, "unenchanted_chance"),
		}
		// The linear form keeps its float64 numbers, as the generated tables do.
		if registry.Qualify(optStr(enchanted, "type")) == "minecraft:linear" {
			out.Base, out.PerLevel = requireNum(enchanted, "base"), requireNum(enchanted, "per_level_above_first")
			return out
		}
		out.Chance = levelValueOf(c["enchanted_chance"])
		return out
	case "minecraft:table_bonus":
		var chances []float64
		for _, v := range list(c["chances"], "chances") {
			f, ok := num(v)
			if !ok {
				fail("a table_bonus chance is not a number")
			}
			chances = append(chances, f)
		}
		if len(chances) == 0 {
			fail("table_bonus without chances")
		}
		return registry.LootTableBonus{Enchantment: registry.Qualify(requireStr(c, "enchantment")), Chances: chances}
	case "minecraft:block_state_property":
		return registry.LootBlockState{Block: registry.Qualify(requireStr(c, "block")), Properties: stateProperties(c["properties"])}
	case "minecraft:match_block":
		unknownKeys(c, "match_block", "type", "blocks", "state")
		out := registry.LootMatchBlock{Properties: stateProperties(c["state"])}
		if raw := c["blocks"]; raw != nil {
			out.Blocks = holderSet(raw, "block", registry.BlockTagMembers)
			if out.Blocks == nil {
				out.Blocks = []string{}
			}
		}
		return out
	case "minecraft:location_check":
		out := locationPredicate(object(valueOr(c, "predicate", obj{}), "predicate"))
		out.OffsetX, out.OffsetY, out.OffsetZ = int32(optNum(c, "offsetX", 0)), int32(optNum(c, "offsetY", 0)), int32(optNum(c, "offsetZ", 0))
		return out
	case "minecraft:entity_properties":
		entity, _ := c["entity"].(string)
		if entity == "" {
			entity = "this"
		}
		return registry.LootEntityProperties{Entity: entity, Predicate: entityPredicate(object(valueOr(c, "predicate", obj{}), "predicate"))}
	case "minecraft:damage_source_properties":
		return damageSource(object(valueOr(c, "predicate", obj{}), "predicate"))
	case "minecraft:inverted":
		return registry.LootInverted{Term: conditionHolder(required(c, "term"))}
	case "minecraft:any_of", "minecraft:all_of":
		terms := []registry.LootCondition{}
		for _, t := range holderList(c["terms"]) {
			terms = append(terms, conditionHolder(t))
		}
		if kind == "minecraft:any_of" {
			return registry.LootAnyOf{Terms: terms}
		}
		return registry.LootAllOf{Terms: terms}
	case "minecraft:reference":
		return registry.LootReferenceCondition{Name: registry.Qualify(requireStr(c, "name"))}
	case "minecraft:time_check":
		clock := registry.Qualify(requireStr(c, "clock"))
		// vanilla: RegistryFixedCodec, which refuses an unknown holder at decode time
		if clocks, _ := registry.Get("minecraft:world_clock"); clocks.ID(clock) < 0 {
			fail("Failed to get element %s from registry minecraft:world_clock", clock)
		}
		out := registry.LootTimeCheck{Clock: clock, Value: contextRange(required(c, "value"), intProvider)}
		if _, ok := c["period"]; ok {
			out.Period = int64(requireNum(c, "period"))
		}
		return out
	case "minecraft:weather_check":
		var out registry.LootWeatherCheck
		if _, ok := c["raining"]; ok {
			out.Raining = new(flag(c, "raining", false))
		}
		if _, ok := c["thundering"]; ok {
			out.Thundering = new(flag(c, "thundering", false))
		}
		return out
	case "minecraft:entity_scores":
		entity := requireStr(c, "entity")
		if !slices.Contains(lootEntityTargets, entity) {
			fail("Unknown entity target: %s", entity)
		}
		scores := object(required(c, "scores"), "scores")
		out := registry.LootEntityScores{Entity: entity}
		for _, k := range sortedKeys(scores) {
			out.Scores = append(out.Scores, registry.LootScoreRange{Objective: k, Range: contextRange(scores[k], intProvider)})
		}
		return out
	case "minecraft:enchantment_active_check":
		return registry.LootEnchantmentActive{Active: requireFlag(c, "active")}
	case "minecraft:environment_attribute_check":
		name := registry.Qualify(requireStr(c, "attribute"))
		attr, ok := registry.EnvAttributeByName(name)
		if !ok {
			fail("Unknown registry key in ResourceKey[minecraft:root / minecraft:environment_attribute]: %s", name)
		}
		if !attr.Float {
			fail("environment attribute %s is not supported by this server", name)
		}
		return registry.LootEnvironmentCheck{Attribute: name, Value: float32(requireNum(c, "value"))}
	case "minecraft:int_value_check", "minecraft:float_value_check":
		provider := intProvider
		if kind == "minecraft:float_value_check" {
			provider = floatProvider
		}
		return registry.LootValueCheck{
			Value: provider(required(c, "value")), Range: contextRange(required(c, "test"), provider),
			Float: kind == "minecraft:float_value_check",
		}
	}
	fail("condition %s is not supported by this server", kind)
	return nil
}

// lootEntityTargets are the context entities a condition may name.
// vanilla: LootContext.EntityTarget
var lootEntityTargets = []string{
	"this", "attacker", "direct_attacker", "attacking_player", "target_entity", "interacting_entity",
}

// contextRange reads an int or float range predicate: a provider is a point,
// an object of optional min and max a line.
// vanilla: IntRangePredicate.CODEC, FloatRangePredicate.CODEC
func contextRange(v any, provider func(any) registry.LootNumber) registry.LootRange {
	if o, ok := v.(map[string]any); ok && o["type"] == nil {
		var out registry.LootRange
		if m, has := o["min"]; has {
			out.Min = provider(m)
		}
		if m, has := o["max"]; has {
			out.Max = provider(m)
		}
		return out
	}
	return registry.LootRange{Exact: provider(v)}
}

func damageSource(p obj) registry.LootCondition {
	unknownKeys(p, "damage source predicate", "tags", "direct_entity", "source_entity", "is_direct")
	// The shapes the generated tables write: one expected tag, optionally with
	// a direct entity type; or only a source entity type and frog variant.
	if tags := list(valueOr(p, "tags", []any{}), "tags"); len(tags) == 1 && (len(p) == 1 || len(p) == 2 && p["direct_entity"] != nil) {
		tag := object(tags[0], "tag")
		if expected, _ := tag["expected"].(bool); expected && len(tag) == 2 {
			var direct string
			if d, isObj := p["direct_entity"].(map[string]any); len(p) == 2 && isObj && len(d) == 1 {
				if t, isStr := d["minecraft:entity_type"].(string); isStr {
					direct = registry.Qualify(t)
				}
			}
			if len(p) == 1 || direct != "" {
				return damageTypeSet(required(tag, "id"), direct)
			}
		}
	}
	if source, isObj := p["source_entity"].(map[string]any); isObj && len(p) == 1 {
		if t, isStr := source["minecraft:entity_type"].(string); isStr {
			rest := len(source) - 1
			var variant string
			ok := true
			if c, has := source["minecraft:components"]; has {
				comps, isMap := c.(map[string]any)
				v, isStr := comps["minecraft:frog/variant"].(string)
				ok = isMap && len(comps) == 1 && isStr
				variant, rest = v, rest-1
			}
			if ok && rest == 0 {
				return registry.LootDamageSource{SourceType: registry.Qualify(t), FrogVariant: variant}
			}
		}
	}
	var out registry.LootDamageSource
	for _, x := range list(valueOr(p, "tags", []any{}), "tags") {
		t := object(x, "a damage source tag")
		out.Tags = append(out.Tags, registry.LootDamageTag{Set: damageHolderSet(required(t, "id")), Expected: requireFlag(t, "expected")})
	}
	for key, dst := range map[string]**registry.LootEntityPredicate{"direct_entity": &out.Direct, "source_entity": &out.Source} {
		if v, ok := p[key]; ok {
			e := entityPredicate(object(v, key))
			*dst = &e
		}
	}
	if _, ok := p["is_direct"]; ok {
		out.IsDirect = new(requireFlag(p, "is_direct"))
	}
	return out
}

func damageTypeSet(id any, direct string) registry.LootCondition {
	set := damageHolderSet(id)
	if set.IsTag || len(set.Types) == 1 {
		return registry.LootDamageSource{DamageTypes: set, DirectType: direct}
	}
	terms := make([]registry.LootCondition, len(set.Types))
	for i, damage := range set.Types {
		terms[i] = registry.LootDamageSource{DamageTypes: &registry.DamageTypeSet{Types: []registry.DamageType{damage}}, DirectType: direct}
	}
	return registry.LootAnyOf{Terms: terms}
}

// damageHolderSet reads a #tag, one damage type or a list. Bare tag keys
// remain readable for packs in the earlier format when no damage type has that name.
// vanilla: TagPredicate.codec
func damageHolderSet(id any) *registry.DamageTypeSet {
	types, _ := registry.Get("minecraft:damage_type")
	known := func(v any) registry.DamageType {
		name := registry.Qualify(str(v, "damage type"))
		if types.ID(name) < 0 {
			fail("Unknown registry key in ResourceKey[minecraft:root / minecraft:damage_type]: %s", name)
		}
		return damageTypeOf(name)
	}
	switch v := id.(type) {
	case string:
		name := registry.Qualify(strings.TrimPrefix(v, "#"))
		if strings.HasPrefix(v, "#") || types.ID(name) < 0 && registry.DamageTypeTagExists(name) {
			return &registry.DamageTypeSet{Tag: registry.DamageTypeTagOf(name), IsTag: true}
		}
		return &registry.DamageTypeSet{Types: []registry.DamageType{known(v)}}
	case []any:
		set := &registry.DamageTypeSet{Types: make([]registry.DamageType, len(v))}
		for i, e := range v {
			set.Types[i] = known(e)
		}
		return set
	}
	fail("damage type is not a name, a list or a tag")
	return nil
}

func itemPredicate(p obj) registry.LootItemPredicate {
	unknownKeys(p, "item predicate", "items", "count", "components", "predicates")
	var out registry.LootItemPredicate
	if needsFullItemPredicate(p) {
		full := registry.DecodeItemPredicate(nbtOf(p))
		out.Full = &full
	}
	if items, ok := p["items"]; ok {
		out.Items = holderSet(items, "item", registry.ItemTagMembers)
	}
	if preds, ok := p["predicates"]; ok {
		po := object(preds, "predicates")
		unknownKeys(po, "item predicates", componentPredicateTypes...)
		// An empty object asks only that the component be there.
		// vanilla: AnyValue
		for _, presence := range []struct {
			key string
			dst *bool
		}{{"minecraft:dyed_color", &out.HasDyedColor}, {"minecraft:map_id", &out.HasMapID}} {
			if v, ok := po[presence.key]; ok {
				unknownKeys(object(v, presence.key+" predicate"), presence.key+" predicate")
				*presence.dst = true
			}
		}
		for _, component := range []struct {
			key string
			dst *[]registry.LootEnchantments
		}{
			{"minecraft:enchantments", &out.Enchantments}, {"minecraft:stored_enchantments", &out.StoredEnchantments},
		} {
			if _, ok := po[component.key]; !ok {
				continue
			}
			for _, e := range list(po[component.key], component.key) {
				eo := object(e, "enchantment predicate")
				unknownKeys(eo, "enchantment predicate", "enchantments", "levels")
				var ench registry.LootEnchantments
				if names, has := eo["enchantments"]; has {
					ench.Enchantments = holderSet(names, "enchantment", registry.EnchantmentTagMembers)
				}
				switch lv := eo["levels"].(type) {
				case nil:
				case json.Number:
					f, _ := lv.Float64()
					ench.Min, ench.Max = int32(f), int32(f)
				default:
					lo := object(lv, "levels")
					ench.Min, ench.Max = int32(optNum(lo, "min", 0)), int32(optNum(lo, "max", 0))
				}
				*component.dst = append(*component.dst, ench)
			}
		}
	}
	return out
}

// componentPredicateTypes are the partial component predicate types an item
// predicate names.
// vanilla: DataComponentPredicates
var componentPredicateTypes = []string{
	"minecraft:damage", "minecraft:enchantments", "minecraft:stored_enchantments", "minecraft:potion_contents",
	"minecraft:custom_data", "minecraft:container", "minecraft:bundle_contents", "minecraft:firework_explosion",
	"minecraft:fireworks", "minecraft:writable_book_content", "minecraft:written_book_content",
	"minecraft:attribute_modifiers", "minecraft:trim", "minecraft:jukebox_playable", "minecraft:villager/variant",
	"minecraft:dyed_color", "minecraft:map_id",
}

// needsFullItemPredicate reports an item predicate that asks for more than the
// item set, enchantments and component presence the loot tables read, which
// registry.ItemPredicate then answers. It checks the shapes vanilla's codec
// rejects the whole file for.
// vanilla: ItemPredicate.CODEC, DataComponentMatchers.CODEC
func needsFullItemPredicate(p obj) bool {
	full := false
	if c, ok := p["count"]; ok {
		switch c.(type) {
		case json.Number:
		case map[string]any:
			unknownKeys(object(c, "count"), "count", "min", "max")
		default:
			fail("count is not a number or a range")
		}
		full = true
	}
	if c, ok := p["components"]; ok {
		for key := range object(c, "components") {
			if _, known := registry.ComponentID(registry.Qualify(key)); !known {
				fail("Unknown registry key in ResourceKey[minecraft:root / minecraft:data_component_type]: %s", registry.Qualify(key))
			}
		}
		full = true
	}
	if preds, ok := p["predicates"]; ok {
		for key := range object(preds, "predicates") {
			if !slices.Contains(componentPredicateTypes, key) || key == "minecraft:enchantments" ||
				key == "minecraft:stored_enchantments" || key == "minecraft:dyed_color" || key == "minecraft:map_id" {
				continue
			}
			full = true
		}
	}
	return full
}

// nbtOf converts a JSON object to the NBT the stored form of the same value
// has, which the codecs of components and predicates read.
func nbtOf(v obj) nbt.Compound {
	compound, err := nbt.ParseOrderedSNBT(snbt(map[string]any(v)))
	if err != nil {
		fail("Failed to convert the item predicate: %v", err)
	}
	return compound.Entries()
}

// componentTests reads an entity's partial component predicates. Only
// villager/variant and custom_data are components an entity holds; the other
// predicate types decode and never match.
// vanilla: EntityPartialComponentsPredicate, DataComponentPredicate.CODEC
func componentTests(predicates obj) []registry.LootComponentTest {
	var out []registry.LootComponentTest
	for name, v := range predicates {
		switch name {
		case "minecraft:villager/variant":
			out = append(out, registry.LootComponentTest{VillagerTypes: holderSet(v, "villager type", func(string) ([]string, bool) { return nil, false })})
		case "minecraft:custom_data":
			text, isStr := v.(string)
			if !isStr {
				fail("custom_data predicate is not a string")
			}
			compound, err := nbt.ParseOrderedSNBT(text)
			if err != nil {
				fail("Failed to parse custom_data predicate: %v", err)
			}
			out = append(out, registry.LootComponentTest{CustomData: compound})
		case "minecraft:potion_contents":
			potion := object(v, "potion_contents predicate")
			unknownKeys(potion, "potion_contents predicate", "potions", "effects")
			if _, has := potion["effects"]; has {
				fail("a potion_contents predicate with effects is not supported by this server")
			}
			test := registry.LootComponentTest{Potion: true}
			if set, has := potion["potions"]; has {
				test.Potions = holderSet(set, "potion", registry.EmptyTagMembers)
			}
			out = append(out, test)
		case "minecraft:damage", "minecraft:enchantments", "minecraft:stored_enchantments",
			"minecraft:container", "minecraft:bundle_contents", "minecraft:firework_explosion", "minecraft:fireworks",
			"minecraft:writable_book_content", "minecraft:written_book_content", "minecraft:attribute_modifiers",
			"minecraft:trim", "minecraft:jukebox_playable":
			out = append(out, registry.LootComponentTest{Never: true})
		default:
			fail("unknown component predicate %s", name)
		}
	}
	return out
}

// locationPredicate reads a LocationPredicate; an empty one matches every
// loaded position.
// vanilla: LocationPredicate.CODEC
func locationPredicate(predicate obj) registry.LootLocationCheck {
	unknownKeys(predicate, "location predicate", "position", "biomes", "structures", "dimension", "smokey", "light", "block", "fluid", "can_see_sky")
	var out registry.LootLocationCheck
	if v, ok := predicate["position"]; ok {
		position := object(v, "position")
		unknownKeys(position, "position predicate", "x", "y", "z")
		out.Position = &[3]registry.AdvancementRange{doubleRange(position, "x"), doubleRange(position, "y"), doubleRange(position, "z")}
	}
	if v, ok := predicate["block"]; ok {
		block := object(v, "block predicate")
		unknownKeys(block, "block predicate", "blocks", "state", "nbt")
		switch blocks := block["blocks"].(type) {
		case nil:
		case string:
			if tag, isTag := strings.CutPrefix(blocks, "#"); isTag {
				out.Block = "#" + registry.Qualify(tag)
			} else {
				out.Block = registry.Qualify(blocks)
			}
		default:
			fail("a block predicate with a list of blocks is not supported by this server")
		}
		if out.Block != "" {
			out.Properties = stateProperties(block["state"])
		} else if _, has := block["state"]; has {
			fail("a block predicate with a state but no blocks is not supported by this server")
		}
		if text, has := block["nbt"]; has {
			compound, err := nbt.ParseOrderedSNBT(str(text, "block nbt"))
			if err != nil {
				fail("Failed to parse block nbt: %v", err)
			}
			out.BlockNBT = compound
		}
	}
	if raw := predicate["biomes"]; raw != nil {
		out.Biomes = holderSet(raw, "worldgen/biome", registry.BiomeTagMembers)
	}
	if raw := predicate["structures"]; raw != nil {
		out.Structures = holderSet(raw, "worldgen/structure", registry.StructureTagMembers)
	}
	if raw, ok := predicate["dimension"]; ok {
		out.Dimension = registry.Qualify(str(raw, "dimension"))
	}
	if _, ok := predicate["smokey"]; ok {
		out.Smokey = new(requireFlag(predicate, "smokey"))
	}
	if _, ok := predicate["can_see_sky"]; ok {
		out.CanSeeSky = new(requireFlag(predicate, "can_see_sky"))
	}
	if v, ok := predicate["light"]; ok {
		light := object(v, "light predicate")
		unknownKeys(light, "light predicate", "light")
		r := doubleRange(light, "light")
		out.Light = &r
	}
	if v, ok := predicate["fluid"]; ok {
		fluid := object(v, "fluid predicate")
		unknownKeys(fluid, "fluid predicate", "fluids", "state")
		out.Fluid = &registry.LootFluidCheck{Properties: stateProperties(fluid["state"])}
		if raw := fluid["fluids"]; raw != nil {
			out.Fluid.Fluids = holderSet(raw, "fluid", fluidTagMembers)
		}
	}
	return out
}

// fluidTagMembers lists the fluids of the two fluid tags.
// vanilla: FluidTags.WATER, FluidTags.LAVA
func fluidTagMembers(tag string) ([]string, bool) {
	switch registry.Qualify(tag) {
	case "minecraft:water":
		return []string{"minecraft:water", "minecraft:flowing_water"}, true
	case "minecraft:lava":
		return []string{"minecraft:lava", "minecraft:flowing_lava"}, true
	}
	return nil, false
}

// playerPredicate reads PlayerPredicate.
// vanilla: PlayerPredicate.CODEC
func playerPredicate(o obj) *registry.LootPlayerPredicate {
	unknownKeys(o, "player predicate", "level", "food", "gamemode", "stats", "recipes", "advancements", "looking_at", "input")
	out := &registry.LootPlayerPredicate{}
	out.LevelMin, out.LevelMax = intRange(o["level"])
	if v, ok := o["food"]; ok {
		food := object(v, "food")
		unknownKeys(food, "food predicate", "level", "saturation")
		out.FoodMin, out.FoodMax = intRange(food["level"])
		out.SaturationMin, out.SaturationMax = intRange(food["saturation"])
	}
	if v, ok := o["gamemode"]; ok {
		out.GameModes = []string{}
		for _, m := range list(v, "gamemode") {
			mode := str(m, "gamemode")
			if !slices.Contains([]string{"survival", "creative", "adventure", "spectator"}, mode) {
				fail("unknown game type %s", mode)
			}
			out.GameModes = append(out.GameModes, mode)
		}
	}
	for _, sv := range list(o["stats"], "stats") {
		so := object(sv, "stat matcher")
		unknownKeys(so, "stat matcher", "type", "stat", "value")
		st := registry.LootStatPredicate{Type: registry.Qualify(requireStr(so, "type")), Stat: registry.Qualify(requireStr(so, "stat"))}
		if _, _, ok := registry.StatID(st.Type, st.Stat); !ok {
			fail("unknown statistic %s of type %s", st.Stat, st.Type)
		}
		st.Min, st.Max = intRange(so["value"])
		out.Stats = append(out.Stats, st)
	}
	if v, ok := o["recipes"]; ok {
		out.Recipes = map[string]bool{}
		for name := range object(v, "recipes") {
			out.Recipes[registry.Qualify(name)] = requireFlag(object(v, "recipes"), name)
		}
	}
	if v, ok := o["advancements"]; ok {
		out.Advancements = map[string]registry.LootAdvancementPredicate{}
		for name, av := range object(v, "advancements") {
			if criteria, isMap := av.(obj); isMap {
				ap := registry.LootAdvancementPredicate{Criteria: map[string]bool{}}
				for c := range criteria {
					ap.Criteria[c] = requireFlag(criteria, c)
				}
				out.Advancements[registry.Qualify(name)] = ap
				continue
			}
			b, isBool := av.(bool)
			if !isBool {
				fail("advancement %s is neither a boolean nor a criteria map", name)
			}
			out.Advancements[registry.Qualify(name)] = registry.LootAdvancementPredicate{Done: b}
		}
	}
	if v, ok := o["looking_at"]; ok {
		nested := entityPredicate(object(v, "looking_at"))
		out.LookingAt = &nested
	}
	if v, ok := o["input"]; ok {
		input := object(v, "input")
		unknownKeys(input, "input predicate", "forward", "backward", "left", "right", "jump", "sneak", "sprint")
		out.Input = &registry.LootInputPredicate{}
		for key, dst := range map[string]**bool{
			"forward": &out.Input.Forward, "backward": &out.Input.Backward, "left": &out.Input.Left, "right": &out.Input.Right,
			"jump": &out.Input.Jump, "sneak": &out.Input.Sneak, "sprint": &out.Input.Sprint,
		} {
			if _, has := input[key]; has {
				*dst = new(flag(input, key, false))
			}
		}
	}
	return out
}

// SlotRangeKnown says whether a slot range name is one /item names; the
// command package sets it. A nil one accepts every name.
// vanilla: SlotRanges.CODEC
var SlotRangeKnown func(name string) bool

// doubleRange reads the bound key of o: one number or a min/max object,
// open when absent.
func doubleRange(o obj, key string) registry.AdvancementRange {
	if v, ok := o[key]; ok {
		return advDoubleRange(key, v)
	}
	return registry.AdvancementRange{}
}

func entityPredicate(p obj) registry.LootEntityPredicate {
	unknownKeys(p, "entity predicate", "minecraft:flags", "minecraft:vehicle", "minecraft:components",
		"minecraft:type_specific/sheep", "minecraft:type_specific/cube_mob", "minecraft:type_specific/raider", "minecraft:entity_type", "minecraft:equipment",
		"minecraft:type_specific/player", "minecraft:type_specific/lightning", "minecraft:type_specific/fishing_hook", "minecraft:predicates",
		"minecraft:effects", "minecraft:nbt", "minecraft:team", "minecraft:entity_tags", "minecraft:periodic_tick", "minecraft:passenger", "minecraft:targeted_entity",
		"minecraft:location", "minecraft:stepping_on", "minecraft:movement_affected_by", "minecraft:distance", "minecraft:movement", "minecraft:slots")
	var out registry.LootEntityPredicate
	if v, ok := p["minecraft:flags"]; ok {
		flags := object(v, "flags")
		unknownKeys(flags, "entity flags", "is_on_ground", "is_on_fire", "is_sneaking", "is_sprinting", "is_swimming", "is_flying", "is_baby", "is_in_water", "is_fall_flying")
		for key, dst := range map[string]**bool{
			"is_on_ground": &out.OnGround, "is_on_fire": &out.OnFire, "is_sneaking": &out.Sneaking,
			"is_sprinting": &out.Sprinting, "is_swimming": &out.Swimming, "is_flying": &out.Flying,
			"is_baby": &out.Baby, "is_in_water": &out.InWater, "is_fall_flying": &out.FallFlying,
		} {
			if _, has := flags[key]; has {
				*dst = new(flag(flags, key, false))
			}
		}
	}
	for key, dst := range map[string]**registry.LootEntityPredicate{
		"minecraft:vehicle": &out.VehiclePredicate, "minecraft:passenger": &out.Passenger, "minecraft:targeted_entity": &out.Targeted,
	} {
		if v, ok := p[key]; ok {
			obj := object(v, key)
			nested := entityPredicate(obj)
			if key == "minecraft:vehicle" && len(obj) == 1 && nested.Type != "" {
				// The generated tables write a plain vehicle type this way.
				out.Vehicle = nested.Type
				continue
			}
			*dst = &nested
		}
	}
	for key, dst := range map[string]**registry.LootLocationCheck{
		"minecraft:location": &out.Location, "minecraft:stepping_on": &out.SteppingOn, "minecraft:movement_affected_by": &out.MovementAffectedBy,
	} {
		if v, ok := p[key]; ok {
			loc := locationPredicate(object(v, key))
			*dst = &loc
		}
	}
	if v, ok := p["minecraft:distance"]; ok {
		d := object(v, "distance")
		unknownKeys(d, "distance predicate", "x", "y", "z", "horizontal", "absolute")
		out.Distance = &registry.AdvancementDistance{X: doubleRange(d, "x"), Y: doubleRange(d, "y"), Z: doubleRange(d, "z"),
			Horizontal: doubleRange(d, "horizontal"), Absolute: doubleRange(d, "absolute")}
	}
	if v, ok := p["minecraft:movement"]; ok {
		m := object(v, "movement")
		unknownKeys(m, "movement predicate", "x", "y", "z", "speed", "horizontal_speed", "vertical_speed", "fall_distance")
		out.Movement = &registry.LootMovement{X: doubleRange(m, "x"), Y: doubleRange(m, "y"), Z: doubleRange(m, "z"), Speed: doubleRange(m, "speed"),
			HorizontalSpeed: doubleRange(m, "horizontal_speed"), VerticalSpeed: doubleRange(m, "vertical_speed"), FallDistance: doubleRange(m, "fall_distance")}
	}
	if v, ok := p["minecraft:slots"]; ok {
		out.Slots = []registry.LootSlotPredicate{}
		for name, item := range object(v, "slots") {
			if SlotRangeKnown != nil && !SlotRangeKnown(name) {
				fail("unknown slot range %s", name)
			}
			out.Slots = append(out.Slots, registry.LootSlotPredicate{Slots: name, Item: itemPredicate(object(item, "item predicate"))})
		}
		slices.SortFunc(out.Slots, func(a, b registry.LootSlotPredicate) int { return strings.Compare(a.Slots, b.Slots) })
	}
	if v, ok := p["minecraft:effects"]; ok {
		out.Effects = lootEffects(v)
	}
	if v, ok := p["minecraft:nbt"]; ok {
		text, isStr := v.(string)
		if !isStr {
			fail("nbt predicate is not a string")
		}
		compound, err := nbt.ParseOrderedSNBT(text)
		if err != nil {
			fail("Failed to parse nbt predicate: %v", err)
		}
		out.NBT = compound
	}
	if v, ok := p["minecraft:team"]; ok {
		out.Team, out.HasTeam = str(v, "team"), true
	}
	if v, ok := p["minecraft:entity_tags"]; ok {
		tags := object(v, "entity tags")
		unknownKeys(tags, "entity tags", "any_of", "all_of", "none_of")
		read := func(key string) []string {
			var names []string
			if lv, has := tags[key]; has {
				names = []string{}
				for _, n := range list(lv, key) {
					names = append(names, str(n, key))
				}
			}
			return names
		}
		out.TagsAny, out.TagsAll, out.TagsNone, out.HasTags = read("any_of"), read("all_of"), read("none_of"), true
	}
	if v, ok := p["minecraft:periodic_tick"]; ok {
		f, isNum := num(v)
		if !isNum || f < 1 {
			fail("periodic_tick must be a positive integer")
		}
		out.Period = int32(f)
	}
	if v, ok := p["minecraft:components"]; ok {
		components := object(v, "components")
		out.Components = map[string]string{}
		for name, value := range components {
			if _, held := registry.LootEntityTagComponents[name]; held {
				tag, err := protocol.JSONValueTag(value)
				if err != nil {
					fail("entity component %s: %v", name, err)
				}
				if out.TagComponents == nil {
					out.TagComponents = map[string]nbt.Tag{}
				}
				out.TagComponents[name] = tag
				continue
			}
			if !slices.Contains(registry.LootEntityComponents[:], name) {
				fail("entity component %s is not modelled", name)
			}
			out.Components[name] = str(value, name)
			if strings.Contains(name, "variant") && !strings.HasPrefix(name, "minecraft:mooshroom") {
				out.Components[name] = registry.Qualify(out.Components[name])
			}
		}
	}
	if v, ok := p["minecraft:predicates"]; ok {
		out.ComponentTests = componentTests(object(v, "predicates"))
	}
	if v, ok := p["minecraft:type_specific/player"]; ok {
		out.Player = playerPredicate(object(v, "player predicate"))
	}
	if v, ok := p["minecraft:type_specific/lightning"]; ok {
		bolt := object(v, "lightning predicate")
		unknownKeys(bolt, "lightning predicate", "blocks_set_on_fire", "entity_struck")
		out.Lightning = &registry.LootLightningPredicate{}
		out.Lightning.FiredMin, out.Lightning.FiredMax = intRange(bolt["blocks_set_on_fire"])
		if struck, has := bolt["entity_struck"]; has {
			nested := entityPredicate(object(struck, "entity_struck"))
			out.Lightning.Struck = &nested
		}
	}
	if v, ok := p["minecraft:type_specific/fishing_hook"]; ok {
		hook := object(v, "fishing hook predicate")
		unknownKeys(hook, "fishing hook predicate", "in_open_water")
		out.FishingHook = &registry.LootFishingHookPredicate{}
		if _, has := hook["in_open_water"]; has {
			out.FishingHook.InOpenWater = new(flag(hook, "in_open_water", false))
		}
	}
	if v, ok := p["minecraft:type_specific/sheep"]; ok {
		sheep := object(v, "sheep predicate")
		unknownKeys(sheep, "sheep predicate", "sheared")
		out.Sheared = new(flag(sheep, "sheared", false))
	}
	if v, ok := p["minecraft:type_specific/raider"]; ok {
		raider := object(v, "raider predicate")
		unknownKeys(raider, "raider predicate", "has_raid", "is_captain")
		out.Captain = new(flag(raider, "is_captain", false))
		out.HasRaid = new(flag(raider, "has_raid", false))
	}
	if v, ok := p["minecraft:type_specific/cube_mob"]; ok {
		cube := object(v, "cube mob predicate")
		unknownKeys(cube, "cube mob predicate", "size")
		out.CubeSizeMin, out.CubeSizeMax = intRange(cube["size"])
	}
	if v, ok := p["minecraft:entity_type"]; ok {
		typ := str(v, "entity type")
		if tag, isTag := strings.CutPrefix(typ, "#"); isTag {
			out.Types = tagMembers("entity type", tag, registry.EntityTypeTagMembers)
		} else {
			out.Type = registry.Qualify(typ)
		}
	}
	if v, ok := p["minecraft:equipment"]; ok {
		equipment := object(v, "equipment")
		unknownKeys(equipment, "equipment", "head", "chest", "legs", "feet", "body", "mainhand", "offhand")
		for key, dst := range map[string]**registry.LootItemPredicate{
			"head": &out.Head, "chest": &out.Chest, "legs": &out.Legs, "feet": &out.Feet,
			"body": &out.Body, "mainhand": &out.Mainhand, "offhand": &out.Offhand,
		} {
			if slot, has := equipment[key]; has {
				pred := itemPredicate(object(slot, key))
				*dst = &pred
			}
		}
	}
	return out
}

// intRange reads an IntRange: absent ends stay nil.
func intRange(v any) (lo, hi *float64) {
	if v == nil {
		return nil, nil
	}
	if f, ok := num(v); ok {
		return new(f), new(f)
	}
	o := object(v, "range")
	if _, ok := o["min"]; ok {
		lo = new(requireNum(o, "min"))
	}
	if _, ok := o["max"]; ok {
		hi = new(requireNum(o, "max"))
	}
	return lo, hi
}

// intLimit reads a limit_count's bounds, context int providers; absent ends
// stay nil.
// vanilla: IntLimit.CODEC
func intLimit(o obj) (lo, hi registry.LootNumber) {
	bound := func(key string) registry.LootNumber {
		if v, ok := o[key]; ok {
			return intProvider(v)
		}
		return nil
	}
	return bound("min"), bound("max")
}

// lootFunctionTypes is every loot function type the game registers, which
// tells a type this server leaves out from one that does not exist.
// vanilla: LootItemFunctions.bootstrap
var lootFunctionTypes = []string{
	"set_count", "set_item", "enchant_with_levels", "enchant_randomly",
	"set_enchantments", "set_custom_data", "set_components", "furnace_smelt",
	"enchanted_count_increase", "set_damage", "set_attributes", "set_name",
	"exploration_map", "set_stew_effect", "copy_name", "set_contents",
	"modify_contents", "filtered", "limit_count", "apply_bonus",
	"set_loot_table", "explosion_decay", "set_lore", "fill_player_head",
	"copy_custom_data", "copy_state", "set_banner_pattern", "set_potion",
	"set_random_dyes", "set_random_potion", "set_instrument", "reference",
	"sequence", "copy_components", "set_fireworks", "set_firework_explosion",
	"set_book_cover", "set_written_book_pages", "set_writable_book_pages",
	"toggle_tooltips", "set_ominous_bottle_amplifier", "set_custom_model_data",
	"discard",
}

func (d lootDecoder) functions(v any) []registry.LootFunction {
	var out []registry.LootFunction
	for _, f := range list(v, "functions") {
		out = append(out, d.function(object(f, "function")))
	}
	return out
}

// holderName reads a holder's registry name; a tag names a holder set,
// which this server does not model.
func holderName(name string) string {
	if strings.HasPrefix(name, "#") {
		fail("a holder set %s is not supported by this server", name)
	}
	return registry.Qualify(name)
}

// modifiers reads the optional modifier holder of a table, a pool or an
// entry as the functions it runs in order. A pack in the earlier format
// lists them under functions instead, which is still read.
// vanilla: LootItemFunctions.CODEC.optionalFieldOf("modifier")
func (d lootDecoder) modifiers(o obj) []registry.LootFunction {
	v, ok := o["modifier"]
	if !ok {
		if !d.inlineLootTable {
			return d.functions(o["functions"])
		}
		return nil
	}
	if d.inlineLootTable {
		defer partialLoot()
	}
	fn := d.modifierHolder(v)
	if seq, ok := fn.(registry.LootFunctionSequence); ok {
		return seq.Functions
	}
	return []registry.LootFunction{fn}
}

// modifierHolder reads an item modifier's name, resolved when applied, an
// inline function, or a list of holders run in order.
// vanilla: LootItemFunctions.CODEC, SequenceFunction.INLINE_CODEC
func (d lootDecoder) modifierHolder(v any) registry.LootFunction {
	switch h := v.(type) {
	case string:
		return registry.LootFunctionReference{Name: holderName(h)}
	case []any:
		return registry.LootFunctionSequence{Functions: d.modifierHolders(h)}
	}
	if d.inlineLootTable {
		defer modifierAlternatives(v, true)
	}
	return d.function(object(v, "modifier"))
}

// modifierAlternatives retains the failed branches of the direct and holder-set
// codecs. A list's partial result already supplies its element's error.
// vanilla: LootItemFunctions.DIRECT_CODEC, HolderSetCodec.directCodec
func modifierAlternatives(v any, direct bool) {
	r := recover()
	if r == nil {
		return
	}
	failure, ok := r.(lootFailure)
	if !ok {
		panic(r)
	}
	if failure.partial {
		panic(failure)
	}
	if _, list := v.([]any); list {
		failure.partial = true
		panic(failure)
	}
	msg := fmt.Sprintf("Failed to parse either. First: Not a string; Second: Failed to parse either. First: Not a list: %s; Second: %s", snbt(v), failure.msg)
	if direct {
		msg = fmt.Sprintf("Failed to parse either. First: %s; Second: %s", failure.msg, msg)
	}
	panic(lootFailure{msg: msg})
}

// modifierHolders reads a list of item modifier holders, each a name or an
// inline typed function.
// vanilla: LootItemFunctions.LIST_CODEC
func (d lootDecoder) modifierHolders(v any) []registry.LootFunction {
	if d.inlineLootTable {
		if _, name := v.(string); !name {
			defer modifierAlternatives(v, false)
		}
	}
	var out []registry.LootFunction
	for _, f := range holderList(v) {
		if name, ok := f.(string); ok {
			out = append(out, registry.LootFunctionReference{Name: holderName(name)})
			continue
		}
		out = append(out, d.function(object(f, "function")))
	}
	return out
}

func enchantOptions(f obj) (names []string, tag string) {
	v, ok := f["options"]
	if !ok {
		return nil, ""
	}
	names = holderSet(v, "enchantment", registry.EnchantmentTagMembers)
	if s, isTag := v.(string); isTag {
		if name, ok := strings.CutPrefix(s, "#"); ok {
			// The members are read when the function runs, so a reload of the tag applies.
			return nil, registry.Qualify(name)
		}
	}
	return names, ""
}

// function reads one item function of the set tools/gen loot reads.
// vanilla: LootItemFunctions
func (d lootDecoder) function(f obj) registry.LootFunction {
	// A pack in the earlier format keys the type as function.
	key := "type"
	if _, ok := f[key]; !ok && d.legacyFunctions {
		if _, ok := f["function"]; ok {
			key = "function"
		}
	}
	typeName, ok := required(f, key).(string)
	if !ok {
		fail("Not a string")
	}
	kind := registry.Qualify(typeName)
	if !slices.Contains(lootFunctionTypes, strings.TrimPrefix(kind, "minecraft:")) {
		fail("Unknown registry key in ResourceKey[minecraft:root / minecraft:loot_function_type]: %s", kind)
	}
	if kind == "minecraft:sequence" {
		return registry.LootFunctionSequence{Functions: d.modifierHolders(f["functions"])}
	}
	conds := conditions(f)
	switch kind {
	case "minecraft:discard":
		return registry.LootDiscard{Conditions: conds}
	case "minecraft:set_item":
		name := registry.Qualify(requireStr(f, "item"))
		if _, ok := registry.ItemID(name); !ok {
			fail("Unknown registry key in ResourceKey[minecraft:root / minecraft:item]: %s", name)
		}
		return registry.LootSetItem{Item: name, Conditions: conds}
	case "minecraft:set_name":
		return setName(f, conds)
	case "minecraft:set_lore":
		unknownKeys(f, "set_lore", key, "condition", "conditions", "lore", "mode", "offset", "size", "entity")
		var lines []protocol.Text
		for _, l := range list(required(f, "lore"), "lore") {
			lines = append(lines, textComponent(l))
		}
		if len(lines) > 256 {
			fail("List is too long: %d > 256", len(lines))
		}
		return registry.LootSetLore{Lines: lines, Operation: listOperation(f, 256), Entity: textEntity(f), Conditions: conds}
	case "minecraft:set_custom_data":
		tag, ok := required(f, "tag").(string)
		if !ok {
			fail("Not a string")
		}
		compound, err := nbt.ParseOrderedSNBT(tag)
		if err != nil {
			fail("Failed to parse tag: %v", err)
		}
		return registry.LootSetCustomData{Tag: compound, Conditions: conds}
	case "minecraft:toggle_tooltips":
		toggles := map[string]bool{}
		for k, v := range object(required(f, "toggles"), "toggles") {
			name := registry.Qualify(k)
			if _, ok := registry.ComponentID(name); !ok {
				fail("data component %s is not supported by this server", name)
			}
			shown, isBool := v.(bool)
			if !isBool {
				fail("Not a boolean: %v", v)
			}
			toggles[name] = shown
		}
		return registry.LootToggleTooltips{Toggles: toggles, Conditions: conds}
	case "minecraft:copy_name":
		source := requireStr(f, "source")
		switch source {
		case "this", "attacker", "direct_attacker", "attacking_player", "target_entity", "interacting_entity", "block_entity":
		default:
			fail("Unknown context source %q", source)
		}
		return registry.LootCopyName{Source: source, Conditions: conds}
	case "minecraft:set_attributes":
		var modifiers []registry.LootAttributeModifier
		for _, m := range list(required(f, "modifiers"), "modifiers") {
			modifiers = append(modifiers, attributeModifier(object(m, "modifier")))
		}
		return registry.LootSetAttributes{Modifiers: modifiers, Replace: flag(f, "replace", true), Conditions: conds}
	case "minecraft:reference":
		return registry.LootFunctionReference{Name: registry.Qualify(requireStr(f, "name")), Conditions: conds}
	case "minecraft:set_count":
		return registry.LootSetCount{Count: intAt(f, "count"), Add: flag(f, "add", false), Conditions: conds}
	case "minecraft:apply_bonus":
		return registry.LootApplyBonus{Enchantment: registry.Qualify(requireStr(f, "enchantment")), Formula: bonusFormula(f), Conditions: conds}
	case "minecraft:explosion_decay":
		return registry.LootExplosionDecay{Conditions: conds}
	case "minecraft:limit_count":
		lo, hi := intLimit(object(valueOr(f, "limit", obj{}), "limit"))
		return registry.LootLimitCount{Min: lo, Max: hi, Conditions: conds}
	case "minecraft:enchanted_count_increase":
		return registry.LootEnchantedCount{
			Enchantment: registry.Qualify(requireStr(f, "enchantment")), Count: floatAt(f, "count"),
			Limit: int32(optNum(f, "limit", 0)), Conditions: conds,
		}
	case "minecraft:furnace_smelt":
		return registry.LootFurnaceSmelt{UseInputCount: flag(f, "use_input_count", true), Conditions: conds}
	case "minecraft:copy_components":
		src := requireStr(f, "source")
		switch src {
		case "block_entity", "tool", "this", "attacker", "direct_attacker", "attacking_player", "target_entity", "interacting_entity":
		default:
			fail("Unknown loot context parameter: %s", src)
		}
		if src == "block_entity" {
			src = "" // the generated tables leave the default source empty
		}
		return registry.LootCopyComponents{Source: src, Include: includeList(f), Exclude: stringList(f["exclude"]), Conditions: conds}
	case "minecraft:set_random_dyes":
		return registry.LootSetRandomDyes{Count: intAt(f, "number_of_dyes"), Conditions: conds}
	case "minecraft:set_random_potion":
		var options []string
		if v, ok := f["options"]; ok {
			options = holderSet(v, "potion", registry.PotionTagMembers)
		}
		return registry.LootSetRandomPotion{Options: options, Conditions: conds}
	case "minecraft:set_potion":
		return registry.LootSetPotion{Potion: registry.Qualify(requireStr(f, "id")), Conditions: conds}
	case "minecraft:set_ominous_bottle_amplifier":
		return registry.LootSetOminousBottleAmplifier{Amplifier: intAt(f, "amplifier"), Conditions: conds}
	case "minecraft:set_stew_effect":
		effects := []registry.LootStewEffect{}
		for _, e := range list(f["effects"], "effects") {
			o := object(e, "a stew effect")
			effects = append(effects, registry.LootStewEffect{
				Effect: registry.Qualify(requireStr(o, "type")), Duration: intAt(o, "duration"),
			})
		}
		return registry.LootSetStewEffect{Effects: effects, Conditions: conds}
	case "minecraft:exploration_map":
		// The defaults are ExplorationMapFunction's DEFAULT_* constants;
		// destination is a holder set, where a pack in the earlier format names a
		// bare tag key, read as one when no structure has that name.
		decoration := "minecraft:mansion"
		var destination any = "#minecraft:on_treasure_maps"
		if v, ok := f["destination"]; ok {
			destination = v
			if s, isStr := v.(string); isStr && !strings.HasPrefix(s, "#") && !registry.StructureExists(registry.Qualify(s)) {
				destination = "#" + s
			}
		}
		if _, ok := f["decoration"]; ok {
			decoration = registry.Qualify(requireStr(f, "decoration"))
		}
		return registry.LootExplorationMap{
			Destination: holderSet(destination, "structure", registry.StructureTagMembers), Decoration: decoration,
			Zoom: int32(optNum(f, "zoom", 2)), SearchRadius: int32(optNum(f, "search_radius", 50)),
			SkipExistingChunks: flag(f, "skip_existing_chunks", true), Conditions: conds,
		}
	case "minecraft:set_instrument":
		return registry.LootSetInstrument{Options: holderSet(f["options"], "instrument", registry.InstrumentTagMembers), Conditions: conds}
	case "minecraft:copy_state":
		return registry.LootCopyState{Block: registry.Qualify(requireStr(f, "block")), Properties: stringList(f["properties"]), Conditions: conds}
	case "minecraft:set_damage":
		return registry.LootSetDamage{Damage: floatAt(f, "damage"), Add: flag(f, "add", false), Conditions: conds}
	case "minecraft:set_enchantments":
		levels := map[string]registry.LootNumber{}
		for k, v := range object(f["enchantments"], "enchantments") {
			levels[registry.Qualify(k)] = intProvider(v)
		}
		return registry.LootSetEnchantments{Enchantments: levels, Add: flag(f, "add", false), Conditions: conds}
	case "minecraft:enchant_randomly":
		options, tag := enchantOptions(f)
		return registry.LootEnchantRandomly{Options: options, OptionsTag: tag, OnlyCompatible: flag(f, "only_compatible", true), IncludeAdditionalCost: flag(f, "include_additional_cost_component", false), Conditions: conds}
	case "minecraft:enchant_with_levels":
		options, tag := enchantOptions(f)
		return registry.LootEnchantWithLevels{Levels: intAt(f, "levels"), Options: options, OptionsTag: tag, IncludeAdditionalCost: flag(f, "include_additional_cost_component", false), Conditions: conds}
	case "minecraft:filtered":
		branch := func(key string) registry.LootFunction {
			if v, ok := f[key]; ok {
				if d.inlineLootTable {
					defer partialLoot()
				}
				return d.modifierHolder(v)
			}
			return nil
		}
		return registry.LootFiltered{Predicate: itemPredicate(object(f["item_filter"], "item_filter")),
			OnPass: branch("on_pass"), OnFail: branch("on_fail"), Conditions: conds}
	case "minecraft:copy_custom_data":
		return registry.LootCopyCustomData{Source: nbtSource(required(f, "source")), Ops: copyOps(f["ops"]), Conditions: conds}
	case "minecraft:set_contents":
		component := registry.Qualify(requireStr(f, "component"))
		switch component {
		case "minecraft:container", "minecraft:bundle_contents", "minecraft:charged_projectiles":
		default:
			fail("Unknown registry key in ResourceKey[minecraft:root / minecraft:data_component_type]: %s", component)
		}
		var entries []registry.LootEntry
		for _, e := range list(f["entries"], "entries") {
			entries = append(entries, d.entry(object(e, "entry")))
		}
		return registry.LootSetContents{Component: component, Entries: entries, Conditions: conds}
	case "minecraft:set_components":
		set := map[string]string{}
		var remove []string
		for k, v := range object(f["components"], "components") {
			if removed, ok := strings.CutPrefix(k, "!"); ok {
				remove = append(remove, registry.Qualify(removed))
				continue
			}
			set[registry.Qualify(k)] = snbt(v)
		}
		slices.Sort(remove)
		return registry.LootSetComponents{Set: set, Remove: remove, Conditions: conds}
	}
	if fn, ok := d.componentFunction(kind, f, conds); ok {
		return fn
	}
	fail("function %s is not supported by this server", kind)
	return nil
}

// contextNBTTargets are the context arguments an NBT provider reads.
// vanilla: LootContextArg.createArgCodec
var contextNBTTargets = []string{
	"this", "attacker", "direct_attacker", "attacking_player", "target_entity", "interacting_entity", "block_entity",
}

// nbtSource reads an NBT provider: a context target as a bare string, or a
// typed context or storage object.
// vanilla: NbtProviders.CODEC
func nbtSource(v any) registry.LootNBTSource {
	target := func(name string) registry.LootNBTSource {
		if !slices.Contains(contextNBTTargets, name) {
			fail("Invalid entity target %q", name)
		}
		return registry.LootNBTSource{Target: name}
	}
	if name, ok := v.(string); ok {
		return target(name)
	}
	o := object(v, "an NBT provider")
	switch kind := registry.Qualify(requireStr(o, "type")); kind {
	case "minecraft:context":
		return target(requireStr(o, "target"))
	case "minecraft:storage":
		return registry.LootNBTSource{Storage: registry.Qualify(requireStr(o, "source"))}
	default:
		fail("Unknown registry key in ResourceKey[minecraft:root / minecraft:loot_nbt_provider_type]: %s", kind)
	}
	return registry.LootNBTSource{}
}

// copyOps reads the operations of copy_custom_data.
// vanilla: CopyCustomDataFunction.CopyOperation.CODEC
func copyOps(v any) []registry.LootCopyOp {
	ops := []registry.LootCopyOp{}
	for _, e := range list(v, "ops") {
		o := object(e, "an operation")
		op := requireStr(o, "op")
		if op != "replace" && op != "append" && op != "merge" {
			fail("Unknown merge strategy %q", op)
		}
		ops = append(ops, registry.LootCopyOp{Source: requireStr(o, "source"), Target: requireStr(o, "target"), Op: op})
	}
	return ops
}

// setName reads set_name; a missing name leaves the stack as it was.
func setName(f obj, conds []registry.LootCondition) registry.LootFunction {
	target, ok := f["target"].(string)
	if ok && target != "custom_name" && target != "item_name" {
		fail("name target %q is not supported by this server", target)
	}
	v, ok := f["name"]
	if !ok {
		return registry.LootFunctionSequence{}
	}
	name := textComponent(v)
	return registry.LootSetName{Name: name, ItemName: target == "item_name", Entity: textEntity(f), Conditions: conds}
}

// textEntity reads the optional entity a text function resolves its
// selectors against.
// vanilla: LootContextArg.ENTITY_CODEC
func textEntity(f obj) string {
	v, ok := f["entity"]
	if !ok {
		return ""
	}
	name := str(v, "entity")
	if name == "block_entity" || !slices.Contains(contextNBTTargets, name) {
		fail("Invalid entity target %q", name)
	}
	return name
}

// textComponent reads a JSON text component, one this model can hold.
func textComponent(v any) protocol.Text {
	raw, _ := json.Marshal(v)
	tag, _, err := protocol.ParseTextJSON(string(raw))
	if err != nil {
		fail("not a text component: %v", err)
	}
	text, ok := protocol.RawTextOf(tag)
	if !ok {
		fail("text component this server does not support")
	}
	return text
}

// listOperation reads the mode, offset and size of a ListOperation whose
// list holds at most maxSize entries.
// vanilla: ListOperation.codec
func listOperation(f obj, maxSize int) registry.LootListOperation {
	op := registry.LootListOperation{Mode: "append", Size: -1}
	if v, ok := f["mode"]; ok {
		op.Mode = str(v, "mode")
	}
	nonNegative := func(key string) int {
		n := optNum(f, key, 0)
		if n < 0 {
			fail("Value must be non-negative: %v", n)
		}
		return int(n)
	}
	switch op.Mode {
	case "replace_all", "append":
	case "insert":
		op.Offset = nonNegative("offset")
	case "replace_section":
		op.Offset = nonNegative("offset")
		if _, ok := f["size"]; ok {
			op.Size = nonNegative("size")
			if op.Size > maxSize {
				fail("Size value too large: %d, max size is %d", op.Size, maxSize)
			}
		}
	default:
		fail("Unknown list operation mode %q", op.Mode)
	}
	return op
}

// attributeModifier reads one modifier of set_attributes.
// vanilla: SetAttributesFunction.Modifier.CODEC
func attributeModifier(m obj) registry.LootAttributeModifier {
	out := registry.LootAttributeModifier{ID: registry.Qualify(requireStr(m, "id")), Amount: floatAt(m, "amount")}
	out.Attribute = registry.Qualify(requireStr(m, "attribute"))
	if _, ok := registry.AttributeByName(out.Attribute); !ok {
		fail("Unknown registry key in ResourceKey[minecraft:root / minecraft:attribute]: %s", out.Attribute)
	}
	op, ok := registry.AttributeOperationByName(requireStr(m, "operation"))
	if !ok {
		fail("Unknown operation %q", requireStr(m, "operation"))
	}
	out.Operation = op
	names := []any{required(m, "slot")}
	if l, isList := names[0].([]any); isList {
		names = l
	}
	if len(names) == 0 {
		fail("List must have contents")
	}
	for _, n := range names {
		id, ok := registry.SlotGroupID(str(n, "slot"))
		if !ok {
			fail("Unknown equipment slot group %q", n)
		}
		out.Slots = append(out.Slots, id)
	}
	return out
}

// includeList reads copy_components' optional include: nil when absent
// (copy everything), non-nil when present, even if empty.
func includeList(f obj) []string {
	if _, ok := f["include"]; !ok {
		return nil
	}
	return append([]string{}, stringList(f["include"])...)
}

func stringList(v any) []string {
	var out []string
	for _, e := range list(v, "list") {
		out = append(out, str(e, "list entry"))
	}
	return out
}

func bonusFormula(f obj) registry.LootBonusFormula {
	switch kind := registry.Qualify(str(f["formula"], "formula")); kind {
	case "minecraft:ore_drops":
		return registry.LootOreDrops{}
	case "minecraft:uniform_bonus_count":
		params := object(valueOr(f, "parameters", obj{}), "parameters")
		return registry.LootUniformBonus{Multiplier: int32(optNum(params, "bonusMultiplier", 1))}
	case "minecraft:binomial_with_bonus_count":
		params := object(valueOr(f, "parameters", obj{}), "parameters")
		return registry.LootBinomialBonus{Extra: int32(optNum(params, "extra", 0)), Probability: optNum(params, "probability", 0)}
	default:
		fail("bonus formula %s is not supported by this server", kind)
	}
	return nil
}

// snbt writes a JSON value as the SNBT a set_components keeps it as.
func snbt(v any) string {
	switch k := v.(type) {
	case map[string]any:
		parts := make([]string, 0, len(k))
		for _, key := range sortedKeys(k) {
			quoted := key
			if strings.Trim(key, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-.+") != "" || key == "true" || key == "false" {
				b, _ := json.Marshal(key)
				quoted = string(b)
			}
			parts = append(parts, quoted+":"+snbt(k[key]))
		}
		return "{" + strings.Join(parts, ",") + "}"
	case []any:
		parts := make([]string, len(k))
		for i, e := range k {
			parts[i] = snbt(e)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case bool:
		return strconv.FormatBool(k)
	case json.Number:
		if text := k.String(); strings.ContainsAny(text, ".eE") {
			f, _ := k.Float64()
			text = strconv.FormatFloat(f, 'g', -1, 64)
			if !strings.ContainsAny(text, ".e") {
				text += ".0"
			}
			return text + "d"
		}
		return k.String()
	case string:
		quoted, _ := json.Marshal(k)
		return string(quoted)
	}
	fail("a component value of this kind is not supported by this server")
	return ""
}

func lootItems(names []string) []registry.Item {
	items := make([]registry.Item, 0, len(names))
	for _, name := range names {
		if item, ok := registry.ItemByName(name); ok {
			items = append(items, item)
		}
	}
	return items
}

// lootEffects reads a MobEffectsPredicate: effect name to the tests of its
// active instance.
// vanilla: MobEffectsPredicate.CODEC
func lootEffects(v any) map[string]registry.LootEffectPredicate {
	out := map[string]registry.LootEffectPredicate{}
	for name, ev := range object(v, "effects") {
		name = registry.Qualify(name)
		if _, known := registry.MobEffectID(name); !known {
			fail("unknown mob effect %s", name)
		}
		eo := object(ev, "effect predicate")
		unknownKeys(eo, "effect predicate", "amplifier", "duration", "ambient", "visible")
		var ep registry.LootEffectPredicate
		ep.AmplifierMin, ep.AmplifierMax = intRange(eo["amplifier"])
		ep.DurationMin, ep.DurationMax = intRange(eo["duration"])
		if _, has := eo["ambient"]; has {
			ep.Ambient = new(flag(eo, "ambient", false))
		}
		if _, has := eo["visible"]; has {
			ep.Visible = new(flag(eo, "visible", false))
		}
		out[name] = ep
	}
	return out
}
