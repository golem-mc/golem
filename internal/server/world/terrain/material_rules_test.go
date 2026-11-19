package terrain

import (
	"slices"
	"testing"
)

func TestMaterialRuleOrder(t *testing.T) {
	c := &materialContext{biomes: &countedMaterialBiomes{names: []string{"plains", "forest"}}, possibleBiomes: []int{0}}
	var trace []string
	tracked := func(name string, r materialRule) materialRule {
		return func(c *materialContext) ruleFn { trace = append(trace, "compile "+name); return r(c) }
	}
	dynamic := func(name string, value bool) materialCondition {
		return func(*materialContext) func() bool {
			trace = append(trace, "compile "+name)
			return func() bool { trace = append(trace, "test "+name); return value }
		}
	}
	apply := func(name string, match bool) materialRule {
		return func(*materialContext) ruleFn {
			trace = append(trace, "compile "+name)
			return func(int32, int32, int32) (Material, bool) { trace = append(trace, "apply "+name); return Dirt, match }
		}
	}
	check := func(want ...string) {
		t.Helper()
		if !slices.Equal(trace, want) {
			t.Fatalf("trace %v, want %v", trace, want)
		}
		trace = nil
	}
	f := sequence(
		tracked("dead", ifTrue(isBiome("forest"), tracked("dead child", block(Stone)))),
		ifTrue(dynamic("false", false), apply("skipped", true)),
		ifTrue(dynamic("empty child", true), tracked("empty", sequence())),
		apply("miss", false), apply("hit", true), apply("after hit", true),
	)(c)
	check("compile dead", "compile dead child", "compile false", "compile skipped", "compile empty child", "compile empty", "compile miss", "compile hit", "compile after hit")
	if m, ok := f(0, 0, 0); !ok || m != Dirt {
		t.Fatal("wrong first match")
	}
	check("test false", "test empty child", "apply miss", "apply hit")
	f = ifTrue(func(*materialContext) func() bool {
		trace = append(trace, "compile condition")
		return func() bool { return false }
	}, tracked("child", ifTrue(dynamic("nested", true), apply("unreachable", true))))(c)
	check("compile condition", "compile child", "compile nested", "compile unreachable")
	if _, ok := f(0, 0, 0); ok {
		t.Fatal("false condition matched")
	}
	check()
	inv := not(dynamic("inverted", true))(c)
	check("compile inverted")
	if inv() {
		t.Fatal("inversion failed")
	}
	check("test inverted")
}
