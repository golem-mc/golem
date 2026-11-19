package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

func mushroomRegion() *decorRegion {
	g := NewNetherGenerator(0)
	r := &decorRegion{src: g, g: g, minY: 0, height: netherGenHeight, center: newChunk(0, 0), rng: levelgen.NewWorldgenRandom(0)}
	empty := &shapedTerrain{}
	for i := range empty.fill {
		empty.fill[i] = airState
	}
	for i := range r.near {
		r.near[i] = empty
	}
	return r
}

func TestNetherMushroomFullSurvival(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		support  registry.Block
		source   registry.Block
		distance int32
		keep     bool
	}{
		{"light13", registry.BlockNetherrack, registry.BlockGlowstone, 2, false},
		{"light12", registry.BlockNetherrack, registry.BlockTorch, 2, true},
		{"override", registry.BlockCrimsonNylium, registry.BlockGlowstone, 1, true},
		{"missing support", registry.BlockAir, registry.BlockAir, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := mushroomRegion()
			r.set(5, 99, 5, tc.support.DefaultState())
			r.set(5+tc.distance, 100, 5, tc.source.DefaultState())
			r.set(5, 100, 5, r.g.redMushroomState)
			r.markPost(5, 100, 5)
			r.reconnectShapes(r.protoAt(5, 5))
			if got := r.get(5, 100, 5) != airState; got != tc.keep {
				t.Fatalf("survives=%v, want %v", got, tc.keep)
			}
		})
	}
}
