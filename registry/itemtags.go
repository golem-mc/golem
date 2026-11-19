package registry

// ItemTagID indexes an item's tag membership bitset. Built-in tags keep their
// sorted positions across reloads; pack-only IDs must be resolved again.
// The zero value is a real tag; noItemTag represents an unknown name.
type ItemTagID uint16

const noItemTag ItemTagID = 0xffff

var baseItemTagNames = builtinTagNames("minecraft:item")

// newItemTagTable assigns built-in names first, then sorted pack-only names.
// A missing built-in tag keeps its ID with no members.
func newItemTagTable(tags map[string][]int32) ItemTagLookup {
	ids, words, bits := newTagBits(baseItemTagNames, tags, ItemCount)
	t := ItemTagLookup{ids: make(map[string]ItemTagID, len(ids)), words: words, bits: bits}
	for name, id := range ids {
		t.ids[name] = ItemTagID(id)
	}
	return t
}

// ItemTagLookup is an immutable snapshot of item tag membership.
type ItemTagLookup struct {
	ids   map[string]ItemTagID
	words int      // bitset width per item
	bits  []uint64 // words per item, indexed by Item
}

// ItemTagOf returns the ID of a named item tag, or an ID with no members
// for an unknown name.
func ItemTagOf(tag string) ItemTagID {
	if id, ok := tagsNow.Load().itemTable.ids[tag]; ok {
		return id
	}
	return noItemTag
}

// InTag reports whether the item belongs to the tag.
func (item Item) InTag(tag ItemTagID) bool {
	tbl := &tagsNow.Load().itemTable
	if item < 0 || item >= ItemCount || tag == noItemTag {
		return false
	}
	return tagBitSet(tbl.bits, tbl.words, int(item), uint16(tag))
}

// itemTagIDs collects the numeric members of the item tags sent to clients.
func itemTagIDs(builtin []BuiltinTaggedRegistry) map[string][]int32 {
	return builtinTagIDs(builtin, "minecraft:item")
}
