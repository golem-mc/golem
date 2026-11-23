package main

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/golem-mc/golem/tools/internal/gogen"
	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func init() {
	register(generator{
		name: "loot",
		doc:  "registry/loot_gen.go: the block, entity, shearing, chest, barrel and other loot tables",
		run:  runLoot,
	})
}

// Inputs: the data pack's loot tables and tags, and the item registry from the data
// report. Tags are resolved into their members, so no tag name reaches the
// generated data.
//
// Every block and entity table is read, with the tables they reference, and the
// shearing table.
// Each becomes the LootTable of registry/loot.go: pools, entries, and the typed
// conditions and functions internal/server/rules switches over.
//
// The reading refuses what it cannot model instead of approximating it: an
// unknown condition or function stops the generation with the table's name and
// shape. Only container, archaeology, spawner and equipment tables are left
// out in that case, so a container keeps its loot table tag unrolled.

// lootReader holds what the walk over the tables collects.
type lootReader struct {
	dp        *vanilla.DataPack
	tables    map[string]string // table name -> Go literal
	pending   nameQueue         // tables named but not yet read
	usedItems map[string]bool
	skipped   []string
	inline    []string
}

func lootQualified(name string) string {
	if strings.HasPrefix(name, "minecraft:") {
		return name
	}
	return "minecraft:" + name
}

// tagMembers lists the members of a tag, the # references inside it resolved
// and each tag followed once.
func (r *lootReader) tagMembers(kind, name string, seen map[string]bool) []string {
	if seen[name] {
		return nil
	}
	seen[name] = true
	file := path.Join("minecraft/tags", kind, strings.TrimPrefix(name, "minecraft:")+".json")
	b, err := dpRead(r.dp, file)
	if err != nil {
		fail("%v", err)
	}
	var members []string
	for _, v := range decode(b, file).list("values") {
		value := asStr(v, "a tag value")
		if strings.HasPrefix(value, "#") {
			members = append(members, r.tagMembers(kind, value[1:], seen)...)
		} else {
			members = append(members, lootQualified(value))
		}
	}
	return members
}

func (r *lootReader) sortedTag(kind, name string) []string {
	members := r.tagMembers(kind, name, map[string]bool{})
	slices.Sort(members)
	return members
}

// fl is a float the way the data pack wrote it, as a Go literal.
func fl(v any, where string) string {
	f, ok := asNum(v)
	if !ok {
		fail("%s: %v is not a number", where, v)
	}
	return gogen.Float(f)
}

// number reads a number provider: a bare JSON number or one of the typed
// shapes. The tables use constant, uniform, binomial and sum providers; anything
// else is a shape the evaluator does not know.
func number(v any, where string) string {
	if _, ok := asNum(v); ok {
		return fmt.Sprintf("LootConstant{Value: %s}", fl(v, where))
	}
	o := asObj(v, "a number provider")
	switch kind := o.strOr("type", ""); kind {
	case "minecraft:constant":
		return fmt.Sprintf("LootConstant{Value: %s}", fl(o.field("value"), where))
	case "minecraft:uniform":
		return fmt.Sprintf("LootUniform{Min: %s, Max: %s}", number(o.field("min"), where), number(o.field("max"), where))
	case "minecraft:sum", "minecraft:add":
		key := "summands"
		if kind == "minecraft:add" {
			key = "inputs"
		}
		if !keysWithin(o, "type", key) {
			fail("%s: unmodelled sum fields", where)
		}
		var summands []string
		for _, v := range o.list(key) {
			summands = append(summands, number(v, where))
		}
		return "LootSum{Summands: []LootNumber{" + strings.Join(summands, ", ") + "}}"
	case "minecraft:binomial":
		return fmt.Sprintf("LootBinomial{N: %s, P: %s}", number(o.field("n"), where), number(o.field("p"), where))
	default:
		fail("%s: number provider %s is one the reading does not know", where, kind)
	}
	return ""
}

// nameList reads what an item or enchantment predicate names: one name, a list
// of them, or a tag with the leading #, resolved into the names the version
// listed (the tag lives among the tags of kind).
func (r *lootReader) nameList(kind string, value any) []string {
	switch v := value.(type) {
	case []any:
		out := make([]string, len(v))
		for i, x := range v {
			out[i] = lootQualified(asStr(x, "a name"))
		}
		return out
	case string:
		if strings.HasPrefix(v, "#") {
			return r.sortedTag(kind, v[1:])
		}
		return []string{lootQualified(v)}
	}
	fail("a name list of a shape the reading does not know: %v", value)
	return nil
}

// levelBounds reads the level range an enchantment predicate asks within:
// absent for any, an exact int, or a min and a max.
func levelBounds(v any) (int, int) {
	if v == nil {
		return 0, 0
	}
	if n, ok := asNum(v); ok {
		return int(n), int(n)
	}
	o := asObj(v, "a level range")
	return o.intOr("min", 0), o.intOr("max", 0)
}

func quotedList(names []string) string { return quoteAll(names) }

