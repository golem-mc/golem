package protocol

import "slices"

// NotePatch records that the patch now holds the component types, an add or a
// removal alike, after those already noted; a type already noted keeps its
// place. Copies share the history, so it is never appended in place.
// vanilla: DataComponentPatch (Reference2ObjectArrayMap)
func (s *ItemStack) NotePatch(types ...int32) {
	for _, t := range types {
		if slices.Contains(s.patchOrder, t) {
			continue
		}
		s.patchOrder = slices.Concat(s.patchOrder, []int32{t})
	}
}

// ForgetPatch drops the component types from the history, so a later add
// lands at the end.
// vanilla: DataComponentPatch (ArrayMap.remove)
func (s *ItemStack) ForgetPatch(types ...int32) {
	if len(s.patchOrder) == 0 {
		return
	}
	kept := make([]int32, 0, len(s.patchOrder))
	for _, t := range s.patchOrder {
		if !slices.Contains(types, t) {
			kept = append(kept, t)
		}
	}
	if len(kept) == len(s.patchOrder) {
		return
	}
	s.patchOrder = kept
}

// PatchOrdered returns a copy of ids stable-sorted by the patch history; ids
// the history lacks keep their input order after the noted ones.
func (s ItemStack) PatchOrdered(ids []int32) []int32 {
	if len(s.patchOrder) == 0 || len(ids) < 2 {
		return ids
	}
	out := slices.Clone(ids)
	s.orderPatch(out)
	return out
}

// orderPatch sorts ids in place like PatchOrdered; ranks are looked up once
// per id, in a stack buffer for the usual few components.
func (s ItemStack) orderPatch(ids []int32) {
	if len(s.patchOrder) == 0 || len(ids) < 2 {
		return
	}
	var buf [32]int32
	ranks := buf[:0]
	for _, id := range ids {
		r := slices.Index(s.patchOrder, id)
		if r < 0 {
			r = len(s.patchOrder)
		}
		ranks = append(ranks, int32(r))
	}
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ranks[j-1] > ranks[j]; j-- {
			ranks[j-1], ranks[j] = ranks[j], ranks[j-1]
			ids[j-1], ids[j] = ids[j], ids[j-1]
		}
	}
}

// DataComponentDebugStickState is minecraft:debug_stick_state, a component
// with no network codec.
const DataComponentDebugStickState = 59
