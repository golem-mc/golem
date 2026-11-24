package datapack

import (
	"slices"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// enchantmentDecoder collects what DecodeEnchantment leaves out.
type enchantmentDecoder struct {
	dropped []string
	// predicates answers a reference to a predicate file with its JSON; nil
	// leaves every reference unresolved.
	predicates func(name string) ([]byte, bool)
	refDepth   int
}

// DecodeEnchantment reads one data/<ns>/enchantment file into the tree the
// server evaluates, the runtime twin of tools/gen's enchantments generator.
// A component, effect or condition the tree cannot represent is left out and
// named in dropped; a file without supported_items or max_level, or with a
// malformed required field, is an error. An omitted weight, cost or
// anvil_cost reads as 1, 0 and 1: vanilla refuses such a file.
// vanilla: Enchantment.DIRECT_CODEC
func DecodeEnchantment(id string, data []byte) (registry.EnchantmentDef, []string, error) {
	return DecodeEnchantmentWith(id, data, nil)
}

// DecodeEnchantmentWith is DecodeEnchantment with the data pack's predicate
// files, which a "minecraft:reference" requirement names: predicates returns
// a predicate's JSON by qualified name.
func DecodeEnchantmentWith(id string, data []byte, predicates func(name string) ([]byte, bool)) (def registry.EnchantmentDef, dropped []string, err error) {
	defer recoverLoot(&err)
	d := &enchantmentDecoder{predicates: predicates}
	o := object(parseJSON(data), "enchantment")
	def = registry.EnchantmentDef{
		Name:      id,
		MaxLevel:  int32(requireNum(o, "max_level")),
		Weight:    int32(optNum(o, "weight", 1)),
		AnvilCost: int32(optNum(o, "anvil_cost", 1)),
	}
	def.SupportedItems, def.SupportedList = itemSet(required(o, "supported_items"))
	if v, ok := o["primary_items"]; ok {
		def.PrimaryItems, def.PrimaryList = itemSet(v)
	}
	if v, ok := o["exclusive_set"]; ok {
		def.ExclusiveSet, def.ExclusiveList = enchantmentSet(v)
	}
	for _, s := range list(o["slots"], "slots") {
		def.Slots = append(def.Slots, str(s, "a slot"))
	}
	def.MinCostBase, def.MinCostPerLevel = linearCost(o, "min_cost")
	def.MaxCostBase, def.MaxCostPerLevel = linearCost(o, "max_cost")
	if v, ok := o["effects"]; ok {
		d.effects(&def.Effects, object(v, "effects"))
	}
	return def, d.dropped, nil
}

func linearCost(o obj, key string) (base, perLevel float32) {
	v, ok := o[key]
	if !ok {
		return 0, 0
	}
	c := object(v, key)
	return float32(requireNum(c, "base")), float32(optNum(c, "per_level_above_first", 0))
}

// itemSet reads a tag, one item or a list of items.
func itemSet(v any) (tag string, items []registry.Item) {
	if s, ok := v.(string); ok && strings.HasPrefix(s, "#") {
		return registry.Qualify(s[1:]), nil
	}
	items = []registry.Item{}
	for _, x := range holderList(v) {
		name := str(x, "an item")
		if strings.HasPrefix(name, "#") {
			fail("a tag inside an item list: %s", name)
		}
		items = append(items, itemNamed(name))
	}
	return "", items
}

func itemNamed(name string) registry.Item {
	item, ok := registry.ItemByName(registry.Qualify(name))
	if !ok {
		fail("Unknown item %s", registry.Qualify(name))
	}
	return item
}

func enchantmentSet(v any) (tag string, names []string) {
	if s, ok := v.(string); ok && strings.HasPrefix(s, "#") {
		return registry.Qualify(s[1:]), nil
	}
	names = []string{}
	for _, x := range holderList(v) {
		name := str(x, "an enchantment")
		if strings.HasPrefix(name, "#") {
			fail("a tag inside an enchantment list: %s", name)
		}
		names = append(names, registry.Qualify(name))
	}
	return "", names
}

// nonEmpty is an entity effect read from an entry; an empty one, whose type
// the tree does not carry and so is already recorded, drops the entry.
func nonEmpty(e registry.EnchEntityEffect) registry.EnchEntityEffect {
	if e == (registry.EnchEntityEffect{}) {
		panic(lootFailure{})
	}
	return e
}

// keep runs f, which reads one entry, and reports whether it did: an entry
// the tree cannot represent is recorded as dropped instead of failing the file.
func (d *enchantmentDecoder) keep(where string, f func()) (kept bool) {
	defer func() {
		switch r := recover().(type) {
		case nil:
		case lootFailure:
			if r.msg != "" {
				d.dropped = append(d.dropped, where+": "+r.msg)
			}
			kept = false
		default:
			panic(r)
		}
	}()
	f()
	return true
}

// entries reads each entry of a component's list through keep.
func entries[T any](d *enchantmentDecoder, where string, raw any, read func(obj) T) []T {
	var out []T
	for _, x := range list(raw, where) {
		var one T
		if d.keep(where, func() { one = read(object(x, where)) }) {
			out = append(out, one)
		}
	}
	return out
}

func (d *enchantmentDecoder) effects(e *registry.EnchantmentEffects, o obj) {
	conditional := func(name string, into *[]registry.EnchConditional) {
		*into = entries(d, name, o[name], func(entry obj) registry.EnchConditional {
			return registry.EnchConditional{
				Effect:   valueEffect(object(required(entry, "effect"), "an effect")),
				Requires: d.requirements(entry),
			}
		})
	}
	spawned := func(name string, into *[]registry.EnchSpawned) {
		*into = entries(d, name, o[name], func(entry obj) registry.EnchSpawned {
			return registry.EnchSpawned{
				Effect:   nonEmpty(d.entityEffect(object(required(entry, "effect"), "an effect"), name)),
				Requires: d.requirements(entry),
			}
		})
	}
	for _, name := range sortedKeys(o) {
		switch name {
		case "minecraft:damage":
			conditional(name, &e.Damage)
		case "minecraft:knockback":
			conditional(name, &e.Knockback)
		case "minecraft:armor_effectiveness":
			conditional(name, &e.ArmorEffectiveness)
		case "minecraft:damage_protection":
			conditional(name, &e.DamageProtection)
		case "minecraft:item_damage":
			conditional(name, &e.ItemDamage)
		case "minecraft:fishing_time_reduction":
			conditional(name, &e.FishingTimeReduction)
		case "minecraft:fishing_luck_bonus":
			conditional(name, &e.FishingLuckBonus)
		case "minecraft:projectile_count":
			conditional(name, &e.ProjectileCount)
		case "minecraft:projectile_spread":
			conditional(name, &e.ProjectileSpread)
		case "minecraft:projectile_piercing":
			conditional(name, &e.ProjectilePiercing)
		case "minecraft:trident_return_acceleration":
			conditional(name, &e.TridentReturnAcceleration)
		case "minecraft:crossbow_charge_time":
			v := valueEffect(object(o[name], "a value effect"))
			e.CrossbowChargeTime = &v
		case "minecraft:trident_spin_attack_strength":
			v := valueEffect(object(o[name], "a value effect"))
			e.TridentSpinAttackStrength = &v
		case "minecraft:ammo_use":
			conditional(name, &e.AmmoUse)
		case "minecraft:repair_with_xp":
			conditional(name, &e.RepairWithXp)
		case "minecraft:block_experience":
			conditional(name, &e.BlockExperience)
		case "minecraft:smash_damage_per_fallen_block":
			conditional(name, &e.SmashDamagePerFallenBlock)
		case "minecraft:mob_experience":
			conditional(name, &e.MobExperience)
		case "minecraft:equipment_drops":
			e.EquipmentDrops = entries(d, name, o[name], func(entry obj) registry.EnchTargetedValue {
				return registry.EnchTargetedValue{
					Enchanted: requireStr(entry, "enchanted"),
					Affected:  "victim",
					Effect:    valueEffect(object(required(entry, "effect"), "an effect")),
					Requires:  d.requirements(entry),
				}
			})
		case "minecraft:prevent_equipment_drop":
			e.PreventEquipmentDrop = true
		case "minecraft:prevent_armor_change":
			e.PreventArmorChange = true
		case "minecraft:damage_immunity":
			e.DamageImmunity = entries(d, name, o[name], func(entry obj) registry.EnchImmunity {
				return registry.EnchImmunity{Requires: d.requirements(entry)}
			})
		case "minecraft:crossbow_charging_sounds":
			e.CrossbowChargingSounds = entries(d, name, o[name], func(entry obj) registry.EnchChargingSounds {
				unknownKeys(entry, "a charging sounds entry", "start", "mid", "end")
				sound := func(key string) protocol.SoundHolder {
					if _, ok := entry[key]; !ok {
						return protocol.SoundHolder{ID: -1}
					}
					return registry.SoundHolderByName(requireStr(entry, key))
				}
				return registry.EnchChargingSounds{Start: sound("start"), Mid: sound("mid"), End: sound("end")}
			})
		case "minecraft:trident_sound":
			for _, n := range list(o[name], name) {
				e.TridentSound = append(e.TridentSound, registry.SoundHolderByName(str(n, "a sound")))
			}
		case "minecraft:post_piercing_attack":
			spawned(name, &e.PostPiercingAttack)
		case "minecraft:projectile_spawned":
			spawned(name, &e.ProjectileSpawned)
		case "minecraft:hit_block":
			spawned(name, &e.HitBlock)
		case "minecraft:post_attack":
			e.PostAttack = entries(d, name, o[name], func(entry obj) registry.EnchTargeted {
				return registry.EnchTargeted{
					Enchanted: requireStr(entry, "enchanted"),
					Affected:  requireStr(entry, "affected"),
					Effect:    nonEmpty(d.entityEffect(object(required(entry, "effect"), "an effect"), name)),
					Requires:  d.requirements(entry),
				}
			})
		case "minecraft:location_changed", "minecraft:tick":
			into := &e.LocationChanged
			if name == "minecraft:tick" {
				into = &e.Tick
			}
			for group, x := range list(o[name], name) {
				d.keep(name, func() {
					entry := object(x, name)
					eff := object(required(entry, "effect"), "an effect")
					requires := d.requirements(entry)
					if name == "minecraft:location_changed" {
						if attrs, ok := attributeEffects(eff); ok {
							for _, a := range attrs {
								*into = append(*into, registry.EnchLocated{Attribute: a, Requires: requires, Group: group})
							}
							return
						}
					}
					*into = append(*into, registry.EnchLocated{Effect: nonEmpty(d.entityEffect(eff, name)), Requires: requires, Group: group})
				})
			}
		case "minecraft:attributes":
			e.Attributes = entries(d, name, o[name], func(a obj) registry.EnchAttribute {
				return registry.EnchAttribute{
					Attribute: requireStr(a, "attribute"),
					ID:        requireStr(a, "id"),
					Operation: requireStr(a, "operation"),
					Amount:    levelValueOf(required(a, "amount")),
				}
			})
		default:
			d.dropped = append(d.dropped, "component "+name)
		}
	}
}

// soundPitch reads a play_sound pitch: a number, or a uniform provider whose
// maximum is exclusive.
// vanilla: FloatProviders.codec
func soundPitch(v any) (low, high float32) {
	if f, ok := num(v); ok {
		return float32(f), 0
	}
	o := object(v, "a pitch")
	if registry.Qualify(requireStr(o, "type")) != "minecraft:uniform" {
		fail("a pitch provider other than a constant or uniform")
	}
	unknownKeys(o, "a uniform pitch", "type", "min_inclusive", "max_exclusive")
	return float32(requireNum(o, "min_inclusive")), float32(requireNum(o, "max_exclusive"))
}

// attributeEffects reads an attribute effect, or an all_of of nothing but
// attribute effects, as its modifiers; false for any other effect.
func attributeEffects(eff obj) ([]*registry.EnchAttribute, bool) {
	switch registry.Qualify(requireStr(eff, "type")) {
	case "minecraft:attribute":
		unknownKeys(eff, "an attribute effect", "type", "attribute", "id", "amount", "operation")
		return []*registry.EnchAttribute{{
			Attribute: requireStr(eff, "attribute"),
			ID:        requireStr(eff, "id"),
			Operation: requireStr(eff, "operation"),
			Amount:    levelValueOf(required(eff, "amount")),
		}}, true
	case "minecraft:all_of":
		var out []*registry.EnchAttribute
		for _, x := range list(required(eff, "effects"), "effects") {
			sub, ok := attributeEffects(object(x, "an entity effect"))
			if !ok {
				return nil, false
			}
			out = append(out, sub...)
		}
		return out, len(out) > 0
	}
	return nil, false
}

func levelValueOf(v any) *registry.EnchLevelValue {
	tag, err := protocol.JSONValueTag(v)
	if err != nil {
		fail("%s", err)
	}
	value, msg := decodeLevelValue(tag)
	if msg != "" {
		fail("%s", msg)
	}
	return value
}

func (d *enchantmentDecoder) requirements(entry obj) *registry.EnchCondition {
	v, ok := entry["requirements"]
	if !ok {
		return nil
	}
	return d.enchCondition(v)
}

func valueEffect(e obj) registry.EnchEffect {
	kind := registry.Qualify(requireStr(e, "type"))
	switch kind {
	case "minecraft:add":
		return registry.EnchEffect{Kind: registry.EnchAdd, Value: levelValueOf(required(e, "value"))}
	case "minecraft:multiply":
		return registry.EnchEffect{Kind: registry.EnchMultiply, Value: levelValueOf(required(e, "factor"))}
	case "minecraft:exponential":
		return registry.EnchEffect{Kind: registry.EnchExponential, Value: levelValueOf(required(e, "base")), Exponent: levelValueOf(required(e, "exponent"))}
	case "minecraft:set":
		return registry.EnchEffect{Kind: registry.EnchSet, Value: levelValueOf(required(e, "value"))}
	case "minecraft:remove_binomial":
		return registry.EnchEffect{Kind: registry.EnchRemoveBinomial, Value: levelValueOf(required(e, "chance"))}
	case "minecraft:all_of":
		out := registry.EnchEffect{Kind: registry.EnchAllOf}
		for _, x := range list(required(e, "effects"), "effects") {
			out.Effects = append(out.Effects, valueEffect(object(x, "a value effect")))
		}
		return out
	}
	fail("value effect type %s is not supported by this server", kind)
	return registry.EnchEffect{}
}

// put stores src in dst, failing when both are set: the sub-effects of an
// all_of share one struct, so two of a kind would overwrite each other.
func put[T comparable](dst *T, src T) {
	var zero T
	if src == zero {
		return
	}
	if *dst != zero {
		fail("an all_of with two entity effects of one kind")
	}
	*dst = src
}

var carriedEntityEffects = []string{
	"minecraft:ignite", "minecraft:apply_mob_effect", "minecraft:damage_entity",
	"minecraft:change_item_damage", "minecraft:all_of", "minecraft:summon_entity",
	"minecraft:play_sound", "minecraft:apply_exhaustion", "minecraft:apply_impulse",
	"minecraft:explode", "minecraft:run_function", "minecraft:replace_block", "minecraft:replace_disk",
	"minecraft:set_block_properties", "minecraft:spawn_particles",
}

// entityEffect reads one entity effect; an effect type the tree does not carry
// is recorded as dropped and leaves the result empty.
func (d *enchantmentDecoder) entityEffect(e obj, where string) registry.EnchEntityEffect {
	var out registry.EnchEntityEffect
	kind := registry.Qualify(requireStr(e, "type"))
	if !slices.Contains(carriedEntityEffects, kind) {
		d.dropped = append(d.dropped, where+": effect "+kind)
		return out
	}
	switch kind {
	case "minecraft:all_of":
		for _, x := range list(required(e, "effects"), "effects") {
			sub := d.entityEffect(object(x, "an entity effect"), where)
			put(&out.Explode, sub.Explode)
			put(&out.SummonEntity, sub.SummonEntity)
			put(&out.PlaySound, sub.PlaySound)
			put(&out.Duration, sub.Duration)
			put(&out.MobEffect, sub.MobEffect)
			put(&out.DamageEntity, sub.DamageEntity)
			put(&out.ChangeItemDamage, sub.ChangeItemDamage)
			put(&out.ApplyExhaustion, sub.ApplyExhaustion)
			put(&out.ApplyImpulse, sub.ApplyImpulse)
			put(&out.RunFunction, sub.RunFunction)
			put(&out.Replace, sub.Replace)
			put(&out.SetProperties, sub.SetProperties)
			put(&out.Particles, sub.Particles)
			put(&out.Summon, sub.Summon)
		}
	case "minecraft:explode":
		out.Explode = explodeEffect(e)
	case "minecraft:summon_entity":
		out.SummonEntity, out.Summon = summonEffect(e)
	case "minecraft:run_function":
		unknownKeys(e, "a run_function effect", "type", "function")
		out.RunFunction = registry.Qualify(requireStr(e, "function"))
	case "minecraft:replace_block":
		out.Replace = replaceEffect(e, false)
	case "minecraft:replace_disk":
		out.Replace = replaceEffect(e, true)
	case "minecraft:set_block_properties":
		out.SetProperties = setPropertiesEffect(e)
	case "minecraft:spawn_particles":
		out.Particles = particlesEffect(e)
	case "minecraft:play_sound":
		unknownKeys(e, "a play sound effect", "type", "sound", "volume", "pitch")
		sound := &registry.EnchPlaySound{Volume: float32(requireNum(e, "volume"))}
		sound.Pitch, sound.PitchMax = soundPitch(required(e, "pitch"))
		if names, ok := e["sound"].([]any); ok {
			sound.Sounds = []protocol.SoundHolder{}
			sound.Sound = protocol.SoundHolder{ID: -1}
			for _, n := range names {
				sound.Sounds = append(sound.Sounds, registry.SoundHolderByName(str(n, "a sound")))
			}
		} else {
			sound.Sound = registry.SoundHolderByName(requireStr(e, "sound"))
		}
		out.PlaySound = sound
	case "minecraft:apply_exhaustion":
		unknownKeys(e, "an apply exhaustion effect", "type", "amount")
		out.ApplyExhaustion = levelValueOf(required(e, "amount"))
	case "minecraft:apply_impulse":
		unknownKeys(e, "an apply impulse effect", "type", "direction", "coordinate_scale", "magnitude")
		// Only the look itself, direction (0, 0, 1) in the look's frame.
		if dir := vec3(e["direction"]); dir != [3]float32{0, 0, 1} {
			fail("an apply_impulse with direction %v", dir)
		}
		out.ApplyImpulse = &registry.EnchApplyImpulse{
			CoordinateScale: vec3(e["coordinate_scale"]),
			Magnitude:       levelValueOf(required(e, "magnitude")),
		}
	case "minecraft:ignite":
		out.Duration = levelValueOf(required(e, "duration"))
	case "minecraft:damage_entity":
		out.DamageEntity = &registry.EnchDamageEntity{
			DamageType: damageTypeOf(requireStr(e, "damage_type")),
			Min:        levelValueOf(required(e, "min_damage")),
			Max:        levelValueOf(required(e, "max_damage")),
		}
	case "minecraft:change_item_damage":
		out.ChangeItemDamage = levelValueOf(required(e, "amount"))
	case "minecraft:apply_mob_effect":
		// One effect by name; a tag or a list is a shape the tree lacks.
		toApply, ok := e["to_apply"].(string)
		if !ok || strings.HasPrefix(toApply, "#") {
			fail("apply_mob_effect whose to_apply is %v", e["to_apply"])
		}
		out.MobEffect = &registry.EnchMobEffect{
			Name:         toApply,
			MinAmplifier: levelValueOf(required(e, "min_amplifier")),
			MaxAmplifier: levelValueOf(required(e, "max_amplifier")),
			MinDuration:  levelValueOf(required(e, "min_duration")),
			MaxDuration:  levelValueOf(required(e, "max_duration")),
		}
	}
	return out
}

func vec3(v any) [3]float32 {
	l := list(v, "a vector")
	if len(l) != 3 {
		fail("a vector of %d numbers", len(l))
	}
	var out [3]float32
	for i, x := range l {
		f, ok := num(x)
		if !ok {
			fail("%v is not a number", x)
		}
		out[i] = float32(f)
	}
	return out
}

// optObject reads an object-valued key that may be absent.
func optObject(o obj, key string) (obj, bool) {
	v, ok := o[key]
	if !ok {
		return nil, false
	}
	return object(v, key), true
}

func optStr(o obj, key string) string {
	v, ok := o[key]
	if !ok {
		return ""
	}
	return str(v, key)
}

// predicateRef reads the pack predicate a requirement names; a list file is
// the conjunction of its conditions.
// vanilla: LootItemConditions.reference (ConditionReference)
func (d *enchantmentDecoder) predicateRef(name string) *registry.EnchCondition {
	name = registry.Qualify(name)
	if d.predicates == nil {
		fail("a predicate reference %s is not supported by this server", name)
	}
	data, ok := d.predicates(name)
	if !ok {
		fail("unknown predicate %s", name)
	}
	if d.refDepth >= 16 {
		fail("a predicate reference loop at %s", name)
	}
	d.refDepth++
	defer func() { d.refDepth-- }()
	v := parseJSON(data)
	if terms, ok := v.([]any); ok {
		out := &registry.EnchCondition{Kind: registry.EnchCondAllOf}
		for _, t := range terms {
			out.Terms = append(out.Terms, d.enchCondition(t))
		}
		return out
	}
	return d.enchCondition(v)
}

// enchCondition reads a "requirements" condition: the variants the tree
// carries, and any other condition the loot codec reads as a loot condition.
// vanilla: LootItemCondition
func (d *enchantmentDecoder) enchCondition(v any) (out *registry.EnchCondition) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		c, isObject := v.(map[string]any)
		// A reference stays an error here: the pack's predicates are all read before it.
		if _, failed := r.(lootFailure); !failed || !isObject || registry.Qualify(optStr(c, "type")) == "minecraft:reference" {
			panic(r)
		}
		out = &registry.EnchCondition{Kind: registry.EnchCondLoot, Loot: []registry.LootCondition{lootDecoder{}.condition(c)}}
	}()
	return d.nativeCondition(v)
}

