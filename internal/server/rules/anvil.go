package rules

// The anvil's maths: what its result slot shows and what taking it costs, a
// pure function of the two inputs, the typed name and whether the player may
// conjure, so recomputing is a function call rather than a process.
// vanilla: AnvilMenu.createResult, setItemName, onTake,
// EnchantmentHelper.getComponentType, ItemStack.calculateIncreasedRepairCost
//
// Deliberate difference: an item's default name is a client-side
// translation the server never resolves, so a rename spelling it out is
// priced like any other rename, where vanilla prices it at nothing.

import (
	"maps"
	"strings"
	"unicode"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// AnvilAnswer is what one run of the anvil's maths decided: the stack the
// result slot shows, the levels taking it costs, how many of the second
// input the taking consumes and whether the taking was a rename alone.
type AnvilAnswer struct {
	// Result is the stack the result slot holds; empty when there is
	// nothing to take.
	Result protocol.ItemStack
	// Cost is the levels taking the result spends: zero for nothing to
	// take, the price the client garnishes its Too Expensive! with
	// beyond thirty-nine even where the result vanished.
	Cost int32
	// Consumed is how many of the second input the taking uses up - the
	// number of repair-material items a worn item's mending needed. Any
	// other combine that is not a rename takes the whole stack.
	Consumed int32
	// OnlyRenaming says the combine was a rename alone: the second input
	// then leaves whole whatever it held, and the stack's repair cost is
	// left unraised - a rename asks no prior work of the item.
	OnlyRenaming bool
}

// isEnchantedBook reports the enchanted book, whose enchantments live in the
// stored component; it decides where a combine reads and writes magic.
// vanilla: EnchantmentHelper.getComponentType
func isEnchantedBook(stack protocol.ItemStack) bool {
	return registry.Item(stack.ItemID) == registry.ItemEnchantedBook
}

// anvilUsingBook reports a second input that carries the stored-enchantments
// component, which an enchanted book does by default even with nothing
// stored; it lets a combine go ahead off a different item and halves the fee.
// vanilla: AnvilMenu.createResult (usingBook), Items.ENCHANTED_BOOK
func anvilUsingBook(extra protocol.ItemStack) bool {
	if isEnchantedBook(extra) {
		return !extra.Removes(protocol.DataComponentStoredEnchantments)
	}
	return extra.StoredEnchantments != nil
}

// anvilEnchantmentsOf is EnchantmentHelper.getEnchantmentsForCrafting: the
// enchantments a combine reads a stack's magic from. An enchanted book
// keeps its levels in the stored component, everything else in its own.
func anvilEnchantmentsOf(stack protocol.ItemStack) map[int32]int32 {
	return *enchantmentComponent(&stack)
}

// enchantmentComponent is the component a stack's kind keeps its levels
// in, pointed at so a strip or a merge can write it.
func enchantmentComponent(stack *protocol.ItemStack) *map[int32]int32 {
	if isEnchantedBook(*stack) {
		return &stack.StoredEnchantments
	}
	return &stack.Enchantments
}

// anvilCanEnchant is Enchantment.canEnchant's supported-items half: the
// supported tag holds the item. No tag accepts everything, like vanilla's
// empty holder.
func anvilCanEnchant(def *registry.EnchantmentDef, itemID int32) bool {
	return def.Supports(registry.Item(itemID))
}

// anvilCompatible is Enchantment.areCompatible for two enchantments: the
// same enchantment never combines, and neither may sit in the other's
// exclusive-set tag.
func anvilCompatible(a, b int32) bool {
	return enchantmentCompatible(EnchOffer{Enchantment: a}, EnchOffer{Enchantment: b})
}

// IncreasedRepairCost is the prior-work cost one more combine raises a stack
// to: doubled plus one, saturating at the int32 maximum.
// vanilla: AnvilMenu.calculateIncreasedRepairCost
func IncreasedRepairCost(cost int32) int32 {
	return clampInt32(int64(cost)*2 + 1)
}

// FilterText keeps the characters chat allows (no section sign, nothing
// below a space, no DEL) and also drops newlines, which the anvil keeps out
// though chat's filter does not.
// vanilla: StringUtil.filterText via AnvilMenu.validateName
func FilterText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, c := range s {
		if c != 0xA7 && c >= 32 && c != 127 {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// AnvilNameLimit is the longest name the anvil accepts, in Java string
// length (UTF-16 code units, so astral characters count double); longer
// is refused whole.
// vanilla: AnvilMenu.validateName
const AnvilNameLimit = 50

// AnvilValidateName filters the typed name and keeps it within the limit,
// refusing a longer one whole. The filtered name is what the window stores
// and compares against the item's name.
// vanilla: AnvilMenu.validateName
func AnvilValidateName(name string) (string, bool) {
	filtered := FilterText(name)
	if javaStringLength(filtered) > AnvilNameLimit {
		return "", false
	}
	return filtered, true
}

// javaStringLength is s's length in UTF-16 code units, the length the
// vanilla limit checks.
func javaStringLength(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r > 0xFFFF {
			n++
		}
	}
	return n
}

// AnvilCombine is the result slot for base (first slot), extra (second) and
// the filtered name (empty for none), with Cost (the window's level property)
// and what the take consumes.
// vanilla: AnvilMenu.createResult
//
// The result's enchantments are the base's plus what of the extra's may
// join: a book hands over its stored levels, a second copy of the item levels
// shared ones up, a repair material hands over none, and anything the base's
// supported tags refuse costs a level and goes nowhere. The extra's magic is walked in registry ID order, since
// vanilla's map order is unspecified.
func AnvilCombine(base, extra protocol.ItemStack, name string, creative bool) AnvilAnswer {
	if base.Empty() {
		return AnvilAnswer{}
	}
	// work counts a level per change beyond the first (a mended quarter, a
	// raised enchantment, a name put on or off); the worth sums both
	// inputs' prior work plus work, narrowed to int32 as it travels.
	var work int32
	sum := int64(base.RepairCost) + int64(extra.RepairCost)
	repairItemCountCost := int32(0)
	namingCost := int32(0)
	usingBook := anvilUsingBook(extra)

	copyOf := base
	copyOf.Enchantments = cloneEnchantments(base.Enchantments)
	copyOf.StoredEnchantments = cloneEnchantments(base.StoredEnchantments)

	// vanilla: ItemStack.isDamageableItem - an unbreakable stack has no
	// wear to mend, so the repair and durability merge treat it as undamageable.
	maxDamage := registry.StackMaxDamage(base)
	if base.Unbreakable {
		maxDamage = 0
	}
	merged := cloneEnchantments(anvilEnchantmentsOf(base))

	if !extra.Empty() {
		// A repair material mends a quarter of the max durability per level
		// until the wear or the material runs out; a material that mends
		// nothing makes nothing to take.
		if maxDamage > 0 && registry.StackRepairs(base, extra.ItemID) {
			k := min(base.Damage, maxDamage/4)
			if k <= 0 {
				return AnvilAnswer{}
			}
			m := int32(0)
			for k > 0 && m < extra.Count {
				copyOf.Damage -= k
				work++
				k = min(copyOf.Damage, maxDamage/4)
				m++
			}
			repairItemCountCost = m
		} else {
			if !usingBook && (!sameItem(base, extra) || maxDamage <= 0) {
				// Not a mending and not the same item: nothing the anvil
				// can make of the pair, unless the extra is a book, whose
				// levels are the point of it.
				return AnvilAnswer{}
			}
			if maxDamage > 0 && !usingBook {
				// Two of a kind: the remaining durability is the sum plus a twelfth
				// of the max, and costs more for it.
				j2 := maxDamage - base.Damage
				k2 := maxDamage - extra.Damage
				restored := j2 + k2 + maxDamage*12/100
				k3 := max(maxDamage-restored, 0)
				if k3 < copyOf.Damage {
					copyOf.Damage = k3
					work += 2
				}
			}

			// The extra's enchantments, low registry ID first. One the base
			// cannot take (unsupported for a survival player, or exclusive with
			// one already on) stays off; each clash costs a level, supported or
			// not; each that joins, clamped to its max level, costs its anvil
			// worth, halved off a book.
			extraEnch := anvilEnchantmentsOf(extra)
			if len(extraEnch) > 0 && merged == nil {
				merged = make(map[int32]int32, len(extraEnch))
			}
			var applied, rejected bool
			for _, id := range sortedEnchantmentIDs(extraEnch) {
				level := extraEnch[id]
				mergedLevel := level
				if own, ok := merged[id]; ok {
					if own == level {
						mergedLevel = level + 1
					} else {
						mergedLevel = max(own, level)
					}
				}
				def, known := registry.EnchantmentEffectsByID(id)
				canEnchant := known
				if canEnchant && !creative && !isEnchantedBook(base) && !anvilCanEnchant(def, base.ItemID) {
					canEnchant = false
				}
				// vanilla: AnvilMenu.createResult - the clash charge applies
				// even to an enchantment the item does not support.
				for other := range merged {
					if other != id && !anvilCompatible(id, other) {
						canEnchant = false
						work++
					}
				}
				if !canEnchant {
					rejected = true
					continue
				}
				applied = true
				if known && mergedLevel > def.MaxLevel {
					mergedLevel = def.MaxLevel
				}
				merged[id] = mergedLevel
				j1 := int32(1)
				if known {
					j1 = def.AnvilCost
					if usingBook {
						j1 = max(j1/2, 1)
					}
				}
				work += j1 * mergedLevel
				if base.Count > 1 {
					// A combine that would hand two of a kind's enchantments over is
					// priced out: worth 40, untakeable in survival.
					work = 40
				}
			}
			if rejected && !applied {
				return AnvilAnswer{}
			}
		}
	}

	// A blank name removes a custom name, a new one adds it: a level each,
	// only when it differs from the item's own name (see the file comment
	// on default names).
	if name == "" || strings.IndexFunc(name, func(r rune) bool { return !isStringWhitespace(r) }) < 0 {
		if base.CustomName != nil {
			namingCost = 1
			work++
			copyOf.CustomName = nil
		}
	} else if !sameHoverName(base, name) {
		namingCost = 1
		work++
		text := protocol.Text{Text: name}
		copyOf.CustomName = &text
	}
	cost := int32(0)
	if work > 0 {
		cost = clampInt32(sum + int64(work))
	}
	// A rename alone is capped at 39, as vanilla lowers a too-dear rename
	// when it was the only work. A rename beside other work is neither
	// capped nor kept.
	onlyRenaming := namingCost == work && namingCost > 0
	if onlyRenaming && cost >= 40 {
		cost = 39
	}
	// No work is nothing to take; at 40 or more survival cannot take it
	// either, the result vanishing and the worth staying (the client's Too
	// Expensive!). Creative has no price limit.
	if work <= 0 || (cost >= 40 && !creative) {
		return AnvilAnswer{Cost: cost}
	}
	// The repair cost rises by calculateIncreasedRepairCost unless the
	// rename was the only work: vanilla skips it while the naming cost
	// equals a non-zero work count, which only a lone rename produces.
	if !onlyRenaming {
		k1 := max(copyOf.RepairCost, extra.RepairCost)
		copyOf.RepairCost = IncreasedRepairCost(k1)
	}
	// The magic the combine ends with goes back into the copy in the
	// component its own kind keeps it in - the base's, not the extra's.
	if isEnchantedBook(base) {
		copyOf.SetEnchantments(true, merged)
	} else {
		copyOf.SetEnchantments(false, merged)
	}
	return AnvilAnswer{Result: copyOf, Cost: cost, Consumed: repairItemCountCost, OnlyRenaming: onlyRenaming}
}

// AnvilTake is what onTake leaves of the second input: less the mending's
// material, less what the combine consumed, or untouched for a lone rename.
// Removing the first input, resetting the worth and spending the levels are
// the caller's.
func AnvilTake(extra protocol.ItemStack, answer AnvilAnswer) protocol.ItemStack {
	if answer.Consumed > 0 {
		if !extra.Empty() && extra.Count > answer.Consumed {
			left := extra
			left.Count = extra.Count - answer.Consumed
			return left
		}
		return protocol.ItemStack{}
	}
	if answer.OnlyRenaming {
		return extra
	}
	return protocol.ItemStack{}
}

// sameItem is the question of two stacks being the same item at all,
// components out of the question, the way ItemStack.is answers it.
func sameItem(a, b protocol.ItemStack) bool {
	return !a.Empty() && a.ItemID == b.ItemID
}

// sameHoverName reports whether the typed name reads as the stack's own: a
// custom name compares as its literal, a raw one as its plain string; a
// translated default name is never resolved, so it never matches.
// vanilla: AnvilMenu.createResult (getHoverName().getString())
func sameHoverName(base protocol.ItemStack, name string) bool {
	if base.CustomName == nil || base.CustomName.Translate != "" {
		return false
	}
	if base.CustomName.Raw != nil {
		return base.CustomName.Plain() == name
	}
	return base.CustomName.Text == name
}

// cloneEnchantments copies a stack's magic map, the combine's own working
// copy being the whole point of taking one first.
func cloneEnchantments(m map[int32]int32) map[int32]int32 {
	if m == nil {
		return nil
	}
	c := make(map[int32]int32, len(m))
	maps.Copy(c, m)
	return c
}

// sortedEnchantmentIDs walks a magic map's keys in the registry's low-ID
// order, the one fixed order a Go map does not give by itself.
func sortedEnchantmentIDs(m map[int32]int32) []int32 {
	if len(m) == 0 {
		return nil
	}
	ids := make([]int32, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j] < ids[j-1]; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
	return ids
}

// isStringWhitespace is Character.isWhitespace or Character.isSpaceChar, so
// the non-breaking spaces count and U+0085 (a control character) does not.
// vanilla: StringUtil.isWhitespace
func isStringWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\x0B', '\f', '\r', 0x1C, 0x1D, 0x1E, 0x1F:
		return true
	}
	return unicode.In(r, unicode.Zs, unicode.Zl, unicode.Zp)
}

// clampInt32 narrows a long to the int32 the worth travels as, the way
// vanilla's Mth.clamp does: past the top it stays at the top.
func clampInt32(v int64) int32 {
	if v > 0x7FFFFFFF {
		return 0x7FFFFFFF
	}
	if v < 0 {
		return 0
	}
	return int32(v)
}

// anvilDamageChance is the chance one survival take wears the anvil by, the
// flat twelfth vanilla's AnvilMenu rolls its own random against on every
// take that is not a creative player's.
const anvilDamageChance = 0.12

// AnvilWears reports whether a survival take wears the anvil, for a roll in
// [0, 1).
// vanilla: AnvilMenu.onTake
func AnvilWears(roll float64) bool { return roll < anvilDamageChance }
