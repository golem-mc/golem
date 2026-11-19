package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestGolemLegacySpotAcceptsEveryAirKind(t *testing.T) {
	stone, _ := registry.FindState("minecraft:stone", nil)
	for _, name := range []string{"minecraft:air", "minecraft:cave_air", "minecraft:void_air"} {
		air, ok := registry.FindState(name, nil)
		if !ok || !golemStandsOn(stone, air) {
			t.Errorf("refused %s above stone", name)
		}
	}
}

// A village spawn checks its column even when nearby blocks obstruct its box.
type golemSpawnTestWorld map[[3]int32]uint16

func (w golemSpawnTestWorld) BlockAt(x, y, z int32) uint16 {
	if state, ok := w[[3]int32{x, y, z}]; ok {
		return state
	}
	return registry.BlockAir.DefaultState()
}

func (w golemSpawnTestWorld) InBorder(_, _ int32) bool { return true }

func TestGolemFitsColumn(t *testing.T) {
	for _, tc := range []struct {
		name   string
		blocks map[[3]int32]registry.Block
		want   bool
	}{
		{"beside stone", map[[3]int32]registry.Block{{1, 0, 0}: registry.BlockStone}, true},
		{"water at feet", map[[3]int32]registry.Block{{0, 0, 0}: registry.BlockWater}, true},
		{"water above feet", map[[3]int32]registry.Block{{0, 1, 0}: registry.BlockWater}, false},
		{"water at head", map[[3]int32]registry.Block{{0, 2, 0}: registry.BlockWater}, false},
		{"stone at feet", map[[3]int32]registry.Block{{0, 0, 0}: registry.BlockStone}, false},
		{"stone above feet", map[[3]int32]registry.Block{{0, 1, 0}: registry.BlockStone}, false},
		{"rail at feet", map[[3]int32]registry.Block{{0, 0, 0}: registry.BlockRail}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := golemSpawnTestWorld{{0, -1, 0}: registry.BlockStone.DefaultState()}
			for pos, block := range tc.blocks {
				w[pos] = block.DefaultState()
			}
			if got := golemFits(w, 0, 0, 0); got != tc.want {
				t.Fatalf("golemFits = %v, want %v", got, tc.want)
			}
		})
	}
}
