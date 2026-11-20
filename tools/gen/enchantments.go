package main

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/golem-mc/golem/tools/internal/gogen"
	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func init() {
	register(generator{
		name: "enchantments",
		doc:  "registry/enchantments_gen.go: every registered enchantment as the typed effect tree",
		run:  runEnchantments,
	})
}

// Inputs: the data pack's enchantment files, and the registry's entry list from
// registry/registries_gen.go (the server's registration order, so the numeric IDs).
//
// The data pack ships more enchantment files than the server registers; only
// registered ones are read, and the header names the rest. The components of each
// "effects" map that this server evaluates become the typed tree of
// registry/enchantments.go and the rest are named in the header. An effect or
// condition type the tree cannot represent stops the generation: a version that
// grew one needs the tree grown to fit it.

// enchComponents are the components the tree carries, by their file names and
// the fields of the generated struct they land in, in the order they are
// written.
var enchComponents = []struct{ name, field string }{
	{"minecraft:damage", "Damage"},
	{"minecraft:knockback", "Knockback"},
	{"minecraft:armor_effectiveness", "ArmorEffectiveness"},
	{"minecraft:damage_protection", "DamageProtection"},
	{"minecraft:item_damage", "ItemDamage"},
	{"minecraft:ammo_use", "AmmoUse"},
	{"minecraft:fishing_time_reduction", "FishingTimeReduction"},
	{"minecraft:fishing_luck_bonus", "FishingLuckBonus"},
	{"minecraft:projectile_count", "ProjectileCount"},
	{"minecraft:projectile_spread", "ProjectileSpread"},
	{"minecraft:projectile_piercing", "ProjectilePiercing"},
	{"minecraft:trident_return_acceleration", "TridentReturnAcceleration"},
	{"minecraft:crossbow_charge_time", "CrossbowChargeTime"},
	{"minecraft:trident_spin_attack_strength", "TridentSpinAttackStrength"},
	{"minecraft:repair_with_xp", "RepairWithXp"},
	{"minecraft:block_experience", "BlockExperience"},
	{"minecraft:mob_experience", "MobExperience"},
	{"minecraft:equipment_drops", "EquipmentDrops"},
	{"minecraft:prevent_equipment_drop", "PreventEquipmentDrop"},
	{"minecraft:prevent_armor_change", "PreventArmorChange"},
	{"minecraft:damage_immunity", "DamageImmunity"},
	{"minecraft:crossbow_charging_sounds", "CrossbowChargingSounds"},
	{"minecraft:trident_sound", "TridentSound"},
	{"minecraft:post_attack", "PostAttack"},
	{"minecraft:post_piercing_attack", "PostPiercingAttack"},
	{"minecraft:hit_block", "HitBlock"},
	{"minecraft:location_changed", "LocationChanged"},
	{"minecraft:tick", "Tick"},
	{"minecraft:projectile_spawned", "ProjectileSpawned"},
	{"minecraft:attributes", "Attributes"},
	{"minecraft:smash_damage_per_fallen_block", "SmashDamagePerFallenBlock"},
}

// enchEntityEffects are the entity effects the tree carries; omitted effects
// are named in the generated header.
var enchEntityEffects = map[string]bool{
	"minecraft:ignite": true, "minecraft:apply_mob_effect": true,
	"minecraft:damage_entity": true, "minecraft:change_item_damage": true,
	"minecraft:all_of": true, "minecraft:summon_entity": true, "minecraft:play_sound": true,
	"minecraft:apply_exhaustion": true, "minecraft:apply_impulse": true,
	"minecraft:explode": true, "minecraft:replace_disk": true, "minecraft:spawn_particles": true,
}

var enchValueKinds = map[string]bool{
	"minecraft:constant": true, "minecraft:linear": true, "minecraft:fraction": true,
	"minecraft:levels_squared": true, "minecraft:clamped": true, "minecraft:lookup": true,
	"minecraft:exponent": true,
}

var enchEffectKinds = map[string]string{
	"minecraft:add":             "EnchAdd",
	"minecraft:multiply":        "EnchMultiply",
	"minecraft:set":             "EnchSet",
	"minecraft:all_of":          "EnchAllOf",
	"minecraft:remove_binomial": "EnchRemoveBinomial",
}

var enchConditionKinds = map[string]bool{
	"minecraft:weather_check": true, "minecraft:location_check": true,
	"minecraft:match_tool": true, "minecraft:entity_properties": true,
	"minecraft:damage_source_properties": true, "minecraft:all_of": true,
	"minecraft:any_of": true, "minecraft:inverted": true, "minecraft:random_chance": true,
	"minecraft:enchantment_active_check": true,
}

