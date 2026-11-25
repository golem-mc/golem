package main

import (
	"fmt"
	"strings"
)

// enchIntVec is a Vec3i as a Go [3]int32, zero when absent.
func enchIntVec(o obj, key string) string {
	if !o.has(key) {
		return "[3]int32{}"
	}
	l := o.list(key)
	if len(l) != 3 {
		noShape("an offset of %d numbers", len(l))
	}
	return fmt.Sprintf("[3]int32{%s, %s, %s}", enchNum(l[0]), enchNum(l[1]), enchNum(l[2]))
}

// enchGameEvent is the trigger_game_event of a replace effect as Go.
func enchGameEvent(o obj) string {
	if !o.has("trigger_game_event") {
		return "GameEventNone"
	}
	name, err := ident("GameEvent", qualified(o.str("trigger_game_event")))
	if err != nil {
		fail("%v", err)
	}
	return name
}

// enchBlockState is a simple state provider's state as Go.
func enchBlockState(v any) string {
	if o, ok := v.(map[string]any); ok && o["type"] != nil {
		p := obj(o)
		if t := qualified(p.str("type")); t != "minecraft:simple" {
			noShape("a block state provider of type %s", t)
		}
		unknownKeys(p, "a simple state provider", "type", "state")
		v = p.field("state")
	}
	if name, ok := v.(string); ok {
		v = map[string]any{"id": name}
	}
	state := asObj(v, "a block state")
	unknownKeys(state, "a block state", "id", "properties")
	props, _ := state.obj("properties")
	var parts []string
	for _, k := range sortedKeys(props) {
		parts = append(parts, fmt.Sprintf("%q: %q", k, asStr(props[k], "a state property")))
	}
	return fmt.Sprintf("MustState(%q, map[string]string{%s})", qualified(state.str("id")), strings.Join(parts, ", "))
}

// enchBlockPredicate is a block predicate as Go, the kinds replace_disk uses.
func enchBlockPredicate(o obj) string {
	kind := qualified(o.str("type"))
	short := strings.TrimPrefix(kind, "minecraft:")
	fields := []string{fmt.Sprintf("Kind: %q", short)}
	if o.has("offset") {
		fields = append(fields, "Offset: "+enchIntVec(o, "offset"))
	}
	holders := func(key string) []string {
		var names []string
		switch v := o.field(key).(type) {
		case string:
			names = []string{v}
		case []any:
			for _, x := range v {
				names = append(names, asStr(x, "a name"))
			}
		}
		for _, n := range names {
			if strings.HasPrefix(n, "#") {
				noShape("a %s predicate with the tag %s", short, n)
			}
		}
		return names
	}
	switch kind {
	case "minecraft:matching_blocks":
		unknownKeys(o, "a matching_blocks predicate", "type", "blocks", "offset")
		symbols, err := idents("Block", qualifiedAll(holders("blocks")))
		if err != nil {
			fail("%v", err)
		}
		fields = append(fields, "Blocks: []Block{"+strings.Join(symbols, ", ")+"}")
	case "minecraft:matching_block_tag":
		unknownKeys(o, "a matching_block_tag predicate", "type", "tag", "offset")
		fields = append(fields, fmt.Sprintf("Tag: mustBlockTag(%q)", strings.TrimPrefix(qualified(o.str("tag")), "#")))
	case "minecraft:matching_fluids":
		unknownKeys(o, "a matching_fluids predicate", "type", "fluids", "offset")
		fields = append(fields, "Names: []string{"+quoteAll(qualifiedAll(holders("fluids")))+"}")
	case "minecraft:true", "minecraft:solid", "minecraft:replaceable", "minecraft:inside_world_bounds", "minecraft:unobstructed":
		unknownKeys(o, "a "+short+" predicate", "type", "offset")
	case "minecraft:not":
		unknownKeys(o, "a not predicate", "type", "predicate")
		fields = append(fields, "Terms: []*EnchBlockPredicate{"+enchBlockPredicate(asObj(o.field("predicate"), "a block predicate"))+"}")
	case "minecraft:all_of", "minecraft:any_of":
		unknownKeys(o, "a "+short+" predicate", "type", "predicates")
		var terms []string
		for _, x := range o.list("predicates") {
			terms = append(terms, enchBlockPredicate(asObj(x, "a block predicate")))
		}
		fields = append(fields, "Terms: []*EnchBlockPredicate{"+strings.Join(terms, ", ")+"}")
	default:
		noShape("block predicate %s", kind)
	}
	return "&EnchBlockPredicate{" + strings.Join(fields, ", ") + "}"
}

func qualifiedAll(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = qualified(n)
	}
	return out
}

// enchReplaceDisk is a replace_disk effect as Go.
func enchReplaceDisk(e obj) string {
	unknownKeys(e, "a replace_disk effect", "type", "offset", "predicate", "block_state", "trigger_game_event", "radius", "height")
	fields := []string{"Offset: " + enchIntVec(e, "offset"), "State: " + enchBlockState(e.field("block_state"))}
	if e.has("predicate") {
		fields = append(fields, "Predicate: "+enchBlockPredicate(asObj(e["predicate"], "a block predicate")))
	}
	fields = append(fields, "GameEvent: "+enchGameEvent(e), "Radius: "+levelValue(e.field("radius")), "Height: "+levelValue(e.field("height")))
	return "Replace: &EnchReplace{" + strings.Join(fields, ", ") + "}"
}

