package registry

import "sort"

// newTagBits numbers the tags and sets one bit per (member, tag) pair, words
// uint64s per member. Built-in names come first in the given order, then the
// pack-only names sorted; members outside [0, count) are dropped.
func newTagBits(builtinNames []string, tags map[string][]int32, count int) (ids map[string]uint16, words int, bits []uint64) {
	names := append([]string(nil), builtinNames...)
	builtin := make(map[string]bool, len(names))
	for _, name := range names {
		builtin[name] = true
	}
	var extra []string
	for name := range tags {
		if !builtin[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	names = append(names, extra...)
	ids = make(map[string]uint16, len(names))
	words = (len(names) + 63) / 64
	bits = make([]uint64, words*count)
	for i, name := range names {
		id := uint16(i)
		ids[name] = id
		for _, member := range tags[name] {
			if member >= 0 && int(member) < count {
				bits[int(member)*words+int(id)/64] |= 1 << (id % 64)
			}
		}
	}
	return ids, words, bits
}

// tagBitSet reads a bit newTagBits set; a tag past the table holds nothing.
func tagBitSet(bits []uint64, words, member int, tag uint16) bool {
	if int(tag)/64 >= words {
		return false
	}
	return bits[member*words+int(tag)/64]&(1<<(tag%64)) != 0
}

// builtinTagNames returns the names of a built-in registry's tags, sorted.
func builtinTagNames(registry string) []string {
	var names []string
	for _, r := range BuiltinTags {
		if r.Registry == registry {
			for _, t := range r.Tags {
				names = append(names, t.Name)
			}
		}
	}
	sort.Strings(names)
	return names
}

// builtinTagIDs collects the numeric members of a built-in registry's tags.
func builtinTagIDs(builtin []BuiltinTaggedRegistry, registry string) map[string][]int32 {
	tags := make(map[string][]int32)
	for _, r := range builtin {
		if r.Registry == registry {
			for _, t := range r.Tags {
				tags[t.Name] = t.IDs
			}
		}
	}
	return tags
}
