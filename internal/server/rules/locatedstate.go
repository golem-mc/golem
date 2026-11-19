package rules

import (
	"github.com/golem-mc/golem/internal/protocol"
)

type locatedKey struct {
	slot, enchantment string
	group             int
}

// LocatedState is the set of location_changed entries that were active on a
// body, per slot and enchantment, so that an entry's condition can ask whether
// it held at the last run. Owned by the body's tick.
// vanilla: LivingEntity.activeLocationDependentEnchantments
type LocatedState struct {
	active map[locatedKey][]LocatedAction
}

// Step runs the location_changed entries of the stack worn in slot. apply
// lists the entries whose condition holds, Became marking those that were not
// active before; remove lists those that stopped holding. restart, for a slot
// whose stack changed, first removes every active entry of the slot.
// An all_of of attribute effects is several entries sharing a Group, which
// match and deactivate together.
// vanilla: Enchantment.runLocationChangedEffects, stopLocationBasedEffects
func (s *LocatedState) Step(slot string, stack protocol.ItemStack, ctx *EnchContext, restart bool) (apply, remove []LocatedAction) {
	if restart || stack.Empty() || broken(stack) {
		remove = s.Stop(slot)
	}
	if stack.Empty() || broken(stack) {
		return nil, remove
	}
	// here and wasActive escape through the condition, so they are made once
	// per call and not per enchantment and group.
	var here EnchContext
	var wasActive bool
	var enchBuf [4]enchInstance
	for _, ench := range enchantsOnInto(enchBuf[:0], stack) {
		if !ench.def.MatchesSlot(slot) {
			continue
		}
		here = *ctx
		here.Level, here.Tool = ench.level, stack
		entries := ench.def.Effects.LocationChanged
		for i := 0; i < len(entries); {
			group := entries[i].Group
			end := i
			for end < len(entries) && entries[end].Group == group {
				end++
			}
			key := locatedKey{slot, ench.def.Name, group}
			held, was := s.active[key]
			wasActive = was
			here.Active = &wasActive
			matched := enchCondition(entries[i].Requires, &here)
			switch {
			case matched && was && held[0].Level == ench.level:
				apply = append(apply, held...)
			case matched:
				now := make([]LocatedAction, 0, end-i)
				for _, entry := range entries[i:end] {
					now = append(now, LocatedAction{Slot: slot, Enchantment: ench.def.Name, Level: ench.level, Entry: entry})
				}
				if s.active == nil {
					s.active = map[locatedKey][]LocatedAction{}
				}
				s.active[key] = now
				start := len(apply)
				apply = append(apply, now...)
				for j := start; !was && j < len(apply); j++ {
					apply[j].Became = true
				}
			case was:
				delete(s.active, key)
				remove = append(remove, held...)
			}
			i = end
		}
	}
	return apply, remove
}

// Stop deactivates every active entry of the slot, as when its stack leaves.
// vanilla: Enchantment.stopLocationBasedEffects
func (s *LocatedState) Stop(slot string) (remove []LocatedAction) {
	if len(s.active) == 0 {
		return nil
	}
	for key, held := range s.active {
		if key.slot == slot {
			remove = append(remove, held...)
			delete(s.active, key)
		}
	}
	return remove
}

// Empty reports whether no entry is active.
func (s *LocatedState) Empty() bool { return len(s.active) == 0 }
