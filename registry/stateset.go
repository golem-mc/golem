package registry

import (
	"slices"
	"strconv"
	"strings"
)

// Prop is a block-state property name ("facing", "power") interned at init,
// so states change without hashing or building strings; one Prop names the
// property on every block, whatever values it takes there.
type Prop uint8

// The properties the engine sets on hot paths.
var (
	PropAge             = mustProp("age")
	PropAttached        = mustProp("attached")
	PropCopperGolemPose = mustProp("copper_golem_pose")
	PropDistance        = mustProp("distance")
	PropDown            = mustProp("down")
	PropEast            = mustProp("east")
	PropEnabled         = mustProp("enabled")
	PropExtended        = mustProp("extended")
	PropFacing          = mustProp("facing")
	PropLevel           = mustProp("level")
	PropLit             = mustProp("lit")
	PropSignalFire      = mustProp("signal_fire")
	PropLocked          = mustProp("locked")
	PropMode            = mustProp("mode")
	PropNorth           = mustProp("north")
	PropPower           = mustProp("power")
	PropPowered         = mustProp("powered")
	PropShort           = mustProp("short")
	PropSouth           = mustProp("south")
	PropTrialState      = mustProp("trial_spawner_state")
	PropTriggered       = mustProp("triggered")
	PropType            = mustProp("type")
	PropUp              = mustProp("up")
	PropWaterlogged     = mustProp("waterlogged")
	PropWest            = mustProp("west")
)

// PropValue is one property and the value a state is to take for it.
type PropValue struct {
	P Prop
	V string
}

// propSlot is one property of one block: where its value sits in the
// block's state index and the values it takes.
type propSlot struct {
	prop Prop
	n    uint16 // number of values
	// stride is how far apart in the block's state index two states are
	// whose value of this property differs by one place.
	stride uint16
	values []string // in the property's declared order, shared between blocks
	// trueIdx and falseIdx are the indexes of "true" and "false", -1 when
	// the property has no such value.
	trueIdx, falseIdx int8
	// intMin is the first value when the values are consecutive ascending
	// integers (intRun), so value v sits at index v-intMin.
	intMin int16
	intRun bool
}

// stateLayout is a block's properties in name order and the map between its
// states and their index, the mixed-radix number of the states' value
// indexes with the last property running fastest.
// vanilla: StateDefinition (the cartesian product of the property values)
type stateLayout struct {
	slots []propSlot
	// indexOf and stateOf are nil when a state's index is its offset from
	// the block's first state, which holds for every block of the generated
	// tables (TestStateLayoutsDense); otherwise they map the offset to the
	// index and the index back to a state, noState where none.
	indexOf []uint16
	stateOf []uint16
}

// noState marks an index of stateLayout.stateOf that no state holds.
const noState = 0xffff

// stateTables holds every block's stateLayout and the Prop names, built
// together because the layouts intern the names they meet.
var stateTables = buildStateTables()

type stateTablesT struct {
	layouts [BlockCount]stateLayout // by Block
	names   []string                // by Prop
	byName  map[string]Prop
}

// mustProp is the Prop of a property name some block has.
func mustProp(name string) Prop {
	p, ok := PropNamed(name)
	if !ok {
		panic("registry: no block has the property " + name)
	}
	return p
}

// PropNamed returns the Prop of a property name, false for a name no block
// has.
func PropNamed(name string) (Prop, bool) {
	p, ok := stateTables.byName[name]
	return p, ok
}

// Name returns the property's name, such as "facing".
func (p Prop) Name() string { return stateTables.names[p] }

// intern returns name's Prop, adding it when it is new.
func (t *stateTablesT) intern(name string) Prop {
	if p, ok := t.byName[name]; ok {
		return p
	}
	if len(t.names) > 0xff {
		panic("registry: more block-state property names than a Prop holds")
	}
	p := Prop(len(t.names))
	t.names = append(t.names, name)
	t.byName[name] = p
	return p
}

// buildStateTables reads every block's states out of AllStates.
func buildStateTables() *stateTablesT {
	t := &stateTablesT{byName: map[string]Prop{}}
	shared := map[string][]string{}
	for b := range Block(BlockCount) {
		states, ok := AllStates[blockInfo[b].name]
		if !ok || states[0].Props == "" {
			continue
		}
		t.layouts[b] = t.buildLayout(blockInfo[b].first, states, shared)
	}
	return t
}

