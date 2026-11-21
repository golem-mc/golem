package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

func TestChunkIndex(t *testing.T) {
	var a, b ChunkIndex
	m, other := &Mob{}, &Mob{}
	m.SetPos(15.99, 80, 3)
	other.SetPos(1, 80, 1)
	a.Add(m)
	a.Add(m)
	a.Add(other)
	c0, c1 := world.ChunkPos{}, world.ChunkPos{X: 1}
	if len(a.In(c0)) != 2 {
		t.Fatalf("chunk 0 = %d, want 2", len(a.In(c0)))
	}
	m.SetPos(16, 80, 3)
	if len(a.In(c0)) != 1 || a.In(c0)[0] != other || len(a.In(c1)) != 1 || a.In(c1)[0] != m {
		t.Fatal("SetPos across the border did not re-file")
	}
	m.SetPos(-0.01, 80, 3)
	if got := a.In(world.ChunkPos{X: -1}); len(got) != 1 {
		t.Fatalf("x=-0.01 not in chunk -1: %v", got)
	}
	if len(a.In(c1)) != 0 || len(a.cells) != 2 {
		t.Fatalf("empty cell not dropped: %d cells", len(a.cells))
	}
	a.Remove(m)
	if len(a.In(world.ChunkPos{X: -1})) != 0 || m.chunks != nil {
		t.Fatal("Remove left the mob filed")
	}
	a.Add(other)
	b.Add(other)
	if len(a.cells) != 0 || len(b.In(c0)) != 1 {
		t.Fatal("Add to a second index did not leave the first")
	}
	other.X = 40
	if n := b.Resync(); n != 1 || len(b.In(world.ChunkPos{X: 2})) != 1 || len(b.In(c0)) != 0 {
		t.Fatalf("Resync moved %d", n)
	}
}
