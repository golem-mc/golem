package main

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

func TestDecorationOrder(t *testing.T) {
	for _, tc := range []struct {
		name     string
		recorded [][2]int32
		want     []world.ChunkPos
	}{
		{name: "missing recording"},
		{name: "empty recording", recorded: [][2]int32{}},
		{
			name:     "interleaved workers",
			recorded: [][2]int32{{-12, -4}, {-12, -5}, {-12, -3}},
			want:     []world.ChunkPos{{X: -12, Z: -4}, {X: -12, Z: -5}, {X: -12, Z: -3}},
		},
		{
			name:     "holes are replayed verbatim",
			recorded: [][2]int32{{-2, 0}, {-3, -2}, {-1, -2}, {-1, 0}},
			want:     []world.ChunkPos{{X: -2, Z: 0}, {X: -3, Z: -2}, {X: -1, Z: -2}, {X: -1, Z: 0}},
		},
		{
			name:     "duplicate task kept for generator to skip",
			recorded: [][2]int32{{5, 6}, {5, 6}},
			want:     []world.ChunkPos{{X: 5, Z: 6}, {X: 5, Z: 6}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := decorationOrder(tc.recorded); !slices.Equal(got, tc.want) {
				t.Fatalf("decoration order = %v, want %v", got, tc.want)
			}
		})
	}
}