// itemPredicate reads an item predicate, the shape match_tool and the
// equipment predicates carry: the items, and the component predicates, of
// which only the enchantments one is used.
func (r *lootReader) itemPredicate(p obj, where string) string {
	var parts []string
	if p.has("items") {
		parts = append(parts, "Items: []string{"+quotedList(r.nameList("item", p["items"]))+"}")
	} else {
		parts = append(parts, "Items: nil")
	}
	predicates, _ := p.obj("predicates")
	for _, component := range []struct{ key, field string }{{"minecraft:enchantments", "Enchantments"}, {"minecraft:stored_enchantments", "StoredEnchantments"}} {
		if enchants, present := predicates[component.key]; present {
			list, ok := enchants.([]any)
			if !ok {
				fail("%s: enchantment predicates must be an array", where)
			}
			var entries []string
			for _, x := range list {
				e := asObj(x, "an enchantment predicate")
				if !keysWithin(e, "enchantments", "levels") {
					fail("%s: enchantment predicate field %v is one the reading does not know", where, sortedKeys(e))
				}
				var names []string
				if e.has("enchantments") {
					names = r.nameList("enchantment", e["enchantments"])
				}
				low, high := levelBounds(e["levels"])
				entries = append(entries, fmt.Sprintf("LootEnchantments{Enchantments: []string{%s}, Min: %d, Max: %d}", quotedList(names), low, high))
			}
			parts = append(parts, component.field+": []LootEnchantments{"+strings.Join(entries, ", ")+"}")
		} else if component.field == "Enchantments" {
			parts = append(parts, component.field+": nil")
		}
	}
	if dye, ok := predicates.obj("minecraft:dyed_color"); ok {
		if len(dye) != 0 {
			fail("%s: only dyed colour presence is modelled", where)
		}
		parts = append(parts, "HasDyedColor: true")
	}
	if mapID, ok := predicates.obj("minecraft:map_id"); ok {
		if len(mapID) != 0 {
			fail("%s: only map ID presence is modelled", where)
		}
		parts = append(parts, "HasMapID: true")
	}
	if !keysWithin(predicates, "minecraft:enchantments", "minecraft:stored_enchantments", "minecraft:dyed_color", "minecraft:map_id") {
		fail("%s: unsupported item component predicate", where)
	}
	if !keysWithin(p, "items", "predicates") {
		// A count or a component matcher the tables do not use would be read
		// as absence, which would read the wrong drops.
		fail("%s: item predicate fields %v are ones the reading does not know", where, sortedKeys(p))
	}
	return "LootItemPredicate{" + strings.Join(parts, ", ") + "}"
}

func lootBool(v any) string {
	if b, _ := v.(bool); b {
		return "true"
	}
	return "false"
}

// entityPredicate reads an entity predicate, the shapes the block and
// roster-entity tables ask after: the flags, the vehicle, the sheep's colour
// and fleece, the entity type and the equipment in the main hand.
func (r *lootReader) entityPredicate(p obj, where string) string {
	var fields []string
	flags, _ := p.obj("minecraft:flags")
	for _, kf := range []struct{ key, field string }{{"is_on_fire", "OnFire"}, {"is_baby", "Baby"}, {"is_on_ground", "OnGround"}, {"is_flying", "Flying"}} {
		if flags.has(kf.key) {
			fields = append(fields, fmt.Sprintf("%s: lootBool(%s)", kf.field, lootBool(flags[kf.key])))
		}
	}
	if !keysWithin(flags, "is_on_fire", "is_baby", "is_on_ground", "is_flying") {
		fail("%s: entity flags %v are ones the reading does not know", where, sortedKeys(flags))
	}
	if p.has("minecraft:vehicle") {
		vehicle := asObj(p["minecraft:vehicle"], "a vehicle predicate")
		if !keysAre(vehicle, "minecraft:entity_type") {
			fail("%s: vehicle predicate %v is one the reading does not know", where, map[string]any(vehicle))
		}
		value := vehicle.str("minecraft:entity_type")
		if strings.HasPrefix(value, "#") {
			fail("%s: a vehicle named by the tag %s is more than the reading does", where, value)
		}
		fields = append(fields, "Vehicle: "+gogen.Quote(lootQualified(value)))
	}
	components, _ := p.obj("minecraft:components")
	if !keysWithin(components, "minecraft:sheep/color", "minecraft:mooshroom/variant", "minecraft:chicken/variant") {
		fail("%s: entity components %v are ones the reading does not know", where, sortedKeys(components))
	}
	if components.has("minecraft:sheep/color") {
		fields = append(fields, "SheepColour: "+gogen.Quote(components.str("minecraft:sheep/color")))
	}
	if components.has("minecraft:mooshroom/variant") {
		fields = append(fields, "Mooshroom: "+gogen.Quote(components.str("minecraft:mooshroom/variant")))
	}
	if components.has("minecraft:chicken/variant") {
		fields = append(fields, "ChickenVariant: "+gogen.Quote(lootQualified(components.str("minecraft:chicken/variant"))))
	}
	if p.has("minecraft:type_specific/sheep") {
		sheep := asObj(p["minecraft:type_specific/sheep"], "a sheep predicate")
		if !keysAre(sheep, "sheared") {
			fail("%s: sheep predicate %v is one the reading does not know", where, map[string]any(sheep))
		}
		fields = append(fields, fmt.Sprintf("Sheared: lootBool(%s)", lootBool(sheep["sheared"])))
	}
	if p.has("minecraft:type_specific/fishing_hook") {
		hook := asObj(p["minecraft:type_specific/fishing_hook"], "a fishing hook predicate")
		if !keysAre(hook, "in_open_water") {
			fail("%s: fishing hook predicate %v is one the reading does not know", where, map[string]any(hook))
		}
		fields = append(fields, fmt.Sprintf("FishingHook: &LootFishingHookPredicate{InOpenWater: lootBool(%s)}", lootBool(hook["in_open_water"])))
	}
	if p.has("minecraft:type_specific/cube_mob") {
		cube := asObj(p["minecraft:type_specific/cube_mob"], "a cube mob predicate")
		if !keysAre(cube, "size") {
			fail("%s: cube mob predicate %v is one the reading does not know", where, map[string]any(cube))
		}
		low, high := limitRange(cube["size"], where)
		fields = append(fields, fmt.Sprintf("CubeSizeMin: %s, CubeSizeMax: %s", low, high))
	}
	if p.has("minecraft:type_specific/raider") {
		raider := asObj(p["minecraft:type_specific/raider"], "a raider predicate")
		if !keysWithin(raider, "has_raid", "is_captain") {
			fail("%s: raider predicate %v is one the reading does not know", where, map[string]any(raider))
		}
		fields = append(fields, fmt.Sprintf("Captain: lootBool(%s), HasRaid: lootBool(%s)", lootBool(raider["is_captain"]), lootBool(raider["has_raid"])))
	}
	if p.has("minecraft:periodic_tick") {
		fields = append(fields, fmt.Sprintf("Period: %d", int(p.num("minecraft:periodic_tick"))))
	}
	if p.has("minecraft:movement") {
		movement := asObj(p["minecraft:movement"], "a movement predicate")
		if !keysWithin(movement, "horizontal_speed", "fall_distance") {
			fail("%s: movement predicate %v is one the reading does not know", where, map[string]any(movement))
		}
		var parts []string
		for _, kf := range []struct{ key, field string }{{"horizontal_speed", "HorizontalSpeed"}, {"fall_distance", "FallDistance"}} {
			if movement.has(kf.key) {
				bound := goRange(doubleRange(where, kf.key, movement[kf.key]))
				if bound == "" {
					bound = "AdvancementRange{}"
				}
				parts = append(parts, kf.field+": "+bound)
			}
		}
		fields = append(fields, "Movement: &LootMovement{"+strings.Join(parts, ", ")+"}")
	}
	if p.has("minecraft:movement_affected_by") {
		affected := asObj(p["minecraft:movement_affected_by"], "a movement_affected_by predicate")
		block, _ := affected.obj("block")
		if !keysAre(affected, "block") || !keysAre(block, "blocks") || !strings.HasPrefix(block.str("blocks"), "#") {
			fail("%s: movement_affected_by %v is more than a block tag", where, map[string]any(affected))
		}
		fields = append(fields, "MovementAffectedBy: &LootLocationCheck{Block: "+gogen.Quote(block.str("blocks"))+"}")
	}
	if p.has("minecraft:entity_type") {
		value := p.str("minecraft:entity_type")
		if strings.HasPrefix(value, "#") {
			fields = append(fields, "Types: []string{"+quotedList(r.sortedTag("entity_type", value[1:]))+"}")
		} else {
			fields = append(fields, "Type: "+gogen.Quote(lootQualified(value)))
		}
	}
	equipment, _ := p.obj("minecraft:equipment")
	if !keysWithin(equipment, "mainhand") {
		fail("%s: equipment slots %v are ones the reading does not know", where, sortedKeys(equipment))
	}
	if equipment.has("mainhand") {
		fields = append(fields, fmt.Sprintf("Mainhand: lootItem(%s)", r.itemPredicate(asObj(equipment["mainhand"], "a mainhand predicate"), where)))
	}
	if !keysWithin(p, "minecraft:flags", "minecraft:vehicle", "minecraft:components", "minecraft:type_specific/sheep",
		"minecraft:type_specific/cube_mob", "minecraft:type_specific/raider", "minecraft:entity_type", "minecraft:equipment",
		"minecraft:periodic_tick", "minecraft:movement", "minecraft:movement_affected_by", "minecraft:type_specific/fishing_hook") {
		fail("%s: entity predicate keys %v are ones the reading does not know", where, sortedKeys(p))
	}
	return "LootEntityPredicate{" + strings.Join(fields, ", ") + "}"
}

