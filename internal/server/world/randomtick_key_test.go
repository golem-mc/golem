package world

import (
	"cmp"
	"math/rand"
	"slices"
	"testing"
)

func TestChunkKeyOrderMatchesXThenZ(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	pos := make([]ChunkPos, 2000)
	for i := range pos {
		pos[i] = ChunkPos{X: int32(rng.Intn(400) - 200), Z: int32(rng.Intn(400) - 200)}
	}
	pos[0] = ChunkPos{X: -1 << 31, Z: 1<<31 - 1}
	pos[1] = ChunkPos{X: 1<<31 - 1, Z: -1 << 31}
	want := slices.Clone(pos)
	slices.SortFunc(want, func(a, b ChunkPos) int {
		return cmp.Or(cmp.Compare(a.X, b.X), cmp.Compare(a.Z, b.Z))
	})
	keys := make([]uint64, len(pos))
	for i, c := range pos {
		keys[i] = packChunkKey(c)
	}
	slices.Sort(keys)
	for i, k := range keys {
		if got := unpackChunkKey(k); got != want[i] {
			t.Fatalf("index %d: got %v, want %v", i, got, want[i])
		}
	}
}
