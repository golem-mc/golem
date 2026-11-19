package registry

import "github.com/golem-mc/golem/nbt"

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