func stateProperties(props obj) string {
	var parts []string
	for _, k := range sortedKeys(props) {
		v, _ := props[k].(string)
		parts = append(parts, fmt.Sprintf("LootStateProperty{Key: %s, Value: %s}", gogen.Quote(k), gogen.Quote(v)))
	}
	return strings.Join(parts, ", ")
}

// onlyExpectedTag reports whether tags is the one entry {expected: true, id}
// naming a tag, and returns the tag without its "#".
func onlyExpectedTag(tags []any) (string, bool) {
	if len(tags) != 1 {
		return "", false
	}
	t := asObjOr(tags[0])
	if !keysAre(t, "expected", "id") || t["expected"] != true {
		return "", false
	}
	id, _ := t["id"].(string)
	return strings.CutPrefix(id, "#")
}

// condition reads one condition, as the struct the evaluator switches over.
func (r *lootReader) condition(c obj, where string) string {
	switch kind := c.strOr("condition", ""); kind {
	case "minecraft:survives_explosion":
		return "LootSurvivesExplosion{}"
	case "minecraft:killed_by_player":
		return "LootKilledByPlayer{}"
	case "minecraft:match_tool":
		predicate, _ := c.obj("predicate")
		return fmt.Sprintf("LootMatchTool{Predicate: %s}", r.itemPredicate(predicate, where))
	case "minecraft:random_chance":
		return fmt.Sprintf("LootRandomChance{Chance: %s}", number(c.field("chance"), where))
	case "minecraft:random_chance_with_enchanted_bonus":
		enchanted, _ := c.obj("enchanted_chance")
		if enchanted["type"] != "minecraft:linear" {
			fail("%s: enchanted chance %v is not the linear shape the reading knows", where, map[string]any(enchanted))
		}
		return fmt.Sprintf("LootEnchantedChance{Enchantment: %s, Unenchanted: %s, Base: %s, PerLevel: %s}",
			gogen.Quote(lootQualified(c.str("enchantment"))), fl(c.field("unenchanted_chance"), where),
			fl(enchanted.field("base"), where), fl(enchanted.field("per_level_above_first"), where))
	case "minecraft:table_bonus":
		var chances []string
		for _, v := range c.list("chances") {
			chances = append(chances, fl(v, where))
		}
		return fmt.Sprintf("LootTableBonus{Enchantment: %s, Chances: []float64{%s}}",
			gogen.Quote(lootQualified(c.str("enchantment"))), strings.Join(chances, ", "))
	case "minecraft:block_state_property":
		props, _ := c.obj("properties")
		return fmt.Sprintf("LootBlockState{Block: %s, Properties: []LootStateProperty{%s}}",
			gogen.Quote(lootQualified(c.str("block"))), stateProperties(props))
	case "minecraft:match_block":
		if !keysWithin(c, "condition", "blocks", "state") {
			fail("%s: unmodelled match_block predicate %v", where, c)
		}
		blocks := r.nameList("block", c.field("blocks"))
		props, _ := c.obj("state")
		var terms []string
		for _, block := range blocks {
			terms = append(terms, fmt.Sprintf("LootBlockState{Block: %s, Properties: []LootStateProperty{%s}}",
				gogen.Quote(block), stateProperties(props)))
		}
		return "LootAnyOf{Terms: []LootCondition{" + strings.Join(terms, ", ") + "}}"
	case "minecraft:location_check":
		var offset [3]int
		for i, axis := range []string{"offsetX", "offsetY", "offsetZ"} {
			offset[i] = c.intOr(axis, 0)
		}
		predicate, _ := c.obj("predicate")
		if !keysWithin(predicate, "block", "biomes") {
			fail("%s: location predicate fields %v are ones the reading does not know", where, sortedKeys(predicate))
		}
		var parts []string
		block, _ := predicate.obj("block")
		if block.has("blocks") {
			if !keysWithin(block, "blocks", "state") {
				fail("%s: location predicate block %v is one the reading does not know", where, map[string]any(block))
			}
			state, _ := block.obj("state")
			parts = append(parts, fmt.Sprintf("Block: %s, Properties: []LootStateProperty{%s}",
				gogen.Quote(lootQualified(block.str("blocks"))), stateProperties(state)))
		}
		if predicate.has("biomes") {
			parts = append(parts, fmt.Sprintf("Biomes: []string{%s}", quotedList(r.nameList("worldgen/biome", predicate["biomes"]))))
		}
		if len(parts) == 0 {
			fail("%s: location predicate %v asks after nothing the reading knows", where, map[string]any(predicate))
		}
		parts = append(parts, fmt.Sprintf("OffsetX: %d, OffsetY: %d, OffsetZ: %d", offset[0], offset[1], offset[2]))
		return "LootLocationCheck{" + strings.Join(parts, ", ") + "}"
	case "minecraft:entity_properties":
		predicate, _ := c.obj("predicate")
		return fmt.Sprintf("LootEntityProperties{Entity: %s, Predicate: %s}",
			gogen.Quote(c.strOr("entity", "this")), r.entityPredicate(predicate, where))
	case "minecraft:damage_source_properties":
		pred, _ := c.obj("predicate")
		tags := pred.listOr("tags")
		if tag, ok := onlyExpectedTag(tags); keysAre(pred, "tags") && ok {
			return fmt.Sprintf("LootDamageSource{DamageTypes: &DamageTypeSet{Tag: %s, IsTag: true}}", damageTypeTagIdent(lootQualified(tag)))
		}
		direct, _ := pred.obj("direct_entity")
		if tag, ok := onlyExpectedTag(tags); keysAre(pred, "direct_entity", "tags") && keysAre(direct, "minecraft:entity_type") && ok {
			return fmt.Sprintf("LootDamageSource{DirectType: %s, DamageTypes: &DamageTypeSet{Tag: %s, IsTag: true}}",
				gogen.Quote(lootQualified(direct.str("minecraft:entity_type"))), damageTypeTagIdent(lootQualified(tag)))
		}
		source, _ := pred.obj("source_entity")
		components, _ := source.obj("minecraft:components")
		if !keysAre(pred, "source_entity") || !keysWithin(source, "minecraft:entity_type", "minecraft:components") ||
			!keysWithin(components, "minecraft:frog/variant") {
			fail("%s: damage source predicate %v is one the reading does not know", where, map[string]any(pred))
		}
		return fmt.Sprintf("LootDamageSource{SourceType: %s, FrogVariant: %s}",
			gogen.Quote(lootQualified(source.str("minecraft:entity_type"))), gogen.Quote(components.strOr("minecraft:frog/variant", "")))
	case "minecraft:inverted":
		return fmt.Sprintf("LootInverted{Term: %s}", r.condition(asObj(c.field("term"), "an inverted term"), where))
	case "minecraft:any_of", "minecraft:all_of":
		var terms []string
		for _, t := range c.listOr("terms") {
			terms = append(terms, r.condition(asObj(t, "a condition term"), where))
		}
		name := "LootAllOf"
		if kind == "minecraft:any_of" {
			name = "LootAnyOf"
		}
		return fmt.Sprintf("%s{Terms: []LootCondition{%s}}", name, strings.Join(terms, ", "))
	default:
		fail("%s: %s is a condition the reading does not know", where, kind)
	}
	return ""
}

