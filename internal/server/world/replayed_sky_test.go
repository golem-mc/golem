package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestReplayedSkyLightKeepsPrimedFloors(t *testing.T) {
	t.Parallel()
	r := overworldMushroomRegion()
	g := r.src.(*OverworldGenerator)
	r.protoAt(0, 0)
	g.BeginLightReplay()
	g.PrimeLightSources(ChunkPos{0, 0})
	leaves, stone := registry.BlockOakLeaves.DefaultState(), registry.BlockStone.DefaultState()
	get := func(x, y, z int32) uint16 {
		switch {
		case x >= 0 && x < 16 && y >= 72 && y <= 74:
			return leaves // written after the column sources were captured
		case x >= 16 && x < 64 && z >= -32 && z < 32 && y == 80:
			return stone // a roof over an unprimed chunk, whose floor is read from get
		}
		return airState
	}
	for _, tc := range []struct {
		x, y, z int32
		want    uint8
	}{
		{5, 67, 5, 15},
		{15, 70, 0, 15},
		{20, 70, 0, 10},
		{40, 70, 0, 0},
	} {
		if got := g.ReplayedSkyLight(get, tc.x, tc.y, tc.z); got != tc.want {
			t.Errorf("(%d,%d,%d): sky light %d, want %d", tc.x, tc.y, tc.z, got, tc.want)
		}
	}
}