// enchFloatRange is a float provider (a number or uniform) as Go.
func enchFloatRange(v any) string {
	if v == nil {
		return "EnchFloatRange{}"
	}
	if _, ok := asNum(v); ok {
		return fmt.Sprintf("EnchFloatRange{Min: %s, Max: %s}", enchNum(v), enchNum(v))
	}
	o := asObj(v, "a float provider")
	if qualified(o.str("type")) != "minecraft:uniform" {
		noShape("a float provider other than a constant or uniform")
	}
	return fmt.Sprintf("EnchFloatRange{Min: %s, Max: %s}", enchNum(o.field("min_inclusive")), enchNum(o.field("max_exclusive")))
}

// enchParticleAxis is one axis of spawn_particles as Go.
func enchParticleAxis(pos, vel obj) string {
	unknownKeys(pos, "a position source", "type", "offset", "scale")
	unknownKeys(vel, "a velocity source", "movement_scale", "base")
	scale := pos.numOr("scale", 1)
	inBox := false
	switch pos.str("type") {
	case "entity_position":
		if scale != 1 {
			noShape("a scaled entity position coordinate source")
		}
	case "in_bounding_box":
		inBox = true
	default:
		noShape("position source %s", pos.str("type"))
	}
	return fmt.Sprintf("EnchParticleAxis{InBox: %t, Offset: %s, Scale: %s, Movement: %s, Base: %s}", inBox,
		enchNum(pos.numOr("offset", 0)), enchNum(scale), enchNum(vel.numOr("movement_scale", 0)), enchFloatRange(vel["base"]))
}

// enchSpawnParticles is a spawn_particles effect with a data-less particle as Go.
func enchSpawnParticles(e obj) string {
	unknownKeys(e, "a spawn_particles effect", "type", "particle", "horizontal_position", "vertical_position",
		"horizontal_velocity", "vertical_velocity", "speed")
	var name string
	switch p := e.field("particle").(type) {
	case string:
		name = p
	case map[string]any:
		unknownKeys(obj(p), "a particle", "type")
		name = obj(p).str("type")
	default:
		noShape("a particle that is neither a name nor an object")
	}
	symbol, err := ident("Particle", qualified(name))
	if err != nil {
		fail("%v", err)
	}
	axis := func(pos, vel string) string {
		return enchParticleAxis(asObj(e.field(pos), "a position source"), asObj(e.field(vel), "a velocity source"))
	}
	return fmt.Sprintf("Particles: &EnchParticles{Particle: protocol.%s, Horizontal: %s, Vertical: %s, Speed: %s}", symbol,
		axis("horizontal_position", "horizontal_velocity"), axis("vertical_position", "vertical_velocity"), enchFloatRange(e["speed"]))
}

// enchLocatedEntries writes a location_changed or tick component. An
// all_of of attribute effects becomes one entry per attribute, sharing the
// original entry's Group.
func (w *enchantmentWriter) locatedEntries(list []any, component string) string {
	var kept []string
	for group, x := range list {
		entry := asObj(x, "a located effect")
		requirements := enchCondition(w.codec.optionalPredicate(entry, "requirements"))
		effect := asObj(entry.field("effect"), "an effect")
		if component == "minecraft:location_changed" {
			if attrs, ok := enchAttributeEffects(effect); ok {
				for _, a := range attrs {
					kept = append(kept, fmt.Sprintf("EnchLocated{Attribute: %s, Requires: %s, Group: %d}", a, requirements, group))
				}
				continue
			}
		}
		if fields, ok := w.entityEffect(effect, component); ok {
			kept = append(kept, fmt.Sprintf("EnchLocated{Effect: EnchEntityEffect{%s}, Requires: %s, Group: %d}", fields, requirements, group))
		}
	}
	if len(kept) == 0 {
		return ""
	}
	return "[]EnchLocated{" + strings.Join(kept, ", ") + "}"
}

// enchAttributeEffects reads an attribute effect, or an all_of of nothing
// but attribute effects, as EnchAttribute literals.
func enchAttributeEffects(e obj) ([]string, bool) {
	switch qualified(e.str("type")) {
	case "minecraft:attribute":
		unknownKeys(e, "an attribute effect", "type", "attribute", "id", "amount", "operation")
		return []string{fmt.Sprintf(`&EnchAttribute{Attribute: %q, ID: %q, Operation: %q, Amount: %s}`,
			e.str("attribute"), e.str("id"), e.str("operation"), levelValue(e.field("amount")))}, true
	case "minecraft:all_of":
		var out []string
		for _, x := range e.list("effects") {
			sub, ok := enchAttributeEffects(asObj(x, "an entity effect"))
			if !ok {
				return nil, false
			}
			out = append(out, sub...)
		}
		return out, len(out) > 0
	}
	return nil, false
}
