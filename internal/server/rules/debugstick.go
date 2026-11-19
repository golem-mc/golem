package rules

import "github.com/golem-mc/golem/registry"

// DebugStickSelect picks the property after current in the block's order,
// wrapping (before, when sneaking); an unknown current picks the first (or
// last). value is its value in state; false for a block without properties.
// vanilla: DebugStickItem.handleInteraction, Util.findNextInIterable
func DebugStickSelect(state uint16, current string, backward bool) (prop, value string, ok bool) {
	name, raw, known := registry.BlockStateName(state)
	names := registry.BlockPropertyNames(name)
	if !known || len(names) == 0 {
		return "", "", false
	}
	prop = relative(names, current, backward)
	return prop, registry.ParseStateProps(raw)[prop], true
}

// DebugStickCycle moves the property to its next value (the previous when
// backward) and returns the new state and the value now set. A property the
// block does not carry picks the block's first, like a fresh stick.
// vanilla: DebugStickItem.cycleState
func DebugStickCycle(state uint16, prop string, backward bool) (newState uint16, usedProp, value string, ok bool) {
	name, raw, known := registry.BlockStateName(state)
	names := registry.BlockPropertyNames(name)
	if !known || len(names) == 0 {
		return state, "", "", false
	}
	order, has := registry.BlockPropertyOrder(name, prop)
	if !has {
		prop = names[0]
		order, _ = registry.BlockPropertyOrder(name, prop)
	}
	props := registry.ParseStateProps(raw)
	value = relative(order, props[prop], backward)
	props[prop] = value
	newState, _ = registry.FindState(name, props)
	return newState, prop, value, true
}

// relative is Util.findNextInIterable and findPreviousInIterable over a
// list: one step on from current with a wrap, and when current is absent the
// first item going forward and the last going back.
func relative(list []string, current string, backward bool) string {
	at := -1
	for i, s := range list {
		if s == current {
			at = i
			break
		}
	}
	n := len(list)
	switch {
	case backward && at < 0:
		return list[n-1]
	case backward:
		return list[(at+n-1)%n]
	case at < 0:
		return list[0]
	default:
		return list[(at+1)%n]
	}
}
