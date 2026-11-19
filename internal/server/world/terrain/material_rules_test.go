package terrain

import (
	"slices"
	"testing"
)

func TestMaterialBranchOutcomes(t *testing.T) {
	biomes := &countedMaterialBiomes{names: []string{"plains", "forest"}}
	c := &materialContext{biomes: biomes, possibleBiomes: []int{0}}
	yes, no := isBiome("plains"), isBiome("forest")
	for _, tt := range []struct {
		name  string
		rule  materialRule
		want  Material
		match bool
	}{
		{"empty", sequence(), 0, false},
		{"all false", sequence(ifTrue(no, block(Dirt)), ifTrue(no, block(Stone))), 0, false},
		{"single match", sequence(ifTrue(no, block(Dirt)), ifTrue(yes, block(Stone))), Stone, true},
		{"first match", sequence(ifTrue(no, block(Dirt)), block(Stone), block(Dirt)), Stone, true},
		{"nested false", ifTrue(yes, ifTrue(no, block(Dirt))), 0, false},
		{"nested true", ifTrue(yes, ifTrue(not(no), block(Dirt))), Dirt, true},
		{"invert true", ifTrue(not(yes), block(Dirt)), 0, false},
		{"double invert", ifTrue(not(not(yes)), block(Dirt)), Dirt, true},
		{"unknown name", ifTrue(isBiome("unknown"), block(Dirt)), 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, ok := tt.rule(c)(0, 0, 0)
			if m != tt.want || ok != tt.match {
				t.Fatalf("got %v,%v, want %v,%v", m, ok, tt.want, tt.match)
			}
		})
	}
	if biomes.calls != 0 {
		t.Fatal("known possibilities sampled a biome")
	}
}

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
