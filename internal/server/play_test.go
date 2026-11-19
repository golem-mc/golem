package server

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

func TestChunkOfRoundsDown(t *testing.T) {
	t.Parallel()
	// A block coordinate belongs to the chunk it sits in, which for negative
	// coordinates is not the one a truncating conversion would pick.
	for _, tc := range []struct {
		coordinate float64
		want       int32
	}{
		{0, 0},
		{0.5, 0},
		{15.9, 0},
		{16, 1},
		{-0.5, -1},
		{-1, -1},
		{-16, -1},
		{-16.5, -2},
		{-20.5, -2},
	} {
		if got := world.ChunkOf(tc.coordinate); got != tc.want {
			t.Errorf("ChunkOf(%v) = %d, want %d", tc.coordinate, got, tc.want)
		}
	}
}

func TestInWorldRejectsUnusablePositions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		x, y, z float64
		want    bool
	}{
		{"spawn", 0.5, float64(world.GroundLevel), 0.5, true},
		{"far but legal", maxPositionXZ, maxPositionY, -maxPositionXZ, true},
		{"nan", math.NaN(), 0, 0, false},
		{"infinity", 0, math.Inf(1), 0, false},
		// Finite but far outside int32, which chunkOf could not convert.
		{"astronomical", 1e300, 0, 0, false},
		{"past the border", 0, 0, maxPositionXZ + 1, false},
	} {
		if got := inWorld(tc.x, tc.y, tc.z); got != tc.want {
			t.Errorf("%s: inWorld(%v, %v, %v) = %v, want %v", tc.name, tc.x, tc.y, tc.z, got, tc.want)
		}
	}
}
