package registry

import (
	"slices"
	"sync"
)

// DamageTypeTagID indexes a damage type's tag membership bitset. IDs stay
// valid across reloads, including pack-only tags.
type DamageTypeTagID uint16

const noDamageTypeTag DamageTypeTagID = 0xffff

var damageTagNames = append([]string(nil), baseDamageTypeTagNames...)
var damageTagMu sync.Mutex

type damageTypeTagLookup struct {
	defined      map[string]bool
	words, count int
	bits         []uint64
}

func newDamageTypeTagTable(tags map[string]map[string]bool) damageTypeTagLookup {
	members := make(map[string][]int32, len(tags))
	defined := make(map[string]bool, len(tags))
	for name, entries := range tags {
		members[name] = nil
		defined[name] = true
		for entry := range entries {
			if d, ok := DamageTypeByName(entry); ok {
				members[name] = append(members[name], int32(d))
			}
		}
	}
	count := len(damageTypesNow.Load().names)
	damageTagMu.Lock()
	defer damageTagMu.Unlock()
	var extra []string
	for name := range tags {
		if !slices.Contains(damageTagNames, name) {
			extra = append(extra, name)
		}
	}
	slices.Sort(extra)
	for _, name := range extra {
		internDamageTag(name)
	}
	_, words, bits := newTagBits(damageTagNames, members, count)
	return damageTypeTagLookup{defined: defined, words: words, count: count, bits: bits}
}

// DamageTypeTagOf binds a tag name at data and component decoding boundaries.
// An undefined tag holds nothing until a reload defines it.
func DamageTypeTagOf(name string) DamageTypeTagID {
	damageTagMu.Lock()
	defer damageTagMu.Unlock()
	return internDamageTag(name)
}

func internDamageTag(name string) DamageTypeTagID {
	if i := slices.Index(damageTagNames, name); i >= 0 {
		return DamageTypeTagID(i)
	}
	if len(damageTagNames) >= int(noDamageTypeTag) {
		return noDamageTypeTag
	}
	id := DamageTypeTagID(len(damageTagNames))
	damageTagNames = append(damageTagNames, name)
	return id
}

// InTag reports whether the damage type belongs to the tag.
// vanilla: DamageSource.is, Holder.Reference.is
func (d DamageType) InTag(tag DamageTypeTagID) bool {
	tbl := &tagsNow.Load().damageTable
	return d > 0 && int(d) < tbl.count && tag != noDamageTypeTag && tagBitSet(tbl.bits, tbl.words, int(d), uint16(tag))
}
