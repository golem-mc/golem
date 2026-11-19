package registry

import (
	"sort"
)

// BlockNames lists every block name, sorted; between them BlockStates and
// AllStates name every block.
var BlockNames = func() []string {
	names := make(map[string]bool, len(BlockStates)+len(AllStates))
	for name := range BlockStates {
		names[name] = true
	}
	for name := range AllStates {
		names[name] = true
	}
	blocks := make([]string, 0, len(names))
	for name := range names {
		blocks = append(blocks, name)
	}
	sort.Strings(blocks)
	return blocks
}()

// stateInfo pairs a block name with the properties string of one of its
// states and that string parsed.
type stateInfo struct {
	name   string
	props  string
	parsed map[string]string
}

// noProps is the parsed properties of a state without any, and of an ID that
// is no state.
var noProps = map[string]string{}

// stateByID holds each state ID's block name and properties string (empty
// name for no state), for converting between in-memory IDs and the region
// format's named palette entries.
//
// AllStates holds blocks with properties plus the property-less blocks with
// no item form (soul fire, end portal, potted plants); BlockStates holds the
// single state of property-less blocks with an item. Together they cover
// every state.
var stateByID = func() []stateInfo {
	n := 0
	for _, states := range AllStates {
		for _, s := range states {
			n = max(n, int(s.ID)+1)
		}
	}
	for _, id := range BlockStates {
		n = max(n, int(id)+1)
	}
	m := make([]stateInfo, n)
	for name, states := range AllStates {
		for _, s := range states {
			m[s.ID] = stateInfo{name: name, props: s.Props}
		}
	}
	for name, id := range BlockStates {
		if m[id].name == "" {
			m[id] = stateInfo{name: name, props: ""}
		}
	}
	// States with the same properties string share one map: there are far
	// fewer distinct strings than states.
	parsed := map[string]map[string]string{"": noProps}
	for i := range m {
		pm, ok := parsed[m[i].props]
		if !ok {
			pm = ParseStateProps(m[i].props)
			parsed[m[i].props] = pm
		}
		m[i].parsed = pm
	}
	return m
}()

// blockPropertyValues maps block → property → accepted values, built once
// from AllStates.
var blockPropertyValues = func() map[string]map[string]map[string]bool {
	m := make(map[string]map[string]map[string]bool, len(AllStates))
	for name, states := range AllStates {
		props := map[string]map[string]bool{}
		for _, s := range states {
			for k, v := range ParseStateProps(s.Props) {
				values := props[k]
				if values == nil {
					values = map[string]bool{}
					props[k] = values
				}
				values[v] = true
			}
		}
		m[name] = props
	}
	return m
}()

// BlockPropertyValues returns the values a block's property accepts, and
// false when the block lacks it, so a block-state argument can refuse it
// rather than fall back to the default. The map is shared and read-only.
func BlockPropertyValues(name, prop string) (map[string]bool, bool) {
	props, ok := blockPropertyValues[name]
	if !ok {
		return nil, false
	}
	values, ok := props[prop]
	return values, ok
}

// blockPropertyOrder maps block → property → values in declaration order.
// AllStates lists the default first, so values are read by global state ID,
// which the palette assigns in declaration order.
var blockPropertyOrder = func() map[string]map[string][]string {
	m := make(map[string]map[string][]string, len(AllStates))
	for name, states := range AllStates {
		byID := append([]StateEntry(nil), states...)
		sort.Slice(byID, func(i, j int) bool { return byID[i].ID < byID[j].ID })
		props := map[string][]string{}
		seen := map[string]map[string]bool{}
		for _, s := range byID {
			for k, v := range ParseStateProps(s.Props) {
				if seen[k] == nil {
					seen[k] = map[string]bool{}
				}
				if !seen[k][v] {
					seen[k][v] = true
					props[k] = append(props[k], v)
				}
			}
		}
		m[name] = props
	}
	return m
}()

// BlockPropertyOrder returns a property's values in declaration order (what
// a ranged matcher ranks enums by), and whether the block has it. The slice
// is shared and read-only.
func BlockPropertyOrder(name, prop string) ([]string, bool) {
	props, ok := blockPropertyOrder[name]
	if !ok {
		return nil, false
	}
	order, ok := props[prop]
	return order, ok
}

// BlockStateName returns a state ID's block name and sorted "k=v,k=v"
// properties ("" for none), the reverse of FindState. false for an unknown
// ID (a newer version's or a mod's), which callers treat as air.
func BlockStateName(id uint16) (name string, props string, ok bool) {
	if int(id) >= len(stateByID) || stateByID[id].name == "" {
		return "", "", false
	}
	info := stateByID[id]
	return info.name, info.props, true
}

// StateProps returns a state's properties, empty for none or an unknown ID.
// The map is shared and read-only.
// vanilla: StateHolder.propertyValues
func StateProps(id uint16) map[string]string {
	if int(id) >= len(stateByID) {
		return noProps
	}
	return stateByID[id].parsed
}

// StateNameProps is BlockStateName and StateProps in one lookup. ok is false
// for an ID that is no state; props is the shared read-only map.
func StateNameProps(id uint16) (name string, props map[string]string, ok bool) {
	if int(id) >= len(stateByID) || stateByID[id].name == "" {
		return "", noProps, false
	}
	info := &stateByID[id]
	return info.name, info.parsed, true
}

// BlockPropertyNames returns a block's property names in definition order,
// which is by name; nil for none.
// vanilla: StateDefinition (propertiesByName is a sorted map)
func BlockPropertyNames(name string) []string {
	props := blockPropertyOrder[name]
	if len(props) == 0 {
		return nil
	}
	names := make([]string, 0, len(props))
	for k := range props {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
