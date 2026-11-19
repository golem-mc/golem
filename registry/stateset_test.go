package registry

import (
	"maps"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// findStateLinear is the reference FindState: the default state's properties
// with the overrides merged, matched as a string against every state.
func findStateLinear(name string, overrides map[string]string) (uint16, bool) {
	states, ok := AllStates[name]
	if !ok {
		id, ok := BlockStates[name]
		return id, ok
	}
	if len(overrides) == 0 {
		return states[0].ID, true
	}
	props := ParseStateProps(states[0].Props)
	for k, v := range overrides {
		if blockPropertyValues[name][k][v] {
			props[k] = v
		}
	}
	keys := make([]string, 0, len(props))
	for k, v := range props {
		keys = append(keys, k+"="+v)
	}
	sort.Strings(keys)
	key := strings.Join(keys, ",")
	for _, s := range states {
		if s.Props == key {
			return s.ID, true
		}
	}
	return states[0].ID, true
}

// TestStateLayoutsDense checks that every block's states are the cartesian
// product of its property values, the last property running fastest, so no
// block needs the fallback tables, and that each state's value indexes give
// back its properties string.
func TestStateLayoutsDense(t *testing.T) {
	t.Parallel()
	for b := range Block(BlockCount) {
		l := &stateTables.layouts[b]
		if l.indexOf != nil || l.stateOf != nil {
			t.Errorf("%s: states are not a cartesian product in property order", b.Name())
		}
		first := blockInfo[b].first
		end := uint16(StateCount)
		if b+1 < BlockCount {
			end = blockInfo[b+1].first
		}
		size := 1
		for _, s := range l.slots {
			size *= int(s.n)
		}
		if int(end-first) != size {
			t.Errorf("%s: %d states, layout holds %d", b.Name(), end-first, size)
		}
		for id := first; id < end; id++ {
			i := l.index(id, first)
			parts := make([]string, len(l.slots))
			for k, s := range l.slots {
				parts[k] = s.prop.Name() + "=" + s.values[i/int(s.stride)%int(s.n)]
			}
			if _, raw, _ := BlockStateName(id); strings.Join(parts, ",") != raw {
				t.Fatalf("state %d: layout reads %q, want %q", id, strings.Join(parts, ","), raw)
			}
		}
	}
}

// TestStateLayoutFallback builds a layout from states whose IDs are not in
// property order, and one missing a state, and checks the fallback tables.
func TestStateLayoutFallback(t *testing.T) {
	t.Parallel()
	tables := &stateTablesT{byName: map[string]Prop{}}
	states := []StateEntry{
		{102, "lit=false,power=1"},
		{100, "lit=true,power=0"},
		{101, "lit=false,power=0"},
		{103, "lit=true,power=1"},
		{104, "lit=true,power=2"},
	}
	l := tables.buildLayout(100, states, map[string][]string{})
	if l.indexOf == nil {
		t.Fatal("layout of out-of-order states is dense")
	}
	lit, power := l.slot(tables.byName["lit"]), l.slot(tables.byName["power"])
	cases := []struct {
		from  uint16
		slot  *propSlot
		value string
		want  uint16
		ok    bool
	}{
		{100, lit, "false", 101, true},
		{101, power, "1", 102, true},
		{102, lit, "true", 103, true},
		{103, power, "2", 104, true},
		{102, power, "2", 0, false}, // lit=false,power=2 is no state
	}
	for _, c := range cases {
		got, ok := l.state(c.slot.withIndex(l.index(c.from, 100), c.slot.valueIndex(c.value)), 100)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("state %d with %s=%s = %d, %v; want %d, %v", c.from, tables.names[c.slot.prop], c.value, got, ok, c.want, c.ok)
		}
	}
}

// TestFindStateMatchesLinear checks FindState against the string-matching
// reference for every block, every state's full property map, and every
// single-property override, an unknown property and an unknown value.
func TestFindStateMatchesLinear(t *testing.T) {
	t.Parallel()
	check := func(name string, over map[string]string) {
		t.Helper()
		got, gotOK := FindState(name, over)
		want, wantOK := findStateLinear(name, over)
		if got != want || gotOK != wantOK {
			t.Fatalf("FindState(%q, %v) = %d, %v; want %d, %v", name, over, got, gotOK, want, wantOK)
		}
	}
	check("minecraft:no_such_block", nil)
	check("minecraft:no_such_block", map[string]string{"facing": "up"})
	for name := range BlockStates {
		check(name, nil)
		check(name, map[string]string{"facing": "up"})
	}
	for name, states := range AllStates {
		check(name, nil)
		check(name, map[string]string{"no_such_property": "true"})
		for _, s := range states {
			check(name, ParseStateProps(s.Props))
		}
		for prop, values := range blockPropertyValues[name] {
			for v := range values {
				check(name, map[string]string{prop: v})
			}
			check(name, map[string]string{prop: "no_such_value"})
			over := ParseStateProps(states[len(states)-1].Props)
			over[prop] = "no_such_value"
			check(name, over)
			over["no_such_property"] = "x"
			check(name, over)
		}
	}
}

