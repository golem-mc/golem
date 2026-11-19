package registry

import (
	"slices"
	"sort"
)

// BlockTagID names a block tag by its position in the tag list, the index
// into each block's membership bitset. Built-in tags keep the position they
// have in the sorted list of built-in names whatever a data pack adds, so an
// ID resolved once stays valid across a reload. The zero value is a real tag;
// noBlockTag marks a name the tag registry does not hold.
type BlockTagID uint16

const noBlockTag BlockTagID = 0xffff

// keyOnlyBlockTags are BlockTags keys vanilla's code tests but its data
// pack does not define, so they have no members until a data pack adds some.
// vanilla: BlockTags.CONSUMABLE_DOES_NOT_TELEPORT_TO
var keyOnlyBlockTags = []string{"minecraft:consumable_does_not_teleport_to"}

// baseBlockTagNames is the sorted list of built-in block tag names, which
// fixes the IDs of those tags.
var baseBlockTagNames = func() []string {
	names := make([]string, 0, len(BlockTags)+len(keyOnlyBlockTags))
	for tag := range BlockTags {
		names = append(names, tag)
	}
	names = append(names, keyOnlyBlockTags...)
	sort.Strings(names)
	return names
}()

// newBlockTagTable builds the bitsets for tags: the built-in names first, in
// their fixed order, then any other tag sorted. A built-in tag that is absent
// from tags keeps its ID with no members.
func newBlockTagTable(tags map[string][]string) BlockTagLookup {
	names := append([]string(nil), baseBlockTagNames...)
	var extra []string
	for tag := range tags {
		if _, builtin := BlockTags[tag]; !builtin && !slices.Contains(keyOnlyBlockTags, tag) {
			extra = append(extra, tag)
		}
	}
	sort.Strings(extra)
	names = append(names, extra...)
	t := BlockTagLookup{ids: make(map[string]BlockTagID, len(names)), words: (len(names) + 63) / 64, members: make([][]Block, len(names))}
	t.bits = make([]uint64, t.words*BlockCount)
	for i, tag := range names {
		id := BlockTagID(i)
		t.ids[tag] = id
		for _, member := range tags[tag] {
			if b, ok := blockByName[member]; ok {
				t.bits[int(b)*t.words+int(id)/64] |= 1 << (id % 64)
				t.members[i] = append(t.members[i], b)
			}
		}
	}
	return t
}

// BlockTagLookup is an immutable snapshot of block tag membership.
type BlockTagLookup struct {
	ids     map[string]BlockTagID
	words   int       // bitset width per block
	bits    []uint64  // words per block, indexed by Block
	members [][]Block // tag encounter order, including repeated members
}

// BlockTagOf returns the ID of the block tag named, such as "minecraft:logs".
// An unknown tag has no members, like state.is(TagKey) on a tag no data pack
// defines.
func BlockTagOf(tag string) BlockTagID {
	if id, ok := tagsNow.Load().blockTable.ids[tag]; ok {
		return id
	}
	return noBlockTag
}

// CurrentBlockTags returns the current snapshot, replaced on each tag reload.
func CurrentBlockTags() *BlockTagLookup {
	return &tagsNow.Load().blockTable
}

// InTag reports whether the block belongs to the tag in this snapshot.
func (tbl *BlockTagLookup) InTag(b Block, t BlockTagID) bool {
	if t == noBlockTag || b >= BlockCount || int(t)/64 >= tbl.words {
		return false
	}
	return tbl.bits[int(b)*tbl.words+int(t)/64]&(1<<(t%64)) != 0
}

// InTag reports whether the block is in the tag, vanilla's BlockState.is(TagKey).
func (b Block) InTag(t BlockTagID) bool {
	return CurrentBlockTags().InTag(b, t)
}

// NameInTag is InTag for a registry name; a name that is no block is in no tag.
//
// Deprecated: resolve the name once with BlockByName and use Block.InTag.
func NameInTag(name string, t BlockTagID) bool {
	b, ok := blockByName[name]
	return ok && b.InTag(t)
}

// BlockTagMembers returns the names of the blocks a block tag groups, after
// the data packs, or false for a tag nothing defines.
func BlockTagMembers(tag string) ([]string, bool) {
	m, ok := tagsNow.Load().blockTags[tag]
	return m, ok
}

// BlockTagNames returns every block tag name, sorted.
func BlockTagNames() []string {
	tags := tagsNow.Load().blockTags
	names := make([]string, 0, len(tags))
	for tag := range tags {
		names = append(names, tag)
	}
	sort.Strings(names)
	return names
}

// Blocks returns the tag's members in encounter order; the returned slice is immutable.
func (tag BlockTagID) Blocks() []Block {
	tbl := CurrentBlockTags()
	if int(tag) >= len(tbl.members) {
		return nil
	}
	return tbl.members[tag]
}
