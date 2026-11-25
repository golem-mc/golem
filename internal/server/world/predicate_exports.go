package world

import (
	"errors"
	"sync"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

// DirIndex is the direction index of a face name ("down", "up", "north", "south",
// "west", "east"), as registry.FaceSturdy takes it.
func DirIndex(face string) int { return dirIndex(face) }

// FluidName is the registry name of the fluid a state holds, "minecraft:empty"
// for none.
func FluidName(state uint16) string { return fluidName(state) }

// PlacementSurvives reports whether state could stand at the cell.
// vanilla: BlockState.canSurvive
func PlacementSurvives(w BlockReader, state uint16, x, y, z int32) bool {
	return placementSurvives(w, state, x, y, z)
}

// StateProviderPicker picks a state for a cell. It is safe for concurrent use.
type StateProviderPicker func(x, y, z int32) uint16

// NewStateProviderPicker compiles a BlockStateProvider that reads no world: a
// simple, rotated, weighted, noise, noise_threshold, dual_noise or
// randomized_int provider. The draws come from a stream seeded by seed.
// vanilla: BlockStateProvider.getState
func NewStateProviderPicker(raw any, seed int64) (pick StateProviderPicker, err error) {
	defer func() {
		if r := recover(); r != nil {
			if msg, ok := r.(unported); ok {
				err = errors.New(string(msg))
				return
			}
			panic(r)
		}
	}()
	provider := compileState(raw)
	region := &decorRegion{rng: levelgen.NewWorldgenRandom(seed)}
	var mu sync.Mutex
	return func(x, y, z int32) uint16 {
		mu.Lock()
		defer mu.Unlock()
		return provider(region, [3]int32{x, y, z})
	}, nil
}

// FindSpawnCalls is how many overworld spawn searches have run.
func FindSpawnCalls() int64 { return findSpawnCalls.Load() }
