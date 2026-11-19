package main

import (
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/golem-mc/golem/tools/internal/vanilla"
)

// lootCodec resolves holders into the condition and function shapes the table
// writers consume. Keep this at the codec boundary: an item's component data
// may use the same keys with unrelated meanings.
// vanilla: LootItemCondition.CODEC, LootItemFunctions.CODEC
type lootCodec struct{ dp *vanilla.DataPack }

// singleRegistryName refuses a holder set the generated tree cannot represent.
func singleRegistryName(o obj, key string) string {
	v := o.field(key)
	if list, ok := v.([]any); ok && len(list) == 1 {
		v = list[0]
	}
	name := asStr(v, "a single "+key+" holder")
	if strings.HasPrefix(name, "#") {
		fail("%s: a %s tag needs a holder-set model", name, key)
	}
	return qualified(name)
}

func (r lootCodec) reference(kind, name string, stack []string, read func(any, []string) any) any {
	if strings.HasPrefix(name, "#") {
		fail("%s: a single %s holder cannot be a tag", name, kind)
	}
	namespace, local, _ := strings.Cut(qualified(name), ":")
	file := path.Join(namespace, kind, local+".json")
	if !fs.ValidPath(namespace) || !fs.ValidPath(local) || slices.Contains(stack, file) {
		fail("%s: cyclic or invalid %s reference via %v", name, kind, stack)
	}
	if r.dp == nil {
		fail("%s: no data pack for %s reference", name, kind)
	}
	return read(decodeValue(mustRead(r.dp, file), file), append(slices.Clone(stack), file))
}

// holders expands a holder set, preserving its order, including named tags.
// vanilla: RegistryCodecs.holderSet
func (r lootCodec) holders(kind string, v any, stack []string) []any {
	if name, ok := v.(string); ok && strings.HasPrefix(name, "#") {
		return r.reference("tags/"+kind, name[1:], stack, func(raw any, stack []string) any {
			tag := asObj(raw, "a "+kind+" tag")
			var out []any
			seen := map[string]bool{} // vanilla: TagLoader.tryBuildTag (LinkedHashSet)
			for _, value := range tag.list("values") {
				// Optional entries need registry membership to decide whether
				// they exist; refuse them rather than silently drop a condition.
				name := asStr(value, "a "+kind+" tag member")
				for _, member := range r.holders(kind, name, stack) {
					id := qualified(asStr(member, "a "+kind+" tag member"))
					if !seen[id] {
						seen[id] = true
						out = append(out, id)
					}
				}
			}
			return out
		}).([]any)
	}
	if list, ok := v.([]any); ok {
		return list
	}
	return []any{v}
}

func (r lootCodec) predicate(v any, stack []string) obj {
	if name, ok := v.(string); ok {
		return r.reference("predicate", name, stack, func(raw any, stack []string) any {
			return r.predicate(raw, stack)
		}).(obj)
	}
	c := maps.Clone(asObj(v, "a loot condition"))
	// A condition already read carries condition instead of type.
	if c.has("type") {
		if c.has("condition") {
			fail("a loot condition has both type and condition: %v", c)
		}
		c["condition"] = c.str("type")
		delete(c, "type")
	}
	switch qualified(c.str("condition")) {
	case "minecraft:all_of", "minecraft:any_of":
		var terms []any
		for _, term := range r.holders("predicate", c.field("terms"), stack) {
			terms = append(terms, map[string]any(r.predicate(term, stack)))
		}
		c["terms"] = terms
	case "minecraft:inverted":
		c["term"] = map[string]any(r.predicate(c.field("term"), stack))
	}
	c["condition"] = qualified(c.str("condition"))
	return c
}

func (r lootCodec) optionalPredicate(o obj, key string) obj {
	if !o.has(key) {
		return nil
	}
	return r.predicate(o[key], nil)
}

// context keeps conjunctions as lists for the advancement readers.
// vanilla: SimpleCriterionTrigger.SimpleInstance, AllOfCondition
func (r lootCodec) context(v any) []any {
	if list, ok := v.([]any); ok {
		out := make([]any, len(list))
		for i, term := range list {
			out[i] = map[string]any(r.predicate(term, nil))
		}
		return out
	}
	c := r.predicate(v, nil)
	if c["condition"] == "minecraft:all_of" && keysAre(c, "condition", "terms") {
		return c.list("terms")
	}
	return []any{map[string]any(c)}
}

// advancementConditions converts only context predicates, leaving item and
// damage predicates to their own codecs.
func (r lootCodec) advancementConditions(o obj) obj {
	o = maps.Clone(o)
	for _, key := range []string{"source", "player", "entity", "child", "parent", "partner", "projectile", "lightning", "bystander", "cause", "location"} {
		if o.has(key) {
			o[key] = r.context(o[key])
		}
	}
	if o.has("victims") {
		victims := slices.Clone(o.list("victims"))
		for i, victim := range victims {
			victims[i] = r.context(victim)
		}
		o["victims"] = victims
	}
	return o
}

// conditions reads the condition holder of a pool, an entry or a modifier, or
// the conditions list modifier leaves on a function it has already read.
func (r lootCodec) conditions(o obj) []any {
	if o.has("condition") {
		if o.has("conditions") {
			fail("both condition and conditions: %v", o)
		}
		return []any{map[string]any(r.predicate(o["condition"], nil))}
	}
	var out []any
	for _, v := range o.listOr("conditions") {
		out = append(out, map[string]any(r.predicate(v, nil)))
	}
	return out
}

// modifier flattens only unconditional sequences; a conditional sequence must
// keep its condition around the whole sequence, which the current tree cannot.
// vanilla: SequenceFunction
func (r lootCodec) modifier(v any, stack []string) []any {
	if name, ok := v.(string); ok {
		return r.reference("item_modifier", name, stack, func(raw any, stack []string) any {
			return r.modifier(raw, stack)
		}).([]any)
	}
	if list, ok := v.([]any); ok {
		var out []any
		for _, term := range list {
			out = append(out, r.modifier(term, stack)...)
		}
		return out
	}
	f := maps.Clone(asObj(v, "an item modifier"))
	// A modifier already read carries function instead of type.
	if f.has("type") {
		if f.has("function") {
			fail("an item modifier has both type and function: %v", f)
		}
		f["function"] = f.str("type")
		delete(f, "type")
	}
	f["function"] = qualified(f.str("function"))
	if f["function"] == "minecraft:sequence" {
		if !keysAre(f, "function", "functions") {
			fail("a conditional or unknown item modifier sequence: %v", f)
		}
		var out []any
		for _, term := range r.holders("item_modifier", f.field("functions"), stack) {
			out = append(out, r.modifier(term, stack)...)
		}
		return out
	}
	if f["function"] == "minecraft:filtered" {
		for _, key := range []string{"on_pass", "on_fail"} {
			if f.has(key) {
				branch := r.modifier(f[key], stack)
				if len(branch) != 1 {
					fail("a filtered %s needs a single item modifier: %v", key, f)
				}
				f[key] = branch[0]
			}
		}
	}
	conditions := r.conditions(f)
	delete(f, "condition")
	if len(conditions) > 0 {
		f["conditions"] = conditions
	}
	return []any{map[string]any(f)}
}

func (r lootCodec) functions(o obj) []any {
	return r.modifiers(o, "modifier")
}

func (r lootCodec) modifiers(o obj, key string) []any {
	if !o.has(key) {
		return nil
	}
	return r.modifier(o[key], nil)
}
