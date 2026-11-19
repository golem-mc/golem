package nbt

import (
	"slices"
	"strings"
	"unicode/utf16"
)

// HashOrderedKeys returns compound keys in Java HashMap bucket order.
// Map literals have no insertion history, so their bucket ties use strings.Compare.
func HashOrderedKeys(compound CompoundTag) []string {
	capacity := 16
	ordered, retainOrder := compound.(*OrderedCompound)
	if retainOrder {
		capacity = max(capacity, ordered.capacity)
	} else {
		for len(compound.Entries()) > capacity-capacity/4 {
			capacity *= 2
		}
	}
	type entry struct {
		key    string
		bucket uint32
	}
	entries := make([]entry, 0, len(compound.Entries()))
	appendKey := func(key string) { entries = append(entries, entry{key, stringHash(key) & uint32(capacity-1)}) }
	if retainOrder {
		for _, key := range ordered.keys {
			appendKey(key)
		}
	} else {
		for key := range compound.Entries() {
			appendKey(key)
		}
	}
	compare := func(a, b entry) int {
		if a.bucket < b.bucket {
			return -1
		}
		if a.bucket > b.bucket {
			return 1
		}
		if !retainOrder {
			return strings.Compare(a.key, b.key)
		}
		return 0
	}
	if retainOrder {
		slices.SortStableFunc(entries, compare)
	} else {
		slices.SortFunc(entries, compare)
	}
	keys := make([]string, len(entries))
	for i, entry := range entries {
		keys[i] = entry.key
	}
	return keys
}

// vanilla: String.hashCode, HashMap.hash
func stringHash(key string) uint32 {
	var hash uint32
	for _, r := range key {
		if r <= 0xffff {
			hash = 31*hash + uint32(r)
		} else {
			hi, lo := utf16.EncodeRune(r)
			hash = 31*hash + uint32(hi)
			hash = 31*hash + uint32(lo)
		}
	}
	return hash ^ (hash >> 16)
}
