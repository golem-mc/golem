package main

import (
	"fmt"
	"maps"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/golem-mc/golem/tools/internal/gogen"
	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func init() {
	register(generator{
		name: "advancements",
		doc:  "registry/advancements_gen.go: the advancement tree, with the conditions of the triggers the server fires",
		run:  runAdvancements,
	})
}

// Inputs: the advancement files and item tags from the data pack, item IDs from the
// data report.
//
// Of the criteria only the conditions of triggers the server fires are kept (item
// predicates with tags expanded, dimensions, biomes, entity types, block checks,
// effects, variants, heights and projectiles). A condition this generator does not
// model marks its criterion unmodelled, which never fires; one it does not know at
// all stops the run, so a newer data pack cannot drop one silently.

// advancementFrames are AdvancementType's ordinals.
// vanilla: AdvancementType
var advancementFrames = map[string]int{"task": 0, "challenge": 1, "goal": 2}

// variantComponents are the variant components an entity predicate may test,
// with the entity type that carries each.
var variantComponents = map[string]string{
	"minecraft:cat/variant":  "minecraft:cat",
	"minecraft:frog/variant": "minecraft:frog",
	"minecraft:wolf/variant": "minecraft:wolf",
}

// keysWithin reports whether every key of o is one of allowed.
func keysWithin(o obj, allowed ...string) bool {
	for k := range o {
		if !slices.Contains(allowed, k) {
			return false
		}
	}
	return true
}

// keysAre reports whether the keys of o are exactly the given ones.
func keysAre(o obj, want ...string) bool {
	return len(o) == len(want) && keysWithin(o, want...)
}

// asObjOr reads a value as an object, nil for anything else.
func asObjOr(v any) obj {
	m, _ := v.(map[string]any)
	return obj(m)
}

// jsonEqual compares two decoded JSON values.
func jsonEqual(a, b any) bool { return reflect.DeepEqual(a, b) }

type blockCheck struct {
	blocks []string
	state  map[string]string
	offset [3]float64
	// smokey requires a lit campfire's smoke to reach the cell.
	smokey bool
}

// condition is a location condition: a block check, or all or any of terms.
type condition struct {
	all, any []*condition
	isAll    bool
	isAny    bool
	check    *blockCheck
}

type advCriterion struct {
	name, trigger                  string
	items                          [][]int
	from, to                       string
	entity, variant, child, parent string
	partner, dimension, biome      string
	whereBiome                     string
	feet                           []int
	steppingOn                     []string
	structures                     []string
	where                          []*condition
	effects                        []string
	direct                         string
	smashMin                       float64
	captain                        bool
	projectile                     bool
	minDistance                    float64
	victims                        []string
	uniqueMin, uniqueMax           int
	count                          int
	minY                           *float64
	levelMin, levelMax             int
	body                           []int
	bodyUndamaged                  bool
	baby, adult                    bool
	notWearing                     []int
	unmodelled                     bool
	lootTable                      string
	recipeID, lookingAt            string
	block, enchantment             string
	bees                           int
	distance                       advDistance
	startY, playerY                advRange
	vehicle, passenger             string
	blocked                        bool
	signal, fires                  advRange
	occupied, full, empty          advRange
}

// advRange is a min/max bound over a double; a nil end is open.
type advRange struct{ min, max *float64 }

// advDistance is a DistancePredicate's bounds: x, y, z, horizontal,
// absolute.
type advDistance [5]advRange

var distanceKeys = [5]string{"x", "y", "z", "horizontal", "absolute"}

type advDisplay struct {
	title, description string
	icon, count, frame int
	background         string
	showToast          bool
	announce, hidden   bool
}

type advancement struct {
	id, parent   string
	display      *advDisplay
	criteria     []advCriterion
	requirements [][]string
	telemetry    bool
	experience   int
	recipes      []string
}

type advancementReader struct {
	items map[string]int
	dp    *vanilla.DataPack
	// jukeboxPlayable is the item IDs whose default components carry
	// minecraft:jukebox_playable, sorted.
	jukeboxPlayable []int
}

func (r advancementReader) itemID(name string) int {
	id, ok := r.items[name]
	if !ok {
		fail("no such item: %s", name)
	}
	return id
}

// expandTag resolves #namespace:tag into the item IDs it collects.
func (r advancementReader) expandTag(name string, seen []string) []int {
	if slices.Contains(seen, name) {
		return nil
	}
	bare := strings.TrimPrefix(name, "#")
	bare = bare[strings.LastIndex(bare, ":")+1:]
	file := path.Join("minecraft/tags/item", bare+".json")
	b, err := dpRead(r.dp, file)
	if err != nil {
		fail("%v", err)
	}
	var ids []int
	for _, v := range decode(b, file).list("values") {
		value := asStr(v, "a tag value")
		if strings.HasPrefix(value, "#") {
			ids = append(ids, r.expandTag(value, append(slices.Clone(seen), name))...)
		} else {
			ids = append(ids, r.itemID(value))
		}
	}
	return ids
}

func (r advancementReader) expand(value any) []int {
	switch v := value.(type) {
	case []any:
		var ids []int
		for _, x := range v {
			ids = append(ids, r.expand(x)...)
		}
		return ids
	case string:
		if strings.HasPrefix(v, "#") {
			return r.expandTag(v, nil)
		}
		return []int{r.itemID(v)}
	}
	fail("an item reference of a shape the generator does not read: %v", value)
	return nil
}

// itemPredicates reads the item predicates under conditions["items"].
func (r advancementReader) itemPredicates(where string, conditions obj) [][]int {
	var out [][]int
	for _, p := range conditions.listOr("items") {
		predicate := asObj(p, "an item predicate")
		if !keysAre(predicate, "items") {
			fail("%s: unmodelled item predicate %v", where, map[string]any(predicate))
		}
		out = append(out, sortedSet(r.expand(predicate["items"])))
	}
	return out
}

// playerHurtEntity reads a player_hurt_entity criterion: projectile damage by
// one kind of shaft, or a player's mace smash that dealt at least a damage,
// the only shapes fired; any other is unmodelled.
func (r advancementReader) playerHurtEntity(where string, conditions obj, c *advCriterion) {
	damage, _ := conditions.obj("damage")
	kind := asObjOr(damage["type"])
	direct := asObjOr(kind["direct_entity"])
	projectileTags := []any{map[string]any{"expected": true, "id": "#minecraft:is_projectile"}}
	smashTags := []any{map[string]any{"expected": true, "id": "#minecraft:mace_smash"}}
	equipment := asObjOr(direct["minecraft:equipment"])
	switch {
	case conditions.has("entity"):
		c.unmodelled = true
	case keysAre(damage, "type") && keysAre(kind, "direct_entity", "tags") && keysAre(direct, "minecraft:entity_type") &&
		jsonEqual(kind["tags"], projectileTags):
		c.direct, _ = direct["minecraft:entity_type"].(string)
	case keysAre(damage, "type", "dealt") && keysAre(kind, "direct_entity", "tags") &&
		keysAre(direct, "minecraft:entity_type", "minecraft:equipment") && keysAre(equipment, "mainhand") &&
		direct["minecraft:entity_type"] == "minecraft:player" && jsonEqual(kind["tags"], smashTags):
		c.items = r.singleItem(where, obj{"item": equipment["mainhand"]}, "item")
		dealt := doubleRange(where, "dealt", damage["dealt"])
		if dealt.min == nil || dealt.max != nil {
			fail("%s: unmodelled player_hurt_entity dealt %v", where, damage["dealt"])
		}
		c.smashMin = *dealt.min
	default:
		c.unmodelled = true
	}
}

// inventoryChanged reads an inventory_changed criterion's item predicates and
// slot counts. vanilla: InventoryChangeTrigger.TriggerInstance
func (r advancementReader) inventoryChanged(where string, conditions obj, c *advCriterion) {
	c.items = r.itemPredicates(where, conditions)
	slots, _ := conditions.obj("slots")
	if !keysWithin(slots, "occupied", "full", "empty") {
		fail("%s: unmodelled inventory_changed slots %v", where, map[string]any(slots))
	}
	for key, bound := range map[string]*advRange{"occupied": &c.occupied, "full": &c.full, "empty": &c.empty} {
		if v, ok := slots[key]; ok {
			*bound = doubleRange(where, "slots "+key, v)
		}
	}
}

// singleItem reads one item predicate condition (filled_bucket and the like).
func (r advancementReader) singleItem(where string, conditions obj, key string) [][]int {
	if !conditions.has(key) {
		return nil
	}
	return r.itemPredicates(where, obj{"items": []any{conditions[key]}})
}

type entityInfo struct{ typ, dimension, variant string }

// entityPredicate reads an entity's type and dimension (and variant when
// variants is set), or false for a predicate beyond them.
func entityPredicate(where string, contexts any, variants bool) (entityInfo, bool) {
	var out entityInfo
	if contexts == nil {
		return out, true
	}
	list, _ := contexts.([]any)
	var first obj
	if len(list) == 1 {
		first = asObjOr(list[0])
	}
	if len(list) != 1 || !keysAre(first, "condition", "entity", "predicate") ||
		first["condition"] != "minecraft:entity_properties" || first["entity"] != "this" {
		fail("%s: unknown entity condition %v", where, contexts)
	}
	predicate := asObjOr(first["predicate"])
	// A variant component names the entity type that carries it, so it
	// settles the type; an entity_type beside it must agree rather than the
	// later key winning.
	var typ string
	for _, key := range sortedKeys(predicate) {
		value := predicate[key]
		switch {
		case key == "minecraft:entity_type":
			typ, _ = value.(string)
		case key == "minecraft:location" && keysAre(asObjOr(value), "dimension"):
			out.dimension, _ = asObjOr(value)["dimension"].(string)
		case variants && key == "minecraft:components" && len(asObjOr(value)) == 1 && variantComponent(asObjOr(value)) != "":
			comp := variantComponent(asObjOr(value))
			out.variant = asObjOr(value)[comp].(string)
			out.typ = variantComponents[comp]
		default:
			return entityInfo{}, false
		}
	}
	if typ != "" {
		if out.typ != "" && out.typ != typ {
			fail("%s: an entity_type of %s beside a variant of %s", where, typ, out.typ)
		}
		out.typ = typ
	}
	return out, true
}

// captainPredicate reads an entity predicate that may also require a raid
// captain: the ominous banner on the head. Only the head item is judged,
// as Raider.isCaptain does; a predicate with other equipment is not modelled.
func captainPredicate(where string, contexts any) (e entityInfo, captain, ok bool) {
	list, _ := contexts.([]any)
	if len(list) != 1 {
		e, ok = entityPredicate(where, contexts, false)
		return e, false, ok
	}
	first := asObjOr(list[0])
	predicate := asObjOr(first["predicate"])
	equipment, has := predicate["minecraft:equipment"]
	if !has {
		e, ok = entityPredicate(where, contexts, false)
		return e, false, ok
	}
	head := asObjOr(asObjOr(equipment)["head"])
	if !keysAre(asObjOr(equipment), "head") || head["items"] != "minecraft:white_banner" {
		return entityInfo{}, false, false
	}
	rest := obj{}
	for k, v := range predicate {
		if k != "minecraft:equipment" {
			rest[k] = v
		}
	}
	stripped := obj{}
	maps.Copy(stripped, first)
	stripped["predicate"] = map[string]any(rest)
	e, ok = entityPredicate(where, []any{map[string]any(stripped)}, false)
	return e, ok, ok
}

// riderPredicate reads a player condition on the player's dimension and
// the type of its vehicle and of one of the vehicle's passengers, or false
// for one beyond them.
func riderPredicate(where string, contexts any) (dimension, vehicle, passenger string, ok bool) {
	list, _ := contexts.([]any)
	if len(list) != 1 {
		return "", "", "", false
	}
	first := asObjOr(list[0])
	predicate := asObjOr(first["predicate"])
	rest := obj{}
	for k, v := range predicate {
		if k != "minecraft:vehicle" {
			rest[k] = v
		}
	}
	stripped := obj{}
	maps.Copy(stripped, first)
	stripped["predicate"] = map[string]any(rest)
	e, ok := entityPredicate(where, []any{map[string]any(stripped)}, false)
	if !ok || e.typ != "" {
		return "", "", "", false
	}
	if !predicate.has("minecraft:vehicle") {
		return e.dimension, "", "", true
	}
	ride := asObjOr(predicate["minecraft:vehicle"])
	aboard := asObjOr(ride["minecraft:passenger"])
	vehicle, _ = ride["minecraft:entity_type"].(string)
	passenger, _ = aboard["minecraft:entity_type"].(string)
	if vehicle == "" || !keysWithin(ride, "minecraft:entity_type", "minecraft:passenger") ||
		ride.has("minecraft:passenger") && (passenger == "" || !keysAre(aboard, "minecraft:entity_type")) {
		return "", "", "", false
	}
	return e.dimension, vehicle, passenger, true
}

// bodyPredicate reads an entity predicate of a type, variant and baby flag
// with, optionally, an equipment.body item (and a damage of 0), or false for
// one beyond them.
func (r advancementReader) bodyPredicate(where string, contexts any) (e entityInfo, flags babyFlag, body []int, undamaged, ok bool) {
	contexts, flags, ok = stripBabyFlag(contexts)
	if !ok {
		return entityInfo{}, babyFlag{}, nil, false, false
	}
	list, _ := contexts.([]any)
	if len(list) != 1 {
		e, ok := entityPredicate(where, contexts, true)
		return e, flags, nil, false, ok && e.dimension == ""
	}
	first := asObjOr(list[0])
	predicate := asObjOr(first["predicate"])
	equipment, has := predicate["minecraft:equipment"]
	if !has {
		e, ok := entityPredicate(where, contexts, true)
		return e, flags, nil, false, ok && e.dimension == ""
	}
	rest := obj{}
	for k, v := range predicate {
		if k != "minecraft:equipment" {
			rest[k] = v
		}
	}
	stripped := obj{}
	maps.Copy(stripped, first)
	stripped["predicate"] = map[string]any(rest)
	e, ok = entityPredicate(where, []any{map[string]any(stripped)}, true)
	slot := asObjOr(asObjOr(equipment)["body"])
	components := asObjOr(slot["components"])
	if !ok || e.dimension != "" || !keysAre(asObjOr(equipment), "body") || !keysWithin(slot, "items", "components") ||
		!slot.has("items") || !keysWithin(components, "minecraft:damage") {
		return entityInfo{}, babyFlag{}, nil, false, false
	}
	if components.has("minecraft:damage") {
		if damage, isNum := asNum(components["minecraft:damage"]); !isNum || damage != 0 {
			return entityInfo{}, babyFlag{}, nil, false, false
		}
		undamaged = true
	}
	return e, flags, sortedSet(r.expand(slot["items"])), undamaged, true
}

// babyFlag is what flags of is_baby alone want: a baby, or an adult.
type babyFlag struct{ baby, adult bool }

// stripBabyFlag takes flags of is_baby alone off an entity predicate; ok is
// false for any other flags.
func stripBabyFlag(contexts any) (rest any, flag babyFlag, ok bool) {
	list, _ := contexts.([]any)
	if len(list) != 1 {
		return contexts, flag, true
	}
	first := asObjOr(list[0])
	predicate := asObjOr(first["predicate"])
	flags, has := predicate["minecraft:flags"]
	switch {
	case !has:
		return contexts, flag, true
	case jsonEqual(flags, map[string]any{"is_baby": true}):
		flag.baby = true
	case jsonEqual(flags, map[string]any{"is_baby": false}):
		flag.adult = true
	default:
		return nil, flag, false
	}
	without := maps.Clone(predicate)
	delete(without, "minecraft:flags")
	stripped := maps.Clone(first)
	stripped["predicate"] = map[string]any(without)
	return []any{map[string]any(stripped)}, flag, true
}

// adultPredicate reads an entity predicate that may also want the entity
// not to be a baby (flags of is_baby false alone).
func adultPredicate(where string, contexts any) (e entityInfo, adult, ok bool) {
	contexts, flag, ok := stripBabyFlag(contexts)
	if !ok || flag.baby {
		return entityInfo{}, false, false
	}
	e, ok = entityPredicate(where, contexts, false)
	return e, flag.adult, ok
}

// playerNotWearing reads a criterion's player condition, if any, as
// notWearing.
func (r advancementReader) playerNotWearing(conditions obj, c *advCriterion) {
	if conditions.has("player") {
		worn, ok := r.notWearing(conditions["player"])
		c.notWearing, c.unmodelled = worn, c.unmodelled || !ok
	}
}

// notWearingSlots are the armor slots a notWearing condition must cover.
var notWearingSlots = []string{"chest", "feet", "head", "legs"}

// notWearing reads a player condition that the player wear an item of one
// set in none of its armor slots: an inverted any_of of one equipment slot
// each, covering the four with the same items. ok is false for any other
// shape.
func (r advancementReader) notWearing(contexts any) (items []int, ok bool) {
	list, _ := contexts.([]any)
	if len(list) != 1 {
		return nil, false
	}
	inverted := asObjOr(list[0])
	anyOf := asObjOr(inverted["term"])
	if !keysAre(inverted, "condition", "term") || inverted["condition"] != "minecraft:inverted" ||
		!keysAre(anyOf, "condition", "terms") || anyOf["condition"] != "minecraft:any_of" {
		return nil, false
	}
	var slots []string
	for i, t := range anyOf.listOr("terms") {
		cond := asObjOr(t)
		predicate := asObjOr(cond["predicate"])
		equipment := asObjOr(predicate["minecraft:equipment"])
		if !keysAre(cond, "condition", "entity", "predicate") || cond["condition"] != "minecraft:entity_properties" ||
			cond["entity"] != "this" || !keysAre(predicate, "minecraft:equipment") || len(equipment) != 1 {
			return nil, false
		}
		for slot, v := range equipment {
			p := asObjOr(v)
			got := sortedSet(r.expand(p["items"]))
			if !keysAre(p, "items") || i > 0 && !slices.Equal(got, items) {
				return nil, false
			}
			slots, items = append(slots, slot), got
		}
	}
	slices.Sort(slots)
	return items, slices.Equal(slots, notWearingSlots)
}

// killingBlowDirect reads a killing_blow predicate: none, or projectile
// damage by one entity type. ok is false for any other shape.
func killingBlowDirect(blow any) (direct string, projectile, ok bool) {
	if blow == nil {
		return "", false, true
	}
	predicate := asObjOr(blow)
	projectileTags := []any{map[string]any{"expected": true, "id": "#minecraft:is_projectile"}}
	if keysAre(predicate, "tags") && jsonEqual(predicate["tags"], projectileTags) {
		return "", true, true
	}
	direct, _ = asObjOr(predicate["direct_entity"])["minecraft:entity_type"].(string)
	ok = keysAre(predicate, "direct_entity", "tags") && keysAre(asObjOr(predicate["direct_entity"]), "minecraft:entity_type") &&
		jsonEqual(predicate["tags"], projectileTags) && direct != ""
	return direct, false, ok
}

// horizontalDistance takes a minimum horizontal distance out of a single
// entity_properties condition's predicate, returning the contexts without it.
// vanilla: DistancePredicate.matches
func horizontalDistance(contexts any) (rest any, min float64, ok bool) {
	list, _ := contexts.([]any)
	if len(list) != 1 {
		return contexts, 0, true
	}
	first := asObjOr(list[0])
	predicate := asObjOr(first["predicate"])
	dist, has := predicate["minecraft:distance"]
	if !has {
		return contexts, 0, true
	}
	d := asObjOr(dist)
	h := asObjOr(d["horizontal"])
	min, isNum := h["min"].(float64)
	if !keysAre(d, "horizontal") || !keysAre(h, "min") || !isNum {
		return contexts, 0, false
	}
	stripped := obj{}
	maps.Copy(stripped, predicate)
	delete(stripped, "minecraft:distance")
	copied := obj{}
	maps.Copy(copied, first)
	copied["predicate"] = map[string]any(stripped)
	return []any{map[string]any(copied)}, min, true
}

// variantComponent is the one key of a components object when it names a
// variantComponents entry held as a string.
func variantComponent(components obj) string {
	for k, v := range components {
		if _, known := variantComponents[k]; !known {
			return ""
		}
		if _, isString := v.(string); !isString {
			return ""
		}
		return k
	}
	return ""
}

func newBlockCheck(blocks any, state obj, offset [3]float64) *blockCheck {
	var names []string
	switch v := blocks.(type) {
	case string:
		names = []string{v}
	case []any:
		for _, x := range v {
			names = append(names, asStr(x, "a block"))
		}
	default:
		fail("blocks of a shape the generator does not read: %v", blocks)
	}
	values := map[string]string{}
	for k, v := range state {
		s, ok := v.(string)
		if !ok {
			return nil
		}
		values[k] = s
	}
	return &blockCheck{blocks: names, state: values, offset: offset}
}

// locationCondition reads a loot condition over the blocks around a position,
// or nil for one beyond block checks.
func locationCondition(where string, cond obj) *condition {
	switch kind := cond.str("condition"); kind {
	case "minecraft:all_of", "minecraft:any_of":
		var terms []*condition
		for _, t := range cond.list("terms") {
			term := locationCondition(where, asObj(t, "a condition term"))
			if term == nil {
				return nil
			}
			terms = append(terms, term)
		}
		if kind == "minecraft:all_of" {
			return &condition{all: terms, isAll: true}
		}
		return &condition{any: terms, isAny: true}
	case "minecraft:block_state_property":
		if !keysWithin(cond, "condition", "block", "properties") {
			fail("%s: unknown block_state_property %v", where, map[string]any(cond))
		}
		props, _ := cond.obj("properties")
		check := newBlockCheck(cond.field("block"), props, [3]float64{})
		if check == nil {
			return nil
		}
		return &condition{check: check}
	case "minecraft:match_block":
		if !keysWithin(cond, "condition", "blocks", "state") {
			fail("%s: unknown match_block %v", where, cond)
		}
		props, _ := cond.obj("state")
		check := newBlockCheck(cond.field("blocks"), props, [3]float64{})
		if check == nil {
			return nil
		}
		return &condition{check: check}
	case "minecraft:location_check":
		if !keysWithin(cond, "condition", "predicate", "offsetX", "offsetY", "offsetZ") {
			fail("%s: unknown location_check %v", where, map[string]any(cond))
		}
		predicate := asObj(cond.field("predicate"), "a location predicate")
		block, _ := predicate.obj("block")
		if !keysWithin(predicate, "block", "smokey") || !predicate.has("block") || !keysWithin(block, "blocks", "state") || !block.has("blocks") {
			return nil
		}
		state, _ := block.obj("state")
		check := newBlockCheck(block["blocks"], state, [3]float64{cond.numOr("offsetX", 0), cond.numOr("offsetY", 0), cond.numOr("offsetZ", 0)})
		if check == nil {
			return nil
		}
		if smokey, ok := predicate["smokey"]; ok {
			if want, isBool := smokey.(bool); !isBool || !want {
				return nil
			}
			check.smokey = true
		}
		return &condition{check: check}
	}
	fail("%s: unknown loot condition %v", where, map[string]any(cond))
	return nil
}

// locationConditions fills c.where and c.items from a trigger's location
// contexts, or marks it unmodelled.
func (r advancementReader) locationConditions(where string, contexts []any, c *advCriterion) {
	for _, x := range contexts {
		cond := asObj(x, "a location condition")
		if cond.str("condition") == "minecraft:match_tool" {
			predicate := asObj(cond.field("predicate"), "a tool predicate")
			// vanilla: JukeboxPlayablePredicate, empty: any stack with the component
			if jsonEqual(map[string]any(predicate), map[string]any{"predicates": map[string]any{"minecraft:jukebox_playable": map[string]any{}}}) {
				c.items = [][]int{r.jukeboxPlayable}
				continue
			}
			if !keysAre(predicate, "items") {
				c.unmodelled = true
				continue
			}
			c.items = r.itemPredicates(where, obj{"items": []any{map[string]any(predicate)}})
			continue
		}
		// Only item_used_on_block reads whereBiome; elsewhere a biome stays in
		// the check, which locationCondition does not model.
		if biome, rest, ok := whereBiome(cond); ok && c.trigger == "minecraft:item_used_on_block" &&
			(c.whereBiome == "" || c.whereBiome == biome) {
			c.whereBiome, cond = biome, rest
		}
		if parsed := locationCondition(where, cond); parsed == nil {
			c.unmodelled = true
		} else {
			c.where = append(c.where, parsed)
		}
	}
}

// whereBiome splits one biome name off a location_check at offset 0,
// returning the check without it; ok is false for any other condition.
func whereBiome(cond obj) (biome string, rest obj, ok bool) {
	predicate := asObjOr(cond["predicate"])
	biome, _ = predicate["biomes"].(string)
	if cond["condition"] != "minecraft:location_check" || biome == "" || strings.HasPrefix(biome, "#") ||
		cond.numOr("offsetX", 0) != 0 || cond.numOr("offsetY", 0) != 0 || cond.numOr("offsetZ", 0) != 0 {
		return "", nil, false
	}
	stripped := maps.Clone(predicate)
	delete(stripped, "biomes")
	rest = maps.Clone(cond)
	rest["predicate"] = map[string]any(stripped)
	return biome, rest, true
}

// rangeOf reads a bound that is either one number or a min/max object.
func rangeOf(where, what string, v any) (lo, hi int) {
	if n, ok := asNum(v); ok {
		return int(n), int(n)
	}
	o := asObjOr(v)
	if !keysWithin(o, "min", "max") {
		fail("%s: unknown %s %v", where, what, v)
	}
	return o.intOr("min", 0), o.intOr("max", 0)
}

// doubleRange reads a bound that is either one number or a min/max object.
func doubleRange(where, what string, v any) advRange {
	if n, ok := asNum(v); ok {
		return advRange{&n, &n}
	}
	o := asObjOr(v)
	if !keysWithin(o, "min", "max") {
		fail("%s: unknown %s %v", where, what, v)
	}
	var r advRange
	if o.has("min") {
		n := o.num("min")
		r.min = &n
	}
	if o.has("max") {
		n := o.num("max")
		r.max = &n
	}
	return r
}

// distanceOf reads a distance predicate. vanilla: DistancePredicate
func distanceOf(where string, v any) advDistance {
	o := asObjOr(v)
	if !keysWithin(o, distanceKeys[:]...) {
		fail("%s: unknown distance %v", where, v)
	}
	var d advDistance
	for i, k := range distanceKeys {
		if o.has(k) {
			d[i] = doubleRange(where, "distance "+k, o[k])
		}
	}
	return d
}

// locationTrigger reads the player's biome, structure, feet slot and
// stepping_on conditions.
func (r advancementReader) locationTrigger(where string, conditions obj, c *advCriterion) {
	players := conditions.list("player")
	if len(players) != 1 {
		c.unmodelled = true
		return
	}
	cond := asObjOr(players[0])
	predicate := asObjOr(cond["predicate"])
	location := asObjOr(predicate["minecraft:location"])
	equipment := asObjOr(predicate["minecraft:equipment"])
	feet := asObjOr(equipment["feet"])
	stepping := asObjOr(predicate["minecraft:stepping_on"])
	block := asObjOr(stepping["block"])
	if !keysAre(cond, "condition", "entity", "predicate") || cond["condition"] != "minecraft:entity_properties" ||
		cond["entity"] != "this" || len(predicate) == 0 ||
		!keysWithin(predicate, "minecraft:location", "minecraft:equipment", "minecraft:stepping_on") ||
		predicate.has("minecraft:location") && (!keysWithin(location, "biomes", "structures") || len(location) == 0) ||
		predicate.has("minecraft:equipment") && (!keysAre(equipment, "feet") || !keysAre(feet, "items")) ||
		predicate.has("minecraft:stepping_on") && (!keysAre(stepping, "block") || !keysAre(block, "blocks")) {
		c.unmodelled = true
		return
	}
	if feet.has("items") {
		c.feet = sortedSet(r.expand(feet["items"]))
	}
	if block.has("blocks") {
		c.steppingOn = newBlockCheck(block["blocks"], nil, [3]float64{}).blocks
	}
	if value, present := location["biomes"]; present {
		biome, ok := value.(string)
		if !ok || strings.HasPrefix(biome, "#") {
			c.unmodelled = true
		} else {
			c.biome = biome
		}
	}
	if value, present := location["structures"]; present {
		c.structures = []string{}
		switch v := value.(type) {
		case string:
			if tag, ok := strings.CutPrefix(v, "#"); ok {
				r := lootReader{dp: r.dp}
				c.structures = append(c.structures, r.sortedTag("worldgen/structure", tag)...)
			} else {
				c.structures = append(c.structures, v)
			}
		case []any:
			for _, id := range v {
				c.structures = append(c.structures, asStr(id, "a structure"))
			}
		default:
			fail("%s: invalid structures %v", where, value)
		}
	}
}

// positionY reads a location predicate that bounds only the position's Y.
func positionY(where string, location obj) (advRange, bool) {
	position := asObjOr(location["position"])
	if !keysAre(location, "position") || !keysAre(position, "y") {
		return advRange{}, false
	}
	return doubleRange(where, "position y", position["y"]), true
}

// playerY reads a player condition that bounds only the player's Y.
func playerY(where string, contexts []any) (advRange, bool) {
	if len(contexts) != 1 {
		return advRange{}, false
	}
	cond := asObjOr(contexts[0])
	predicate := asObjOr(cond["predicate"])
	if !keysAre(cond, "condition", "entity", "predicate") || cond.strOr("condition", "") != "minecraft:entity_properties" ||
		cond.strOr("entity", "") != "this" || !keysAre(predicate, "minecraft:location") {
		return advRange{}, false
	}
	return positionY(where, asObjOr(predicate["minecraft:location"]))
}

// lookingAtType reads a player condition that only names the entity type the
// player looks at.
func lookingAtType(contexts []any) (string, bool) {
	if len(contexts) != 1 {
		return "", false
	}
	cond := asObjOr(contexts[0])
	predicate := asObjOr(cond["predicate"])
	if !keysAre(cond, "condition", "entity", "predicate") || cond.strOr("condition", "") != "minecraft:entity_properties" ||
		cond.strOr("entity", "") != "this" || !keysAre(predicate, "minecraft:type_specific/player") {
		return "", false
	}
	player := asObjOr(predicate["minecraft:type_specific/player"])
	looking := asObjOr(player["looking_at"])
	if !keysAre(player, "looking_at") || !keysAre(looking, "minecraft:entity_type") {
		return "", false
	}
	typ, ok := looking["minecraft:entity_type"].(string)
	return typ, ok
}

// silkLikeEnchantment reads an item predicate that only wants one enchantment
// at level 1 or more and returns its ID.
func silkLikeEnchantment(where string, item obj) string {
	predicates := asObjOr(item["predicates"])
	list, _ := predicates["minecraft:enchantments"].([]any)
	if !keysAre(item, "predicates") || !keysAre(predicates, "minecraft:enchantments") || len(list) != 1 {
		fail("%s: unmodelled item predicate %v", where, map[string]any(item))
	}
	e := asObjOr(list[0])
	levels := asObjOr(e["levels"])
	id, _ := e["enchantments"].(string)
	if !keysAre(e, "enchantments", "levels") || !keysAre(levels, "min") || levels.num("min") != 1 || id == "" || id[0] == '#' {
		fail("%s: unmodelled enchantment predicate %v", where, map[string]any(e))
	}
	return id
}

func (r advancementReader) criterion(where, name string, value obj) advCriterion {
	trigger := value.str("trigger")
	conditions, _ := value.obj("conditions")
	conditions = (lootCodec{r.dp}).advancementConditions(conditions)
	c := advCriterion{name: name, trigger: trigger}
	unknown := func(what string) {
		fail("%s: unknown %s %v", where, what, map[string]any(conditions))
	}
	switch trigger {
	case "minecraft:consume_item":
		if !keysWithin(conditions, "item") {
			fail("%s: unmodelled consume_item conditions %v", where, map[string]any(conditions))
		}
		c.items = r.singleItem(where, conditions, "item")
	case "minecraft:enchanted_item":
		if len(conditions) > 0 {
			fail("%s: unmodelled enchanted_item conditions %v", where, map[string]any(conditions))
		}
	case "minecraft:entity_killed_player":
		c.unmodelled = len(conditions) > 0
	case "minecraft:player_killed_entity":
		if !keysWithin(conditions, "entity", "killing_blow") {
			unknown("player_killed_entity conditions")
		}
		contexts, minDistance, distOK := horizontalDistance(conditions["entity"])
		e, captain, ok := captainPredicate(where, contexts)
		direct, projectile, blowOK := killingBlowDirect(conditions["killing_blow"])
		if !ok || !blowOK || !distOK {
			c.unmodelled = true
		} else {
			c.entity, c.dimension, c.direct, c.captain = e.typ, e.dimension, direct, captain
			c.projectile, c.minDistance = projectile, minDistance
		}
	case "minecraft:bred_animals":
		if !keysWithin(conditions, "child", "parent", "partner") {
			unknown("bred_animals conditions")
		}
		for _, key := range []string{"child", "parent", "partner"} {
			e, ok := entityPredicate(where, conditions[key], false)
			if !ok || e.dimension != "" {
				c.unmodelled = true
				continue
			}
			switch key {
			case "child":
				c.child = e.typ
			case "parent":
				c.parent = e.typ
			case "partner":
				c.partner = e.typ
			}
		}
	case "minecraft:location":
		if !keysAre(conditions, "player") {
			unknown("location conditions")
		}
		r.locationTrigger(where, conditions, &c)
	case "minecraft:placed_block", "minecraft:item_used_on_block", "minecraft:allay_drop_item_on_block":
		if !keysWithin(conditions, "location", "player") {
			unknown(trigger + " conditions")
		}
		c.unmodelled = conditions.has("player")
		r.locationConditions(where, conditions.listOr("location"), &c)
	case "minecraft:filled_bucket", "minecraft:shot_crossbow":
		if !keysWithin(conditions, "item") {
			unknown(trigger + " conditions")
		}
		c.items = r.singleItem(where, conditions, "item")
	case "minecraft:villager_trade":
		if !keysWithin(conditions, "player") {
			unknown("villager_trade conditions")
		}
		if conditions.has("player") {
			player := conditions.list("player")
			predicate := obj{}
			if len(player) == 1 {
				predicate = asObj(asObj(player[0], "a player condition").field("predicate"), "a predicate")
			}
			position := asObjOr(asObjOr(predicate["minecraft:location"])["position"])
			y := asObjOr(position["y"])
			if keysAre(predicate, "minecraft:location") && keysAre(position, "y") && keysAre(y, "min") {
				v := y.num("min")
				c.minY = &v
			} else {
				c.unmodelled = true
			}
		}
	case "minecraft:construct_beacon":
		if !keysWithin(conditions, "level") {
			unknown("construct_beacon conditions")
		}
		if conditions.has("level") {
			c.levelMin, c.levelMax = rangeOf(where, "beacon level", conditions["level"])
		}
	case "minecraft:brewed_potion", "minecraft:cured_zombie_villager", "minecraft:used_ender_eye", "minecraft:voluntary_exile", "minecraft:default_block_use", "minecraft:any_block_use":
		c.unmodelled = len(conditions) > 0
	case "minecraft:fishing_rod_hooked":
		if !keysWithin(conditions, "item") {
			unknown("fishing_rod_hooked conditions")
		}
		c.items = r.singleItem(where, conditions, "item")
	case "minecraft:player_hurt_entity":
		if !keysWithin(conditions, "damage", "entity") {
			unknown("player_hurt_entity conditions")
		}
		r.playerHurtEntity(where, conditions, &c)
	case "minecraft:killed_by_arrow":
		if !keysWithin(conditions, "fired_from_weapon", "victims", "unique_entity_types") {
			unknown("killed_by_arrow conditions")
		}
		c.items = r.singleItem(where, conditions, "fired_from_weapon")
		victims(where, conditions, &c)
		if conditions.has("unique_entity_types") {
			c.uniqueMin, c.uniqueMax = rangeOf(where, "unique entity types", conditions["unique_entity_types"])
		}
	case "minecraft:channeled_lightning":
		if !keysWithin(conditions, "victims") {
			unknown("channeled_lightning conditions")
		}
		victims(where, conditions, &c)
	case "minecraft:spear_mobs":
		if !keysWithin(conditions, "count") {
			unknown("spear_mobs conditions")
		}
		c.count = conditions.intOr("count", 0)
		if conditions.has("count") && c.count <= 0 {
			unknown("spear_mobs count")
		}
	case "minecraft:player_generates_container_loot":
		if !keysWithin(conditions, "loot_tables") {
			fail("%s: unmodelled player_generates_container_loot conditions %v", where, map[string]any(conditions))
		}
		c.lootTable = singleRegistryName(conditions, "loot_tables")
	case "minecraft:recipe_crafted", "minecraft:crafter_recipe_crafted", "minecraft:recipe_unlocked":
		if !keysWithin(conditions, "recipes", "ingredients") {
			unknown(trigger + " conditions")
		}
		c.recipeID = singleRegistryName(conditions, "recipes")
		c.items = r.itemPredicates(where, obj{"items": conditions.listOr("ingredients")})
	case "minecraft:using_item":
		if !keysWithin(conditions, "item", "player") {
			unknown("using_item conditions")
		}
		c.items = r.singleItem(where, conditions, "item")
		if looking, ok := lookingAtType(conditions.listOr("player")); ok {
			c.lookingAt = looking
		} else if conditions.has("player") {
			c.unmodelled = true
		}
	case "minecraft:bee_nest_destroyed":
		if !keysWithin(conditions, "blocks", "item", "num_bees_inside", "player") || conditions.has("player") {
			unknown("bee_nest_destroyed conditions")
		}
		if conditions.has("blocks") {
			c.block = singleRegistryName(conditions, "blocks")
		}
		c.bees = conditions.intOr("num_bees_inside", 0)
		if conditions.has("num_bees_inside") && c.bees <= 0 {
			unknown("bee_nest_destroyed num_bees_inside")
		}
		if conditions.has("item") {
			c.enchantment = silkLikeEnchantment(where, asObjOr(conditions["item"]))
		}
	case "minecraft:slept_in_bed":
		if len(conditions) > 0 {
			fail("%s: unmodelled slept_in_bed conditions %v", where, map[string]any(conditions))
		}
	case "minecraft:player_interacted_with_entity", "minecraft:player_sheared_equipment":
		if !keysWithin(conditions, "item", "entity", "player") {
			unknown(trigger + " conditions")
		}
		c.items = r.singleItem(where, conditions, "item")
		e, flag, body, undamaged, ok := r.bodyPredicate(where, conditions["entity"])
		c.entity, c.variant, c.baby, c.adult, c.body, c.bodyUndamaged = e.typ, e.variant, flag.baby, flag.adult, body, undamaged
		c.unmodelled = !ok
		r.playerNotWearing(conditions, &c)
	case "minecraft:thrown_item_picked_up_by_entity", "minecraft:thrown_item_picked_up_by_player":
		if !keysWithin(conditions, "item", "entity", "player") {
			unknown(trigger + " conditions")
		}
		c.items = r.singleItem(where, conditions, "item")
		e, adult, ok := adultPredicate(where, conditions["entity"])
		c.entity, c.adult = e.typ, adult
		c.unmodelled = !ok || e.dimension != ""
		r.playerNotWearing(conditions, &c)
	case "minecraft:tame_animal", "minecraft:summoned_entity":
		if !keysWithin(conditions, "entity") {
			unknown(trigger + " conditions")
		}
		e, ok := entityPredicate(where, conditions["entity"], true)
		if !ok || e.dimension != "" {
			c.unmodelled = true
		} else {
			c.entity, c.variant = e.typ, e.variant
		}
	case "minecraft:effects_changed":
		if !keysWithin(conditions, "effects", "source") {
			unknown("effects_changed conditions")
		}
		effects, _ := conditions.obj("effects")
		c.effects = sortedKeys(effects)
		source, ok := entityPredicate(where, conditions["source"], true)
		c.entity = source.typ
		c.unmodelled = !ok || source.dimension != "" || source.variant != "" || conditions.has("source") && source.typ == ""
		for _, v := range effects {
			if len(asObjOr(v)) > 0 {
				c.unmodelled = true
			}
		}
	case "minecraft:inventory_changed":
		if !keysWithin(conditions, "items", "slots") {
			fail("%s: unmodelled inventory_changed conditions %v", where, map[string]any(conditions))
		}
		r.inventoryChanged(where, conditions, &c)
	case "minecraft:changed_dimension":
		if !keysWithin(conditions, "from", "to") {
			fail("%s: unmodelled changed_dimension conditions %v", where, map[string]any(conditions))
		}
		c.from, c.to = conditions.strOr("from", ""), conditions.strOr("to", "")
	case "minecraft:nether_travel", "minecraft:fall_from_height", "minecraft:levitation":
		if !keysWithin(conditions, "distance", "start_position", "player") {
			unknown(trigger + " conditions")
		}
		c.distance = distanceOf(where, conditions["distance"])
		if conditions.has("start_position") {
			y, ok := positionY(where, asObj(conditions.field("start_position"), "a start position"))
			c.startY, c.unmodelled = y, !ok
		}
		if conditions.has("player") {
			y, ok := playerY(where, conditions.list("player"))
			c.playerY, c.unmodelled = y, c.unmodelled || !ok
		}
	case "minecraft:fall_after_explosion":
		fallAfterExplosion(where, &c, conditions, unknown)
	case "minecraft:lightning_strike":
		lightningStrike(where, &c, conditions, unknown)
	case "minecraft:used_totem":
		if !keysWithin(conditions, "item") {
			unknown("used_totem conditions")
		}
		c.items = r.singleItem(where, conditions, "item")
	case "minecraft:entity_hurt_player":
		if !keysWithin(conditions, "damage", "player") {
			unknown("entity_hurt_player conditions")
		}
		damage, _ := conditions.obj("damage")
		kind := asObjOr(damage["type"])
		projectileTags := []any{map[string]any{"expected": true, "id": "#minecraft:is_projectile"}}
		c.projectile = damage.has("type")
		c.blocked, _ = damage["blocked"].(bool)
		c.unmodelled = conditions.has("player") || !keysWithin(damage, "type", "blocked") ||
			damage.has("blocked") && !c.blocked || c.projectile && (!keysAre(kind, "tags") || !jsonEqual(kind["tags"], projectileTags))
	case "minecraft:target_hit":
		if !keysWithin(conditions, "signal_strength", "projectile", "player") {
			unknown("target_hit conditions")
		}
		if conditions.has("signal_strength") {
			c.signal = doubleRange(where, "signal strength", conditions["signal_strength"])
		}
		c.unmodelled = conditions.has("player")
		if conditions.has("projectile") {
			list := conditions.list("projectile")
			var first obj
			if len(list) == 1 {
				first = asObjOr(list[0])
			}
			predicate := asObjOr(first["predicate"])
			if keysAre(first, "condition", "entity", "predicate") && first["condition"] == "minecraft:entity_properties" &&
				first["entity"] == "this" && keysAre(predicate, "minecraft:distance") {
				c.distance = distanceOf(where, predicate["minecraft:distance"])
			} else {
				c.unmodelled = true
			}
		}
	case "minecraft:started_riding", "minecraft:item_durability_changed", "minecraft:ride_entity_in_lava":
		if !keysWithin(conditions, "player", "item", "durability", "delta", "distance") {
			unknown(trigger + " conditions")
		}
		c.items = r.singleItem(where, conditions, "item")
		c.distance = distanceOf(where, conditions["distance"])
		c.unmodelled = conditions.has("durability") || conditions.has("delta")
		if conditions.has("player") {
			var ok bool
			c.dimension, c.vehicle, c.passenger, ok = riderPredicate(where, conditions["player"])
			c.unmodelled = c.unmodelled || !ok
		}
	case "minecraft:enter_block":
		if !keysWithin(conditions, "blocks", "state", "player") {
			unknown("enter_block conditions")
		}
		state, _ := conditions.obj("state")
		check := newBlockCheck(singleRegistryName(conditions, "blocks"), state, [3]float64{})
		if check == nil || conditions.has("player") {
			c.unmodelled = true
		} else {
			c.where = []*condition{{check: check}}
		}
	}
	return c
}

// victims reads a victims list of entity type predicates.
func victims(where string, conditions obj, c *advCriterion) {
	for _, victim := range conditions.listOr("victims") {
		e, ok := entityPredicate(where, victim, false)
		if !ok || e.dimension != "" {
			c.unmodelled = true
		} else {
			c.victims = append(c.victims, e.typ)
		}
	}
}

func translate(where string, component any) string {
	o := asObjOr(component)
	if !keysAre(o, "translate") {
		fail("%s: unmodelled text component %v", where, component)
	}
	return o.str("translate")
}

func goCondition(c *condition) string {
	if c.isAll || c.isAny {
		key, terms := "Any", c.any
		if c.isAll {
			key, terms = "All", c.all
		}
		parts := make([]string, len(terms))
		for i, t := range terms {
			parts[i] = "{" + goCondition(t) + "}"
		}
		return fmt.Sprintf("%s: []AdvancementCondition{%s}", key, strings.Join(parts, ", "))
	}
	blocks := make([]string, len(c.check.blocks))
	for i, b := range c.check.blocks {
		blocks[i] = gogen.Quote(b)
	}
	fields := []string{"Blocks: []string{" + strings.Join(blocks, ", ") + "}"}
	if len(c.check.state) > 0 {
		var pairs []string
		for _, k := range sortedKeys(c.check.state) {
			pairs = append(pairs, gogen.Quote(k)+": "+gogen.Quote(c.check.state[k]))
		}
		fields = append(fields, "State: map[string]string{"+strings.Join(pairs, ", ")+"}")
	}
	if c.check.smokey {
		fields = append(fields, "Smokey: new(true)")
	}
	if c.check.offset != [3]float64{} {
		var offs []string
		for _, o := range c.check.offset {
			offs = append(offs, fmt.Sprintf("%v", o))
		}
		fields = append(fields, "Offset: [3]int32{"+strings.Join(offs, ", ")+"}")
	}
	return strings.Join(fields, ", ")
}

// goRange writes r as an AdvancementRange literal, empty for an open one.
func goRange(r advRange) string {
	var parts []string
	if r.min != nil {
		parts = append(parts, "Min: "+gogen.Float(*r.min))
	}
	if r.max != nil {
		parts = append(parts, "Max: "+gogen.Float(*r.max))
	}
	if r.min != nil {
		parts = append(parts, "HasMin: true")
	}
	if r.max != nil {
		parts = append(parts, "HasMax: true")
	}
	if len(parts) == 0 {
		return ""
	}
	return "AdvancementRange{" + strings.Join(parts, ", ") + "}"
}

func quoteAll(values []string) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = gogen.Quote(v)
	}
	return strings.Join(parts, ", ")
}