// buildLayout builds the layout of a block whose states start at first.
// shared interns the value lists between blocks.
func (t *stateTablesT) buildLayout(first uint16, states []StateEntry, shared map[string][]string) stateLayout {
	byID := make([]StateEntry, len(states))
	copy(byID, states)
	for i := 1; i < len(byID); i++ {
		for j := i; j > 0 && byID[j-1].ID > byID[j].ID; j-- {
			byID[j-1], byID[j] = byID[j], byID[j-1]
		}
	}
	// Every state lists the same keys in the same (sorted) order, so a
	// value's position in the string is its slot. The values are met in
	// state ID order, which is the order the property declares them.
	var l stateLayout
	for kv := range strings.SplitSeq(byID[0].Props, ",") {
		k, _, _ := strings.Cut(kv, "=")
		l.slots = append(l.slots, propSlot{prop: t.intern(k)})
	}
	values := make([][]string, len(l.slots))
	ns := len(l.slots)
	// idx holds each state's value indexes, ns per state.
	idx := make([]uint16, len(byID)*ns)
	for si, s := range byID {
		pos := 0
		for k := range l.slots {
			pos += len(t.names[l.slots[k].prop]) + 1 // the key and '='
			end := strings.IndexByte(s.Props[pos:], ',')
			if end < 0 {
				end = len(s.Props)
			} else {
				end += pos
			}
			v := s.Props[pos:end]
			pos = end + 1
			at := -1
			for i, have := range values[k] {
				if have == v {
					at = i
					break
				}
			}
			if at < 0 {
				at = len(values[k])
				values[k] = append(values[k], v)
			}
			idx[si*ns+k] = uint16(at)
		}
	}
	size := 1
	for k := len(l.slots) - 1; k >= 0; k-- {
		sl := &l.slots[k]
		key := strings.Join(values[k], ",")
		if v, ok := shared[key]; ok {
			sl.values = v
		} else {
			sl.values = values[k]
			shared[key] = values[k]
		}
		sl.n = uint16(len(sl.values))
		sl.stride = uint16(size)
		size *= len(sl.values)
		sl.trueIdx, sl.falseIdx = -1, -1
		sl.intRun = true
		for i, v := range sl.values {
			switch v {
			case "true":
				sl.trueIdx = int8(i)
			case "false":
				sl.falseIdx = int8(i)
			}
			n, err := strconv.Atoi(v)
			if i == 0 {
				sl.intMin = int16(n)
			}
			if err != nil || n != int(sl.intMin)+i {
				sl.intRun = false
			}
		}
	}
	dense := size == len(byID)
	for si, s := range byID {
		if l.indexOfValues(idx[si*ns:(si+1)*ns]) != int(s.ID-first) {
			dense = false
		}
	}
	if !dense {
		l.indexOf = make([]uint16, len(byID))
		l.stateOf = make([]uint16, size)
		for i := range l.stateOf {
			l.stateOf[i] = noState
		}
		for si, s := range byID {
			i := l.indexOfValues(idx[si*ns : (si+1)*ns])
			l.indexOf[s.ID-first] = uint16(i)
			l.stateOf[i] = s.ID
		}
	}
	return l
}

// indexOfValues is the state index of the given value indexes, one per slot.
func (l *stateLayout) indexOfValues(values []uint16) int {
	i := 0
	for k, v := range values {
		i += int(v) * int(l.slots[k].stride)
	}
	return i
}

// index is the state index of state, a state of the block whose layout l is
// and whose first state is first.
func (l *stateLayout) index(state, first uint16) int {
	if l.indexOf != nil {
		return int(l.indexOf[state-first])
	}
	return int(state - first)
}

// state is the state at index i, false when no state holds it.
func (l *stateLayout) state(i int, first uint16) (uint16, bool) {
	if l.stateOf != nil {
		s := l.stateOf[i]
		return s, s != noState
	}
	return first + uint16(i), true
}

// slot is p's slot, nil when the block has no such property.
func (l *stateLayout) slot(p Prop) *propSlot {
	for k := range l.slots {
		if l.slots[k].prop == p {
			return &l.slots[k]
		}
	}
	return nil
}

// slotNamed is slot by the property's name.
func (l *stateLayout) slotNamed(name string) *propSlot {
	for k := range l.slots {
		if stateTables.names[l.slots[k].prop] == name {
			return &l.slots[k]
		}
	}
	return nil
}

// valueIndex is v's index among the slot's values, -1 when it is none.
func (s *propSlot) valueIndex(v string) int {
	for i, have := range s.values {
		if have == v {
			return i
		}
	}
	return -1
}

// withIndex moves the state index i to value index v of the slot.
func (s *propSlot) withIndex(i, v int) int {
	cur := i / int(s.stride) % int(s.n)
	return i + (v-cur)*int(s.stride)
}

// locate is the layout, first state and index of state, and p's slot in it:
// nil when the state's block has no such property.
func locate(state uint16, p Prop) (*stateLayout, *propSlot, uint16, int) {
	if int(state) >= StateCount {
		return nil, nil, 0, 0
	}
	b := blockOfState[state]
	l := &stateTables.layouts[b]
	first := blockInfo[b].first
	return l, l.slot(p), first, l.index(state, first)
}

