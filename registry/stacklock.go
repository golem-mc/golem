package registry

import (
	"strings"

	"github.com/golem-mc/golem/nbt"
)

// LockTag writes the shared item predicate codec used by stacks and containers.
// Unchanged children are borrowed, as in StackTag; rebuilt records own their roots.
// vanilla: LockCode.CODEC, ItemPredicate.CODEC, DataComponentMatchers.CODEC
func LockTag(value nbt.CompoundTag) *nbt.OrderedCompound {
	if value == nil {
		return nil
	}
	out := nbt.NewOrderedCompound(4)
	for _, key := range []string{"items", "count", "components", "predicates"} {
		if field, ok := value.Entries()[key]; ok {
			if key == "items" {
				field = qualifiedHolderSet(field)
			}
			out.Set(key, field)
		}
	}
	for _, key := range []string{"components", "predicates"} {
		entries, ok := value.Entries()[key].(nbt.CompoundTag)
		if !ok || len(entries.Entries()) == 0 {
			out.Delete(key)
			continue
		}
		copy := nbt.NewOrderedCompound(len(entries.Entries()))
		for _, name := range nbt.HashOrderedKeys(entries) {
			field := entries.Entries()[name]
			if Qualify(name) == "minecraft:custom_data" {
				if data, ok := compoundTagOrSNBT(field); ok {
					if key == "predicates" {
						// vanilla: NbtPredicate.CODEC, TagParser.FLATTENED_CODEC
						field = nbt.String(nbt.Stringify(data))
					} else {
						field = nbt.OrderedCopy(data)
					}
				}
			}
			copy.Set(Qualify(name), field)
		}
		out.Set(key, copy)
	}
	return out
}

// qualifiedHolderSet namespaces an item holder set: an id, a #tag or a list of
// ids; anything else is kept.
// vanilla: RegistryCodecs.homogeneousList
func qualifiedHolderSet(tag nbt.Tag) nbt.Tag {
	switch v := tag.(type) {
	case nbt.String:
		if name, isTag := strings.CutPrefix(string(v), "#"); isTag {
			return nbt.String("#" + Qualify(name))
		}
		return nbt.String(Qualify(string(v)))
	case nbt.List:
		if v.Elem != nbt.TypeString {
			return tag
		}
		// vanilla: ExtraCodecs.compactListCodec writes a one-element list as the element.
		if len(v.Items) == 1 {
			return qualifiedHolderSet(v.Items[0])
		}
		items := make([]nbt.Tag, len(v.Items))
		for i, item := range v.Items {
			items[i] = qualifiedHolderSet(item)
		}
		v.Items = items
		return v
	}
	return tag
}
