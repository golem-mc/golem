package datapack

import (
	"log/slog"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The advancement codec reads data pack JSON into registry.Advancement. It
// reads what tools/gen advancements reads: of a criterion only the
// conditions of the triggers the server fires, any other condition marking
// the criterion unmodelled (it never fires), and keys vanilla's codec
// ignores ignored with a log line. A file is refused where vanilla refuses
// it (malformed shape, unknown trigger, item or frame, bad parent id, bad
// requirements) and where the shapes cannot carry it (loot and function
// rewards).
// vanilla: Advancement.CODEC

// advancementTriggers are the registered trigger types; a criterion naming
// another is refused.
// vanilla: CriteriaTriggers.CODEC
var advancementTriggers = map[string]bool{}

func init() {
	for name := range strings.FieldsSeq(`impossible player_killed_entity entity_killed_player enter_block
		inventory_changed recipe_unlocked player_hurt_entity entity_hurt_player enchanted_item filled_bucket
		brewed_potion construct_beacon used_ender_eye summoned_entity bred_animals location slept_in_bed
		cured_zombie_villager villager_trade item_durability_changed levitation changed_dimension tick
		tame_animal placed_block consume_item effects_changed used_totem nether_travel fishing_rod_hooked
		channeled_lightning shot_crossbow spear_mobs killed_by_arrow hero_of_the_village voluntary_exile
		slide_down_block bee_nest_destroyed target_hit item_used_on_block default_block_use any_block_use
		player_generates_container_loot thrown_item_picked_up_by_entity thrown_item_picked_up_by_player
		player_interacted_with_entity player_sheared_equipment started_riding lightning_strike using_item
		fall_from_height ride_entity_in_lava kill_mob_near_sculk_catalyst allay_drop_item_on_block
		avoid_vibration recipe_crafted crafter_recipe_crafted fall_after_explosion`) {
		advancementTriggers["minecraft:"+name] = true
	}
}

// ignoreKeys logs the keys of o outside known; vanilla's codecs skip them.
func ignoreKeys(id, what string, o obj, known ...string) {
	for _, k := range sortedKeys(o) {
		if !slices.Contains(known, k) {
			slog.Info("Ignoring unknown field of advancement", "advancement", id, "in", what, "field", k)
		}
	}
}

// qualifiedID qualifies an id string and refuses one that is not a valid
// resource id.
// vanilla: Identifier.parse
func qualifiedID(s, what string) string {
	q := registry.Qualify(s)
	ns, path, _ := strings.Cut(q, ":")
	if !validNamespace(ns) || !validPath(path) {
		fail("%s %q is not a valid id", what, s)
	}
	return q
}

// advancementFrames are AdvancementType's ordinals.
var advancementFrames = map[string]int32{"task": 0, "challenge": 1, "goal": 2}

// variantComponents are the variant components an entity predicate may test,
// with the entity type that carries each.
var variantComponents = map[string]string{
	"minecraft:cat/variant":  "minecraft:cat",
	"minecraft:frog/variant": "minecraft:frog",
	"minecraft:wolf/variant": "minecraft:wolf",
}

// DecodeAdvancement reads one advancement file; id is the file's resource
// id. A file without criteria, or with requirements that do not name exactly
// its criteria, is refused as vanilla refuses it.
func DecodeAdvancement(id string, data []byte) (adv registry.Advancement, err error) {
	return liveResourceDecoder().DecodeAdvancement(id, data)
}

func (decoder ResourceDecoder) DecodeAdvancement(id string, data []byte) (adv registry.Advancement, err error) {
	defer recoverLoot(&err)
	o := object(parseJSON(data), "advancement")
	ignoreKeys(id, "advancement", o, "parent", "display", "rewards", "criteria", "requirements", "sends_telemetry_event")
	adv = registry.Advancement{ID: id, Telemetry: flag(o, "sends_telemetry_event", false)}
	if _, ok := o["parent"]; ok {
		adv.Parent = qualifiedID(requireStr(o, "parent"), "parent")
	}
	if d, ok := o["display"]; ok {
		adv.Display = decoder.advDisplay(id, object(d, "display"))
	}
	if r, ok := o["rewards"]; ok {
		ro := object(r, "rewards")
		ignoreKeys(id, "rewards", ro, "experience", "recipes", "loot", "function")
		adv.Experience = int32(optNum(ro, "experience", 0))
		if v, ok := ro["recipes"]; ok {
			for _, r := range list(v, "recipes") {
				adv.Recipes = append(adv.Recipes, qualifiedID(str(r, "a recipe"), "recipe"))
			}
		}
		// vanilla: LootTable.LIST_CODEC reads one id or a list
		if v, ok := ro["loot"]; ok {
			if _, isList := v.([]any); isList {
				for _, t := range list(v, "loot") {
					adv.Loot = append(adv.Loot, qualifiedID(str(t, "a loot table"), "loot table"))
				}
			} else {
				adv.Loot = append(adv.Loot, qualifiedID(str(v, "a loot table"), "loot table"))
			}
		}
		if _, ok := ro["function"]; ok {
			adv.Function = qualifiedID(requireStr(ro, "function"), "function")
		}
	}
	criteria := object(required(o, "criteria"), "criteria")
	if len(criteria) == 0 {
		fail("advancement criteria cannot be empty")
	}
	names := sortedKeys(criteria)
	for _, name := range names {
		adv.Criteria = append(adv.Criteria, decoder.advCriterion(id, name, object(criteria[name], "criterion")))
	}
	adv.Requirements = advRequirements(o["requirements"], names)
	for _, c := range adv.Criteria {
		if c.Unmodelled {
			slog.Info("Advancement criterion has conditions this server does not model, it never fires", "advancement", id, "criterion", c.Name)
		}
	}
	return adv, nil
}

// advRequirements reads the requirement groups, all-of the criteria when
// absent, and checks that they name exactly the criteria.
// vanilla: AdvancementRequirements.validate
func advRequirements(v any, names []string) [][]string {
	var groups [][]string
	if v == nil {
		for _, n := range names {
			groups = append(groups, []string{n})
		}
		return groups
	}
	seen := map[string]bool{}
	for _, g := range list(v, "requirements") {
		var group []string
		for _, n := range list(g, "requirements group") {
			name := str(n, "a requirement")
			if !slices.Contains(names, name) {
				fail("requirement %q is not a criterion", name)
			}
			seen[name] = true
			group = append(group, name)
		}
		if len(group) == 0 {
			fail("a requirements group is empty")
		}
		groups = append(groups, group)
	}
	for _, n := range names {
		if !seen[n] {
			fail("criterion %q is in no requirements group", n)
		}
	}
	return groups
}

func (decoder ResourceDecoder) advDisplay(id string, o obj) *registry.AdvancementDisplay {
	ignoreKeys(id, "display", o, "icon", "title", "description", "frame", "background", "show_toast", "announce_to_chat", "hidden")
	stack := decoder.tradeResult(required(o, "icon"))
	var iconStack *protocol.ItemStack
	if icon, ok := o["icon"].(obj); ok {
		ignoreKeys(id, "icon", icon, "id", "count", "components")
		if comps, _ := icon["components"].(obj); len(comps) > 0 {
			iconStack = &stack
		}
	}
	d := &registry.AdvancementDisplay{
		Icon: stack.ItemID, IconCount: stack.Count, IconStack: iconStack,
		ShowToast: flag(o, "show_toast", true), AnnounceChat: flag(o, "announce_to_chat", true), Hidden: flag(o, "hidden", false),
	}
	d.Title, d.TitleLiteral, d.TitleText = advText(required(o, "title"))
	d.Description, d.DescriptionLiteral, d.DescriptionText = advText(required(o, "description"))
	frame := "task"
	if f, ok := o["frame"]; ok {
		frame = str(f, "frame")
	}
	frameID, ok := advancementFrames[frame]
	if !ok {
		fail("unknown frame %q", frame)
	}
	d.Frame = frameID
	if _, ok := o["background"]; ok {
		d.Background = requireStr(o, "background")
	}
	return d
}

// advText reads a text component: a bare translation key or literal text stays
// in the string fields so vanilla advancements match the generated tree; any
// richer component is returned in full.
// vanilla: ComponentSerialization.CODEC
func advText(v any) (text string, literal bool, full *protocol.Text) {
	if s, ok := v.(string); ok {
		return s, true, nil
	}
	if o, ok := v.(map[string]any); ok {
		switch {
		case keysAre(o, "translate"):
			return requireStr(o, "translate"), false, nil
		case keysAre(o, "text"):
			return requireStr(o, "text"), true, nil
		}
	}
	parsed := textComponent(v)
	return "", false, &parsed
}

// qualifyRef gives a name, or a #tag, its namespace.
func qualifyRef(s string) string {
	if tag, ok := strings.CutPrefix(s, "#"); ok {
		return "#" + registry.Qualify(tag)
	}
	if s == "" {
		return s
	}
	return registry.Qualify(s)
}

func keysWithin(o obj, allowed ...string) bool {
	for k := range o {
		if !slices.Contains(allowed, k) {
			return false
		}
	}
	return true
}

func keysAre(o obj, want ...string) bool { return len(o) == len(want) && keysWithin(o, want...) }

func asObjOr(v any) obj {
	m, _ := v.(map[string]any)
	return m
}

// advItems reads one item reference, a list of them or a #tag, as the sorted
// item IDs they cover.
func (decoder ResourceDecoder) advItems(v any) []int32 {
	var ids []int32
	for _, name := range holderSet(v, "item", decoder.tags.ItemTagMembers) {
		id, ok := registry.ItemID(name)
		if !ok {
			fail("unknown item %s", name)
		}
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

// advItemPredicates reads the item predicates of conditions["items"]: the
// item IDs each accepts, and the full predicate of each that tests more than
// the item, nil for the others. It is false for a predicate with a key this
// model does not read.
// vanilla: ItemPredicate.CODEC
func (decoder ResourceDecoder) advItemPredicates(conditions obj) (ids [][]int32, tests []*registry.ItemPredicate, ok bool) {
	for i, p := range list(conditions["items"], "items") {
		predicate := object(p, "an item predicate")
		if !keysWithin(predicate, "items", "count", "components", "predicates") {
			return nil, nil, false
		}
		var test *registry.ItemPredicate
		if !keysAre(predicate, "items") && len(predicate) > 0 {
			needsFullItemPredicate(predicate)
			full := decoder.tags.DecodeItemPredicate(nbtOf(predicate))
			test = &full
			if tests == nil {
				tests = make([]*registry.ItemPredicate, i, len(list(conditions["items"], "items")))
			}
		}
		if tests != nil {
			tests = append(tests, test)
		}
		var accepted []int32
		if v, has := predicate["items"]; has {
			accepted = decoder.advItems(v)
		}
		ids = append(ids, accepted)
	}
	return ids, tests, true
}

// advSetItems fills c.Items from conditions["items"], or marks c
// unmodelled for a predicate that tests more than the item.
func (decoder ResourceDecoder) advSetItems(c *registry.AdvancementCriterion, conditions obj) {
	items, tests, ok := decoder.advItemPredicates(conditions)
	if !ok || tests != nil {
		c.Unmodelled = true
		return
	}
	c.Items = items
}

// advSetTestedItems is advSetItems for a trigger that tests whole stacks: a
// predicate beyond the item IDs lands in c.ItemTests.
func (decoder ResourceDecoder) advSetTestedItems(c *registry.AdvancementCriterion, conditions obj) {
	items, tests, ok := decoder.advItemPredicates(conditions)
	if !ok {
		c.Unmodelled = true
		return
	}
	c.Items, c.ItemTests = items, tests
}

// advSetSingleItem is advSetItems for the one item predicate under key.
func (decoder ResourceDecoder) advSetSingleItem(c *registry.AdvancementCriterion, conditions obj, key string) {
	if _, ok := conditions[key]; ok {
		decoder.advSetItems(c, obj{"items": []any{conditions[key]}})
	}
}

// advSetTestedItem is advSetTestedItems for the one item predicate under key.
func (decoder ResourceDecoder) advSetTestedItem(c *registry.AdvancementCriterion, conditions obj, key string) {
	if _, ok := conditions[key]; ok {
		decoder.advSetTestedItems(c, obj{"items": []any{conditions[key]}})
	}
}

type advEntity struct{ typ, dimension, variant string }

// advEntityPredicate reads an entity's type and dimension (and its variant
// when variants is set) out of a trigger's entity contexts, or false for a
// predicate beyond them.
func advEntityPredicate(contexts any, variants bool) (advEntity, bool) {
	var out advEntity
	if contexts == nil {
		return out, true
	}
	l, _ := contexts.([]any)
	var first obj
	if len(l) == 1 {
		first = asObjOr(l[0])
	}
	if len(l) != 1 || !advThisProperties(first) {
		return advEntity{}, false
	}
	predicate := asObjOr(first["predicate"])
	var typ string
	for _, key := range sortedKeys(predicate) {
		value := predicate[key]
		switch {
		case key == "minecraft:entity_type":
			typ, _ = value.(string)
			typ = qualifyRef(typ)
		case key == "minecraft:location" && keysAre(asObjOr(value), "dimension"):
			dimension, _ := asObjOr(value)["dimension"].(string)
			out.dimension = qualifyRef(dimension)
		case variants && key == "minecraft:components" && len(asObjOr(value)) == 1 && variantComponent(asObjOr(value)) != "":
			comp := variantComponent(asObjOr(value))
			out.variant = qualifyRef(asObjOr(value)[comp].(string))
			out.typ = variantComponents[comp]
		default:
			return advEntity{}, false
		}
	}
	if typ != "" {
		if out.typ != "" && out.typ != typ {
			fail("an entity_type of %s beside a variant of %s", typ, out.typ)
		}
		out.typ = typ
	}
	return out, true
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

func advBlockCheck(blocks any, state obj, offset [3]int32) (registry.AdvancementCondition, bool) {
	var names []string
	switch v := blocks.(type) {
	case string:
		names = []string{qualifyRef(v)}
	case []any:
		for _, x := range v {
			names = append(names, qualifyRef(str(x, "a block")))
		}
	default:
		fail("blocks are not a name, a list or a tag")
	}
	values := map[string]string{}
	for k, v := range state {
		s, ok := v.(string)
		if !ok {
			return registry.AdvancementCondition{}, false
		}
		values[k] = s
	}
	cond := registry.AdvancementCondition{Blocks: names, Offset: offset}
	if len(values) > 0 {
		cond.State = values
	}
	return cond, true
}

// advLocation reads a loot condition over the blocks around a position, or
// false for one beyond block checks.
func advLocation(cond obj) (registry.AdvancementCondition, bool) {
	switch kind := requireStr(cond, "condition"); kind {
	case "minecraft:all_of", "minecraft:any_of":
		var terms []registry.AdvancementCondition
		for _, t := range holderList(required(cond, "terms")) {
			term, ok := advLocation(object(t, "a condition term"))
			if !ok {
				return registry.AdvancementCondition{}, false
			}
			terms = append(terms, term)
		}
		if kind == "minecraft:all_of" {
			return registry.AdvancementCondition{All: terms}, true
		}
		return registry.AdvancementCondition{Any: terms}, true
	case "minecraft:block_state_property":
		if !keysWithin(cond, "condition", "block", "properties") {
			return registry.AdvancementCondition{}, false
		}
		return advBlockCheck(required(cond, "block"), asObjOr(cond["properties"]), [3]int32{})
	case "minecraft:match_block":
		if !keysWithin(cond, "condition", "blocks", "state") || cond["blocks"] == nil {
			return registry.AdvancementCondition{}, false
		}
		return advBlockCheck(cond["blocks"], asObjOr(cond["state"]), [3]int32{})
	case "minecraft:location_check":
		if !keysWithin(cond, "condition", "predicate", "offsetX", "offsetY", "offsetZ") {
			return registry.AdvancementCondition{}, false
		}
		predicate := object(required(cond, "predicate"), "a location predicate")
		block := asObjOr(predicate["block"])
		smokey, hasSmokey := predicate["smokey"]
		if !keysWithin(predicate, "block", "smokey") || !keysWithin(block, "blocks", "state") || block["blocks"] == nil ||
			hasSmokey && smokey != true {
			return registry.AdvancementCondition{}, false
		}
		offset := [3]int32{int32(optNum(cond, "offsetX", 0)), int32(optNum(cond, "offsetY", 0)), int32(optNum(cond, "offsetZ", 0))}
		check, ok := advBlockCheck(block["blocks"], asObjOr(block["state"]), offset)
		check.Smokey = hasSmokey
		return check, ok
	}
	return registry.AdvancementCondition{}, false
}

// advLocations fills c.Where and c.Items from a trigger's location contexts,
// or marks it unmodelled.
func (decoder ResourceDecoder) advLocations(contexts []any, c *registry.AdvancementCriterion) {
	for _, x := range contexts {
		cond := object(x, "a location condition")
		if cond["condition"] == "minecraft:match_tool" {
			predicate := object(required(cond, "predicate"), "a tool predicate")
			// vanilla: JukeboxPlayablePredicate, empty: any stack with the component
			if reflect.DeepEqual(map[string]any(predicate), map[string]any{"predicates": map[string]any{"minecraft:jukebox_playable": map[string]any{}}}) {
				c.Items = [][]int32{jukeboxPlayable()}
				continue
			}
			decoder.advSetTestedItems(c, obj{"items": []any{predicate}})
			continue
		}
		// Only item_used_on_block reads WhereBiome; elsewhere a biome stays in
		// the check, which advLocation does not model.
		if biome, rest, ok := advWhereBiome(cond); ok && c.Trigger == "minecraft:item_used_on_block" &&
			(c.WhereBiome == "" || c.WhereBiome == biome) {
			c.WhereBiome, cond = biome, rest
		}
		if parsed, ok := advLocation(cond); ok {
			c.Where = append(c.Where, parsed)
		} else {
			c.Unmodelled = true
		}
	}
}

// advWhereBiome splits one biome name off a location_check at offset 0,
// returning the check without it; ok is false for any other condition.
func advWhereBiome(cond obj) (biome string, rest obj, ok bool) {
	predicate := asObjOr(cond["predicate"])
	biome, _ = predicate["biomes"].(string)
	if cond["condition"] != "minecraft:location_check" || biome == "" || strings.HasPrefix(biome, "#") ||
		optNum(cond, "offsetX", 0) != 0 || optNum(cond, "offsetY", 0) != 0 || optNum(cond, "offsetZ", 0) != 0 {
		return "", nil, false
	}
	stripped := maps.Clone(predicate)
	delete(stripped, "biomes")
	rest = maps.Clone(cond)
	rest["predicate"] = map[string]any(stripped)
	return registry.Qualify(biome), rest, true
}

// jukeboxPlayable lists the items a jukebox plays, sorted.
func jukeboxPlayable() []int32 {
	var ids []int32
	for id := range int32(registry.ItemCount) {
		if _, ok := registry.JukeboxSongForItem(id); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

// advRange reads a bound that is one number or a min/max object.
func advRange(what string, v any) (lo, hi int32) {
	if n, ok := num(v); ok {
		return int32(n), int32(n)
	}
	o := asObjOr(v)
	if !keysWithin(o, "min", "max") {
		fail("unsupported %s %v", what, v)
	}
	return int32(optNum(o, "min", 0)), int32(optNum(o, "max", 0))
}

// advOnly marks c unmodelled when conditions has a key outside keys.
func advOnly(c *registry.AdvancementCriterion, conditions obj, keys ...string) {
	if !keysWithin(conditions, keys...) {
		c.Unmodelled = true
	}
}

// advCriterion reads one criterion: its trigger and the conditions the
// server tests of that trigger.
func (decoder ResourceDecoder) advCriterion(id, name string, value obj) registry.AdvancementCriterion {
	ignoreKeys(id, "criterion "+name, value, "trigger", "conditions")
	trigger := qualifiedID(requireStr(value, "trigger"), "trigger")
	if !advancementTriggers[trigger] {
		fail("unknown trigger %s", trigger)
	}
	conditions := advContexts(asObjOr(value["conditions"]))
	c := decoder.advTrigger(name, trigger, conditions)
	if !c.Unmodelled {
		return c
	}
	if read := decoder.advLootContexts(name, trigger, conditions, c); !read.Unmodelled {
		return read
	}
	if combat, ok := decoder.advCombat(name, trigger, conditions); ok {
		return combat
	}
	return c
}

// advCombat reads a player_hurt_entity, entity_hurt_player,
// player_killed_entity or entity_killed_player criterion in full, for the
// shapes the bespoke readers leave unmodelled: the player and entity
// conditions as loot conditions, and the damage or killing blow as an
// AdvancementDamage. ok is false for another trigger or a condition outside
// these or one the loot codec refuses.
// vanilla: PlayerHurtEntityTrigger, EntityHurtPlayerTrigger, KilledTrigger
func (decoder ResourceDecoder) advCombat(name, trigger string, conditions obj) (c registry.AdvancementCriterion, ok bool) {
	var blowKey string
	var entity bool
	switch trigger {
	case "minecraft:player_hurt_entity":
		blowKey, entity = "damage", true
	case "minecraft:entity_hurt_player":
		blowKey = "damage"
	case "minecraft:player_killed_entity", "minecraft:entity_killed_player":
		blowKey, entity = "killing_blow", true
	default:
		return c, false
	}
	if !keysWithin(conditions, "player", "entity", blowKey) || !entity && conditions["entity"] != nil {
		return c, false
	}
	c = registry.AdvancementCriterion{Name: name, Trigger: trigger, Damage: &registry.AdvancementDamage{}}
	if v, has := conditions["player"]; has {
		if c.Player, ok = decoder.advLootConditions(v); !ok {
			return c, false
		}
	}
	if v, has := conditions["entity"]; has {
		conds, ok := decoder.advLootConditions(v)
		if !ok {
			return c, false
		}
		c.Contexts = map[string][]registry.LootCondition{"entity": conds}
	}
	blow := asObjOr(conditions[blowKey])
	if blowKey == "damage" {
		if !keysWithin(blow, "dealt", "taken", "blocked", "type", "source_entity") {
			return c, false
		}
		if v, has := blow["dealt"]; has {
			c.Damage.Dealt = advDoubleRange("dealt", v)
		}
		if v, has := blow["taken"]; has {
			c.Damage.Taken = advDoubleRange("taken", v)
		}
		if v, has := blow["blocked"]; has {
			b, isBool := v.(bool)
			if !isBool {
				return c, false
			}
			c.Damage.Blocked = &b
		}
	}
	// A killing blow is a damage source predicate alone.
	var terms []any
	source := blow
	if blowKey == "damage" {
		source = obj{"type": blow["type"]}
		if se, has := blow["source_entity"]; has {
			terms = append(terms, obj{"condition": "minecraft:entity_properties", "entity": "attacker", "predicate": se})
		}
	}
	if blowKey == "killing_blow" || blow["type"] != nil {
		terms = append(terms, obj{"condition": "minecraft:damage_source_properties", "predicate": map[string]any(source)})
	}
	c.Damage.Conds, ok = decoder.advLootConditions(terms)
	return c, ok
}

// advLootContexts re-reads a criterion the bespoke readers left unmodelled
// without its entity conditions, which it keeps as the loot codec's decoding,
// or returns unmodelled when it still is without them or the codec refuses one.
func (decoder ResourceDecoder) advLootContexts(name, trigger string, conditions obj, unmodelled registry.AdvancementCriterion) registry.AdvancementCriterion {
	rest := maps.Clone(conditions)
	var keys []string
	for _, key := range advLootKeys(trigger) {
		if _, ok := conditions[key]; ok {
			delete(rest, key)
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return unmodelled
	}
	c := decoder.advTrigger(name, trigger, rest)
	if c.Unmodelled {
		return unmodelled
	}
	for _, key := range keys {
		switch key {
		case "victims":
			victims, isList := conditions[key].([]any)
			if !isList {
				return unmodelled
			}
			for _, victim := range victims {
				conds, ok := decoder.advLootConditions(victim)
				if !ok {
					return unmodelled
				}
				c.VictimConds = append(c.VictimConds, conds)
			}
			continue
		}
		conds, ok := decoder.advLootConditions(conditions[key])
		if !ok {
			return unmodelled
		}
		if key == "player" {
			c.Player = conds
			continue
		}
		if c.Contexts == nil {
			c.Contexts = map[string][]registry.LootCondition{}
		}
		c.Contexts[key] = conds
	}
	return c
}

// advLootConditions decodes one context condition's conjuncts with the loot
// codec, or false for one it refuses.
func (decoder ResourceDecoder) advLootConditions(terms any) (out []registry.LootCondition, ok bool) {
	var err error
	func() {
		defer recoverLoot(&err)
		for _, term := range terms.([]any) {
			out = append(out, decoder.conditionHolder(advTyped(term)))
		}
	}()
	return out, err == nil
}

// advTyped is advCondition's inverse: it keys a condition and its terms by
// "type" again, for the loot codec.
func advTyped(v any) any {
	c, ok := v.(obj)
	if !ok {
		return v
	}
	c = maps.Clone(c)
	if kind, keyed := c["condition"]; keyed {
		c["type"] = kind
		delete(c, "condition")
	}
	if terms, ok := c["terms"].([]any); ok {
		c["terms"] = slices.Collect(func(yield func(any) bool) {
			for _, t := range terms {
				if !yield(advTyped(t)) {
					return
				}
			}
		})
	}
	if term, ok := c["term"]; ok {
		c["term"] = advTyped(term)
	}
	return c
}

// advLootKeys are the context conditions of a trigger that can be tested
// as loot conditions with the trigger's entities.
// vanilla: the ContextAwarePredicate fields of each *Trigger.TriggerInstance
func advLootKeys(trigger string) []string {
	switch trigger {
	case "minecraft:player_interacted_with_entity", "minecraft:player_sheared_equipment", "minecraft:tame_animal",
		"minecraft:summoned_entity", "minecraft:thrown_item_picked_up_by_entity", "minecraft:thrown_item_picked_up_by_player":
		return []string{"player", "entity"}
	case "minecraft:bred_animals":
		return []string{"player", "parent", "partner", "child"}
	case "minecraft:cured_zombie_villager":
		return []string{"player", "zombie", "villager"}
	case "minecraft:effects_changed":
		return []string{"player"}
	case "minecraft:killed_by_arrow", "minecraft:channeled_lightning":
		return []string{"player", "victims"}
	}
	return []string{"player"}
}

// advTrigger reads the conditions the server tests of one trigger.
func (decoder ResourceDecoder) advTrigger(name, trigger string, conditions obj) registry.AdvancementCriterion {
	c := registry.AdvancementCriterion{Name: name, Trigger: trigger}
	only := func(keys ...string) { advOnly(&c, conditions, keys...) }
	switch trigger {
	case "minecraft:consume_item", "minecraft:shot_crossbow":
		only("item")
		decoder.advSetTestedItem(&c, conditions, "item")
	case "minecraft:filled_bucket", "minecraft:fishing_rod_hooked":
		only("item")
		decoder.advSetTestedItem(&c, conditions, "item")
	case "minecraft:inventory_changed":
		only("items", "slots")
		decoder.advSetTestedItems(&c, conditions)
		slots := asObjOr(conditions["slots"])
		advOnly(&c, slots, "occupied", "full", "empty")
		for key, bound := range map[string]*registry.AdvancementRange{"occupied": &c.Occupied, "full": &c.Full, "empty": &c.Empty} {
			if v, ok := slots[key]; ok {
				*bound = advDoubleRange("slots "+key, v)
			}
		}
	case "minecraft:enchanted_item":
		only("item", "levels")
		decoder.advSetTestedItem(&c, conditions, "item")
		if v, ok := conditions["levels"]; ok {
			c.Levels = advDoubleRange("levels", v)
		}
	case "minecraft:brewed_potion":
		only("potion")
		advBrewedPotion(&c, conditions["potion"])
	case "minecraft:slept_in_bed":
		only()
	case "minecraft:entity_killed_player", "minecraft:cured_zombie_villager", "minecraft:tick":
		c.Unmodelled = len(conditions) > 0
	case "minecraft:changed_dimension":
		only("from", "to")
		if s, ok := conditions["from"]; ok {
			c.From = registry.Qualify(str(s, "from"))
		}
		if s, ok := conditions["to"]; ok {
			c.To = registry.Qualify(str(s, "to"))
		}
	default:
		decoder.advCriterionEntities(&c, conditions)
	}
	return c
}

// advContextKeys are the trigger conditions that hold a loot condition over
// the trigger's context.
var advContextKeys = []string{"source", "player", "entity", "child", "parent", "partner", "projectile", "lightning", "bystander", "cause", "location", "zombie", "villager"}

// advContexts returns conditions with each context condition as the list of
// conjuncts the readers below take, matching tools/gen's lootCodec.context.
// vanilla: LootItemCondition.CODEC (one condition keyed by "type"); a list or
// a "condition" key is read for packs written for earlier versions.
func advContexts(conditions obj) obj {
	out := maps.Clone(conditions)
	for _, key := range advContextKeys {
		if v, ok := out[key]; ok {
			out[key] = advContext(v)
		}
	}
	if v, ok := out["victims"]; ok {
		victims := slices.Clone(list(v, "victims"))
		for i, victim := range victims {
			victims[i] = advContext(victim)
		}
		out["victims"] = victims
	}
	return out
}

// advContext turns one context condition, or a list of them, into a list of
// conjuncts, flattening a lone all_of.
func advContext(v any) []any {
	if l, ok := v.([]any); ok {
		out := make([]any, len(l))
		for i, term := range l {
			out[i] = advCondition(term)
		}
		return out
	}
	n := advCondition(v)
	if c, ok := n.(obj); ok && c["condition"] == "minecraft:all_of" && keysAre(c, "condition", "terms") {
		return holderList(c["terms"])
	}
	return []any{n}
}

// advCondition keys a loot condition and its terms by "condition", qualified;
// anything else, such as a predicate reference, is returned as it is.
func advCondition(v any) any {
	o, ok := v.(obj)
	if !ok {
		return v
	}
	c := maps.Clone(o)
	if kind, typed := c["type"]; typed {
		c["condition"] = kind
		delete(c, "type")
	}
	kind, ok := c["condition"].(string)
	if !ok {
		return c
	}
	c["condition"] = registry.Qualify(kind)
	switch c["condition"] {
	case "minecraft:all_of", "minecraft:any_of":
		if terms, ok := c["terms"].([]any); ok {
			terms = slices.Clone(terms)
			for i, term := range terms {
				terms[i] = advCondition(term)
			}
			c["terms"] = terms
		}
	case "minecraft:inverted":
		if term, ok := c["term"]; ok {
			c["term"] = advCondition(term)
		}
	}
	return c
}

// advCriterionEntities is advCriterion for the triggers that read entities,
// blocks and positions.
func (decoder ResourceDecoder) advCriterionEntities(c *registry.AdvancementCriterion, conditions obj) {
	only := func(keys ...string) { advOnly(c, conditions, keys...) }
	switch c.Trigger {
	case "minecraft:player_killed_entity":
		only("entity", "killing_blow")
		contexts, minDistance, distOK := advHorizontalDistance(conditions["entity"])
		e, captain, ok := advCaptainPredicate(contexts)
		direct, projectile, blowOK := advKillingBlowDirect(conditions["killing_blow"])
		if !ok || !blowOK || !distOK {
			c.Unmodelled = true
		} else {
			c.Entity, c.Dimension, c.Direct, c.Captain = e.typ, e.dimension, direct, captain
			c.Projectile, c.MinDistance = projectile, minDistance
		}
	case "minecraft:player_interacted_with_entity", "minecraft:player_sheared_equipment":
		only("item", "entity", "player")
		decoder.advSetTestedItem(c, conditions, "item")
		e, baby, adult, body, undamaged, ok := decoder.advBodyPredicate(conditions["entity"])
		c.Entity, c.Variant, c.Baby, c.Adult, c.Body, c.BodyUndamaged = e.typ, e.variant, baby, adult, body, undamaged
		c.Unmodelled = c.Unmodelled || !ok
		if player, has := conditions["player"]; has {
			worn, ok := decoder.advNotWearing(player)
			c.NotWearing, c.Unmodelled = worn, c.Unmodelled || !ok
		}
	case "minecraft:thrown_item_picked_up_by_entity", "minecraft:thrown_item_picked_up_by_player":
		only("item", "entity", "player")
		decoder.advSetTestedItem(c, conditions, "item")
		e, adult, ok := advAdultPredicate(conditions["entity"])
		c.Entity, c.Adult = e.typ, adult
		c.Unmodelled = c.Unmodelled || !ok || e.dimension != ""
		if player, has := conditions["player"]; has {
			worn, ok := decoder.advNotWearing(player)
			c.NotWearing, c.Unmodelled = worn, c.Unmodelled || !ok
		}
	case "minecraft:bred_animals":
		only("child", "parent", "partner")
		for _, key := range []string{"child", "parent", "partner"} {
			e, ok := advEntityPredicate(conditions[key], false)
			if !ok || e.dimension != "" {
				c.Unmodelled = true
				continue
			}
			switch key {
			case "child":
				c.Child = e.typ
			case "parent":
				c.Parent = e.typ
			case "partner":
				c.Partner = e.typ
			}
		}
	case "minecraft:tame_animal", "minecraft:summoned_entity":
		only("entity")
		e, ok := advEntityPredicate(conditions["entity"], true)
		if !ok || e.dimension != "" {
			c.Unmodelled = true
		} else {
			c.Entity, c.Variant = e.typ, e.variant
		}
	case "minecraft:location":
		decoder.advLocationTrigger(c, conditions)
	case "minecraft:placed_block", "minecraft:item_used_on_block", "minecraft:allay_drop_item_on_block",
		"minecraft:default_block_use", "minecraft:any_block_use":
		only("location", "player")
		_, player := conditions["player"]
		c.Unmodelled = player
		decoder.advLocations(list(conditions["location"], "location"), c)
	case "minecraft:villager_trade":
		only("player")
		if _, ok := conditions["player"]; ok {
			advVillagerPlayer(c, conditions)
		}
	case "minecraft:construct_beacon":
		only("level")
		if v, ok := conditions["level"]; ok {
			c.LevelMin, c.LevelMax = advRange("beacon level", v)
		}
	case "minecraft:effects_changed":
		only("effects", "source")
		effects := asObjOr(conditions["effects"])
		for _, name := range sortedKeys(effects) {
			c.Effects = append(c.Effects, registry.Qualify(name))
		}
		if len(effects) > 0 {
			if states := lootEffects(conditions["effects"]); slices.ContainsFunc(slices.Collect(maps.Values(states)), func(e registry.LootEffectPredicate) bool { return e != (registry.LootEffectPredicate{}) }) {
				c.EffectStates = states
			}
		}
		source, ok := advEntityPredicate(conditions["source"], true)
		c.Entity = source.typ
		_, hasSource := conditions["source"]
		c.Unmodelled = c.Unmodelled || !ok || source.dimension != "" || source.variant != "" || hasSource && source.typ == ""
	case "minecraft:player_hurt_entity":
		decoder.advHurtEntity(c, conditions)
	case "minecraft:killed_by_arrow":
		only("fired_from_weapon", "victims", "unique_entity_types")
		decoder.advSetTestedItem(c, conditions, "fired_from_weapon")
		advVictims(c, conditions)
		if v, ok := conditions["unique_entity_types"]; ok {
			c.UniqueMin, c.UniqueMax = advRange("unique entity types", v)
		}
	case "minecraft:channeled_lightning":
		only("victims")
		advVictims(c, conditions)
	case "minecraft:spear_mobs":
		only("count")
		if v, ok := conditions["count"]; ok {
			// vanilla: ExtraCodecs.POSITIVE_INT, which truncates a fraction
			n, isNum := num(v)
			if !isNum || int32(n) < 1 {
				fail("count is not a positive integer")
			}
			c.Count = int32(n)
		}
	case "minecraft:kill_mob_near_sculk_catalyst":
		c.Unmodelled = len(conditions) > 0
	case "minecraft:nether_travel", "minecraft:fall_from_height", "minecraft:levitation":
		advDistanceTrigger(c, conditions)
	case "minecraft:fall_after_explosion":
		advFallAfterExplosion(c, conditions)
	case "minecraft:used_totem":
		only("item")
		decoder.advSetTestedItem(c, conditions, "item")
	case "minecraft:entity_hurt_player":
		advHurtPlayer(c, conditions)
	case "minecraft:target_hit":
		advTargetHit(c, conditions)
	case "minecraft:started_riding", "minecraft:item_durability_changed", "minecraft:ride_entity_in_lava":
		decoder.advRiding(c, conditions)
	case "minecraft:enter_block":
		only("block", "blocks", "state")
		block, ok := advSingleName(conditions, "block", "blocks")
		if !ok {
			c.Unmodelled = true
			return
		}
		cond, ok := advBlockCheck(block, asObjOr(conditions["state"]), [3]int32{})
		c.Unmodelled = c.Unmodelled || !ok
		c.Where = []registry.AdvancementCondition{cond}
	case "minecraft:lightning_strike":
		advLightningStrike(c, conditions)
	case "minecraft:player_generates_container_loot":
		only("loot_table", "loot_tables")
		c.LootTable = advRequiredName(c, conditions, "loot_table", "loot_tables")
	case "minecraft:recipe_unlocked":
		only("recipes")
		c.RecipeID = advRequiredName(c, conditions, "recipe", "recipes")
	case "minecraft:used_ender_eye":
		only("distance")
		if v, ok := conditions["distance"]; ok {
			c.Distance.Horizontal = advDoubleRange("distance", v)
		}
	case "minecraft:voluntary_exile":
		c.Unmodelled = len(conditions) > 0
	case "minecraft:recipe_crafted", "minecraft:crafter_recipe_crafted":
		only("recipe_id", "recipes", "ingredients")
		c.RecipeID = advRequiredName(c, conditions, "recipe_id", "recipes")
		if ingredients, ok := conditions["ingredients"]; ok {
			decoder.advSetTestedItems(c, obj{"items": ingredients})
		}
	case "minecraft:using_item":
		only("item", "player")
		decoder.advSetTestedItem(c, conditions, "item")
		if player, ok := conditions["player"]; ok {
			looking, ok := advLookingAt(player)
			c.LookingAt, c.Unmodelled = looking, c.Unmodelled || !ok
		}
	case "minecraft:bee_nest_destroyed":
		advBeeNest(c, conditions)
	}
	// Any other trigger is never fired, whatever its conditions.
}

// advVictims reads a victims list of entity type predicates.
func advVictims(c *registry.AdvancementCriterion, conditions obj) {
	for _, victim := range list(conditions["victims"], "victims") {
		e, ok := advEntityPredicate(victim, false)
		if !ok || e.dimension != "" {
			c.Unmodelled = true
		} else {
			c.Victims = append(c.Victims, e.typ)
		}
	}
}

// advLocationTrigger reads the player's biome, structure, feet slot and
// stepping_on conditions.
func (decoder ResourceDecoder) advLocationTrigger(c *registry.AdvancementCriterion, conditions obj) {
	if len(conditions) == 0 {
		return
	}
	if !keysAre(conditions, "player") {
		c.Unmodelled = true
		return
	}
	players := list(conditions["player"], "player")
	if len(players) != 1 {
		c.Unmodelled = true
		return
	}
	cond := asObjOr(players[0])
	predicate := asObjOr(cond["predicate"])
	location := asObjOr(predicate["minecraft:location"])
	equipment := asObjOr(predicate["minecraft:equipment"])
	feet := asObjOr(equipment["feet"])
	stepping := asObjOr(predicate["minecraft:stepping_on"])
	block := asObjOr(stepping["block"])
	_, hasLocation := predicate["minecraft:location"]
	_, hasEquipment := predicate["minecraft:equipment"]
	_, hasStepping := predicate["minecraft:stepping_on"]
	if !advThisProperties(cond) || len(predicate) == 0 ||
		!keysWithin(predicate, "minecraft:location", "minecraft:equipment", "minecraft:stepping_on") ||
		hasLocation && (!keysWithin(location, "biomes", "structures") || len(location) == 0) ||
		hasEquipment && (!keysAre(equipment, "feet") || !keysAre(feet, "items")) ||
		hasStepping && (!keysAre(stepping, "block") || !keysAre(block, "blocks")) {
		c.Unmodelled = true
		return
	}
	if hasEquipment {
		c.Feet = decoder.advItems(feet["items"])
	}
	if hasStepping {
		check, ok := advBlockCheck(block["blocks"], nil, [3]int32{})
		if !ok {
			c.Unmodelled = true
			return
		}
		c.SteppingOn = check.Blocks
	}
	if value, present := location["biomes"]; present {
		if biome, ok := value.(string); ok && !strings.HasPrefix(biome, "#") {
			c.Biome = registry.Qualify(biome)
		} else {
			c.Unmodelled = true
		}
	}
	if value, present := location["structures"]; present {
		c.Structures = append([]string{}, holderSet(value, "structure", registry.StructureTagMembers)...)
		for _, id := range c.Structures {
			if !registry.StructureExists(id) {
				fail("unknown structure %s", id)
			}
		}
	}
}

// advVillagerPlayer reads a villager_trade's player condition: the height
// the player must be at or above.
func advVillagerPlayer(c *registry.AdvancementCriterion, conditions obj) {
	player := list(conditions["player"], "player")
	var predicate obj
	if len(player) == 1 {
		predicate = asObjOr(asObjOr(player[0])["predicate"])
	}
	position := asObjOr(asObjOr(predicate["minecraft:location"])["position"])
	y := asObjOr(position["y"])
	if lo, ok := num(y["min"]); keysAre(predicate, "minecraft:location") && keysAre(position, "y") && keysAre(y, "min") && ok {
		c.MinY, c.HasMinY = lo, true
	} else {
		c.Unmodelled = true
	}
}

// advDoubleRange reads a bound that is one number or a min/max object.
func advDoubleRange(what string, v any) registry.AdvancementRange {
	if n, ok := num(v); ok {
		return registry.AdvancementRange{Min: n, Max: n, HasMin: true, HasMax: true}
	}
	o := asObjOr(v)
	if !keysWithin(o, "min", "max") {
		fail("unsupported %s %v", what, v)
	}
	var r registry.AdvancementRange
	r.Min, r.HasMin = num(o["min"])
	r.Max, r.HasMax = num(o["max"])
	return r
}

// advPositionY reads a location predicate that bounds only the position's
// Y, or false for any other.
func advPositionY(location obj) (registry.AdvancementRange, bool) {
	position := asObjOr(location["position"])
	if !keysAre(location, "position") || !keysAre(position, "y") {
		return registry.AdvancementRange{}, false
	}
	return advDoubleRange("position y", position["y"]), true
}

// advDistanceTrigger reads nether_travel, fall_from_height and levitation:
// a distance, a start position's Y and a player condition on the player's
// Y. vanilla: DistanceTrigger.TriggerInstance, LevitationTrigger.TriggerInstance
func advDistanceTrigger(c *registry.AdvancementCriterion, conditions obj) {
	advOnly(c, conditions, "distance", "start_position", "player")
	advDistance(c, asObjOr(conditions["distance"]))
	// Levitation has no start_position field, so vanilla reads past one.
	if start, ok := conditions["start_position"]; ok && c.Trigger != "minecraft:levitation" {
		y, ok := advPositionY(asObjOr(start))
		c.StartY, c.Unmodelled = y, c.Unmodelled || !ok
	}
	if v, ok := conditions["player"]; ok {
		players := list(v, "player")
		var cond obj
		if len(players) == 1 {
			cond = asObjOr(players[0])
		}
		predicate := asObjOr(cond["predicate"])
		y, ok := advPositionY(asObjOr(predicate["minecraft:location"]))
		ok = ok && advThisProperties(cond) && keysAre(predicate, "minecraft:location")
		c.PlayerY, c.Unmodelled = y, c.Unmodelled || !ok
	}
}

// advFallAfterExplosion reads a distance, a start position's Y and the
// type of the entity that caused the explosion.
// vanilla: FallAfterExplosionTrigger.TriggerInstance
func advFallAfterExplosion(c *registry.AdvancementCriterion, conditions obj) {
	advOnly(c, conditions, "distance", "start_position", "cause")
	advDistance(c, asObjOr(conditions["distance"]))
	if start, ok := conditions["start_position"]; ok {
		y, ok := advPositionY(asObjOr(start))
		c.StartY, c.Unmodelled = y, c.Unmodelled || !ok
	}
	cause, ok := advEntityPredicate(conditions["cause"], false)
	if !ok || cause.dimension != "" {
		c.Unmodelled = true
	}
	c.Entity = cause.typ
}

// advDistance reads a distance predicate into c.Distance.
// vanilla: DistancePredicate
func advDistance(c *registry.AdvancementCriterion, distance obj) {
	advOnly(c, distance, "x", "y", "z", "horizontal", "absolute")
	for k, r := range map[string]*registry.AdvancementRange{
		"x": &c.Distance.X, "y": &c.Distance.Y, "z": &c.Distance.Z,
		"horizontal": &c.Distance.Horizontal, "absolute": &c.Distance.Absolute,
	} {
		if v, ok := distance[k]; ok {
			*r = advDoubleRange("distance "+k, v)
		}
	}
}

// advKillingBlowDirect reads a killing_blow predicate: none, any projectile
// damage, or projectile damage by one entity type. ok is false for any other
// shape.
func advKillingBlowDirect(blow any) (direct string, projectile, ok bool) {
	if blow == nil {
		return "", false, true
	}
	predicate := asObjOr(blow)
	if keysAre(predicate, "tags") && advTagIs(predicate["tags"], "minecraft:is_projectile") {
		return "", true, true
	}
	direct, _ = asObjOr(predicate["direct_entity"])["minecraft:entity_type"].(string)
	ok = keysAre(predicate, "direct_entity", "tags") && keysAre(asObjOr(predicate["direct_entity"]), "minecraft:entity_type") &&
		advTagIs(predicate["tags"], "minecraft:is_projectile") && direct != ""
	return qualifyRef(direct), false, ok
}

// advTagIs reports whether tags is the one tag predicate expecting tag. 26.3
// names the tag as a "#" holder set; a bare name is read for earlier packs.
// vanilla: TagPredicate.codec
func advTagIs(tags any, tag string) bool {
	l, _ := tags.([]any)
	if len(l) != 1 {
		return false
	}
	t := asObjOr(l[0])
	id, _ := t["id"].(string)
	return keysAre(t, "expected", "id") && t["expected"] == true && registry.Qualify(strings.TrimPrefix(id, "#")) == tag
}

// advThisProperties reports whether cond is an entity_properties condition
// on the trigger's own entity.
func advThisProperties(cond obj) bool {
	return keysAre(cond, "condition", "entity", "predicate") && cond["condition"] == "minecraft:entity_properties" &&
		cond["entity"] == "this"
}

// advStrip takes key out of a lone condition's entity predicate, returning
// the contexts without it and its value; has is false, and contexts are
// returned as they are, when there is no such key.
func advStrip(contexts any, key string) (rest, value any, has bool) {
	l, _ := contexts.([]any)
	if len(l) != 1 {
		return contexts, nil, false
	}
	first := asObjOr(l[0])
	predicate := asObjOr(first["predicate"])
	value, has = predicate[key]
	if !has {
		return contexts, nil, false
	}
	p := maps.Clone(predicate)
	delete(p, key)
	stripped := maps.Clone(first)
	stripped["predicate"] = p
	return []any{stripped}, value, true
}

// advHorizontalDistance takes a minimum horizontal distance out of an entity
// predicate, returning the contexts without it; ok is false for any other
// distance.
// vanilla: DistancePredicate.matches
func advHorizontalDistance(contexts any) (rest any, minimum float64, ok bool) {
	rest, dist, has := advStrip(contexts, "minecraft:distance")
	if !has {
		return contexts, 0, true
	}
	d := asObjOr(dist)
	h := asObjOr(d["horizontal"])
	minimum, isNum := num(h["min"])
	if !keysAre(d, "horizontal") || !keysAre(h, "min") || !isNum {
		return contexts, 0, false
	}
	return rest, minimum, true
}

// advCaptainPredicate is advEntityPredicate that may also want a raid
// captain: the ominous banner on the head, judged by the item alone as
// Raider.isCaptain does.
func advCaptainPredicate(contexts any) (e advEntity, captain, ok bool) {
	rest, equipment, has := advStrip(contexts, "minecraft:equipment")
	e, ok = advEntityPredicate(rest, false)
	if !has {
		return e, false, ok
	}
	head := asObjOr(asObjOr(equipment)["head"])
	item, _ := head["items"].(string)
	if !keysAre(asObjOr(equipment), "head") || qualifyRef(item) != "minecraft:white_banner" {
		return advEntity{}, false, false
	}
	return e, ok, ok
}

// advAdultPredicate is advEntityPredicate that may also want the entity not
// to be a baby (flags of is_baby false alone).
func advAdultPredicate(contexts any) (e advEntity, adult, ok bool) {
	rest, baby, adult, ok := advBabyFlag(contexts)
	if !ok || baby {
		return advEntity{}, false, false
	}
	e, ok = advEntityPredicate(rest, false)
	return e, adult, ok
}

// advBabyFlag strips flags of is_baby alone off an entity predicate: baby
// when they want true, adult when false; ok is false for any other flags.
func advBabyFlag(contexts any) (rest any, baby, adult, ok bool) {
	rest, flags, has := advStrip(contexts, "minecraft:flags")
	baby = reflect.DeepEqual(flags, map[string]any{"is_baby": true})
	adult = reflect.DeepEqual(flags, map[string]any{"is_baby": false})
	return rest, baby, adult, !has || baby || adult
}

// advNotWearing reads a player condition that the player wear an item of one
// set in none of its armor slots: an inverted any_of of one equipment slot
// each, covering the four with the same items. ok is false for any other
// shape.
func (decoder ResourceDecoder) advNotWearing(contexts any) (items []int32, ok bool) {
	l, _ := contexts.([]any)
	if len(l) != 1 {
		return nil, false
	}
	inverted := asObjOr(l[0])
	anyOf := asObjOr(inverted["term"])
	if !keysAre(inverted, "condition", "term") || inverted["condition"] != "minecraft:inverted" ||
		!keysAre(anyOf, "condition", "terms") || anyOf["condition"] != "minecraft:any_of" {
		return nil, false
	}
	var slots []string
	for i, t := range holderList(anyOf["terms"]) {
		cond := asObjOr(t)
		predicate := asObjOr(cond["predicate"])
		equipment := asObjOr(predicate["minecraft:equipment"])
		if !advThisProperties(cond) || !keysAre(predicate, "minecraft:equipment") || len(equipment) != 1 {
			return nil, false
		}
		for slot, v := range equipment {
			p := asObjOr(v)
			if !keysAre(p, "items") {
				return nil, false
			}
			got := decoder.advItems(p["items"])
			if i > 0 && !slices.Equal(got, items) {
				return nil, false
			}
			slots, items = append(slots, slot), got
		}
	}
	slices.Sort(slots)
	return items, slices.Equal(slots, []string{"chest", "feet", "head", "legs"})
}

// advLookingAt reads a player condition that only names the entity type the
// player looks at.
func advLookingAt(contexts any) (string, bool) {
	l, _ := contexts.([]any)
	if len(l) != 1 {
		return "", false
	}
	cond := asObjOr(l[0])
	predicate := asObjOr(cond["predicate"])
	player := asObjOr(predicate["minecraft:type_specific/player"])
	looking := asObjOr(player["looking_at"])
	typ, ok := looking["minecraft:entity_type"].(string)
	ok = ok && advThisProperties(cond) && keysAre(predicate, "minecraft:type_specific/player") &&
		keysAre(player, "looking_at") && keysAre(looking, "minecraft:entity_type")
	return qualifyRef(typ), ok
}

// advSingleName reads the one registry name under key, or under oldKey in
// packs written before 26.3; ok is false when neither is there and for a tag
// or several names, which the criterion's one name cannot hold.
func advSingleName(o obj, oldKey, key string) (string, bool) {
	v, has := o[key]
	if !has {
		v = o[oldKey]
	}
	if l, isList := v.([]any); isList && len(l) == 1 {
		v = l[0]
	}
	s, isStr := v.(string)
	if !isStr || strings.HasPrefix(s, "#") {
		return "", false
	}
	return qualifiedID(s, key), true
}

// advRequiredName is advSingleName for a field vanilla requires, marking c
// unmodelled for a name it cannot hold.
func advRequiredName(c *registry.AdvancementCriterion, o obj, oldKey, key string) string {
	if _, has := o[oldKey]; !has {
		required(o, key)
	}
	name, ok := advSingleName(o, oldKey, key)
	c.Unmodelled = c.Unmodelled || !ok
	return name
}

// advBeeNest reads a bee_nest_destroyed: the block, an enchantment of level
// 1 or more on the tool, and the exact number of bees inside.
// vanilla: BeeNestDestroyedTrigger.TriggerInstance
func advBeeNest(c *registry.AdvancementCriterion, conditions obj) {
	advOnly(c, conditions, "block", "blocks", "item", "num_bees_inside")
	_, hasOld := conditions["block"]
	if _, has := conditions["blocks"]; has || hasOld {
		block, ok := advSingleName(conditions, "block", "blocks")
		c.Block, c.Unmodelled = block, c.Unmodelled || !ok
	}
	if v, has := conditions["num_bees_inside"]; has {
		n, isNum := num(v)
		c.Bees, c.Unmodelled = int32(n), c.Unmodelled || !isNum || n < 1 || n != float64(int32(n))
	}
	if v, has := conditions["item"]; has {
		enchantment, ok := advSilkLikeEnchantment(asObjOr(v))
		c.Enchantment, c.Unmodelled = enchantment, c.Unmodelled || !ok
	}
}

// advSilkLikeEnchantment reads an item predicate that only wants one
// enchantment at level 1 or more and returns its ID.
func advSilkLikeEnchantment(item obj) (string, bool) {
	predicates := asObjOr(item["predicates"])
	l, _ := predicates["minecraft:enchantments"].([]any)
	if !keysAre(item, "predicates") || !keysAre(predicates, "minecraft:enchantments") || len(l) != 1 {
		return "", false
	}
	e := asObjOr(l[0])
	levels := asObjOr(e["levels"])
	id, _ := e["enchantments"].(string)
	lo, _ := num(levels["min"])
	if !keysAre(e, "enchantments", "levels") || !keysAre(levels, "min") || lo != 1 || id == "" || id[0] == '#' {
		return "", false
	}
	return registry.Qualify(id), true
}

// advLightningStrike reads a lightning_strike: the distance from the player
// to the bolt, the blocks it set on fire, and the type of the bystander.
// vanilla: LightningStrikeTrigger.TriggerInstance
func advLightningStrike(c *registry.AdvancementCriterion, conditions obj) {
	advOnly(c, conditions, "lightning", "bystander")
	if v, has := conditions["lightning"]; has {
		l, _ := v.([]any)
		var first obj
		if len(l) == 1 {
			first = asObjOr(l[0])
		}
		predicate := asObjOr(first["predicate"])
		specific := asObjOr(predicate["minecraft:type_specific/lightning"])
		if !advThisProperties(first) || !keysWithin(predicate, "minecraft:distance", "minecraft:type_specific/lightning") ||
			!keysWithin(specific, "blocks_set_on_fire") {
			c.Unmodelled = true
		} else {
			advDistance(c, asObjOr(predicate["minecraft:distance"]))
			if fires, has := specific["blocks_set_on_fire"]; has {
				c.Fires = advDoubleRange("blocks set on fire", fires)
			}
		}
	}
	e, ok := advEntityPredicate(conditions["bystander"], false)
	if !ok || e.dimension != "" {
		c.Unmodelled = true
	} else {
		c.Entity = e.typ
	}
}

// advBodyPredicate reads an entity predicate of a type, variant and baby flag
// with, optionally, an equipment.body item (and a damage of 0), or false for
// one beyond them.
func (decoder ResourceDecoder) advBodyPredicate(contexts any) (e advEntity, baby, adult bool, body []int32, undamaged, ok bool) {
	rest, baby, adult, flagsOK := advBabyFlag(contexts)
	rest, equipment, has := advStrip(rest, "minecraft:equipment")
	e, ok = advEntityPredicate(rest, true)
	ok = ok && flagsOK
	if !has {
		return e, baby, adult, nil, false, ok && e.dimension == ""
	}
	slot := asObjOr(asObjOr(equipment)["body"])
	components := asObjOr(slot["components"])
	if _, hasItems := slot["items"]; !ok || e.dimension != "" || !keysAre(asObjOr(equipment), "body") ||
		!keysWithin(slot, "items", "components") || !hasItems || !keysWithin(components, "minecraft:damage") {
		return advEntity{}, false, false, nil, false, false
	}
	if damage, has := components["minecraft:damage"]; has {
		if n, isNum := num(damage); !isNum || n != 0 {
			return advEntity{}, false, false, nil, false, false
		}
		undamaged = true
	}
	return e, baby, adult, decoder.advItems(slot["items"]), undamaged, true
}

// advHurtEntity reads a player_hurt_entity's modelled shapes: projectile
// damage by one kind of shaft, or a player's mace smash that dealt at least
// a damage.
func (decoder ResourceDecoder) advHurtEntity(c *registry.AdvancementCriterion, conditions obj) {
	if !keysWithin(conditions, "damage", "entity") {
		c.Unmodelled = true
		return
	}
	damage := asObjOr(conditions["damage"])
	kind := asObjOr(damage["type"])
	direct := asObjOr(kind["direct_entity"])
	typ, _ := direct["minecraft:entity_type"].(string)
	equipment := asObjOr(direct["minecraft:equipment"])
	_, entity := conditions["entity"]
	switch {
	case entity || !keysAre(kind, "direct_entity", "tags"):
		c.Unmodelled = true
	case keysAre(damage, "type") && keysAre(direct, "minecraft:entity_type") &&
		advTagIs(kind["tags"], "minecraft:is_projectile"):
		c.Direct = qualifyRef(typ)
	case keysAre(damage, "type", "dealt") && keysAre(direct, "minecraft:entity_type", "minecraft:equipment") &&
		keysAre(equipment, "mainhand") && qualifyRef(typ) == "minecraft:player" && advTagIs(kind["tags"], "minecraft:mace_smash"):
		decoder.advSetSingleItem(c, obj{"item": equipment["mainhand"]}, "item")
		dealt := advDoubleRange("dealt", damage["dealt"])
		c.SmashMin, c.Unmodelled = dealt.Min, c.Unmodelled || !dealt.HasMin || dealt.HasMax
	default:
		c.Unmodelled = true
	}
}

// advHurtPlayer reads an entity_hurt_player's one modelled shape: any hurt,
// or one of the is_projectile tag, either optionally blocked by a shield.
func advHurtPlayer(c *registry.AdvancementCriterion, conditions obj) {
	advOnly(c, conditions, "damage")
	damage := asObjOr(conditions["damage"])
	kind, typed := damage["type"]
	c.Projectile = typed
	c.Blocked, _ = damage["blocked"].(bool)
	_, hasBlocked := damage["blocked"]
	if !keysWithin(damage, "type", "blocked") || hasBlocked && !c.Blocked ||
		typed && (!keysAre(asObjOr(kind), "tags") || !advTagIs(asObjOr(kind)["tags"], "minecraft:is_projectile")) {
		c.Unmodelled = true
	}
}

// advTargetHit reads a target_hit: the signal and the projectile's
// distance from the player.
func advTargetHit(c *registry.AdvancementCriterion, conditions obj) {
	advOnly(c, conditions, "signal_strength", "projectile")
	if v, ok := conditions["signal_strength"]; ok {
		c.Signal = advDoubleRange("signal strength", v)
	}
	v, ok := conditions["projectile"]
	if !ok {
		return
	}
	l, _ := v.([]any)
	var first obj
	if len(l) == 1 {
		first = asObjOr(l[0])
	}
	predicate := asObjOr(first["predicate"])
	if !advThisProperties(first) || !keysAre(predicate, "minecraft:distance") {
		c.Unmodelled = true
		return
	}
	advDistance(c, asObjOr(predicate["minecraft:distance"]))
}

// advBrewedPotion reads brewed_potion's potion predicate: the potions it
// accepts, or unmodelled for one that tests the effects or a tag.
// vanilla: PotionsPredicate.CODEC
func advBrewedPotion(c *registry.AdvancementCriterion, v any) {
	if v == nil {
		return
	}
	predicate := object(v, "a potions predicate")
	if !keysWithin(predicate, "potions") {
		c.Unmodelled = true
		return
	}
	names, ok := predicate["potions"]
	if !ok {
		return
	}
	if tag, ok := names.(string); ok && strings.HasPrefix(tag, "#") {
		c.Unmodelled = true
		return
	}
	c.Potions = []int32{}
	for _, name := range holderSet(names, "potion", nil) {
		id, ok := registry.PotionID(registry.Qualify(name))
		if !ok {
			fail("unknown potion %s", name)
		}
		c.Potions = append(c.Potions, id)
	}
}

// advRiding reads started_riding, item_durability_changed and
// ride_entity_in_lava: a held item, a distance and a player condition on
// the dimension, the vehicle's type and one of its passengers' types.
func (decoder ResourceDecoder) advRiding(c *registry.AdvancementCriterion, conditions obj) {
	if c.Trigger == "minecraft:item_durability_changed" {
		advOnly(c, conditions, "player", "item", "durability", "delta")
		decoder.advSetTestedItem(c, conditions, "item")
		for key, bound := range map[string]*registry.AdvancementRange{"durability": &c.Durability, "delta": &c.Delta} {
			if v, ok := conditions[key]; ok {
				*bound = advDoubleRange(key, v)
			}
		}
	} else {
		advOnly(c, conditions, "player", "item", "distance")
		decoder.advSetSingleItem(c, conditions, "item")
	}
	advDistance(c, asObjOr(conditions["distance"]))
	v, ok := conditions["player"]
	if !ok {
		return
	}
	rest, ride, riding := advStrip(v, "minecraft:vehicle")
	e, ok := advEntityPredicate(rest, false)
	c.Dimension = e.dimension
	if v == nil || !ok || e.typ != "" {
		c.Unmodelled = true
		return
	}
	if !riding {
		return
	}
	vehicle := asObjOr(ride)
	aboard, carries := vehicle["minecraft:passenger"]
	typ, _ := vehicle["minecraft:entity_type"].(string)
	passenger, _ := asObjOr(aboard)["minecraft:entity_type"].(string)
	c.Vehicle, c.Passenger = qualifyRef(typ), qualifyRef(passenger)
	if typ == "" || !keysWithin(vehicle, "minecraft:entity_type", "minecraft:passenger") ||
		carries && (passenger == "" || !keysAre(asObjOr(aboard), "minecraft:entity_type")) {
		c.Unmodelled = true
	}
}