// TestWithPropMatchesProps checks WithProp, WithBool and WithInt against the
// property maps for every state, property and value.
func TestWithPropMatchesProps(t *testing.T) {
	t.Parallel()
	for id := range uint16(StateCount) {
		name, props, _ := StateNameProps(id)
		for k, cur := range props {
			p, ok := PropNamed(k)
			if !ok {
				t.Fatalf("PropNamed(%q) found nothing", k)
			}
			if got, ok := WithProp(id, p, "no_such_value"); ok || got != id {
				t.Fatalf("state %d %s=no_such_value = %d, %v", id, k, got, ok)
			}
			for v := range blockPropertyValues[name][k] {
				want := maps.Clone(props)
				want[k] = v
				got, ok := WithProp(id, p, v)
				if !ok || !maps.Equal(StateProps(got), want) || BlockOf(got) != BlockOf(id) {
					t.Fatalf("WithProp(%d, %s, %s) = %d (%v), %v; want %v", id, k, v, got, StateProps(got), ok, want)
				}
				if v == "true" || v == "false" {
					b := v == "true"
					if g, ok := WithBool(id, p, b); !ok || g != got {
						t.Fatalf("WithBool(%d, %s, %v) = %d, %v; want %d", id, k, b, g, ok, got)
					}
				}
				if n, err := strconv.Atoi(v); err == nil {
					if g, ok := WithInt(id, p, n); !ok || g != got {
						t.Fatalf("WithInt(%d, %s, %d) = %d, %v; want %d", id, k, n, g, ok, got)
					}
				}
				if g, ok := WithProps(id, PropValue{p, cur}, PropValue{p, v}); !ok || g != got {
					t.Fatalf("WithProps(%d, %s=%s) = %d, %v; want %d", id, k, v, g, ok, got)
				}
			}
		}
	}
}

func TestWithPropRefuses(t *testing.T) {
	t.Parallel()
	stone := BlockStone.DefaultState()
	if got, ok := WithProp(stone, PropFacing, "north"); ok || got != stone {
		t.Errorf("stone with facing = %d, %v; want %d, false", got, ok, stone)
	}
	if got, ok := WithInt(StateCount, PropPower, 1); ok || got != StateCount {
		t.Errorf("past the last state = %d, %v", got, ok)
	}
	wire := BlockRedstoneWire.DefaultState()
	if got, ok := WithInt(wire, PropPower, 16); ok || got != wire {
		t.Errorf("wire power 16 = %d, %v", got, ok)
	}
	if got, ok := WithInt(wire, PropPower, -1); ok || got != wire {
		t.Errorf("wire power -1 = %d, %v", got, ok)
	}
	if got, ok := WithBool(wire, PropNorth, true); ok || got != wire {
		t.Errorf("wire north=true = %d, %v; north is side/up/none", got, ok)
	}
	if got, ok := WithProps(wire, PropValue{PropPower, "3"}, PropValue{PropLit, "true"}); ok || got != wire {
		t.Errorf("wire with lit = %d, %v", got, ok)
	}
	want, _ := FindState("minecraft:redstone_wire", map[string]string{"power": "3", "east": "up"})
	if got, ok := WithProps(wire, PropValue{PropPower, "3"}, PropValue{PropEast, "up"}); !ok || got != want {
		t.Errorf("wire power 3 east up = %d, %v; want %d", got, ok, want)
	}
	if got := PropFacing.Name(); got != "facing" {
		t.Errorf("PropFacing.Name() = %q", got)
	}
	if _, ok := PropNamed("no_such_property"); ok {
		t.Error("PropNamed found a property no block has")
	}
}

// TestWithPropertiesOfMatchesFindState checks WithPropertiesOf against
// FindState with the source state's properties: every state carried onto its
// own block, and each weathering copper state onto its next stage.
func TestWithPropertiesOfMatchesFindState(t *testing.T) {
	t.Parallel()
	for s := range uint16(StateCount) {
		if got, ok := WithPropertiesOf(BlockOf(s), s); !ok || got != s {
			t.Fatalf("WithPropertiesOf(%s, %d) = %d, %v; want the state itself", BlockOf(s).Name(), s, got, ok)
		}
	}
	for more, less := range CopperScraped {
		to, ok := BlockByName(more)
		if !ok {
			t.Fatalf("no block %s", more)
		}
		for _, s := range AllStates[less] {
			want, _ := FindState(more, ParseStateProps(s.Props))
			if got, ok := WithPropertiesOf(to, s.ID); !ok || got != want {
				t.Errorf("WithPropertiesOf(%s, %s[%s]) = %d, %v; want %d", more, less, s.Props, got, ok, want)
			}
		}
	}
	stairs := BlockOakStairs.DefaultState()
	if got, ok := WithPropertiesOf(BlockOakStairs, BlockStone.DefaultState()); !ok || got != stairs {
		t.Errorf("stairs from stone = %d, %v; want the default stairs %d", got, ok, stairs)
	}
	if _, ok := WithPropertiesOf(BlockRedstoneWire, MustState("minecraft:oak_fence", map[string]string{"north": "true"})); ok {
		t.Error("the wire took a fence's north=true as a value of its north side")
	}
}