func (r *lootReader) conditions(list []any, where string) string {
	if len(list) == 0 {
		return "nil"
	}
	parts := make([]string, len(list))
	for i, c := range list {
		parts[i] = r.condition(asObj(c, "a condition"), where)
	}
	return "[]LootCondition{" + strings.Join(parts, ", ") + "}"
}

func bonusFormula(f obj, where string) string {
	switch kind := f.strOr("formula", ""); kind {
	case "minecraft:ore_drops":
		return "LootOreDrops{}"
	case "minecraft:uniform_bonus_count":
		params, _ := f.obj("parameters")
		return fmt.Sprintf("LootUniformBonus{Multiplier: %d}", params.intOr("bonusMultiplier", 1))
	case "minecraft:binomial_with_bonus_count":
		params, _ := f.obj("parameters")
		return fmt.Sprintf("LootBinomialBonus{Extra: %d, Probability: %s}", params.intOr("extra", 0), fl(params.numOr("probability", 0), where))
	default:
		fail("%s: bonus formula %s is one the reading does not know", where, kind)
	}
	return ""
}

// limitRange reads an IntRange: absent ends nil, numbers otherwise.
func limitRange(v any, where string) (string, string) {
	if v == nil {
		return "nil", "nil"
	}
	bound := func(x any) string {
		if x == nil {
			return "nil"
		}
		return "lootFloat(" + fl(x, where) + ")"
	}
	if _, ok := asNum(v); ok {
		return bound(v), bound(v)
	}
	o := asObj(v, "a range")
	return bound(o["min"]), bound(o["max"])
}

// intLimit reads a limit_count's IntLimit: absent ends nil, int providers
// otherwise.
func intLimit(v any, where string) (string, string) {
	if v == nil {
		return "nil", "nil"
	}
	o := asObj(v, "a limit")
	bound := func(key string) string {
		if x, ok := o[key]; ok {
			return number(x, where)
		}
		return "nil"
	}
	return bound("min"), bound("max")
}

