package rules

import (
	"slices"
	"strconv"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Adventure gates: a hand the mode keeps from building and digging may act
// only where the held stack's minecraft:can_place_on and can_break (raw wire
// bytes) allow.

// AdventureTarget supplies the clicked state and lazily reads its block entity.
// A nil reader or compound means the block has no entity.
type AdventureTarget struct {
	State   uint16
	ReadNBT func() nbt.Compound
}

// CanPlaceOnInAdventureMode reports whether the stack's can_place_on names a
// predicate the block fits, the gate for every use of the item by a hand that
// may not build. No component, or an empty predicate list, matches nothing.
// vanilla: ItemStack.useOn, AdventureModePredicate.test
func CanPlaceOnInAdventureMode(stack protocol.ItemStack, target AdventureTarget) bool {
	predicates, _ := stack.CanPlaceOn()
	return predicatesMatch(predicates, target)
}

// CanBreakInAdventureMode reports whether the held stack may dig the block,
// the minecraft:can_break half of the same rule.
// vanilla: ItemStack.canBreakBlockInAdventureMode
func CanBreakInAdventureMode(stack protocol.ItemStack, target AdventureTarget) bool {
	predicates, _ := stack.CanBreak()
	return predicatesMatch(predicates, target)
}

// BlockActionRestricted reports whether the mode keeps the player's hands off
// the block: building modes never, spectator always, adventure unless the
// main hand's can_break names it.
// vanilla: Player.blockActionRestricted
func BlockActionRestricted(mode int32, mainHand protocol.ItemStack, target AdventureTarget) bool {
	switch {
	case ModeMayBuild(mode):
		return false
	case mode == protocol.GameModeSpectator:
		return true
	default:
		return mainHand.Empty() || !CanBreakInAdventureMode(mainHand, target)
	}
}

// MayUseItemAt reports whether an item may be used on the clicked block:
// building modes freely, spectator never (its item use passes), adventure
// where can_place_on names it. An empty hand may not.
// vanilla: Player.mayUseItemAt, ServerPlayerGameMode.useItem
func MayUseItemAt(mode int32, held protocol.ItemStack, target AdventureTarget) bool {
	switch {
	case mode == protocol.GameModeSpectator:
		return false
	case ModeMayBuild(mode):
		return true
	default:
		return !held.Empty() && CanPlaceOnInAdventureMode(held, target)
	}
}

// predicatesMatch reports whether one of the predicates fits the block state,
// the any-of AdventureModePredicate.test answers.
func predicatesMatch(predicates []protocol.BlockPredicate, target AdventureTarget) bool {
	name, rawProps, ok := registry.BlockStateName(target.State)
	if !ok {
		return false
	}
	props := registry.ParseStateProps(rawProps)
	var tag nbt.Compound
	read := false
	for _, predicate := range predicates {
		if !predicateMatches(predicate, name, props) {
			continue
		}
		if predicate.NBT == nil {
			return true
		}
		if !read {
			read = true
			if target.ReadNBT != nil {
				tag = target.ReadNBT()
			}
		}
		if tag != nil && nbt.Matches(predicate.NBT, tag) {
			return true
		}
	}
	return false
}

// predicateMatches tests the block set and properties before NBT is read.
// vanilla: BlockPredicate.matches
func predicateMatches(predicate protocol.BlockPredicate, name string, props map[string]string) bool {
	if !blockSetMatches(predicate.Blocks, name) {
		return false
	}
	for _, matcher := range predicate.Props {
		if !propMatcherMatches(name, matcher, props) {
			return false
		}
	}
	return true
}

// blockSetMatches reports whether the block is in the predicate's block set:
// by tag membership for a named set, by registry ID for an inline one. An
// absent set passes every block; a present-but-empty inline set passes none.
// A tag without a namespace takes the default namespace.
// vanilla: BlockState.is(HolderSet)
func blockSetMatches(set protocol.BlockPredicateSet, name string) bool {
	if !set.Present {
		return true
	}
	if set.Tag != "" {
		return registry.BlockInTag(name, registry.Qualify(set.Tag))
	}
	id, ok := registry.BlockIDs[name]
	return ok && slices.Contains(set.Inline, id)
}

// blockSetMatchesBlock is blockSetMatches for a block already resolved.
func blockSetMatchesBlock(set protocol.BlockPredicateSet, b registry.Block) bool {
	if !set.Present {
		return true
	}
	if set.Tag != "" {
		return b.InTag(registry.BlockTagOf(registry.Qualify(set.Tag)))
	}
	return slices.Contains(set.Inline, int32(b))
}

// propKind is which of the game's property types a matcher's property is,
// decided from the values the block's states carry.
type propKind int

const (
	propInteger propKind = iota
	propBoolean
	propEnum
)

// classifyProperty names the property type from its values: the two booleans,
// a run of numbers, and everything else an enum.
func classifyProperty(values []string) propKind {
	boolean, integer := true, true
	for _, v := range values {
		if v != "true" && v != "false" {
			boolean = false
		}
		if _, err := strconv.Atoi(v); err != nil {
			integer = false
		}
	}
	switch {
	case boolean:
		return propBoolean
	case integer:
		return propInteger
	default:
		return propEnum
	}
}

// propertyRank is the rank vanilla compares a value by: the number for a
// numbered property (so "07" is 7), false before true, else the state-list
// position. false for a value the property rejects or outside its bounds.
// vanilla: StatePropertiesPredicate.matches, IntegerProperty.getValue
func propertyRank(kind propKind, values []string, value string) (int, bool) {
	switch kind {
	case propBoolean:
		switch value {
		case "false":
			return 0, true
		case "true":
			return 1, true
		default:
			return 0, false
		}
	case propInteger:
		n, err := strconv.Atoi(value)
		if err != nil {
			return 0, false
		}
		low, err := strconv.Atoi(values[0])
		if err != nil {
			return 0, false
		}
		high := low
		for _, v := range values[1:] {
			bound, err := strconv.Atoi(v)
			if err != nil {
				continue
			}
			low, high = min(low, bound), max(high, bound)
		}
		if n < low || n > high {
			return 0, false
		}
		return n, true
	default:
		for i, v := range values {
			if v == value {
				return i, true
			}
		}
		return 0, false
	}
}

// propMatcherMatches reports whether the state's property matches, compared
// by the property's type (numeric, false before true, enum order). A value
// or bound the property rejects fits nothing.
// vanilla: StatePropertiesPredicate.matches
func propMatcherMatches(block string, matcher protocol.BlockPredicateProp, props map[string]string) bool {
	value, has := props[matcher.Name]
	if !has {
		return false
	}
	values, ok := registry.BlockPropertyOrder(block, matcher.Name)
	if !ok {
		return false
	}
	kind := classifyProperty(values)
	rank, ok := propertyRank(kind, values, value)
	if !ok {
		return false
	}
	if !matcher.Ranged {
		want, ok := propertyRank(kind, values, matcher.Value)
		return ok && rank == want
	}
	if matcher.Min != "" {
		bound, ok := propertyRank(kind, values, matcher.Min)
		if !ok || rank < bound {
			return false
		}
	}
	if matcher.Max != "" {
		bound, ok := propertyRank(kind, values, matcher.Max)
		if !ok || rank > bound {
			return false
		}
	}
	return true
}
