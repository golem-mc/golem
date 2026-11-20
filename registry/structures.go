package registry

import (
	"slices"
	"sync/atomic"
)

// The structure registry and its tags (data/minecraft/tags/worldgen/structure
// in the server jar). The client holds no copy, so the names never travel.

// packStructures holds the ids of the structures the data packs add to the
// registry.
var packStructures atomic.Pointer[[]string]

// ApplyPackStructures sets the structures the data packs add; nil clears them.
func ApplyPackStructures(ids []string) {
	if len(ids) == 0 {
		packStructures.Store(nil)
		return
	}
	packStructures.Store(&ids)
}

// StructureExists reports whether the structure registry holds the id,
// whether built in or from a data pack.
func StructureExists(id string) bool {
	if slices.Contains(structureIDs, id) {
		return true
	}
	p := packStructures.Load()
	return p != nil && slices.Contains(*p, id)
}

// StructureTagMembers returns the structures a tag holds, nested tags
// expanded, and false for a tag that does not exist.
func StructureTagMembers(tag string) ([]string, bool) {
	members, ok := structureTags[tag]
	if !ok {
		return nil, false
	}
	var out []string
	for _, m := range members {
		if inner, isTag := cutHash(m); isTag {
			sub, _ := StructureTagMembers(inner)
			out = append(out, sub...)
		} else {
			out = append(out, m)
		}
	}
	return out, true
}

func cutHash(s string) (string, bool) {
	if len(s) > 0 && s[0] == '#' {
		return s[1:], true
	}
	return s, false
}

// PointOfInterestTypeExists reports whether the point of interest registry
// holds the id.
func PointOfInterestTypeExists(id string) bool { return slices.Contains(pointOfInterestTypes, id) }

// PointOfInterestTagExists reports whether the point of interest registry
// has a tag of the name.
func PointOfInterestTagExists(tag string) bool {
	_, ok := PointOfInterestTagMembers(tag)
	return ok
}

// PointOfInterestTagMembers returns the point-of-interest types a tag holds,
// and false for a tag that does not exist.
func PointOfInterestTagMembers(tag string) ([]string, bool) {
	for _, r := range BuiltinTags {
		if r.Registry != "minecraft:point_of_interest_type" {
			continue
		}
		for _, t := range r.Tags {
			if t.Name != tag {
				continue
			}
			out := make([]string, 0, len(t.IDs))
			for _, id := range t.IDs {
				out = append(out, pointOfInterestTypes[id])
			}
			return out, true
		}
	}
	return nil, false
}
