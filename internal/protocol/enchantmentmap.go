package protocol

import (
	"maps"
	"slices"
)

// EnchantmentIDs returns the retained traversal, with deterministic construction for map literals.
// Vanilla's holder hash-map ties depend on JVM identities; retain each decoded map's traversal.
func (s ItemStack) EnchantmentIDs(stored bool) []int32 {
	levels, order := s.Enchantments, s.enchantmentOrder
	if stored {
		levels, order = s.StoredEnchantments, s.storedEnchantmentOrder
	}
	ids := make([]int32, 0, len(levels))
	seen := make(map[int32]bool, len(levels))
	for _, id := range order {
		if _, present := levels[id]; present && !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	if len(ids) != len(levels) {
		for _, id := range slices.Sorted(maps.Keys(levels)) {
			if !seen[id] {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// SetEnchantments replaces a component without sharing writable maps or traversal with copies.
// An explicit order belongs to a codec read; mutations retain surviving entries.
// vanilla: ItemEnchantments.CODEC/STREAM_CODEC, ItemEnchantments.Mutable.toImmutable
func (s *ItemStack) SetEnchantments(stored bool, levels map[int32]int32, order ...int32) {
	if len(order) == 0 {
		order = s.EnchantmentIDs(stored)
	}
	component := int32(DataComponentEnchantments)
	if stored {
		component = DataComponentStoredEnchantments
	}
	if len(levels) > 0 {
		s.NotePatch(component)
	} else {
		s.ForgetPatch(component)
	}
	if stored {
		s.StoredEnchantments, s.storedEnchantmentOrder = maps.Clone(levels), slices.Clone(order)
		s.storedEnchantmentOrder = s.EnchantmentIDs(true)
	} else {
		s.Enchantments, s.enchantmentOrder = maps.Clone(levels), slices.Clone(order)
		s.enchantmentOrder = s.EnchantmentIDs(false)
	}
}

// SetEnchantment sets or removes one level while isolating copied stacks.
// vanilla: ItemEnchantments.Mutable.set
func (s *ItemStack) SetEnchantment(stored bool, id, level int32) {
	levels := s.Enchantments
	if stored {
		levels = s.StoredEnchantments
	}
	next := make(map[int32]int32, len(levels)+1)
	maps.Copy(next, levels)
	if level <= 0 {
		delete(next, id)
	} else {
		next[id] = min(level, 255)
	}
	s.SetEnchantments(stored, next)
}
