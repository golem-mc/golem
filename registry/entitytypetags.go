package registry

// EntityTypeTagID indexes an entity type's tag membership bitset. Built-in tags keep their
// sorted positions across reloads; pack-only IDs must be resolved again.
// The zero value is a real tag; noEntityTypeTag represents an unknown name.
type EntityTypeTagID uint16

const noEntityTypeTag EntityTypeTagID = 0xffff

var baseEntityTypeTagNames = builtinTagNames("minecraft:entity_type")

// newEntityTypeTagTable assigns built-in names first, then sorted pack-only names.
// A missing built-in tag keeps its ID with no members.
func newEntityTypeTagTable(tags map[string][]int32) EntityTypeTagLookup {
	ids, words, bits := newTagBits(baseEntityTypeTagNames, tags, EntityTypeCount)
	t := EntityTypeTagLookup{ids: make(map[string]EntityTypeTagID, len(ids)), words: words, bits: bits}
	for name, id := range ids {
		t.ids[name] = EntityTypeTagID(id)
	}
	return t
}

// EntityTypeTagLookup is an immutable snapshot of entity type tag membership.
type EntityTypeTagLookup struct {
	ids   map[string]EntityTypeTagID
	words int      // bitset width per entity type
	bits  []uint64 // words per entity type, indexed by EntityType
}

// EntityTypeTagOf returns the ID of a named entity type tag, or an ID with no members
// for an unknown name.
func EntityTypeTagOf(tag string) EntityTypeTagID {
	if id, ok := tagsNow.Load().entityTable.ids[tag]; ok {
		return id
	}
	return noEntityTypeTag
}

// InTag reports whether the entity type belongs to the tag.
func (t EntityType) InTag(tag EntityTypeTagID) bool {
	tbl := &tagsNow.Load().entityTable
	if t < 0 || t >= EntityTypeCount || tag == noEntityTypeTag {
		return false
	}
	return tagBitSet(tbl.bits, tbl.words, int(t), uint16(tag))
}

// entityTypeTagIDs collects the numeric members of the entity type tags sent to clients.
func entityTypeTagIDs(builtin []BuiltinTaggedRegistry) map[string][]int32 {
	return builtinTagIDs(builtin, "minecraft:entity_type")
}