// enchantOptions reads the Options of an enchanting function: nil for every
// enchantment when the table names none, else the names the list or tag
// resolves to.
func (r *lootReader) enchantOptions(f obj) string {
	if !f.has("options") || enchantTag(f) != "" {
		return "nil"
	}
	return "[]string{" + quotedList(r.nameList("enchantment", f["options"])) + "}"
}

// enchantTag is the OptionsTag field of an enchant function whose options
// name a tag: the tag stays a reference the server binds to the loaded tags.
func enchantTag(f obj) string {
	if s, ok := f["options"].(string); ok && strings.HasPrefix(s, "#") {
		return fmt.Sprintf(", OptionsTag: %s", gogen.Quote(lootQualified(s[1:])))
	}
	return ""
}

// snbt writes a JSON value as the SNBT the set_components function keeps it as.
func snbt(v any) string {
	switch x := v.(type) {
	case map[string]any:
		parts := make([]string, 0, len(x))
		for _, k := range sortedKeys(x) {
			parts = append(parts, gogen.Quote(k)+":"+snbt(x[k]))
		}
		return "{" + strings.Join(parts, ",") + "}"
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = snbt(e)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case bool:
		return fmt.Sprint(x)
	case float64:
		return gogen.Float(x) + "d"
	case int64:
		return fmt.Sprint(x)
	case string:
		return gogen.Quote(x)
	case nil:
		return "null"
	}
	fail("a value the SNBT writer does not know: %v", v)
	return ""
}

func (r *lootReader) function(f obj, where string) string {
	kind := f.strOr("function", "")
	conds := r.conditions(lootCodec{r.dp}.conditions(f), where)
	add := func() string { return lootBool(f["add"]) }
	costField := ""
	if f.boolOr("include_additional_cost_component", false) {
		costField = ", IncludeAdditionalCost: true"
	}
	switch kind {
	case "minecraft:set_count":
		return fmt.Sprintf("LootSetCount{Count: %s, Add: %s, Conditions: %s}", number(f.field("count"), where), add(), conds)
	case "minecraft:apply_bonus":
		return fmt.Sprintf("LootApplyBonus{Enchantment: %s, Formula: %s, Conditions: %s}",
			gogen.Quote(lootQualified(f.str("enchantment"))), bonusFormula(f, where), conds)
	case "minecraft:explosion_decay":
		return fmt.Sprintf("LootExplosionDecay{Conditions: %s}", conds)
	case "minecraft:limit_count":
		low, high := intLimit(f["limit"], where)
		return fmt.Sprintf("LootLimitCount{Min: %s, Max: %s, Conditions: %s}", low, high, conds)
	case "minecraft:enchanted_count_increase":
		limit := 0
		if f["limit"] != nil {
			limit = f.intOr("limit", 0)
		}
		return fmt.Sprintf("LootEnchantedCount{Enchantment: %s, Count: %s, Limit: %d, Conditions: %s}",
			gogen.Quote(lootQualified(f.str("enchantment"))), number(f.field("count"), where), limit, conds)
	case "minecraft:furnace_smelt":
		return fmt.Sprintf("LootFurnaceSmelt{UseInputCount: %t, Conditions: %s}", f.boolOr("use_input_count", true), conds)
	case "minecraft:copy_components":
		if f["source"] != "block_entity" {
			fail("%s: copy_components from %v is more than the reading does", where, f["source"])
		}
		var include, exclude []string
		for _, v := range f.listOr("include") {
			include = append(include, asStr(v, "an include"))
		}
		for _, v := range f.listOr("exclude") {
			exclude = append(exclude, asStr(v, "an exclude"))
		}
		return fmt.Sprintf("LootCopyComponents{Include: []string{%s}, Exclude: []string{%s}, Conditions: %s}",
			quotedList(include), quotedList(exclude), conds)
	case "minecraft:set_random_dyes":
		return fmt.Sprintf("LootSetRandomDyes{Count: %s, Conditions: %s}", number(f.field("number_of_dyes"), where), conds)
	case "minecraft:set_random_potion":
		options := "nil"
		if f.has("options") {
			options = "[]string{" + quotedList(r.nameList("potion", f["options"])) + "}"
		}
		return fmt.Sprintf("LootSetRandomPotion{Options: %s, Conditions: %s}", options, conds)
	case "minecraft:set_potion":
		return fmt.Sprintf("LootSetPotion{Potion: %s, Conditions: %s}", gogen.Quote(lootQualified(f.str("id"))), conds)
	case "minecraft:set_ominous_bottle_amplifier":
		return fmt.Sprintf("LootSetOminousBottleAmplifier{Amplifier: %s, Conditions: %s}", number(f.field("amplifier"), where), conds)
	case "minecraft:set_stew_effect":
		var effects []string
		for _, v := range f.listOr("effects") {
			e := asObj(v, "a stew effect")
			effects = append(effects, fmt.Sprintf("LootStewEffect{Effect: %s, Duration: %s}",
				gogen.Quote(lootQualified(e.str("type"))), number(e.field("duration"), where)))
		}
		return fmt.Sprintf("LootSetStewEffect{Effects: []LootStewEffect{%s}, Conditions: %s}", strings.Join(effects, ", "), conds)
	case "minecraft:set_instrument":
		return fmt.Sprintf("LootSetInstrument{Options: []string{%s}, Conditions: %s}",
			quotedList(r.nameList("instrument", f.field("options"))), conds)
	case "minecraft:set_name":
		target := f.strOr("target", "custom_name")
		if target != "custom_name" && target != "item_name" || f.has("entity") {
			fail("%s: a set_name to %s or resolved against an entity is more than the reading does", where, target)
		}
		if !f.has("name") {
			return "LootFunctionSequence{}" // no name leaves the stack as it was
		}
		return fmt.Sprintf("LootSetName{Name: %s, ItemName: %t, Conditions: %s}", lootText(f["name"], where), target == "item_name", conds)
	case "minecraft:exploration_map":
		// The defaults are ExplorationMapFunction's DEFAULT_* constants.
		return fmt.Sprintf("LootExplorationMap{Destination: []string{%s}, Decoration: %s, Zoom: %d, SearchRadius: %d, SkipExistingChunks: %t, Conditions: %s}",
			quotedList(r.sortedTag("worldgen/structure", lootQualified(strings.TrimPrefix(f.strOr("destination", "minecraft:on_treasure_maps"), "#")))),
			gogen.Quote(lootQualified(f.strOr("decoration", "minecraft:mansion"))),
			f.intOr("zoom", 2), f.intOr("search_radius", 50), f.boolOr("skip_existing_chunks", true), conds)
	case "minecraft:copy_state":
		var props []string
		for _, v := range f.listOr("properties") {
			props = append(props, asStr(v, "a property"))
		}
		return fmt.Sprintf("LootCopyState{Block: %s, Properties: []string{%s}, Conditions: %s}",
			gogen.Quote(lootQualified(f.str("block"))), quotedList(props), conds)
	case "minecraft:set_damage":
		return fmt.Sprintf("LootSetDamage{Damage: %s, Add: %s, Conditions: %s}", number(f.field("damage"), where), add(), conds)
	case "minecraft:set_enchantments":
		levels := asObj(f.field("enchantments"), "the enchantments")
		var parts []string
		for _, k := range sortedKeys(levels) {
			parts = append(parts, fmt.Sprintf("%s: %s", gogen.Quote(lootQualified(k)), number(levels[k], where)))
		}
		return fmt.Sprintf("LootSetEnchantments{Enchantments: map[string]LootNumber{%s}, Add: %s, Conditions: %s}",
			strings.Join(parts, ", "), add(), conds)
	case "minecraft:enchant_randomly":
		return fmt.Sprintf("LootEnchantRandomly{Options: %s%s, OnlyCompatible: %t%s, Conditions: %s}",
			r.enchantOptions(f), enchantTag(f), f.boolOr("only_compatible", true), costField, conds)
	case "minecraft:enchant_with_levels":
		return fmt.Sprintf("LootEnchantWithLevels{Levels: %s, Options: %s%s%s, Conditions: %s}",
			number(f.field("levels"), where), r.enchantOptions(f), enchantTag(f), costField, conds)
	case "minecraft:discard":
		return fmt.Sprintf("LootDiscard{Conditions: %s}", conds)
	case "minecraft:filtered":
		branch := func(key string) string {
			if v, ok := f[key]; ok {
				return r.function(asObj(v, key), where)
			}
			return "nil"
		}
		return fmt.Sprintf("LootFiltered{Predicate: %s, OnPass: %s, OnFail: %s, Conditions: %s}",
			r.itemPredicate(asObj(f.field("item_filter"), where), where), branch("on_pass"), branch("on_fail"), conds)
	case "minecraft:set_components":
		components := asObj(f.field("components"), "the components")
		var sets, removed []string
		for _, k := range sortedKeys(components) {
			if strings.HasPrefix(k, "!") {
				removed = append(removed, gogen.Quote(lootQualified(k[1:])))
			} else {
				sets = append(sets, fmt.Sprintf("%s: %s", gogen.Quote(lootQualified(k)), gogen.Quote(snbt(components[k]))))
			}
		}
		return fmt.Sprintf("LootSetComponents{Set: map[string]string{%s}, Remove: []string{%s}, Conditions: %s}",
			strings.Join(sets, ", "), strings.Join(removed, ", "), conds)
	default:
		fail("%s: %s is a function the reading does not know", where, kind)
	}
	return ""
}

