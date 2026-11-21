package entity

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

func filed(ix *ChunkIndex[*Mob], pos world.ChunkPos) []*Mob {
	return slices.Collect(ix.In(pos))
}

func TestChunkIndex(t *testing.T) {
	var a, b ChunkIndex[*Mob]
	m, other := &Mob{}, &Mob{}
	m.SetPos(15.99, 80, 3)
	other.SetPos(1, 80, 1)
	a.Add(m, &m.Filing, ChunkAt(m.X, m.Z))
	a.Add(m, &m.Filing, ChunkAt(m.X, m.Z))
	a.Add(other, &other.Filing, ChunkAt(other.X, other.Z))
	c0, c1 := world.ChunkPos{}, world.ChunkPos{X: 1}
	if len(filed(&a, c0)) != 2 || a.Len() != 2 {
		t.Fatalf("chunk 0 = %d, want 2", len(filed(&a, c0)))
	}
	m.SetPos(16, 80, 3)
	if got := filed(&a, c0); len(got) != 1 || got[0] != other || len(filed(&a, c1)) != 1 || !a.Holds(&m.Filing, c1) {
		t.Fatal("SetPos across the border did not re-file")
	}
	m.SetPos(-0.01, 80, 3)
	if got := filed(&a, world.ChunkPos{X: -1}); len(got) != 1 {
		t.Fatalf("x=-0.01 not in chunk -1: %v", got)
	}
	if len(filed(&a, c1)) != 0 || len(a.cells) != 2 {
		t.Fatalf("empty cell not dropped: %d cells", len(a.cells))
	}
	m.Unfile()
	if len(filed(&a, world.ChunkPos{X: -1})) != 0 || a.Holds(&m.Filing, world.ChunkPos{X: -1}) {
		t.Fatal("Unfile left the mob filed")
	}
	a.Add(other, &other.Filing, c0)
	b.Add(other, &other.Filing, c0)
	if a.Len() != 0 || len(filed(&b, c0)) != 1 {
		t.Fatal("Add to a second index did not leave the first")
	}
}

func TestChunkIndexSwapFixup(t *testing.T) {
	var ix ChunkIndex[*Mob]
	ms := []*Mob{{}, {}, {}}
	for _, m := range ms {
		ix.Add(m, &m.Filing, world.ChunkPos{})
	}
	ms[0].Unfile()
	for _, m := range ms[1:] {
		if !ix.Holds(&m.Filing, world.ChunkPos{}) {
			t.Fatal("swap left a stale slot")
		}
	}
	ms[2].SetPos(40, 0, 0)
	ms[1].Unfile()
	if ix.Len() != 1 || !ix.Holds(&ms[2].Filing, world.ChunkPos{X: 2}) {
		t.Fatal("remove after move broke the index")
	}
}

func TestChunkIndexStructCopy(t *testing.T) {
	var ix ChunkIndex[*Mob]
	m := &Mob{}
	ix.Add(m, &m.Filing, world.ChunkPos{})
	cp := *m
	cp.SetPos(40, 0, 0)
	if !ix.Holds(&m.Filing, world.ChunkPos{}) || ix.Len() != 1 {
		t.Fatal("a copy's SetPos moved the original")
	}
	cp.Unfile()
	if !ix.Holds(&m.Filing, world.ChunkPos{}) || ix.Len() != 1 {
		t.Fatal("a copy's Unfile removed the original")
	}
}
