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

// StateProviderPicker picks a state for a cell, reading w's live blocks for
// the providers that look at the world. It is safe for concurrent use.
type StateProviderPicker func(w *World, x, y, z int32) uint16

// NewStateProviderPicker compiles a BlockStateProvider. The draws come from a
// stream seeded by seed.
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
	return func(w *World, x, y, z int32) uint16 {
		mu.Lock()
		defer mu.Unlock()
		region.live, region.minY, region.height = w, w.MinY(), w.Height()
		return provider(region, [3]int32{x, y, z})
	}, nil
}

// FindSpawnCalls is how many overworld spawn searches have run.
func FindSpawnCalls() int64 { return findSpawnCalls.Load() }
