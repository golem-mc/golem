package rules

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestPlacementShapeDrawsHeadAgeFromLevelRandom(t *testing.T) {
	for _, body := range []registry.Block{registry.BlockKelpPlant, registry.BlockCaveVinesPlant, registry.BlockTwistingVinesPlant, registry.BlockWeepingVinesPlant} {
		t.Run(body.Name(), func(t *testing.T) {
			rng, expected := rand.New(rand.NewSource(42)), rand.New(rand.NewSource(42))
			for range 64 {
				got := NeighborShapedState(testWorld{}, rng, body.DefaultState(), 0, 80, 0)
				age, ok := registry.StateInt(got, registry.PropAge)
				if want := expected.Intn(25); !ok || age != want {
					t.Fatalf("age = %d, present=%v; want level random draw %d", age, ok, want)
				}
			}
		})
	}
}
