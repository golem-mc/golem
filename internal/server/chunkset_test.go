package server

import (
	"math/rand/v2"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

func TestChunkSetWrapMoveResizeAndTeleport(t *testing.T) {
	rng := rand.New(rand.NewPCG(6555, 1))
	var set *chunkSet
	want := make(map[world.ChunkPos]bool)
	center := world.ChunkPos{X: -33, Z: -1}
	for step := range 250 {
		radius := int32(rng.IntN(33))
		center.X += int32(rng.IntN(7) - 3)
		center.Z += int32(rng.IntN(7) - 3)
		if step%17 == 0 {
			center.X, center.Z = int32(rng.IntN(4000000)-2000000), int32(rng.IntN(4000000)-2000000)
		}
		for p := range set.all() {
			if !world.InViewOf(p, center, radius) {
				set.delete(p)
				delete(want, p)
			}
		}
		set = set.reframe(center, radius)
		for range 100 {
			p := world.ChunkPos{X: center.X + int32(rng.IntN(int(2*radius+1))) - radius, Z: center.Z + int32(rng.IntN(int(2*radius+1))) - radius}
			set.add(p)
			set.add(p)
			want[p] = true
		}
		if set.len() != len(want) {
			t.Fatalf("step %d: count = %d, want %d", step, set.len(), len(want))
		}
		seen := make(map[world.ChunkPos]bool)
		for p := range set.all() {
			if seen[p] || !want[p] || !set.has(p) {
				t.Fatalf("step %d: unexpected or duplicate position %v", step, p)
			}
			seen[p] = true
		}
		if len(seen) != len(want) {
			t.Fatalf("step %d: iteration lost positions", step)
		}
		for x := center.X - radius - 1; x <= center.X+radius+1; x++ {
			for z := center.Z - radius - 1; z <= center.Z+radius+1; z++ {
				p := world.ChunkPos{X: x, Z: z}
				if set.has(p) != want[p] {
					t.Fatalf("step %d: membership differs at %v", step, p)
				}
			}
		}
		// Coordinates a whole wrap away must never alias an in-window bit.
		for p := range want {
			alias := world.ChunkPos{X: p.X + set.side, Z: p.Z}
			if set.has(alias) {
				t.Fatalf("step %d: wrap alias %v", step, alias)
			}
			set.delete(alias)
		}
		if set.len() != len(want) {
			t.Fatal("out-of-window deletion cleared an in-window bit")
		}
	}
}
