package world

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// A growth that a check refuses spends the transfer and growth draws only;
// the direction draw comes after the checks.
// vanilla: SpeleothemBlock.randomTick
func TestRefusedDripstoneGrowthSpendsNoDirectionDraw(t *testing.T) {
	var seed int64
	for ; ; seed++ {
		r := rand.New(rand.NewSource(seed))
		r.Float32()
		if r.Float32() < dripstoneGrowthChance {
			break
		}
	}
	w, p := newChorusWorld(t, seed)
	tip, ok := registry.FindState("minecraft:pointed_dripstone", map[string]string{
		"vertical_direction": "down", "thickness": "tip", "waterlogged": "false"})
	if !ok {
		t.Fatal("no pointed dripstone state")
	}
	// No dripstone block above the tip, so canGrow refuses.
	w.SetBlock(0, 70, 0, tip)
	p.RandomTickDripstone(0, seed, 0, 70, 0)
	want := rand.New(rand.NewSource(seed))
	want.Float32()
	want.Float32()
	if got, exp := p.rng.Float32(), want.Float32(); got != exp {
		t.Fatalf("next draw = %v, want %v: a refused growth spent the direction draw", got, exp)
	}
}
