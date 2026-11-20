package registry

import (
	"math"
	"reflect"
	"slices"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// ItemPredicate is a decoded item predicate: item set, count range, exact components and
// partial component predicates. A rejected field or entry is dropped alone
// (TagValueInput.read's partial decode), not the whole lock.
// vanilla: ItemPredicate
type ItemPredicate struct {
	hasItems bool
	items    []int32
	tag      string
	min, max int64
	name     *protocol.Text
	// damage is the exact minecraft:damage; customData the exact
	// minecraft:custom_data; durability and wear the partial damage
	// predicate's bounds; dataPartial the partial custom_data predicate.
	damage      *int32
	customData  nbt.CompoundTag
	durability  *[2]int64
	wear        *[2]int64
	dataPartial nbt.CompoundTag
	// exact holds the other exact components by name, tests the other partial
	// predicates this model reads.
	exact map[string]nbt.Tag
	tests []stackTest
}

// DecodeItemPredicate reads a stored predicate (a lock) as ItemPredicate.CODEC does; every
// field is optional, so a rejected one leaves the rest.
// vanilla: ItemPredicate.CODEC
func DecodeItemPredicate(lock nbt.Compound) ItemPredicate {
	pred := ItemPredicate{min: int64(math.MinInt32), max: int64(math.MaxInt32)}
	if items, ok := lock["items"]; ok {
		decodePredicateItems(&pred, items)
	}
	if count, ok := lock["count"]; ok {
		decodePredicateCount(&pred, count)
	}
	if exact, ok := nbt.Get[nbt.Compound](lock, "components"); ok {
		for key, value := range exact {
			switch Qualify(key) {
			case "minecraft:custom_name":
				if name, ok := protocol.StyledTextOf(value); ok {
					pred.name = &name
				}
			case "minecraft:damage":
				if n, ok := nbt.IntValue(value); ok && n >= 0 {
					pred.damage = &n
				}
			case "minecraft:custom_data":
				if c, ok := compoundTagOrSNBT(value); ok {
					pred.customData = c
				}
			default:
				name := Qualify(key)
				if ItemComponents[name] {
					probe := protocol.ItemStack{ItemID: 1, Count: 1}
					if ReadComponent(&probe, name, value) {
						if pred.exact == nil {
							pred.exact = map[string]nbt.Tag{}
						}
						pred.exact[name] = value
					}
				}
			}
		}
	}
	// DataComponentMatchers' partial predicates hold only types this model
	// cannot run, so each known one refuses every stack; an unknown type drops
	// its entry.
	if partial, ok := nbt.Get[nbt.Compound](lock, "predicates"); ok {
		for key, value := range partial {
			switch Qualify(key) {
			case "minecraft:damage":
				if c, ok := value.(nbt.CompoundTag); ok {
					pred.durability = decodePredicateBounds(c.Entries()["durability"])
					pred.wear = decodePredicateBounds(c.Entries()["damage"])
					if pred.durability == nil && pred.wear == nil {
						// An empty predicate still demands a damageable item.
						pred.durability = &[2]int64{math.MinInt32, math.MaxInt32}
					}
				}
			case "minecraft:custom_data":
				if c, ok := compoundTagOrSNBT(value); ok {
					pred.dataPartial = c
				}
			default:
				name := Qualify(key)
				// A type or value that does not read drops its entry.
				if test, ok := decodeComponentPredicate(name, value); ok {
					pred.tests = append(pred.tests, test)
				}
			}
		}
	}
	return pred
}

// decodePredicateItems reads the item holder set: a string is one item or an
// item tag, a list is items only, keeping entries that decode. An undefined
// tag, unknown bare item or other shape drops the field; an emptied list
// matches nothing.
// vanilla: RegistryCodecs.homogeneousList, HolderSetCodec
func decodePredicateItems(pred *ItemPredicate, items nbt.Tag) {
	if name, ok := items.(nbt.String); ok {
		if strings.HasPrefix(string(name), "#") {
			tag := Qualify(string(name)[1:])
			if !ItemTagExists(tag) {
				return
			}
			pred.hasItems, pred.tag = true, tag
			return
		}
		id, ok := ItemID(Qualify(string(name)))
		if !ok {
			return
		}
		pred.hasItems, pred.items = true, []int32{id}
		return
	}
	list, ok := items.(nbt.List)
	if !ok {
		return
	}
	pred.hasItems = true
	for _, entry := range list.Items {
		text, ok := entry.(nbt.String)
		if !ok || strings.HasPrefix(string(text), "#") {
			continue
		}
		if id, ok := ItemID(Qualify(string(text))); ok {
			pred.items = append(pred.items, id)
		}
	}
}

// decodePredicateBounds reads MinMaxBounds.Ints as a number or {min,max}; nil
// for an absent or rejected value, which bounds nothing.
// vanilla: MinMaxBounds.Ints.CODEC
func decodePredicateBounds(tag nbt.Tag) *[2]int64 {
	if tag == nil {
		return nil
	}
	lo, hi, ok := parseBounds(tag)
	if !ok || (lo == math.MinInt32 && hi == math.MaxInt32) {
		return nil
	}
	return &[2]int64{lo, hi}
}

// decodePredicateCount reads the count bounds as a number or {min,max},
// dropping the count when shape or bounds are rejected.
// vanilla: MinMaxBounds.Ints.CODEC
func decodePredicateCount(pred *ItemPredicate, count nbt.Tag) {
	boundsTag, ok := count.(nbt.CompoundTag)
	if !ok {
		n, ok := nbt.LongValue(count)
		if !ok {
			return
		}
		pred.min, pred.max = n, n
		return
	}
	min, max := pred.min, pred.max
	if raw, ok := boundsTag.Entries()["min"]; ok {
		if n, ok := nbt.LongValue(raw); ok {
			min = n
		}
	}
	if raw, ok := boundsTag.Entries()["max"]; ok {
		if n, ok := nbt.LongValue(raw); ok {
			max = n
		}
	}
	if min > max {
		return
	}
	pred.min, pred.max = min, max
}

// Matches reports whether the hand satisfies the predicate.
// vanilla: ItemPredicate.test
func (pred ItemPredicate) Matches(hand protocol.ItemStack) bool {
	if pred.hasItems {
		if pred.tag != "" {
			if !Item(hand.ItemID).InTag(ItemTagOf(pred.tag)) {
				return false
			}
		} else if !slices.Contains(pred.items, hand.ItemID) {
			return false
		}
	}
	if int64(hand.Count) < pred.min || int64(hand.Count) > pred.max {
		return false
	}
	if pred.name != nil && (hand.CustomName == nil ||
		!reflect.DeepEqual(protocol.TextTag(*pred.name), protocol.TextTag(*hand.CustomName))) {
		return false
	}
	if pred.damage != nil && (StackMaxDamage(hand) <= 0 || hand.Damage != *pred.damage) {
		return false
	}
	for name, value := range pred.exact {
		if !exactComponentMatches(hand, name, value) {
			return false
		}
	}
	for _, test := range pred.tests {
		if !test(hand) {
			return false
		}
	}
	data := hand.CustomDataTag()
	if pred.customData != nil && (data == nil || !nbt.Equal(pred.customData, data)) {
		return false
	}
	if pred.dataPartial != nil {
		// vanilla: CustomDataPredicate.Matches, getOrDefault(CUSTOM_DATA, EMPTY)
		if data == nil {
			data = nbt.NewOrderedCompound(0)
		}
		if !nbt.Matches(pred.dataPartial, data) {
			return false
		}
	}
	if pred.durability != nil || pred.wear != nil {
		maxDamage := StackMaxDamage(hand)
		in := func(b *[2]int64, v int64) bool { return b == nil || (v >= b[0] && v <= b[1]) }
		if maxDamage <= 0 || !in(pred.durability, int64(maxDamage-hand.Damage)) || !in(pred.wear, int64(hand.Damage)) {
			return false
		}
	}
	return true
}
