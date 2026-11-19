package world

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestConfiguredGrowerRefusalPreservesSaplings(t *testing.T) {
	for _, tc := range []struct {
		name          string
		block         registry.Block
		square, water bool
	}{
		{"single", registry.BlockCherrySapling, false, false},
		{"mega", registry.BlockDarkOakSapling, true, false},
		{"waterlogged", registry.BlockMangrovePropagule, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, p := newPhysicsWorld(t)
			p.rng = rand.New(rand.NewSource(21))
			state, _ := registry.WithInt(tc.block.DefaultState(), registry.PropStage, 1)
			if tc.water {
				state, _ = registry.WithBool(state, registry.PropWaterlogged, true)
			}
			side := int32(1)
			if tc.square {
				side = 2
			}
			for dx := range side {
				for dz := range side {
					w.SetBlock(5+dx, 63, dz, registry.BlockDirt.DefaultState())
					w.SetBlock(5+dx, 64, dz, state)
				}
			}
			// A solid roof prevents even mangrove's raised trunk and roots.
			for x := int32(-2); x <= 12; x++ {
				for z := int32(-7); z <= 8; z++ {
					w.SetBlock(x, 65, z, registry.BlockStone.DefaultState())
				}
			}
			var changes []BlockChange
			// Start from the last corner to exercise both negative offsets.
			p.advanceTree(5+side-1, 64, side-1, state, &changes)
			if len(changes) != 0 {
				t.Fatalf("refused feature published %d changes", len(changes))
			}
			for dx := range side {
				for dz := range side {
					if got := w.BlockAt(5+dx, 64, dz); got != state {
						t.Errorf("sapling changed from %d to %d", state, got)
					}
				}
			}
		})
	}
}

// fixedRoll is an intner whose every draw is n, recording the bounds asked.
type fixedRoll struct {
	n      int
	bounds []int
}

func (r *fixedRoll) Intn(bound int) int {
	r.bounds = append(r.bounds, bound)
	return r.n
}

func TestGrowerWeightedDraws(t *testing.T) {
	for _, tc := range []struct {
		name       string
		block      registry.Block
		mega       bool
		hasFlowers bool
		roll       int
		want       string
		bound      int // 0: no draw
	}{
		{"oak low", registry.BlockOakSapling, false, false, 8, "oak", 10},
		{"oak high", registry.BlockOakSapling, false, false, 9, "fancy_oak", 10},
		{"oak flowers", registry.BlockOakSapling, false, true, 0, "oak_bees_005", 10},
		{"oak flowers high", registry.BlockOakSapling, false, true, 9, "fancy_oak_bees_005", 10},
		{"oak mega", registry.BlockOakSapling, true, false, 0, "", 0},
		{"spruce single", registry.BlockSpruceSapling, false, false, 0, "spruce", 1},
		{"spruce flowers", registry.BlockSpruceSapling, false, true, 0, "spruce", 1},
		{"spruce mega low", registry.BlockSpruceSapling, true, false, 0, "mega_spruce", 2},
		{"spruce mega high", registry.BlockSpruceSapling, true, false, 1, "mega_pine", 2},
		{"mangrove low", registry.BlockMangrovePropagule, false, false, 14, "mangrove", 100},
		{"mangrove high", registry.BlockMangrovePropagule, false, false, 15, "tall_mangrove", 100},
		{"poplar red", registry.BlockPoplarSapling, false, false, 0, "red_poplar", 3},
		{"poplar orange", registry.BlockPoplarSapling, false, false, 1, "orange_poplar", 3},
		{"poplar yellow", registry.BlockPoplarSapling, false, true, 2, "yellow_poplar", 3},
		{"dark oak single", registry.BlockDarkOakSapling, false, false, 0, "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := saplingGrowers[tc.block]
			r := &fixedRoll{n: tc.roll}
			var got string
			if tc.mega {
				got = pick(g.megaTrees, r)
			} else {
				got = g.feature(r, tc.hasFlowers)
			}
			if got != tc.want {
				t.Errorf("drew %q, want %q", got, tc.want)
			}
			var want []int
			if tc.bound != 0 {
				want = []int{tc.bound}
			}
			if len(r.bounds) != len(want) || (len(want) == 1 && r.bounds[0] != want[0]) {
				t.Errorf("draw bounds %v, want %v", r.bounds, want)
			}
		})
	}
}