// nativeCondition reads the variants the tree carries, with a predicate
// reference resolved through d.predicates.
func (d *enchantmentDecoder) nativeCondition(v any) *registry.EnchCondition {
	if name, isRef := v.(string); isRef {
		return d.predicateRef(name)
	}
	c := object(v, "a loot condition")
	kind := registry.Qualify(requireStr(c, "type"))
	predicate, _ := optObject(c, "predicate")
	switch kind {
	case "minecraft:reference":
		return d.predicateRef(requireStr(c, "name"))
	case "minecraft:match_tool":
		unknownKeys(predicate, "a match_tool predicate", "items")
		out := &registry.EnchCondition{Kind: registry.EnchCondMatchTool}
		switch items := predicate["items"].(type) {
		case string:
			if strings.HasPrefix(items, "#") {
				out.ToolTag = registry.Qualify(items[1:])
			} else {
				out.ToolItems = []registry.Item{itemNamed(items)}
			}
		case []any:
			for _, x := range items {
				one := str(x, "a match_tool item")
				if strings.HasPrefix(one, "#") {
					fail("a tag inside a match_tool item list: %s", one)
				}
				out.ToolItems = append(out.ToolItems, itemNamed(one))
			}
		}
		return out
	case "minecraft:weather_check":
		unknownKeys(c, "a weather check", "type", "thundering")
		return &registry.EnchCondition{Kind: registry.EnchCondWeatherCheck, Thundering: new(requireFlag(c, "thundering"))}
	case "minecraft:location_check":
		unknownKeys(c, "a location check", "type", "predicate")
		out := &registry.EnchCondition{Kind: registry.EnchCondLocationCheck}
		enchLocation(out, predicate)
		return out
	case "minecraft:entity_properties":
		return entityProperties(c, predicate)
	case "minecraft:damage_source_properties":
		unknownKeys(predicate, "a damage_source_properties predicate", "tags", "is_direct")
		out := &registry.EnchCondition{Kind: registry.EnchCondDamageSource, Direct: -1}
		for _, x := range list(predicate["tags"], "tags") {
			t := object(x, "a damage source tag")
			out.DamageTags = append(out.DamageTags, registry.EnchTagExpect{
				Types: damageHolderSet(required(t, "id")), Expected: requireFlag(t, "expected"),
			})
		}
		if _, ok := predicate["is_direct"]; ok {
			out.Direct = 0
			if flag(predicate, "is_direct", false) {
				out.Direct = 1
			}
		}
		return out
	case "minecraft:random_chance":
		chance := required(c, "chance")
		if m, ok := chance.(map[string]any); ok {
			if registry.Qualify(optStr(m, "type")) != "minecraft:enchantment_level" {
				fail("a random_chance whose chance is %v", m)
			}
			chance = required(m, "amount")
		}
		return &registry.EnchCondition{Kind: registry.EnchCondRandomChance, Chance: levelValueOf(chance)}
	case "minecraft:enchantment_active_check":
		unknownKeys(c, "an enchantment active check", "type", "active")
		return &registry.EnchCondition{Kind: registry.EnchCondActive, Active: requireFlag(c, "active")}
	case "minecraft:inverted":
		return &registry.EnchCondition{Kind: registry.EnchCondInverted, Term: d.enchCondition(required(c, "term"))}
	case "minecraft:all_of", "minecraft:any_of":
		out := &registry.EnchCondition{Kind: registry.EnchCondAnyOf}
		if kind == "minecraft:all_of" {
			out.Kind = registry.EnchCondAllOf
		}
		for _, x := range list(required(c, "terms"), "terms") {
			out.Terms = append(out.Terms, d.enchCondition(x))
		}
		return out
	}
	fail("condition type %s is not supported by this server", kind)
	return nil
}