// lootText writes a set_name's text component as a protocol.Text literal: a
// plain string, or an object holding only a text or a translation key.
func lootText(v any, where string) string {
	if t, ok := v.(string); ok {
		return fmt.Sprintf("protocol.Text{Text: %s}", gogen.Quote(t))
	}
	o := asObj(v, "a text component")
	for k := range o {
		if k != "text" && k != "translate" {
			fail("%s: a text component with %q is more than the reading does", where, k)
		}
	}
	if o.has("translate") {
		return fmt.Sprintf("protocol.Text{Translate: %s}", gogen.Quote(o.str("translate")))
	}
	return fmt.Sprintf("protocol.Text{Text: %s}", gogen.Quote(o.str("text")))
}

func (r *lootReader) functions(list []any, where string) string {
	if len(list) == 0 {
		return "nil"
	}
	parts := make([]string, len(list))
	for i, f := range list {
		parts[i] = r.function(asObj(f, "a function"), where)
	}
	return "[]LootFunction{" + strings.Join(parts, ", ") + "}"
}

func (r *lootReader) entry(e obj, where string) string {
	kind := e.strOr("type", "")
	if kind == "minecraft:item" {
		r.usedItems[lootQualified(e.str("name"))] = true
	}
	// The weight's text, with the quality only where an entry has one, so
	// the tables without stay as they read.
	weight := fmt.Sprint(e.intOr("weight", 1))
	if quality := e.intOr("quality", 0); quality != 0 {
		weight += fmt.Sprintf(", Quality: %d", quality)
	}
	conds := r.conditions(lootCodec{r.dp}.conditions(e), where)
	if e.has("modifier") && kind != "minecraft:item" && kind != "minecraft:loot_table" && kind != "minecraft:tag" {
		fail("%s: a modifier on %s needs an entry modifier model", where, kind)
	}
	switch kind {
	case "minecraft:item":
		return fmt.Sprintf("LootItemEntry{Item: %s, Weight: %s, Conditions: %s, Functions: %s}",
			lootItemIdent(lootQualified(e.str("name"))), weight, conds, r.functions(lootCodec{r.dp}.functions(e), where))
	case "minecraft:alternatives", "minecraft:group", "minecraft:sequence":
		structName := map[string]string{"minecraft:alternatives": "LootAlternatives", "minecraft:group": "LootGroup", "minecraft:sequence": "LootSequence"}[kind]
		var children []string
		for _, c := range e.listOr("children") {
			children = append(children, r.entry(asObj(c, "a child entry"), where))
		}
		return fmt.Sprintf("%s{Children: []LootEntry{%s}, Conditions: %s}", structName, strings.Join(children, ", "), conds)
	case "minecraft:empty":
		return fmt.Sprintf("LootEmpty{Weight: %s, Conditions: %s}", weight, conds)
	case "minecraft:loot_table":
		var reference string
		switch v := e["value"].(type) {
		case string:
			reference = v
		case map[string]any:
			// An inline table is stored under a name of its own.
			reference = fmt.Sprintf("%s/inline%d", where, len(r.inline))
			r.inline = append(r.inline, reference)
			r.tables[reference] = r.tableData(obj(v), reference)
		}
		if !strings.HasPrefix(reference, "minecraft:") {
			fail("%s refers to %s", where, reference)
		}
		r.pending.add(reference)
		return fmt.Sprintf("LootReference{Table: %s, Weight: %s, Conditions: %s, Functions: %s}",
			gogen.Quote(reference), weight, conds, r.functions(lootCodec{r.dp}.functions(e), where))
	case "minecraft:tag":
		items := r.nameList("item", e.field("items"))
		for _, item := range items {
			r.usedItems[item] = true
		}
		for i, item := range items {
			items[i] = lootItemIdent(item)
		}
		return fmt.Sprintf("LootTag{Items: []Item{%s}, Expand: %t, Weight: %s, Conditions: %s, Functions: %s}",
			strings.Join(items, ", "), e.boolOr("expand", false), weight, conds, r.functions(lootCodec{r.dp}.functions(e), where))
	case "minecraft:dynamic":
		return fmt.Sprintf("LootDynamic{Name: %s, Weight: %s, Conditions: %s}", gogen.Quote(lootQualified(e.str("name"))), weight, conds)
	default:
		fail("%s: %s is an entry type the reading does not know", where, kind)
	}
	return ""
}

