package world

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func sulfurGrowthSeed(t *testing.T, down bool) int64 {
	t.Helper()
	for seed := range int64(10000) {
		r := rand.New(rand.NewSource(seed))
		if r.Float32() < dripstoneGrowthChance && (r.Intn(2) == 0) == down {
			return seed
		}
	}
	t.Fatal("no growth seed")
	return 0
}

// vanilla: SpeleothemBlock.findTip, SulfurSpikeBlock.getMaxGrowthLength
func TestSulfurSpikeGrowthLimit(t *testing.T) {
	w, p := newPhysicsWorld(t)
	const y = int32(70)
	tip := speleothemState(t, "minecraft:sulfur_spike", "down", "tip")
	w.SetBlock(0, y+1, 0, registry.BlockSulfur.DefaultState())
	w.SetBlock(0, y, 0, tip)
	seed := sulfurGrowthSeed(t, true)
	for tick := range 5 {
		p.rng = rand.New(rand.NewSource(seed))
		var changes []BlockChange
		var decays []Decay
		state := w.BlockAt(0, y, 0)
		if !randomlyTicking(state) {
			t.Fatal("sulfur spike is not random ticking")
		}
		p.randomVisit(0, y, 0, state, &changes, &decays)
		// The search visits offsets strictly below 2, then grows one more
		// block: vanilla allows three cells, not a two-cell total height.
		length := min(tick+2, 3)
		if got := w.BlockAt(0, y-int32(length)+1, 0); got != tip {
			t.Fatalf("tick %d: last tip = %d, want %d", tick, got, tip)
		}
		if got := w.BlockAt(0, y-int32(length), 0); got != airState {
			t.Fatalf("tick %d: grew beyond length %d: %d", tick, length, got)
		}
	}
}

func TestSulfurSpikeStalagmiteGrowth(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, down := range []bool{false, true} {
			if down && !existing {
				continue
			}
			t.Run(map[bool]string{false: "floor", true: "tip"}[existing]+map[bool]string{false: "up", true: "merge"}[down], func(t *testing.T) {
				w, p := newPhysicsWorld(t)
				tip := speleothemState(t, "minecraft:sulfur_spike", "down", "tip")
				w.SetBlock(0, 71, 0, registry.BlockSulfur.DefaultState())
				w.SetBlock(0, 70, 0, tip)
				floor := int32(66)
				if down {
					floor = 68
				}
				w.SetBlock(0, floor, 0, registry.BlockStone.DefaultState())
				if existing {
					w.SetBlock(0, floor+1, 0, speleothemState(t, "minecraft:sulfur_spike", "up", "tip"))
				}
				p.rng = rand.New(rand.NewSource(sulfurGrowthSeed(t, down)))
				var changes []BlockChange
				p.speleothemRandomTick(0, 70, 0, tip, &changes)
				target := floor + 1
				if existing && !down {
					target++
				}
				thickness := "tip"
				if down {
					thickness = "tip_merge"
				}
				want := speleothemState(t, "minecraft:sulfur_spike", "up", thickness)
				if got := w.BlockAt(0, target, 0); got != want {
					t.Fatalf("grown stalagmite = %d, want %d", got, want)
				}
				if down {
					want = speleothemState(t, "minecraft:sulfur_spike", "down", "tip_merge")
					if got := w.BlockAt(0, 70, 0); got != want {
						t.Fatalf("merged stalactite = %d, want %d", got, want)
					}
				}
			})
		}
	}
}