func entityProperties(c, predicate obj) *registry.EnchCondition {
	entity := optStr(c, "entity")
	flags, _ := optObject(predicate, "minecraft:flags")
	if entity == "this" && asksAfterBody(predicate, flags) {
		body := entityPredicate(predicate)
		return &registry.EnchCondition{Kind: registry.EnchCondBody, Entity: entity, Body: &body}
	}
	_, hasMovement := predicate["minecraft:movement"]
	if _, hasFlying := flags["is_flying"]; hasMovement || hasFlying {
		unknownKeys(predicate, "an attacker movement predicate", "minecraft:flags", "minecraft:movement")
		if entity != "direct_attacker" {
			fail("movement predicate on %s", entity)
		}
		unknownKeys(flags, "entity flags", "is_flying")
		if _, ok := flags["is_flying"]; !ok {
			fail("entity flags without is_flying")
		}
		movement := object(required(predicate, "minecraft:movement"), "entity movement")
		unknownKeys(movement, "entity movement", "fall_distance")
		fall := object(required(movement, "fall_distance"), "fall distance")
		unknownKeys(fall, "fall distance", "min")
		minFall := requireNum(fall, "min")
		return &registry.EnchCondition{Kind: registry.EnchCondEntityProperties, Entity: entity,
			IsFlying: new(flag(flags, "is_flying", false)), MinFallDistance: &minFall}
	}
	unknownKeys(predicate, "an entity_properties predicate", "minecraft:entity_type", "minecraft:location",
		"minecraft:vehicle", "minecraft:flags", "minecraft:type_specific/player")
	if state := entityState(predicate); state != nil {
		return &registry.EnchCondition{Kind: registry.EnchCondEntityState, Entity: entity, State: state}
	}
	if location, ok := optObject(predicate, "minecraft:location"); ok {
		if len(predicate) != 1 {
			fail("combined entity type and location predicate")
		}
		out := &registry.EnchCondition{Kind: registry.EnchCondEntityProperties, Entity: entity}
		enchLocation(out, location)
		return out
	}
	entityType, ok := predicate["minecraft:entity_type"].(string)
	if !ok {
		fail("an entity_properties predicate whose entity_type is %v", predicate["minecraft:entity_type"])
	}
	out := &registry.EnchCondition{Kind: registry.EnchCondEntityProperties, Entity: entity}
	if strings.HasPrefix(entityType, "#") {
		out.EntityTypeTag = entityType[1:]
	} else {
		out.EntityTypeID = entityType
	}
	return out
}

