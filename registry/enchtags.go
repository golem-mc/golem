package registry

// Tag membership for the entity, item and damage-type tests the enchantment
// conditions name by tag, resolved against the tag tables configuration
// sends, so server and client agree. Tag names live in the generated table,
// not at call sites; an unknown tag holds nothing.

// builtinTagIndex indexes one built-in registry's tags by tag name: the tags
// of a built-in registry carry their members by numeric ID.
func builtinTagIndex(builtin []BuiltinTaggedRegistry, registry string) map[string]map[int32]bool {
	m := make(map[string]map[int32]bool)
	for _, r := range builtin {
		if r.Registry != registry {
			continue
		}
		for _, tag := range r.Tags {
			members := make(map[int32]bool, len(tag.IDs))
			for _, id := range tag.IDs {
				members[id] = true
			}
			m[tag.Name] = members
		}
	}
	return m
}

// EntityTypeInTag reports whether an entity type belongs to a tag, by the
// tag's name in the minecraft:entity_type tag registry.
func EntityTypeInTag(id int32, tag string) bool {
	return tagsNow.Load().entityType[tag][id]
}

// ItemTagExists reports whether the minecraft:item tag registry defines a tag
// of the name, which an item predicate's tag argument is looked up against.
func ItemTagExists(tag string) bool {
	_, ok := tagsNow.Load().item[tag]
	return ok
}

// syncTagIndex indexes one synchronized registry's tags by tag name, their
// members travelling by name.
func syncTagIndex(sync []TaggedRegistry, registry string) map[string]map[string]bool {
	m := make(map[string]map[string]bool)
	for _, r := range sync {
		if r.Registry != registry {
			continue
		}
		for _, tag := range r.Tags {
			members := make(map[string]bool, len(tag.Entries))
			for _, e := range tag.Entries {
				members[e] = true
			}
			m[tag.Name] = members
		}
	}
	return m
}

// DamageTypeTagExists reports whether the minecraft:damage_type registry
// has a tag of this name.
func DamageTypeTagExists(tag string) bool {
	return tagsNow.Load().damageTable.defined[tag]
}

// EnchantmentInTag reports whether an enchantment is in a minecraft:enchantment
// tag; the table's exclusive-set test runs on it.
func EnchantmentInTag(id int32, tag string) bool {
	name, ok := EnchantmentName(id)
	if !ok {
		return false
	}
	return tagsNow.Load().enchantment[tag][name]
}

// enchantingTableIDs is EnchantingTableOrder as numeric IDs, built once since
// every offer walks it; unknown names are skipped.
var enchantingTableIDs = func() []int32 {
	ids := make([]int32, 0, len(EnchantingTableOrder))
	for _, name := range EnchantingTableOrder {
		if id, ok := EnchantmentID(name); ok {
			ids = append(ids, id)
		}
	}
	return ids
}()

// EnchantingTableIDs is the enchanting table's pool by numeric ID, in the
// in_enchanting_table tag's order: the registry's alphabetical order would
// land every weighted draw elsewhere.
func EnchantingTableIDs() []int32 { return enchantingTableIDs }

// enchantmentTagIDs resolves every enchantment tag to member IDs in tag
// order, once per tag load: a treasure roll reads on_random_loot per catch.
func enchantmentTagIDs(sync []TaggedRegistry) map[string][]int32 {
	m := make(map[string][]int32)
	for _, r := range sync {
		if r.Registry != "minecraft:enchantment" {
			continue
		}
		for _, tag := range r.Tags {
			ids := make([]int32, 0, len(tag.Entries))
			for _, name := range tag.Entries {
				if id, ok := EnchantmentID(name); ok {
					ids = append(ids, id)
				}
			}
			m[tag.Name] = ids
		}
	}
	return m
}

// EnchantmentTagIDs resolves an enchantment tag to member IDs in tag order,
// the order a weighted draw walks.
func EnchantmentTagIDs(tag string) []int32 {
	return tagsNow.Load().enchantmentIDs[tag]
}
