package registry

import (
	"slices"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// stackTest is one decoded test of a stack's component.
type stackTest func(protocol.ItemStack) bool

// collectionTest is CollectionPredicate over elements of type T: every
// "contains" test needs an element, every "count" entry a count in range, and
// the element count must lie in "size".
// vanilla: CollectionPredicate.test, CollectionContentsPredicate, CollectionCountsPredicate
type collectionTest[T any] struct {
	contains []func(T) bool
	counts   []struct {
		test     func(T) bool
		min, max int64
	}
	size *[2]int64
}

func (c collectionTest[T]) test(values []T) bool {
	for _, want := range c.contains {
		if !slices.ContainsFunc(values, want) {
			return false
		}
	}
	for _, entry := range c.counts {
		n := int64(0)
		for _, v := range values {
			if entry.test(v) {
				n++
			}
		}
		if n < entry.min || n > entry.max {
			return false
		}
	}
	return c.size == nil || (int64(len(values)) >= c.size[0] && int64(len(values)) <= c.size[1])
}

// decodeCollection reads CollectionPredicate.codec; any malformed field fails
// the whole record, as a record codec does.
// vanilla: CollectionPredicate.codec
func decodeCollection[T any](tag nbt.Tag, elem func(nbt.Tag) (func(T) bool, bool)) (collectionTest[T], bool) {
	var out collectionTest[T]
	fields, ok := nbt.CompoundView(tag)
	if !ok {
		return out, false
	}
	if raw, ok := fields["contains"]; ok {
		list, ok := raw.(nbt.List)
		if !ok {
			return out, false
		}
		for _, item := range list.Items {
			test, ok := elem(item)
			if !ok {
				return out, false
			}
			out.contains = append(out.contains, test)
		}
	}
	if raw, ok := fields["count"]; ok {
		list, ok := raw.(nbt.List)
		if !ok {
			return out, false
		}
		for _, item := range list.Items {
			entry, ok := nbt.CompoundView(item)
			if !ok {
				return out, false
			}
			test, ok := elem(entry["test"])
			if !ok {
				return out, false
			}
			lo, hi, ok := parseBounds(entry["count"])
			if !ok {
				return out, false
			}
			out.counts = append(out.counts, struct {
				test     func(T) bool
				min, max int64
			}{test, lo, hi})
		}
	}
	if raw, ok := fields["size"]; ok {
		lo, hi, ok := parseBounds(raw)
		if !ok {
			return out, false
		}
		out.size = &[2]int64{lo, hi}
	}
	return out, true
}

// parseBounds reads MinMaxBounds.Ints strictly: a number, or {min,max} with
// min <= max; absent bounds are unbounded.
// vanilla: MinMaxBounds.Ints.CODEC
func parseBounds(tag nbt.Tag) (lo, hi int64, ok bool) {
	lo, hi = -1<<31, 1<<31-1
	if tag == nil {
		return 0, 0, false
	}
	fields, isCompound := nbt.CompoundView(tag)
	if !isCompound {
		n, ok := nbt.IntValue(tag)
		return int64(n), int64(n), ok
	}
	if raw, ok := fields["min"]; ok {
		n, ok := nbt.IntValue(raw)
		if !ok {
			return 0, 0, false
		}
		lo = int64(n)
	}
	if raw, ok := fields["max"]; ok {
		n, ok := nbt.IntValue(raw)
		if !ok {
			return 0, 0, false
		}
		hi = int64(n)
	}
	return lo, hi, lo <= hi
}

// optionalBounds is parseBounds for a field that may be absent, in which case
// there is no bound.
func optionalBounds(fields nbt.Compound, key string) (lo, hi int64, ok bool) {
	if _, present := fields[key]; !present {
		return -1 << 31, 1<<31 - 1, true
	}
	return parseBounds(fields[key])
}

// DecodeComponentPredicate reads one entry of DataComponentMatchers' partial
// predicates. typed reports whether vanilla tests the component's '~' value
// with a predicate type of its own rather than by presence; ok is false when
// the value cannot be read (always, for a typed component this package does
// not model).
// vanilla: DataComponentPredicates
func DecodeComponentPredicate(name string, value nbt.Tag) (test func(protocol.ItemStack) bool, typed, ok bool) {
	switch name {
	case "minecraft:enchantments", "minecraft:stored_enchantments":
		test, ok = decodeEnchantmentsTest(name == "minecraft:stored_enchantments", value)
		return test, true, ok
	case "minecraft:firework_explosion", "minecraft:fireworks", "minecraft:trim", "minecraft:jukebox_playable",
		"minecraft:villager/variant", "minecraft:writable_book_content", "minecraft:written_book_content",
		"minecraft:attribute_modifiers", "minecraft:potion_contents":
		test, ok = decodeMorePredicate(name, value)
		return test, true, ok
	case "minecraft:container", "minecraft:bundle_contents":
		fields, isCompound := nbt.CompoundView(value)
		if !isCompound {
			return nil, true, false
		}
		contents := func(hand protocol.ItemStack) ([]protocol.ItemStack, bool) {
			if name == "minecraft:container" {
				var items []protocol.ItemStack
				for _, s := range hand.Container {
					if !s.Empty() {
						items = append(items, s)
					}
				}
				return items, hand.Container != nil || (!hand.Removes(protocol.DataComponentContainer) && ItemHasComponentDefault(hand.ItemID, name))
			}
			items := hand.BundleContents()
			return items, items != nil || (!hand.Removes(protocol.DataComponentBundleContents) && ItemHasComponentDefault(hand.ItemID, name))
		}
		var coll *collectionTest[protocol.ItemStack]
		if raw, ok := fields["items"]; ok {
			c, ok := decodeCollection(raw, func(t nbt.Tag) (func(protocol.ItemStack) bool, bool) {
				entry, ok := nbt.CompoundView(t)
				if !ok {
					return nil, false
				}
				return DecodeItemPredicate(entry).Matches, true
			})
			if !ok {
				return nil, true, false
			}
			coll = &c
		}
		return func(hand protocol.ItemStack) bool {
			items, present := contents(hand)
			return present && (coll == nil || coll.test(items))
		}, true, true
	}
	return nil, false, false
}

// decodeEnchantmentsTest reads the list of EnchantmentPredicate: each needs a
// listed enchantment (any when unlisted) at a level in range.
// vanilla: EnchantmentsPredicate.codec, EnchantmentPredicate.containedIn
func decodeEnchantmentsTest(stored bool, value nbt.Tag) (stackTest, bool) {
	list, ok := value.(nbt.List)
	if !ok {
		return nil, false
	}
	type entry struct {
		ids      []int32
		listed   bool
		min, max int64
		any      bool
	}
	var entries []entry
	for _, item := range list.Items {
		fields, ok := nbt.CompoundView(item)
		if !ok {
			return nil, false
		}
		e := entry{}
		var ok2 bool
		if e.min, e.max, ok2 = optionalBounds(fields, "levels"); !ok2 {
			return nil, false
		}
		e.any = e.min == -1<<31 && e.max == 1<<31-1
		if raw, ok := fields["enchantments"]; ok {
			if e.ids, ok = decodeEnchantmentSet(raw); !ok {
				return nil, false
			}
			e.listed = true
		}
		entries = append(entries, e)
	}
	return func(hand protocol.ItemStack) bool {
		have := hand.Enchantments
		if stored {
			have = hand.StoredEnchantments
			if have == nil && (hand.Removes(protocol.DataComponentStoredEnchantments) || !ItemHasComponentDefault(hand.ItemID, "minecraft:stored_enchantments")) {
				return false
			}
		}
		for _, e := range entries {
			found := false
			switch {
			case e.listed:
				for _, id := range e.ids {
					if level := int64(have[id]); level != 0 && (e.any || (level >= e.min && level <= e.max)) {
						found = true
						break
					}
				}
			case !e.any:
				for _, level := range have {
					if int64(level) >= e.min && int64(level) <= e.max {
						found = true
						break
					}
				}
			default:
				found = len(have) > 0
			}
			if !found {
				return false
			}
		}
		return true
	}, true
}

// decodeEnchantmentSet reads an enchantment holder set: one name, a #tag or a
// list of names.
// vanilla: RegistryCodecs.homogeneousList
func decodeEnchantmentSet(tag nbt.Tag) ([]int32, bool) {
	one := func(s string) ([]int32, bool) {
		if tagName, isTag := strings.CutPrefix(s, "#"); isTag {
			tagName = Qualify(tagName)
			if _, ok := EnchantmentTagMembers(tagName); !ok {
				return nil, false
			}
			return EnchantmentTagIDs(tagName), true
		}
		id, ok := EnchantmentID(Qualify(s))
		return []int32{id}, ok
	}
	if s, ok := tag.(nbt.String); ok {
		return one(string(s))
	}
	list, ok := tag.(nbt.List)
	if !ok {
		return nil, false
	}
	ids := []int32{}
	for _, item := range list.Items {
		if s, ok := item.(nbt.String); ok && !strings.HasPrefix(string(s), "#") {
			if got, ok := one(string(s)); ok {
				ids = append(ids, got...)
			}
		}
	}
	return ids, true
}

// exactComponentMatches compares one exact component with the stack's: the
// value read onto a bare stack of the hand's item must be the one the hand's
// patch holds, or both must hold the item's default.
// vanilla: DataComponentExactPredicate.test
func exactComponentMatches(hand protocol.ItemStack, name string, want nbt.Tag) bool {
	probe := protocol.ItemStack{ItemID: hand.ItemID, Count: 1}
	if !ReadComponent(&probe, name, want) {
		return true
	}
	wanted, ok := StackComponents(probe)[name]
	have, held := StackComponents(hand)[name]
	if name == "minecraft:lore" && ok {
		// Every item's lore defaults to empty, which a patch of no lines equals.
		_, removed := StackComponents(hand)["!"+name]
		wantedList, _ := wanted.(nbt.List)
		haveList, _ := have.(nbt.List)
		if len(wantedList.Items) == 0 {
			return !removed && len(haveList.Items) == 0
		}
	}
	if ok {
		return held && nbt.Equal(wanted, have)
	}
	_, removed := StackComponents(hand)["!"+name]
	return !held && !removed && ItemHasComponentDefault(hand.ItemID, name)
}
