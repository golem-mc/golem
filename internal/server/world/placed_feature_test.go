package world

import (
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
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