// jukeboxPlayable lists the items whose default components carry
// minecraft:jukebox_playable.
func jukeboxPlayable(e *env, items map[string]int) []int {
	var ids []int
	for name, id := range items {
		namespace, local, _ := strings.Cut(name, ":")
		file := e.ref.ReportPath(filepath.Join(namespace, "components/item", local+".json"))
		components, ok := readObj(file).obj("components")
		if !ok {
			fail("%s: missing item components", file)
		}
		if components.has("minecraft:jukebox_playable") {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		fail("no item is jukebox_playable")
	}
	return sortedSet(ids)
}

func runAdvancements(e *env) error {
	registries, err := e.ref.Registries()
	if err != nil {
		return err
	}
	items := map[string]int{}
	for name, entry := range registries["minecraft:item"].Entries.All() {
		items[name] = entry.ProtocolID
	}
	dp, err := e.ref.DataPack()
	if err != nil {
		return err
	}
	defer dp.Close()
	r := advancementReader{items: items, dp: dp, jukeboxPlayable: jukeboxPlayable(e, items)}

	files, err := vanilla.Files(dp, "minecraft/advancement", ".json")
	if err != nil {
		return err
	}
	var advancements []advancement
	for _, rel := range files {
		name := strings.TrimSuffix(rel, ".json")
		id := "minecraft:" + name
		b, err := dpRead(dp, path.Join("minecraft/advancement", rel))
		if err != nil {
			return err
		}
		data := decode(b, rel)
		a := advancement{id: id, parent: data.strOr("parent", "")}
		if display, ok := data.obj("display"); ok {
			icon := asObj(display.field("icon"), "an icon")
			frame, known := advancementFrames[display.strOr("frame", "task")]
			if !known {
				fail("%s: unknown frame %v", id, display["frame"])
			}
			a.display = &advDisplay{
				title: translate(id, display.field("title")), description: translate(id, display.field("description")),
				icon: r.itemID(icon.str("id")), count: icon.intOr("count", 1), frame: frame,
				background: display.strOr("background", ""), showToast: display.boolOr("show_toast", true),
				announce: display.boolOr("announce_to_chat", true), hidden: display.boolOr("hidden", false),
			}
		}
		rewards, _ := data.obj("rewards")
		if !keysWithin(rewards, "experience", "recipes") {
			return fmt.Errorf("%s: unmodelled rewards %v", id, map[string]any(rewards))
		}
		criteria := asObj(data.field("criteria"), "criteria")
		names := sortedKeys(criteria)
		for _, n := range names {
			a.criteria = append(a.criteria, r.criterion(id, n, asObj(criteria[n], "a criterion")))
		}
		// vanilla: Advancement.CODEC, AdvancementRequirements.allOf
		if reqs := data.listOr("requirements"); len(reqs) > 0 {
			for _, group := range reqs {
				var g []string
				for _, n := range group.([]any) {
					g = append(g, asStr(n, "a requirement"))
				}
				a.requirements = append(a.requirements, g)
			}
		} else {
			for _, n := range names {
				a.requirements = append(a.requirements, []string{n})
			}
		}
		a.telemetry = data.boolOr("sends_telemetry_event", false)
		a.experience = rewards.intOr("experience", 0)
		for _, recipe := range rewards.listOr("recipes") {
			a.recipes = append(a.recipes, qualified(asStr(recipe, "a recipe reward")))
		}
		advancements = append(advancements, a)
	}
	if len(advancements) == 0 {
		// An empty tree would generate cleanly and hide every advancement.
		return fmt.Errorf("no advancements in the data pack under minecraft/advancement")
	}

	f := e.file("registry", "from the vanilla {version} data pack's advancements")
	f.WriteString(`// Advancements is every advancement of the vanilla data pack, sorted by
// ID. tools/gen advancements builds it.
var Advancements = []Advancement{
`)
	for _, a := range advancements {
		f.Printf("\t{\n\t\tID: %s,\n", gogen.Quote(a.id))
		if a.parent != "" {
			f.Printf("\t\tParent: %s,\n", gogen.Quote(a.parent))
		}
		if d := a.display; d != nil {
			f.Printf("\t\tDisplay: &AdvancementDisplay{Title: %s, Description: %s, Icon: %d, IconCount: %d, Frame: %d, Background: %s, ShowToast: %t, AnnounceChat: %t, Hidden: %t},\n",
				gogen.Quote(d.title), gogen.Quote(d.description), d.icon, d.count, d.frame,
				gogen.Quote(d.background), d.showToast, d.announce, d.hidden)
		}
		f.WriteString("\t\tCriteria: []AdvancementCriterion{\n")
		for _, c := range a.criteria {
			fields := []string{"Name: " + gogen.Quote(c.name), "Trigger: " + gogen.Quote(c.trigger)}
			if len(c.items) > 0 {
				preds := make([]string, len(c.items))
				for i, p := range c.items {
					preds[i] = goInts(p)
				}
				fields = append(fields, "Items: [][]int32{"+strings.Join(preds, ", ")+"}")
			}
			if c.from != "" {
				fields = append(fields, "From: "+gogen.Quote(c.from))
			}
			if c.to != "" {
				fields = append(fields, "To: "+gogen.Quote(c.to))
			}
			for _, kv := range []struct{ field, value string }{
				{"Entity", c.entity}, {"Child", c.child}, {"Parent", c.parent}, {"Partner", c.partner},
				{"Dimension", c.dimension}, {"Biome", c.biome}, {"WhereBiome", c.whereBiome}, {"Variant", c.variant},
				{"Vehicle", c.vehicle}, {"Passenger", c.passenger},
			} {
				if kv.value != "" {
					fields = append(fields, kv.field+": "+gogen.Quote(kv.value))
				}
			}
			if len(c.feet) > 0 {
				fields = append(fields, "Feet: []int32"+goInts(c.feet))
			}
			if c.steppingOn != nil {
				fields = append(fields, "SteppingOn: []string{"+quoteAll(c.steppingOn)+"}")
			}
			if c.structures != nil {
				fields = append(fields, "Structures: []string{"+quotedList(c.structures)+"}")
			}
			if len(c.where) > 0 {
				parts := make([]string, len(c.where))
				for i, w := range c.where {
					parts[i] = "{" + goCondition(w) + "}"
				}
				fields = append(fields, "Where: []AdvancementCondition{"+strings.Join(parts, ", ")+"}")
			}
			if len(c.effects) > 0 {
				fields = append(fields, "Effects: []string{"+quoteAll(c.effects)+"}")
			}
			if c.direct != "" {
				fields = append(fields, "Direct: "+gogen.Quote(c.direct))
			}
			if c.smashMin != 0 {
				fields = append(fields, "SmashMin: "+gogen.Float(c.smashMin))
			}
			if c.captain {
				fields = append(fields, "Captain: true")
			}
			if c.projectile {
				fields = append(fields, "Projectile: true")
			}
			if c.blocked {
				fields = append(fields, "Blocked: true")
			}
			if c.minDistance != 0 {
				fields = append(fields, "MinDistance: "+gogen.Float(c.minDistance))
			}
			if len(c.victims) > 0 {
				fields = append(fields, "Victims: []string{"+quoteAll(c.victims)+"}")
			}
			if c.uniqueMin != 0 {
				fields = append(fields, fmt.Sprintf("UniqueMin: %d", c.uniqueMin))
			}
			if c.uniqueMax != 0 {
				fields = append(fields, fmt.Sprintf("UniqueMax: %d", c.uniqueMax))
			}
			if c.count != 0 {
				fields = append(fields, fmt.Sprintf("Count: %d", c.count))
			}
			if c.minY != nil {
				fields = append(fields, "MinY: "+gogen.Float(*c.minY)+", HasMinY: true")
			}
			if c.levelMin != 0 {
				fields = append(fields, fmt.Sprintf("LevelMin: %d", c.levelMin))
			}
			if c.levelMax != 0 {
				fields = append(fields, fmt.Sprintf("LevelMax: %d", c.levelMax))
			}
			if len(c.body) > 0 {
				fields = append(fields, "Body: []int32"+goInts(c.body))
			}
			if c.bodyUndamaged {
				fields = append(fields, "BodyUndamaged: true")
			}
			if c.baby {
				fields = append(fields, "Baby: true")
			}
			if c.adult {
				fields = append(fields, "Adult: true")
			}
			if len(c.notWearing) > 0 {
				fields = append(fields, "NotWearing: []int32"+goInts(c.notWearing))
			}
			if c.block != "" {
				fields = append(fields, "Block: "+gogen.Quote(c.block))
			}
			if c.enchantment != "" {
				fields = append(fields, "Enchantment: "+gogen.Quote(c.enchantment))
			}
			if c.bees != 0 {
				fields = append(fields, "Bees: "+fmt.Sprint(c.bees))
			}
			if c.recipeID != "" {
				fields = append(fields, "RecipeID: "+gogen.Quote(c.recipeID))
			}
			if c.lookingAt != "" {
				fields = append(fields, "LookingAt: "+gogen.Quote(c.lookingAt))
			}
			if c.lootTable != "" {
				fields = append(fields, "LootTable: "+gogen.Quote(c.lootTable))
			}
			var distance []string
			for i, k := range distanceKeys {
				if r := goRange(c.distance[i]); r != "" {
					distance = append(distance, strings.ToUpper(k[:1])+k[1:]+": "+r)
				}
			}
			if len(distance) > 0 {
				fields = append(fields, "Distance: AdvancementDistance{"+strings.Join(distance, ", ")+"}")
			}
			if r := goRange(c.startY); r != "" {
				fields = append(fields, "StartY: "+r)
			}
			if r := goRange(c.playerY); r != "" {
				fields = append(fields, "PlayerY: "+r)
			}
			if r := goRange(c.signal); r != "" {
				fields = append(fields, "Signal: "+r)
			}
			if r := goRange(c.fires); r != "" {
				fields = append(fields, "Fires: "+r)
			}
			for _, b := range []struct {
				name  string
				bound advRange
			}{{"Occupied", c.occupied}, {"Full", c.full}, {"Empty", c.empty}} {
				if r := goRange(b.bound); r != "" {
					fields = append(fields, b.name+": "+r)
				}
			}
			if c.unmodelled {
				fields = append(fields, "Unmodelled: true")
			}
			f.Printf("\t\t\t{%s},\n", strings.Join(fields, ", "))
		}
		f.WriteString("\t\t},\n")
		groups := make([]string, len(a.requirements))
		for i, g := range a.requirements {
			groups[i] = "{" + quoteAll(g) + "}"
		}
		f.Printf("\t\tRequirements: [][]string{%s},\n", strings.Join(groups, ", "))
		if a.telemetry {
			f.WriteString("\t\tTelemetry: true,\n")
		}
		if a.experience != 0 {
			f.Printf("\t\tExperience: %d,\n", a.experience)
		}
		if len(a.recipes) > 0 {
			f.Printf("\t\tRecipes: []string{%s},\n", quoteAll(a.recipes))
		}
		f.WriteString("\t},\n")
	}
	f.WriteString("}\n")
	e.logf("advancements: %d", len(advancements))
	return e.emit("registry/advancements_gen.go", f)
}

// fallAfterExplosion reads fall_after_explosion's distance, start Y and the
// type of the entity that caused the explosion.
func fallAfterExplosion(where string, c *advCriterion, conditions obj, unknown func(string)) {
	if !keysWithin(conditions, "distance", "start_position", "cause") {
		unknown("fall_after_explosion conditions")
	}
	c.distance = distanceOf(where, conditions["distance"])
	if conditions.has("start_position") {
		y, ok := positionY(where, asObj(conditions.field("start_position"), "a start position"))
		c.startY, c.unmodelled = y, !ok
	}
	e, ok := entityPredicate(where, conditions["cause"], false)
	if !ok || e.dimension != "" {
		c.unmodelled = true
	} else {
		c.entity = e.typ
	}
}

// lightningStrike reads lightning_strike's distance from the player to the
// bolt, the blocks the bolt set on fire, and the type of the bystander.
func lightningStrike(where string, c *advCriterion, conditions obj, unknown func(string)) {
	if !keysWithin(conditions, "player", "lightning", "bystander") {
		unknown("lightning_strike conditions")
	}
	c.unmodelled = conditions.has("player")
	if conditions.has("lightning") {
		list := conditions.list("lightning")
		var first obj
		if len(list) == 1 {
			first = asObjOr(list[0])
		}
		predicate := asObjOr(first["predicate"])
		specific := asObjOr(predicate["minecraft:type_specific/lightning"])
		if !keysAre(first, "condition", "entity", "predicate") || first["condition"] != "minecraft:entity_properties" ||
			first["entity"] != "this" || !keysWithin(predicate, "minecraft:distance", "minecraft:type_specific/lightning") ||
			!keysWithin(specific, "blocks_set_on_fire") {
			c.unmodelled = true
		} else {
			c.distance = distanceOf(where, predicate["minecraft:distance"])
			if specific.has("blocks_set_on_fire") {
				c.fires = doubleRange(where, "blocks set on fire", specific["blocks_set_on_fire"])
			}
		}
	}
	e, ok := entityPredicate(where, conditions["bystander"], false)
	if !ok || e.dimension != "" {
		c.unmodelled = true
	} else {
		c.entity = e.typ
	}
}
