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