// set returns the state at index i with slot s moved to value index v, and
// the state given when v is no value of s or no state holds the result.
func (l *stateLayout) set(state, first uint16, i int, s *propSlot, v int) (uint16, bool) {
	if v < 0 || v >= int(s.n) {
		return state, false
	}
	if out, ok := l.state(s.withIndex(i, v), first); ok {
		return out, true
	}
	return state, false
}

// WithProp returns state with property p set to value and every other
// property kept. false, with state itself, when the state's block has no
// such property or the value is not one of it.
// vanilla: StateHolder.setValue
func WithProp(state uint16, p Prop, value string) (uint16, bool) {
	l, s, first, i := locate(state, p)
	if s == nil {
		return state, false
	}
	return l.set(state, first, i, s, s.valueIndex(value))
}

// WithBool is WithProp for a boolean property.
func WithBool(state uint16, p Prop, value bool) (uint16, bool) {
	l, s, first, i := locate(state, p)
	if s == nil {
		return state, false
	}
	v := s.falseIdx
	if value {
		v = s.trueIdx
	}
	return l.set(state, first, i, s, int(v))
}

// WithInt is WithProp for an integer property such as power or age.
func WithInt(state uint16, p Prop, value int) (uint16, bool) {
	l, s, first, i := locate(state, p)
	if s == nil {
		return state, false
	}
	if !s.intRun {
		return l.set(state, first, i, s, s.valueIndex(strconv.Itoa(value)))
	}
	return l.set(state, first, i, s, value-int(s.intMin))
}

// WithProps is WithProp for several properties at once; false, with state
// itself, when any is not the block's or takes no such value.
func WithProps(state uint16, values ...PropValue) (uint16, bool) {
	if int(state) >= StateCount {
		return state, false
	}
	b := blockOfState[state]
	l := &stateTables.layouts[b]
	first := blockInfo[b].first
	i := l.index(state, first)
	for _, pv := range values {
		s := l.slot(pv.P)
		if s == nil {
			return state, false
		}
		vi := s.valueIndex(pv.V)
		if vi < 0 {
			return state, false
		}
		i = s.withIndex(i, vi)
	}
	if out, ok := l.state(i, first); ok {
		return out, true
	}
	return state, false
}

// WithPropertiesOf returns b's default state with every property it shares
// with from set to from's value, such as a copper door one stage on. false,
// with b's default state, when one of those values is none of b's.
// vanilla: Block.withPropertiesOf
func WithPropertiesOf(b Block, from uint16) (uint16, bool) {
	if b >= BlockCount {
		return 0, false
	}
	l := &stateTables.layouts[b]
	first, deflt := blockInfo[b].first, blockInfo[b].deflt
	i := l.index(deflt, first)
	for k := range l.slots {
		s := &l.slots[k]
		v := StateValue(from, s.prop)
		if v == "" {
			continue
		}
		vi := s.valueIndex(v)
		if vi < 0 {
			return deflt, false
		}
		i = s.withIndex(i, vi)
	}
	if out, ok := l.state(i, first); ok {
		return out, true
	}
	return deflt, false
}

// WithStatePropertiesOf copies properties with the same name and value domain
// onto state, preserving its other values.
// vanilla: BlockBehaviour.BlockStateBase.withPropertiesOf, Property.equals
func WithStatePropertiesOf(state, from uint16) (uint16, bool) {
	b, source := BlockOf(state), BlockOf(from)
	if b >= BlockCount || source >= BlockCount {
		return state, false
	}
	l, other := &stateTables.layouts[b], &stateTables.layouts[source]
	first := blockInfo[b].first
	i := l.index(state, first)
	for k := range l.slots {
		slot := &l.slots[k]
		src := other.slot(slot.prop)
		// Integer and enum properties with different domains are distinct properties.
		if src == nil || !slices.Equal(slot.values, src.values) {
			continue
		}
		i = slot.withIndex(i, slot.valueIndex(StateValue(from, slot.prop)))
	}
	if out, ok := l.state(i, first); ok {
		return out, true
	}
	return state, false
}

// FindState returns a block's state with property overrides, the rest at
// their defaults. Unknown properties and invalid values are ignored;
// false only for an unknown block. Hot paths use WithProp, which hashes no name.
// vanilla: StateDefinition.appendPropertyCodec
func FindState(name string, overrides map[string]string) (uint16, bool) {
	b, ok := blockByName[name]
	if !ok {
		id, ok := BlockStates[name]
		return id, ok
	}
	deflt := blockInfo[b].deflt
	if len(overrides) == 0 {
		return deflt, true
	}
	l := &stateTables.layouts[b]
	first := blockInfo[b].first
	i := l.index(deflt, first)
	for k, v := range overrides {
		s := l.slotNamed(k)
		if s == nil {
			continue
		}
		vi := s.valueIndex(v)
		if vi < 0 {
			continue
		}
		i = s.withIndex(i, vi)
	}
	if id, ok := l.state(i, first); ok {
		return id, true
	}
	return deflt, true
}