// asksAfterBody reports whether an entity_properties predicate on "this" asks
// after what only a snapshot of the body knows: its movement, the block under
// it, its age, or a flag besides the user-state ones.
func asksAfterBody(predicate, flags obj) bool {
	for _, key := range []string{"minecraft:movement_affected_by", "minecraft:movement", "minecraft:periodic_tick"} {
		if _, ok := predicate[key]; ok {
			return true
		}
	}
	for key := range flags {
		if key != "is_fall_flying" && key != "is_in_water" {
			return true
		}
	}
	return false
}

// entityState reads the vehicle, flags and player parts of an entity_properties
// predicate; nil when it asks for none.
func entityState(p obj) *registry.EnchEntityState {
	var s registry.EnchEntityState
	found := false
	if v, ok := optObject(p, "minecraft:vehicle"); ok {
		if len(v) != 0 {
			fail("a vehicle predicate with %v", v)
		}
		s.Vehicle, found = true, true
	}
	if f, ok := optObject(p, "minecraft:flags"); ok {
		unknownKeys(f, "a flags predicate", "is_fall_flying", "is_in_water")
		for key, field := range map[string]**bool{"is_fall_flying": &s.FallFlying, "is_in_water": &s.InWater} {
			if _, ok := f[key]; ok {
				*field, found = new(flag(f, key, false)), true
			}
		}
	}
	if pl, ok := optObject(p, "minecraft:type_specific/player"); ok {
		unknownKeys(pl, "a player predicate", "gamemode", "food")
		s.Player, found = true, true
		for _, m := range list(pl["gamemode"], "gamemode") {
			s.Gamemodes = append(s.Gamemodes, str(m, "a game mode"))
		}
		if food, ok := optObject(pl, "food"); ok {
			unknownKeys(food, "a food predicate", "level")
			level := object(required(food, "level"), "a food level range")
			unknownKeys(level, "a food level range", "min")
			foodMin := int32(requireNum(level, "min"))
			s.FoodMin = &foodMin
		}
	}
	if !found {
		return nil
	}
	return &s
}