// enchantmentWriter holds what the walk over one data pack collects on the
// side: the effects the tree has no shape for.
type enchantmentWriter struct {
	skippedEffects []string
	codec          lootCodec
	sounds         soundHolders
}

func noShape(format string, args ...any) {
	fail("%s; the generated tree has no shape for it", fmt.Sprintf(format, args...))
}

// enchNum is one float as a Go float32 literal, whole numbers whole.
func enchNum(v any) string {
	f, ok := asNum(v)
	if !ok {
		fail("%v is not a number", v)
	}
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return gogen.Float(f)
}

// levelValue is one level-based value as Go, the bare number the constant
// variant.
func levelValue(v any) string {
	if _, isNumber := asNum(v); isNumber {
		return fmt.Sprintf("&EnchLevelValue{Kind: EnchValueConstant, Constant: %s}", enchNum(v))
	}
	m, ok := v.(map[string]any)
	if !ok {
		noShape("a level-based value of %v", v)
	}
	o := obj(m)
	kind, _ := o["type"].(string)
	if !enchValueKinds[kind] {
		noShape("level-based value type %s", kind)
	}
	switch kind {
	case "minecraft:constant":
		return fmt.Sprintf("&EnchLevelValue{Kind: EnchValueConstant, Constant: %s}", enchNum(o.field("value")))
	case "minecraft:linear":
		return fmt.Sprintf("&EnchLevelValue{Kind: EnchValueLinear, Base: %s, PerLevel: %s}",
			enchNum(o.field("base")), enchNum(o.field("per_level_above_first")))
	case "minecraft:levels_squared":
		return fmt.Sprintf("&EnchLevelValue{Kind: EnchValueLevelsSquared, Added: %s}", enchNum(o.field("added")))
	case "minecraft:fraction":
		return fmt.Sprintf("&EnchLevelValue{Kind: EnchValueFraction, Num: %s, Den: %s}",
			levelValue(o.field("numerator")), levelValue(o.field("denominator")))
	case "minecraft:clamped":
		return fmt.Sprintf("&EnchLevelValue{Kind: EnchValueClamped, Min: %s, Max: %s, Value: %s}",
			enchNum(o.field("min")), enchNum(o.field("max")), levelValue(o.field("value")))
	case "minecraft:lookup":
		var values []string
		for _, x := range o.list("values") {
			values = append(values, enchNum(x))
		}
		return fmt.Sprintf("&EnchLevelValue{Kind: EnchValueLookup, Values: []float32{%s}, Fallback: %s}",
			strings.Join(values, ", "), levelValue(o.field("fallback")))
	}
	// exponent
	return fmt.Sprintf("&EnchLevelValue{Kind: EnchValueExponent, ExpBase: %s, ExpPower: %s}",
		levelValue(o.field("base")), levelValue(o.field("power")))
}

// field is a required value of any shape.
func (o obj) field(key string) any {
	v, ok := o[key]
	if !ok {
		fail("%q is missing", key)
	}
	return v
}

