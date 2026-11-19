package world

import (
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

func TestRandomChancePlacementRoundsToFloatAndUsesStrictCutoff(t *testing.T) {
	const seed = 12345
	reference := levelgen.NewWorldgenRandom(seed)
	draw := reference.NextFloat()
	wantNext := reference.NextLong()
	for _, tc := range []struct {
		chance float64
		accept bool
	}{{0, false}, {1, true}, {float64(draw), false},
		{math.Nextafter(float64(draw), math.Inf(1)), false},
		{float64(math.Nextafter32(draw, float32(math.Inf(1)))), true}} {
		r := &decorRegion{rng: levelgen.NewWorldgenRandom(seed)}
		mod := compileMod(parseJSON(fmt.Sprintf(`{"type":"random_chance","chance":%.17g}`, tc.chance)), 0, false)
		accepted := false
		r.walk([]placementMod{mod}, [3]int32{1, 2, 3}, func(_ *decorRegion, pos [3]int32) {
			accepted = true
			if pos != [3]int32{1, 2, 3} {
				t.Fatalf("random_chance moved the position: %v", pos)
			}
		})
		if accepted != tc.accept || r.rng.NextLong() != wantNext {
			t.Fatalf("chance %.17g: accepted=%v, want %v with exactly one draw", tc.chance, accepted, tc.accept)
		}
	}
}

func TestFlatFeatureHeightAnchors(t *testing.T) {
	g := NewFlatOverworldGeneratorFor(0, `{"layers":[{"block":"bedrock","height":1}]}`)
	r := newDecorRegion(g, newChunk(0, 0), worldMinY, worldHeight)
	for _, tc := range []struct {
		anchor string
		want   int32
	}{{`{"above_bottom":4}`, 4}, {`{"below_top":4}`, 379}, {`{"absolute":-61}`, -61}} {
		for _, kind := range []string{"uniform", "trapezoid"} {
			mod := compileMod(parseJSON(fmt.Sprintf(`{"type":"height_range","height":{"type":%q,"min_inclusive":%s,"max_inclusive":%s}}`, kind, tc.anchor, tc.anchor)), 0, true)
			r.walk([]placementMod{mod}, [3]int32{7, 0, 9}, func(_ *decorRegion, p [3]int32) {
				if p != [3]int32{7, tc.want, 9} {
					t.Errorf("%s %s: got %v, want y=%d", kind, tc.anchor, p, tc.want)
				}
			})
		}
	}
}

func TestPlacedFeatureTableCompiles(t *testing.T) {
	data, err := os.ReadFile("levelgen/placed_features.txt")
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		name, doc, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		t.Run(name, func(t *testing.T) {
			defer func() {
				if err := recover(); err != nil {
					t.Fatalf("compilePlaced: %v", err)
				}
			}()
			compilePlaced(parseJSON(doc), 0, true)
		})
	}
}

func TestSequenceStopsOnFailureWithoutUndoingEarlierChildren(t *testing.T) {
	world := newGoldenTerrain()
	r := newDecorRegion(world, newChunk(0, 0), worldMinY, worldHeight)
	place := compileFeature(parseJSON(`{"type":"sequence","config":{"features":[
		{"feature":{"type":"simple_block","config":{"to_place":{"type":"simple_state_provider","state":{"Name":"minecraft:stone"}}}},"placement":[]},
		{"feature":{"type":"simple_block","config":{"to_place":{"type":"simple_state_provider","state":{"Name":"minecraft:dirt"}}}},"placement":[{"type":"block_predicate_filter","predicate":{"type":"matching_blocks","blocks":"minecraft:water"}}]},
		{"feature":{"type":"simple_block","config":{"to_place":{"type":"simple_state_provider","state":{"Name":"minecraft:gold_block"}}}},"placement":[]}
	]}}`))
	if place(r, [3]int32{8, 80, 8}) {
		t.Fatal("sequence succeeded despite its failed second child")
	}
	if got := r.get(8, 80, 8); got != registry.BlockStone.DefaultState() {
		t.Fatalf("first child's write was lost or third child ran: got %s", registry.BlockOf(got).Name())
	}
}

func TestInsideWorldBoundsPredicateUsesOffsetAndBuildHeight(t *testing.T) {
	r := &decorRegion{minY: -64, height: 384}
	pred := compilePred(parseJSON(`{"type":"inside_world_bounds","offset":[16,-5,-16]}`))
	for _, tc := range []struct {
		y    int32
		want bool
	}{{-60, false}, {-59, true}, {324, true}, {325, false}} {
		if got := pred(r, [3]int32{30_000_000, tc.y, -30_000_000}); got != tc.want {
			t.Errorf("y=%d: got %v, want %v", tc.y, got, tc.want)
		}
	}
}

func TestWeightedSelectorWithZeroTotalWeightFails(t *testing.T) {
	for _, doc := range []string{
		`{"type":"weighted_random_selector","config":{"features":[]}}`,
		`{"type":"weighted_random_selector","config":{"features":[{"data":{"feature":{"type":"simple_block","config":{"to_place":{"type":"simple_state_provider","state":{"Name":"minecraft:stone"}}}},"placement":[]},"weight":0}]}}`,
	} {
		if compileFeature(parseJSON(doc))(nil, [3]int32{}) {
			t.Fatal("empty weighted selection succeeded")
		}
	}
}

func TestOverlayContinuesAfterFailedChild(t *testing.T) {
	world := newGoldenTerrain()
	r := newDecorRegion(world, newChunk(0, 0), worldMinY, worldHeight)
	place := compileFeature(parseJSON(`{"type":"overlay","features":[
 {"feature":{"type":"simple_block","to_place":{"id":"minecraft:stone"}},"placement":[{"type":"offset","x":1,"y":0,"z":0}]},
 {"feature":{"type":"simple_block","to_place":{"id":"minecraft:dirt"}},"placement":[{"type":"block_predicate_filter","predicate":{"type":"matching_blocks","blocks":"minecraft:water"}}]},
 {"feature":{"type":"simple_block","to_place":{"id":"minecraft:gold_block"}},"placement":[]}
 ]}`))
	if !place(r, [3]int32{8, 80, 8}) {
		t.Fatal("overlay reported failure despite successful children")
	}
	if got := r.get(9, 80, 8); got != registry.BlockStone.DefaultState() {
		t.Fatalf("first child's write: got %s", registry.BlockOf(got).Name())
	}
	if got := r.get(8, 80, 8); got != registry.BlockGoldBlock.DefaultState() {
		t.Fatalf("third child's write: got %s", registry.BlockOf(got).Name())
	}
}