// enchLocation carries sky visibility and a block tag, refusing richer predicates.
func enchLocation(out *registry.EnchCondition, p obj) {
	unknownKeys(p, "an enchantment location", "can_see_sky", "block")
	if _, ok := p["can_see_sky"]; ok {
		out.CanSeeSky = new(flag(p, "can_see_sky", false))
	}
	if block, ok := optObject(p, "block"); ok {
		unknownKeys(block, "an enchantment block predicate", "blocks")
		tag := requireStr(block, "blocks")
		if !strings.HasPrefix(tag, "#") {
			fail("an enchantment location block without a tag")
		}
		out.BlockTag = tag[1:]
	}
}

func damageTypeOf(name string) registry.DamageType { d, _ := registry.DamageTypeByName(name); return d }

// explodeEffect reads an explode effect. A wind-burst look is dropped so that
// the burst's own is used.
// vanilla: ExplodeEffect.CODEC
func explodeEffect(e obj) *registry.EnchExplode {
	unknownKeys(e, "an explode effect", "type", "radius", "knockback_multiplier", "block_interaction", "immune_blocks",
		"attribute_to_user", "damage_type", "offset", "create_fire", "small_particle", "large_particle", "block_particles", "sound")
	out := &registry.EnchExplode{Radius: levelValueOf(required(e, "radius"))}
	if v, ok := e["knockback_multiplier"]; ok {
		out.KnockbackMultiplier = levelValueOf(v)
	}
	switch out.BlockInteraction = requireStr(e, "block_interaction"); out.BlockInteraction {
	case "none", "block", "mob", "tnt", "trigger":
	default:
		fail("an explode effect with block_interaction %q", out.BlockInteraction)
	}
	smallID, smallOpts := particleOf(required(e, "small_particle"))
	largeID, largeOpts := particleOf(required(e, "large_particle"))
	look := &registry.EnchExplosionLook{Small: smallID, Large: largeID, SmallOptions: smallOpts, LargeOptions: largeOpts, Sound: soundOf(required(e, "sound"))}
	for _, x := range list(e["block_particles"], "block_particles") {
		entry := object(x, "a block particle entry")
		unknownKeys(entry, "a block particle entry", "data", "weight")
		data := object(required(entry, "data"), "a block particle")
		unknownKeys(data, "a block particle", "particle", "scaling", "speed")
		id, opts := particleOf(required(data, "particle"))
		look.Blocks = append(look.Blocks, registry.EnchBlockParticle{Particle: id, Options: opts, Scaling: float32(optNum(data, "scaling", 1)),
			Speed: float32(optNum(data, "speed", 1)), Weight: int32(optNum(entry, "weight", 1))})
	}
	if v, ok := e["immune_blocks"]; ok {
		if tag, isStr := v.(string); isStr && strings.HasPrefix(tag, "#") {
			out.ImmuneBlocks = registry.Qualify(tag[1:])
		} else {
			for _, name := range holderList(v) {
				out.ImmuneList = append(out.ImmuneList, registry.Qualify(str(name, "a block")))
			}
			if out.ImmuneList == nil {
				out.ImmuneList = []string{}
			}
		}
	}
	if v, ok := e["attribute_to_user"].(bool); ok {
		out.AttributeToUser = v
	}
	if v, ok := e["create_fire"].(bool); ok {
		out.CreateFire = v
	}
	if v, ok := e["damage_type"]; ok {
		out.DamageType = registry.Qualify(str(v, "a damage type"))
	}
	if v, ok := e["offset"]; ok {
		for i, f := range vec3(v) {
			out.Offset[i] = float64(f)
		}
	}
	windLook := smallID == protocol.ParticleGustEmitterSmall && largeID == protocol.ParticleGustEmitterLarge && len(look.Blocks) == 0 &&
		look.Sound == registry.SoundHolderByName("minecraft:entity.wind_charge.wind_burst")
	if !out.WindBurst() || !windLook {
		out.Look = look
	}
	return out
}

// soundOf reads a sound holder: an event name, or {sound_id, range}.
// vanilla: SoundEvent.CODEC
func soundOf(v any) protocol.SoundHolder {
	if name, ok := v.(string); ok {
		return registry.SoundHolderByName(registry.Qualify(name))
	}
	o := object(v, "a sound")
	unknownKeys(o, "a sound", "sound_id", "range")
	h := protocol.SoundHolder{ID: -1, Name: registry.Qualify(requireStr(o, "sound_id"))}
	if r, ok := o["range"]; ok {
		f, isNum := num(r)
		if !isNum {
			fail("a sound range that is not a number")
		}
		h.HasRange, h.Range = true, float32(f)
	}
	return h
}