// unknownKeys fails on a predicate carrying keys beyond the allowed ones.
func unknownKeys(predicate obj, what string, allowed ...string) {
	var unknown []string
	for k := range predicate {
		if !slices.Contains(allowed, k) {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		noShape("%s with %s", what, strings.Join(unknown, ", "))
	}
}

// condition is one condition as Go, the variants the tree carries.
func enchCondition(c obj) string {
	if c == nil {
		return "nil"
	}
	kind, _ := c["condition"].(string)
	if !enchConditionKinds[kind] {
		noShape("condition type %s", kind)
	}
	predicate := obj{}
	if p, ok := c.obj("predicate"); ok {
		predicate = p
	}
	switch kind {
	case "minecraft:match_tool":
		unknownKeys(predicate, "a match_tool predicate", "items")
		var names []string
		tag := ""
		switch items := predicate["items"].(type) {
		case string:
			if strings.HasPrefix(items, "#") {
				tag = items[1:]
			} else {
				names = append(names, items)
			}
		case []any:
			for _, x := range items {
				one := asStr(x, "a match_tool item")
				if strings.HasPrefix(one, "#") {
					noShape("a tag inside a match_tool item list: %s", one)
				}
				names = append(names, one)
			}
		}
		for _, n := range names {
			if enchItems != nil && !enchItems[n] {
				fail("match_tool item %s is not in the item registry", n)
			}
		}
		symbols, err := idents("Item", names)
		if err != nil {
			fail("%v", err)
		}
		return fmt.Sprintf(`&EnchCondition{Kind: EnchCondMatchTool, ToolItems: []Item{%s}, ToolTag: "%s"}`,
			strings.Join(symbols, ", "), tag)
	case "minecraft:weather_check":
		unknownKeys(c, "a weather check", "condition", "thundering")
		if !c.has("thundering") {
			noShape("a weather check without thundering")
		}
		return fmt.Sprintf("&EnchCondition{Kind: EnchCondWeatherCheck, Thundering: new(%t)}", c.boolOr("thundering", false))
	case "minecraft:location_check":
		unknownKeys(c, "a location check", "condition", "predicate")
		return "&EnchCondition{Kind: EnchCondLocationCheck, " + enchLocation(predicate) + "}"
	case "minecraft:entity_properties":
		flags, _ := predicate.obj("minecraft:flags")
		if c.str("entity") == "this" && asksAfterBody(predicate, flags) {
			return fmt.Sprintf("&EnchCondition{Kind: EnchCondBody, Entity: \"this\", Body: &%s}",
				(&lootReader{}).entityPredicate(predicate, "an enchantment condition"))
		}
		if predicate.has("minecraft:movement") || flags.has("is_flying") {
			unknownKeys(predicate, "an attacker movement predicate", "minecraft:flags", "minecraft:movement")
			if c.str("entity") != "direct_attacker" {
				noShape("movement predicate on %s", c.str("entity"))
			}
			flags := asObj(predicate.field("minecraft:flags"), "entity flags")
			unknownKeys(flags, "entity flags", "is_flying")
			if !flags.has("is_flying") {
				noShape("entity flags without is_flying")
			}
			movement := asObj(predicate.field("minecraft:movement"), "entity movement")
			unknownKeys(movement, "entity movement", "fall_distance")
			fall := asObj(movement.field("fall_distance"), "fall distance")
			unknownKeys(fall, "fall distance", "min")
			return fmt.Sprintf("&EnchCondition{Kind: EnchCondEntityProperties, Entity: %q, IsFlying: new(%t), MinFallDistance: new(float64(%s))}",
				c.str("entity"), flags.boolOr("is_flying", false), enchNum(fall.field("min")))
		}
		unknownKeys(predicate, "an entity_properties predicate", "minecraft:entity_type", "minecraft:location",
			"minecraft:vehicle", "minecraft:flags", "minecraft:type_specific/player")
		if state, ok := enchEntityState(predicate); ok {
			return fmt.Sprintf("&EnchCondition{Kind: EnchCondEntityState, Entity: %q, State: %s}", c.str("entity"), state)
		}
		if location, ok := predicate.obj("minecraft:location"); ok {
			if len(predicate) != 1 {
				noShape("combined entity type and location predicate")
			}
			return fmt.Sprintf("&EnchCondition{Kind: EnchCondEntityProperties, Entity: %q, %s}", c.str("entity"), enchLocation(location))
		}
		entityType, ok := predicate["minecraft:entity_type"].(string)
		if !ok {
			noShape("an entity_properties predicate whose entity_type is %v", predicate["minecraft:entity_type"])
		}
		tag, single := "", entityType
		if strings.HasPrefix(entityType, "#") {
			tag, single = entityType[1:], ""
		}
		return fmt.Sprintf(`&EnchCondition{Kind: EnchCondEntityProperties, Entity: "%s", EntityTypeTag: "%s", EntityTypeID: "%s"}`,
			c.str("entity"), tag, single)
	case "minecraft:damage_source_properties":
		unknownKeys(predicate, "a damage_source_properties predicate", "tags", "is_direct")
		var tags []string
		for _, x := range predicate.listOr("tags") {
			t := asObj(x, "a damage source tag")
			expected := "false"
			if on, _ := t.field("expected").(bool); on {
				expected = "true"
			}
			tags = append(tags, fmt.Sprintf(`{Types: %s, Expected: %s}`, damageTypeSetLiteral("damage source predicate", t.field("id")), expected))
		}
		direct := "-1"
		if predicate.has("is_direct") {
			direct = "0"
			if predicate.boolOr("is_direct", false) {
				direct = "1"
			}
		}
		return fmt.Sprintf("&EnchCondition{Kind: EnchCondDamageSource, DamageTags: []EnchTagExpect{%s}, Direct: %s}",
			strings.Join(tags, ", "), direct)
	case "minecraft:random_chance":
		chance := c.field("chance")
		if m, ok := chance.(map[string]any); ok {
			if m["type"] != "minecraft:enchantment_level" {
				noShape("a random_chance whose chance is %v", m)
			}
			chance = obj(m).field("amount")
		}
		return fmt.Sprintf("&EnchCondition{Kind: EnchCondRandomChance, Chance: %s}", levelValue(chance))
	case "minecraft:enchantment_active_check":
		unknownKeys(c, "an enchantment active check", "condition", "active")
		return fmt.Sprintf("&EnchCondition{Kind: EnchCondActive, Active: %t}", c.boolOr("active", false))
	case "minecraft:inverted":
		return fmt.Sprintf("&EnchCondition{Kind: EnchCondInverted, Term: %s}",
			enchCondition(asObj(c.field("term"), "an inverted term")))
	}
	var terms []string
	for _, x := range c.list("terms") {
		terms = append(terms, enchCondition(asObj(x, "a condition term")))
	}
	name := "EnchCondAnyOf"
	if kind == "minecraft:all_of" {
		name = "EnchCondAllOf"
	}
	return fmt.Sprintf("&EnchCondition{Kind: %s, Terms: []*EnchCondition{%s}}", name, strings.Join(terms, ", "))
}

// asksAfterBody reports whether an entity_properties predicate asks after
// what only a snapshot of the wearer knows: movement, the block under it, its
// age, or a flag besides the user-state ones.
func asksAfterBody(predicate, flags obj) bool {
	if slices.ContainsFunc([]string{"minecraft:movement_affected_by", "minecraft:movement", "minecraft:periodic_tick"}, predicate.has) {
		return true
	}
	for key := range flags {
		if key != "is_fall_flying" && key != "is_in_water" {
			return true
		}
	}
	return false
}

// enchEntityState is the user-state predicate of an entity_properties
// condition: vehicle, flags and player data. False when it asks for none.
func enchEntityState(p obj) (string, bool) {
	var fields []string
	if v, ok := p.obj("minecraft:vehicle"); ok {
		if len(v) != 0 {
			noShape("a vehicle predicate with %v", v)
		}
		fields = append(fields, "Vehicle: true")
	}
	if f, ok := p.obj("minecraft:flags"); ok {
		unknownKeys(f, "a flags predicate", "is_fall_flying", "is_in_water")
		for _, k := range []struct{ key, field string }{{"is_fall_flying", "FallFlying"}, {"is_in_water", "InWater"}} {
			if f.has(k.key) {
				fields = append(fields, fmt.Sprintf("%s: new(%t)", k.field, f.boolOr(k.key, false)))
			}
		}
	}
	if pl, ok := p.obj("minecraft:type_specific/player"); ok {
		unknownKeys(pl, "a player predicate", "gamemode", "food")
		fields = append(fields, "Player: true")
		if pl.has("gamemode") {
			var modes []string
			for _, m := range pl.list("gamemode") {
				modes = append(modes, fmt.Sprintf("%q", asStr(m, "a game mode")))
			}
			fields = append(fields, "Gamemodes: []string{"+strings.Join(modes, ", ")+"}")
		}
		if food, ok := pl.obj("food"); ok {
			unknownKeys(food, "a food predicate", "level")
			level := asObj(food.field("level"), "a food level range")
			unknownKeys(level, "a food level range", "min")
			fields = append(fields, fmt.Sprintf("FoodMin: new(int32(%d))", int(level.num("min"))))
		}
	}
	if len(fields) == 0 {
		return "", false
	}
	return "&EnchEntityState{" + strings.Join(fields, ", ") + "}", true
}

// enchLocation carries sky visibility and a block tag, refusing richer predicates.
func enchLocation(p obj) string {
	unknownKeys(p, "an enchantment location", "can_see_sky", "block")
	var fields []string
	if p.has("can_see_sky") {
		fields = append(fields, fmt.Sprintf("CanSeeSky: new(%t)", p.boolOr("can_see_sky", false)))
	}
	if block, ok := p.obj("block"); ok {
		unknownKeys(block, "an enchantment block predicate", "blocks")
		tag := block.str("blocks")
		if !strings.HasPrefix(tag, "#") {
			noShape("an enchantment location block without a tag")
		}
		fields = append(fields, fmt.Sprintf("BlockTag: %q", tag[1:]))
	}
	return strings.Join(fields, ", ")
}

// valueEffect is one value effect as Go; remove_binomial's field is the
// chance, multiply's the factor, the others' the value.
func valueEffect(e obj) string {
	kind, _ := e["type"].(string)
	goKind, ok := enchEffectKinds[kind]
	if !ok {
		noShape("value effect type %s", kind)
	}
	if kind == "minecraft:all_of" {
		var subs []string
		for _, x := range e.list("effects") {
			subs = append(subs, valueEffect(asObj(x, "a value effect")))
		}
		return fmt.Sprintf("EnchEffect{Kind: EnchAllOf, Effects: []EnchEffect{%s}}", strings.Join(subs, ", "))
	}
	field := "value"
	switch kind {
	case "minecraft:remove_binomial":
		field = "chance"
	case "minecraft:multiply":
		field = "factor"
	}
	return fmt.Sprintf("EnchEffect{Kind: %s, Value: %s}", goKind, levelValue(e.field(field)))
}

// entityEffect is one entity effect's EnchEntityEffect fields as Go, or "" and
// false for one the tree does not carry, named in the header.
func (w *enchantmentWriter) entityEffect(e obj, where string) (string, bool) {
	kind, _ := e["type"].(string)
	if !enchEntityEffects[kind] {
		w.skippedEffects = append(w.skippedEffects, where+": "+kind)
		return "", false
	}
	switch kind {
	case "minecraft:explode":
		// Only the wind-burst shape is interpreted by the server adapter.
		unknownKeys(e, "an explode effect", "type", "radius", "knockback_multiplier", "block_interaction", "immune_blocks", "small_particle", "large_particle", "sound")
		if e.str("block_interaction") != "trigger" || e.str("immune_blocks") != "#minecraft:blocks_wind_charge_explosions" ||
			e.str("sound") != "minecraft:entity.wind_charge.wind_burst" {
			noShape("an explode effect other than a wind burst")
		}
		for _, size := range []string{"small", "large"} {
			particle := asObj(e.field(size+"_particle"), "an explosion particle")
			unknownKeys(particle, "an explosion particle", "type")
			if particle.str("type") != "minecraft:gust_emitter_"+size {
				noShape("an explode effect with %s particle %s", size, particle.str("type"))
			}
		}
		return fmt.Sprintf("Explode: &EnchExplode{Radius: %s, KnockbackMultiplier: %s, BlockInteraction: %q, ImmuneBlocks: %q}",
			levelValue(e.field("radius")), levelValue(e.field("knockback_multiplier")), e.str("block_interaction"), strings.TrimPrefix(e.str("immune_blocks"), "#")), true
	case "minecraft:all_of":
		// The sub-effects land in one struct, each in the field of its own
		// kind; two of a kind would overwrite one another.
		var parts, fields []string
		for _, x := range e.list("effects") {
			if p, ok := w.entityEffect(asObj(x, "an entity effect"), where); ok {
				parts = append(parts, p)
				fields = append(fields, strings.SplitN(p, ":", 2)[0])
			}
		}
		seen := map[string]bool{}
		for _, f := range fields {
			if seen[f] {
				noShape("an all_of with two entity effects of one kind")
			}
			seen[f] = true
		}
		if len(parts) == 0 {
			return "", false
		}
		return strings.Join(parts, ", "), true
	case "minecraft:summon_entity":
		unknownKeys(e, "a summon entity effect", "type", "entity")
		name := e.str("entity")
		if name != "minecraft:lightning_bolt" {
			noShape("summon_entity for %s", name)
		}
		return fmt.Sprintf("SummonEntity: %q", name), true
	case "minecraft:play_sound":
		unknownKeys(e, "a play sound effect", "type", "sound", "volume", "pitch")
		// A list of sounds is picked by level.
		var sound string
		if names, ok := e["sound"].([]any); ok {
			var quoted []string
			for _, n := range names {
				quoted = append(quoted, w.sounds.holder(asStr(n, "a sound")))
			}
			sound = "Sound: protocol.SoundHolder{ID: -1}, Sounds: []protocol.SoundHolder{" + strings.Join(quoted, ", ") + "}"
		} else {
			sound = "Sound: " + w.sounds.holder(e.str("sound"))
		}
		var pitch string
		if _, constant := asNum(e.field("pitch")); constant {
			pitch = "Pitch: " + enchNum(e.field("pitch"))
		} else {
			uniform := asObj(e.field("pitch"), "a pitch")
			if uniform.str("type") != "minecraft:uniform" {
				noShape("a pitch provider other than a constant or uniform")
			}
			pitch = fmt.Sprintf("Pitch: %s, PitchMax: %s", enchNum(uniform.field("min_inclusive")), enchNum(uniform.field("max_exclusive")))
		}
		return fmt.Sprintf("PlaySound: &EnchPlaySound{%s, Volume: %s, %s}", sound, enchNum(e.field("volume")), pitch), true
	case "minecraft:replace_disk":
		return enchReplaceDisk(e), true
	case "minecraft:spawn_particles":
		return enchSpawnParticles(e), true
	case "minecraft:apply_exhaustion":
		unknownKeys(e, "an apply exhaustion effect", "type", "amount")
		return "ApplyExhaustion: " + levelValue(e.field("amount")), true
	case "minecraft:apply_impulse":
		unknownKeys(e, "an apply impulse effect", "type", "direction", "coordinate_scale", "magnitude")
		// The tree carries only the look itself, direction (0, 0, 1) in the look's frame.
		if d := vec3(e.list("direction")); d != "[3]float32{0, 0, 1}" {
			noShape("an apply_impulse with direction %s", d)
		}
		return fmt.Sprintf("ApplyImpulse: &EnchApplyImpulse{CoordinateScale: %s, Magnitude: %s}",
			vec3(e.list("coordinate_scale")), levelValue(e.field("magnitude"))), true
	case "minecraft:ignite":
		return "Duration: " + levelValue(e.field("duration")), true
	case "minecraft:damage_entity":
		return fmt.Sprintf(`DamageEntity: &EnchDamageEntity{DamageType: %s, Min: %s, Max: %s}`,
			damageTypeIdent(e.str("damage_type")), levelValue(e.field("min_damage")), levelValue(e.field("max_damage"))), true
	case "minecraft:change_item_damage":
		return "ChangeItemDamage: " + levelValue(e.field("amount")), true
	}
	// The one effect left on the body by name; a tag or a list of them is a
	// shape the tree does not carry, so the generation stops on it.
	toApply, ok := e["to_apply"].(string)
	if !ok || strings.HasPrefix(toApply, "#") {
		noShape("apply_mob_effect whose to_apply is %v", e["to_apply"])
	}
	return fmt.Sprintf(`MobEffect: &EnchMobEffect{Name: "%s", MinAmplifier: %s, MaxAmplifier: %s, MinDuration: %s, MaxDuration: %s}`,
		toApply, levelValue(e.field("min_amplifier")), levelValue(e.field("max_amplifier")),
		levelValue(e.field("min_duration")), levelValue(e.field("max_duration"))), true
}

// vec3 is a three-number list as a Go [3]float32.
func vec3(l []any) string {
	if len(l) != 3 {
		noShape("a vector of %d numbers", len(l))
	}
	return fmt.Sprintf("[3]float32{%s, %s, %s}", enchNum(l[0]), enchNum(l[1]), enchNum(l[2]))
}

// targeted is one post-attack effect as Go, or false for one whose effect the
// tree does not carry.
func (w *enchantmentWriter) targeted(entry obj, component string) (string, bool) {
	effect, ok := w.entityEffect(asObj(entry.field("effect"), "an effect"), component)
	if !ok {
		return "", false
	}
	requirements := w.codec.optionalPredicate(entry, "requirements")
	return fmt.Sprintf(`EnchTargeted{Enchanted: "%s", Affected: "%s", Effect: EnchEntityEffect{%s}, Requires: %s}`,
		entry.str("enchanted"), entry.str("affected"), effect, enchCondition(requirements)), true
}

// enchItems is the item registry's names, set by runEnchantments for the
// match_tool check; nil skips it.
var enchItems map[string]bool

func runEnchantments(e *env) error {
	registries, err := e.ref.Registries()
	if err != nil {
		return err
	}
	enchItems = map[string]bool{}
	for _, n := range registries["minecraft:item"].Entries.Keys() {
		enchItems[n] = true
	}
	registered, err := syncedEntries(e, "minecraft:enchantment")
	if err != nil {
		return err
	}
	dp, err := e.ref.DataPack()
	if err != nil {
		return err
	}
	defer dp.Close()

	readAll := func(dir string) (map[string]obj, error) {
		files, err := vanilla.Files(dp, dir, ".json")
		if err != nil {
			return nil, err
		}
		out := map[string]obj{}
		for _, rel := range files {
			b, err := dpRead(dp, path.Join(dir, rel))
			if err != nil {
				return nil, err
			}
			out["minecraft:"+strings.TrimSuffix(rel, ".json")] = decode(b, rel)
		}
		return out, nil
	}
	byName, err := readAll("minecraft/enchantment")
	if err != nil {
		return err
	}
	for _, name := range registered {
		if _, ok := byName[name]; !ok {
			return fmt.Errorf("%s is registered but the data pack has no file for it", name)
		}
	}
	var skippedFiles []string
	for name := range byName {
		if !slices.Contains(registered, name) {
			skippedFiles = append(skippedFiles, name)
		}
	}
	slices.Sort(skippedFiles)

	w := &enchantmentWriter{codec: lootCodec{dp}, sounds: registeredSoundHolders(registries)}
	var body []string
	type unmodelled struct {
		name string
		left []string
	}
	var leftOut []unmodelled
	for _, name := range registered {
		data := byName[name]
		effects, _ := data.obj("effects")
		var fields []string
		for _, comp := range enchComponents {
			raw, present := effects[comp.name]
			if !present {
				continue
			}
			if comp.name == "minecraft:prevent_equipment_drop" || comp.name == "minecraft:prevent_armor_change" {
				fields = append(fields, "\t\t\t"+comp.field+": true,")
				continue
			}
			if comp.name == "minecraft:crossbow_charge_time" || comp.name == "minecraft:trident_spin_attack_strength" {
				fields = append(fields, "\t\t\t"+comp.field+": &"+valueEffect(asObj(raw, "a value effect"))+",")
				continue
			}
			list, ok := raw.([]any)
			if !ok {
				fail("%s of %s is not a list", comp.name, name)
			}
			var literal string
			switch comp.name {
			case "minecraft:equipment_drops":
				var kept []string
				for _, x := range list {
					entry := asObj(x, "an equipment drop effect")
					requirements := w.codec.optionalPredicate(entry, "requirements")
					kept = append(kept, fmt.Sprintf(`EnchTargetedValue{Enchanted: "%s", Affected: "victim", Effect: %s, Requires: %s}`,
						entry.str("enchanted"),
						valueEffect(asObj(entry.field("effect"), "an effect")), enchCondition(requirements)))
				}
				literal = "[]EnchTargetedValue{" + strings.Join(kept, ", ") + "}"
			case "minecraft:damage_immunity":
				var kept []string
				for _, x := range list {
					entry := asObj(x, "a damage immunity")
					requirements := w.codec.optionalPredicate(entry, "requirements")
					kept = append(kept, fmt.Sprintf("EnchImmunity{Requires: %s}", enchCondition(requirements)))
				}
				literal = "[]EnchImmunity{" + strings.Join(kept, ", ") + "}"
			case "minecraft:crossbow_charging_sounds":
				var kept []string
				for _, x := range list {
					entry := asObj(x, "a charging sounds entry")
					unknownKeys(entry, "a charging sounds entry", "start", "mid", "end")
					parts := make([]string, 0, 3)
					for _, key := range []string{"start", "mid", "end"} {
						holder := "protocol.SoundHolder{ID: -1}"
						if entry.has(key) {
							holder = w.sounds.holder(entry.str(key))
						}
						parts = append(parts, strings.ToUpper(key[:1])+key[1:]+": "+holder)
					}
					kept = append(kept, "EnchChargingSounds{"+strings.Join(parts, ", ")+"}")
				}
				literal = "[]EnchChargingSounds{" + strings.Join(kept, ", ") + "}"
			case "minecraft:trident_sound":
				var kept []string
				for _, x := range list {
					kept = append(kept, w.sounds.holder(asStr(x, "a sound")))
				}
				literal = "[]protocol.SoundHolder{" + strings.Join(kept, ", ") + "}"
			case "minecraft:attributes":
				// The id is required: the codec reads it with no default,
				// and the equipment walk builds the modifier's own id from it.
				var attrs []string
				for _, x := range list {
					a := asObj(x, "an attribute effect")
					if !a.has("id") {
						noShape("an attribute effect without an id")
					}
					attrs = append(attrs, fmt.Sprintf(`EnchAttribute{Attribute: "%s", ID: "%s", Operation: "%s", Amount: %s}`,
						a.str("attribute"), a.str("id"), a.str("operation"), levelValue(a.field("amount"))))
				}
				literal = "[]EnchAttribute{" + strings.Join(attrs, ", ") + "}"
			case "minecraft:post_attack":
				var kept []string
				for _, x := range list {
					if t, ok := w.targeted(asObj(x, "a post-attack effect"), comp.name); ok {
						kept = append(kept, t)
					}
				}
				if len(kept) == 0 {
					continue
				}
				literal = "[]EnchTargeted{" + strings.Join(kept, ", ") + "}"
			case "minecraft:location_changed", "minecraft:tick":
				literal = w.locatedEntries(list, comp.name)
				if literal == "" {
					continue
				}
			case "minecraft:projectile_spawned", "minecraft:hit_block", "minecraft:post_piercing_attack":
				var kept []string
				for _, x := range list {
					entry := asObj(x, "a projectile-spawned effect")
					effect, ok := w.entityEffect(asObj(entry.field("effect"), "an effect"), comp.name)
					if !ok {
						continue
					}
					requirements := w.codec.optionalPredicate(entry, "requirements")
					kept = append(kept, fmt.Sprintf("EnchSpawned{Effect: EnchEntityEffect{%s}, Requires: %s}",
						effect, enchCondition(requirements)))
				}
				if len(kept) == 0 {
					continue
				}
				literal = "[]EnchSpawned{" + strings.Join(kept, ", ") + "}"
			default:
				var conds []string
				for _, x := range list {
					entry := asObj(x, "an effect entry")
					requirements := w.codec.optionalPredicate(entry, "requirements")
					conds = append(conds, fmt.Sprintf("EnchConditional{Effect: %s, Requires: %s}",
						valueEffect(asObj(entry.field("effect"), "an effect")), enchCondition(requirements)))
				}
				literal = "[]EnchConditional{" + strings.Join(conds, ", ") + "}"
			}
			fields = append(fields, fmt.Sprintf("\t\t\t%s: %s,", comp.field, literal))
		}
		var slots []string
		for _, s := range data.list("slots") {
			slots = append(slots, `"`+asStr(s, "a slot")+`"`)
		}
		minCost, _ := data.obj("min_cost")
		maxCost, _ := data.obj("max_cost")
		body = append(body,
			"\t{",
			fmt.Sprintf("\t\tName: %q,", name),
			fmt.Sprintf("\t\tSlots: []string{%s},", strings.Join(slots, ", ")),
			fmt.Sprintf("\t\tMaxLevel: %d,", int(data.num("max_level"))),
			fmt.Sprintf("\t\tWeight: %d,", int(data.num("weight"))),
			fmt.Sprintf("\t\tAnvilCost: %d,", data.intOr("anvil_cost", 1)),
			fmt.Sprintf("\t\tMinCostBase: %s, MinCostPerLevel: %s,", enchNum(minCost.field("base")), enchNum(minCost.field("per_level_above_first"))),
			fmt.Sprintf("\t\tMaxCostBase: %s, MaxCostPerLevel: %s,", enchNum(maxCost.field("base")), enchNum(maxCost.field("per_level_above_first"))))
		for _, kf := range []struct{ key, field string }{
			{"supported_items", "SupportedItems"}, {"primary_items", "PrimaryItems"}, {"exclusive_set", "ExclusiveSet"},
		} {
			ref, present := data[kf.key]
			switch s, isString := ref.(string); {
			case !present || ref == nil:
				body = append(body, fmt.Sprintf(`		%s: "",`, kf.field))
			case isString && strings.HasPrefix(s, "#"):
				body = append(body, fmt.Sprintf(`		%s: "%s",`, kf.field, s[1:]))
			default:
				fail("%s of %s is %v, not a tag", kf.key, name, ref)
			}
		}
		body = append(body, "\t\tEffects: EnchantmentEffects{")
		body = append(body, fields...)
		body = append(body, "\t\t},", "\t},")
		var left []string
		for c := range effects {
			if !slices.ContainsFunc(enchComponents, func(k struct{ name, field string }) bool { return k.name == c }) {
				left = append(left, c)
			}
		}
		if len(left) > 0 {
			slices.Sort(left)
			leftOut = append(leftOut, unmodelled{name, left})
		}
	}

	f := e.file("registry", "by tools/gen enchantments from a vanilla {version} server")
	f.WriteString("import \"github.com/golem-mc/golem/internal/protocol\"\n\n")
	f.WriteString(`// EnchantmentEffectsTable is every enchantment the minecraft:enchantment
// registry registers, with the effects its data pack file carries that this
// server evaluates: the value effects of damage, knockback, armour
// effectiveness, damage protection, item damage, ammo use, repair from xp, block experience, mob experience, equipment drops and smash damage
// per fallen block, the entity effects of post attack and projectile
// spawned and post piercing attack - ignite, apply_mob_effect, damage_entity,
// change_item_damage, apply_exhaustion, apply_impulse, explode, play_sound,
// replace_disk and spawn_particles - the location_changed and tick entries and
// the attribute modifiers.
// The tree is registry/enchantments.go's; the interpreting of it is
// package rules'.
//
// The components no wiring asks after are not carried at all:
`)
	for _, u := range leftOut {
		f.Printf("//\t%s: %s.\n", u.name, strings.Join(u.left, ", "))
	}
	if len(skippedFiles) > 0 {
		f.WriteString("//\n// The data pack also ships files the server does not register:\n")
		f.Printf("// %s.\n", strings.Join(skippedFiles, ", "))
	}
	if len(w.skippedEffects) > 0 {
		f.WriteString("//\n// Within the carried components, the effects this server cannot act on\n// were left out entry by entry:\n")
		sorted := slices.Clone(w.skippedEffects)
		slices.Sort(sorted)
		for _, one := range sorted {
			f.Printf("//\t%s\n", one)
		}
	}
	f.WriteString("\nvar EnchantmentEffectsTable = []EnchantmentDef{\n")
	for _, line := range body {
		f.WriteString(line + "\n")
	}
	f.WriteString("}\n")
	e.logf("enchantments: %d enchantments, %d files unregistered, %d effects left out",
		len(registered), len(skippedFiles), len(w.skippedEffects))
	return e.emit("registry/enchantments_gen.go", f)
}
