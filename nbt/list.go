package nbt

import "slices"

// NewList builds a homogeneous list, copying the input slice only when mixed
// entries need compound wrappers. Entries must be non-nil.
// vanilla: ListTag.add
func NewList(items []Tag) List {
	return normalizedList(items, true)
}

// ListOf builds a list holding items as they are, a mixed list's entries
// unwrapped as the SNBT reader holds them; the encoder wraps them on write.
// vanilla: ListTag.addTag
func ListOf(items []Tag) List {
	if len(items) == 0 {
		return List{Elem: TypeEnd}
	}
	return List{Elem: listElem(items, items[0].Type()), Items: items}
}

// wrapIfNeeded wraps a mixed list's entry under the empty name unless it is
// a compound that does not itself look like a wrapper.
// vanilla: ListTag.wrapIfNeeded
func wrapIfNeeded(item Tag) CompoundTag {
	if compound, ok := unwrappedCompound(item); ok {
		return compound
	}
	return Compound{"": item}
}

func unwrappedCompound(item Tag) (CompoundTag, bool) {
	if compound, ok := item.(CompoundTag); ok {
		_, wrapper := unwrap(item)
		return compound, !wrapper
	}
	return nil, false
}

// normalizedList wraps parser-owned slices in place; other callers keep their aliases.
func normalizedList(items []Tag, copyMixed bool) List {
	if len(items) == 0 {
		return List{Elem: TypeEnd}
	}
	if heterogeneous(items) {
		if copyMixed {
			items = slices.Clone(items)
		}
		for i, item := range items {
			items[i] = wrapIfNeeded(item)
		}
		return List{Elem: TypeCompound, Items: items}
	}
	return List{Elem: items[0].Type(), Items: items}
}