func (r *lootReader) pool(p obj, where string) string {
	rolls := "LootConstant{Value: 1.0}"
	if p.has("rolls") {
		rolls = number(p["rolls"], where)
	}
	// Bonus rolls only ever add to a roll through the luck of the context,
	// which every roll this server makes carries none of, so a table that set
	// them is read with them at nothing: the provider kept as data.
	bonus := "LootConstant{}"
	if p.has("bonus_rolls") {
		bonus = number(p["bonus_rolls"], where)
	}
	var entries []string
	for _, e := range p.listOr("entries") {
		entries = append(entries, r.entry(asObj(e, "an entry"), where))
	}
	return fmt.Sprintf("LootPool{Rolls: %s, BonusRolls: %s, Entries: []LootEntry{%s}, Conditions: %s, Functions: %s}",
		rolls, bonus, strings.Join(entries, ", "), r.conditions(lootCodec{r.dp}.conditions(p), where), r.functions(lootCodec{r.dp}.functions(p), where))
}

func (r *lootReader) tableData(table obj, name string) string {
	var sequence string
	if table.has("random_sequence") {
		sequence = fmt.Sprintf("RandomSequence: %q, ", lootQualified(table.str("random_sequence")))
	}
	var pools []string
	for _, p := range table.listOr("pools") {
		pools = append(pools, r.pool(asObj(p, "a pool"), name))
	}
	return fmt.Sprintf("LootTable{%sPools: []LootPool{%s}, Functions: %s}", sequence, strings.Join(pools, ", "), r.functions(lootCodec{r.dp}.functions(table), name))
}

// nameQueue is a set of table names that hands out the smallest first, so the
// tables are read in an order that does not depend on how they were reached.
type nameQueue []string

func (q *nameQueue) add(name string) {
	if i, found := slices.BinarySearch(*q, name); !found {
		*q = slices.Insert(*q, i, name)
	}
}

func (q *nameQueue) pop() string {
	name := (*q)[0]
	*q = (*q)[1:]
	return name
}

var lootRefPattern = regexp.MustCompile(`LootReference\{Table: "([^"]+)"`)

// lootTableDirs maps a table name's first segment to its directory under
// loot_table/.
var lootTableDirs = map[string]string{
	"blocks": "blocks", "entities": "entities", "shearing": "shearing", "carve": "carve",
	"harvest": "harvest", "chests": "chests", "charged_creeper": "charged_creeper",
	"archaeology": "archaeology", "spawners": "spawners", "equipment": "equipment", "gameplay": "gameplay",
	"pots": "pots", "dispensers": "dispensers", "brush": "brush", "barrels": "barrels", "till": "till",
}

// Tables of these kinds are left out, rather than stopping the generation,
// when they use a function or condition the reading does not know.
var lootOptionalKinds = []string{"chests", "barrels", "pots", "dispensers", "archaeology", "spawners", "equipment"}

