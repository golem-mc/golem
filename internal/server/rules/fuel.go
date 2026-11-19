package rules

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/registry"
)

// ResolveFuel evaluates a stack's own cooking_fuel or brewing_fuel against the
// block entity's block: the burn time or uses (0 when the provider is missing
// or throws) and the speed multiplier (1 likewise).
// vanilla: ResolvableInt.get, ResolvableFloat.get
func ResolveFuel(count, speed registry.LootNumber, state uint16) (int32, float32) {
	ctx := &LootContext{BlockState: state, HasBlock: true}
	rng := rand.New(rand.NewSource(rand.Int63()))
	n, err := EvalIntProvider(count, ctx, rng)
	if err != nil {
		n = 0
	}
	s, err := EvalFloatProvider(speed, ctx, rng)
	if err != nil || math.IsNaN(float64(s)) || math.IsInf(float64(s), 0) {
		s = 1
	}
	return n, s
}