func runLoot(e *env) error {
	registries, err := e.ref.Registries()
	if err != nil {
		return err
	}
	itemRegistry, ok := registries["minecraft:item"]
	if !ok {
		return fmt.Errorf("the registries report has no minecraft:item")
	}
	dp, err := e.ref.DataPack()
	if err != nil {
		return err
	}
	defer dp.Close()
	r := &lootReader{dp: dp, tables: map[string]string{}, usedItems: map[string]bool{}}

	// The tables to write: every block's and entity's, and what they reference
	// (the sheep's colours). The shearing table is asked for by name: nothing in the
	// block or entity tables references it.
	for _, name := range []string{"gameplay/fishing", "gameplay/chicken_lay", "gameplay/turtle_grow", "harvest/beehive", "till/rooted_dirt", "gameplay/armadillo_shed", "gameplay/panda_sneeze", "gameplay/sniffer_digging", "brush/armadillo", "shearing/sheep", "carve/pumpkin", "harvest/sweet_berry_bush", "harvest/cave_vine",
		"shearing/bogged", "shearing/snow_golem", "charged_creeper/root", "shearing/mooshroom", "gameplay/piglin_bartering",
		"gameplay/cat_morning_gift", "gameplay/hero_of_the_village/armorer_gift", "gameplay/hero_of_the_village/baby_gift", "gameplay/hero_of_the_village/butcher_gift", "gameplay/hero_of_the_village/cartographer_gift", "gameplay/hero_of_the_village/cleric_gift", "gameplay/hero_of_the_village/farmer_gift", "gameplay/hero_of_the_village/fisherman_gift", "gameplay/hero_of_the_village/fletcher_gift", "gameplay/hero_of_the_village/leatherworker_gift", "gameplay/hero_of_the_village/librarian_gift", "gameplay/hero_of_the_village/mason_gift", "gameplay/hero_of_the_village/shepherd_gift", "gameplay/hero_of_the_village/toolsmith_gift", "gameplay/hero_of_the_village/unemployed_gift", "gameplay/hero_of_the_village/weaponsmith_gift"} {
		r.pending.add("minecraft:" + name)
	}
	const root = "minecraft/loot_table"
	for _, kind := range []string{"chests", "barrels", "pots", "dispensers", "spawners", "equipment", "archaeology", "blocks", "entities"} {
		files, err := vanilla.Files(dp, path.Join(root, kind), ".json")
		if err != nil {
			return err
		}
		for _, rel := range files {
			// Equipment and archaeology tables are one directory deep; the sheep's
			// colour tables under entities/ are reached through entities/sheep.
			if (kind == "equipment" || kind == "archaeology" || kind == "blocks" || kind == "entities") && strings.Contains(rel, "/") {
				continue
			}
			r.pending.add("minecraft:" + kind + "/" + strings.TrimSuffix(rel, ".json"))
		}
	}
	for len(r.pending) > 0 {
		name := r.pending.pop()
		if _, done := r.tables[name]; done {
			continue
		}
		kind, rest, _ := strings.Cut(strings.TrimPrefix(name, "minecraft:"), "/")
		dir, known := lootTableDirs[kind]
		if !known {
			return fmt.Errorf("%s is a table outside the blocks, entities, shearing, carve, harvest, chests, charged_creeper, archaeology, spawners, equipment, gameplay, pots, dispensers and till tables the reading covers", name)
		}
		file := path.Join(root, dir, rest+".json")
		read := func() {
			b, err := dpRead(dp, file)
			if err != nil {
				fail("%v", err)
			}
			r.tables[name] = r.tableData(decode(b, file), name)
		}
		if slices.Contains(lootOptionalKinds, kind) {
			if err := tryRead(read); err != nil {
				r.skipped = append(r.skipped, name)
			}
			continue
		}
		if err := tryRead(read); err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
	}

	// A container table that points at a table left out goes with it.
	for changed := true; changed; {
		changed = false
		for _, name := range sortedKeys(r.tables) {
			if !slices.ContainsFunc(lootOptionalKinds, func(k string) bool { return strings.HasPrefix(name, "minecraft:"+k+"/") }) {
				continue
			}
			for _, m := range lootRefPattern.FindAllStringSubmatch(r.tables[name], -1) {
				if _, ok := r.tables[m[1]]; !ok {
					delete(r.tables, name)
					r.skipped = append(r.skipped, name)
					changed = true
					break
				}
			}
		}
	}

	// The items every entry names must be items this version has, so a table
	// that names a dropped or renamed item stops the generation instead of
	// dropping its loot.
	var unknown []string
	for item := range r.usedItems {
		if _, ok := itemRegistry.Entries.Get(item); !ok {
			unknown = append(unknown, item)
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		return fmt.Errorf("items the tables name that the item registry does not have: %s", strings.Join(unknown, ", "))
	}

	f := e.file("registry", "from the vanilla {version} data pack")
	for _, table := range r.tables {
		if strings.Contains(table, "protocol.Text{") {
			f.WriteString(`import "github.com/golem-mc/golem/internal/protocol"` + "\n\n")
			break
		}
	}
	f.WriteString(`// The loot tables of the data pack, whole: every block's table and the
// entity tables of the mobs this server simulates, with the tables they
// reference (the sheep's sixteen colours) alongside, and the shearing
// table the shears roll with its own sixteen, and the chest, barrel, pot
// and dispenser tables whose functions the reading knows. Each is the
// LootTable data of registry/loot.go, and the engine that rolls one is
// the rules package's; see tools/gen/loot.go for the reading that got here
// and for the shapes it refuses rather than approximates.
//
// The tags the tables named were resolved into the members the version
// listed at generation time, so no tag name appears here.

func init() {
	LootTables = map[string]LootTable{
`)
	count := map[string]int{}
	for _, name := range sortedKeys(r.tables) {
		f.Printf("\t\t%s: %s,\n", gogen.Quote(name), r.tables[name])
		kind, _, _ := strings.Cut(strings.TrimPrefix(name, "minecraft:"), "/")
		count[kind]++
	}
	f.WriteString("\t}\n}\n")
	e.logf("tables: %d blocks, %d entities, %d shearing, %d chests, %d barrels, %d pots, %d dispensers",
		count["blocks"], count["entities"], count["shearing"], count["chests"], count["barrels"], count["pots"], count["dispensers"])
	if len(r.skipped) > 0 {
		slices.Sort(r.skipped)
		e.logf("container tables left out: %s", strings.Join(r.skipped, ", "))
	}
	return e.emit("registry/loot_gen.go", f)
}

// tryRead runs read and reports a refusal it raised with fail as an error.
func tryRead(read func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if ge, ok := r.(genError); ok {
				err = fmt.Errorf("%s", string(ge))
				return
			}
			panic(r)
		}
	}()
	read()
	return nil
}

func lootItemIdent(name string) string {
	id, err := ident("Item", name)
	if err != nil {
		fail("%v", err)
	}
	return id
}
