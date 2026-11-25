package entity

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/google/uuid"
)

func filed(ix *ChunkIndex[*Mob], pos world.ChunkPos) []*Mob {
	return slices.Collect(ix.In(pos))
}

func checkChunkIndexUUID(t *testing.T, ix *ChunkIndex[*Mob], id uuid.UUID, want *Mob) {
	t.Helper()
	if got, ok := ix.ByUUID(id); got != want || ok != (want != nil) {
		t.Errorf("ByUUID(%s) = (%p, %t), want (%p, %t)", id, got, ok, want, want != nil)
	}
}

func TestChunkIndex(t *testing.T) {
	var a, b ChunkIndex[*Mob]
	m, other := &Mob{}, &Mob{}
	mID, otherID := uuid.UUID{1}, uuid.UUID{2}
	checkChunkIndexUUID(t, &a, mID, nil)
	m.SetPos(15.99, 80, 3)
	other.SetPos(1, 80, 1)
	a.Add(m, &m.Filing, ChunkAt(m.X, m.Z), mID)
	a.Add(m, &m.Filing, ChunkAt(m.X, m.Z), mID)
	a.Add(other, &other.Filing, ChunkAt(other.X, other.Z), otherID)
	checkChunkIndexUUID(t, &a, mID, m)
	checkChunkIndexUUID(t, &a, otherID, other)
	c0, c1 := world.ChunkPos{}, world.ChunkPos{X: 1}
	if len(filed(&a, c0)) != 2 || a.Len() != 2 {
		t.Fatalf("chunk 0 = %d, want 2", len(filed(&a, c0)))
	}
	m.SetPos(16, 80, 3)
	if got := filed(&a, c0); len(got) != 1 || got[0] != other || len(filed(&a, c1)) != 1 || !a.Holds(&m.Filing, c1) {
		t.Fatal("SetPos across the border did not re-file")
	}
	checkChunkIndexUUID(t, &a, mID, m)
	checkChunkIndexUUID(t, &a, otherID, other)
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
	checkChunkIndexUUID(t, &a, mID, nil)
	a.Add(other, &other.Filing, c0, otherID)
	b.Add(other, &other.Filing, c0, otherID)
	if a.Len() != 0 || len(filed(&b, c0)) != 1 {
		t.Fatal("Add to a second index did not leave the first")
	}
	checkChunkIndexUUID(t, &a, otherID, nil)
	checkChunkIndexUUID(t, &b, otherID, other)
}

func TestChunkIndexSwapFixup(t *testing.T) {
	var ix ChunkIndex[*Mob]
	ms := []*Mob{{}, {}, {}}
	ids := []uuid.UUID{{1}, {2}, {3}}
	for i, m := range ms {
		ix.Add(m, &m.Filing, world.ChunkPos{}, ids[i])
	}
	ms[0].Unfile()
	checkChunkIndexUUID(t, &ix, ids[0], nil)
	for i := 1; i < len(ms); i++ {
		if !ix.Holds(&ms[i].Filing, world.ChunkPos{}) {
			t.Fatal("swap left a stale slot")
		}
		checkChunkIndexUUID(t, &ix, ids[i], ms[i])
	}
	ms[2].SetPos(40, 0, 0)
	ms[1].Unfile()
	if ix.Len() != 1 || !ix.Holds(&ms[2].Filing, world.ChunkPos{X: 2}) {
		t.Fatal("remove after move broke the index")
	}
	checkChunkIndexUUID(t, &ix, ids[1], nil)
	checkChunkIndexUUID(t, &ix, ids[2], ms[2])
}

func TestChunkIndexChunksInSkipsEmptyRegions(t *testing.T) {
	ix := NewRegionalChunkIndex[*Mob]()
	west, middle, east := world.ChunkPos{X: -1}, world.ChunkPos{}, world.ChunkPos{X: 1}
	check := func(want ...world.ChunkPos) {
		t.Helper()
		if got := slices.Collect(ix.ChunksIn(west, east)); !slices.Equal(got, want) {
			t.Errorf("chunks to probe = %v, want %v", got, want)
		}
	}
	check()
	m, other := &Mob{}, &Mob{}
	ix.Add(m, &m.Filing, west, uuid.UUID{1})
	ix.Add(other, &other.Filing, west, uuid.UUID{2})
	check(west)
	m.Refile(east)
	check(west, middle, east)
	other.Unfile()
	check(middle, east)
	m.Refile(world.ChunkPos{X: 32})
	check()
	otherChunk := world.ChunkPos{X: 33}
	ix.Add(other, &other.Filing, otherChunk, uuid.UUID{2})
	m.Unfile()
	if got := slices.Collect(ix.ChunksIn(otherChunk, otherChunk)); !slices.Equal(got, []world.ChunkPos{otherChunk}) {
		t.Errorf("chunks to probe after removing a neighboring chunk = %v, want %v", got, otherChunk)
	}
	other.Unfile()
	if got := slices.Collect(ix.ChunksIn(otherChunk, otherChunk)); len(got) != 0 {
		t.Errorf("chunks to probe after removing the last chunk = %v, want none", got)
	}
}

func TestChunkIndexStructCopy(t *testing.T) {
	var ix ChunkIndex[*Mob]
	m := &Mob{}
	id := uuid.UUID{1}
	ix.Add(m, &m.Filing, world.ChunkPos{}, id)
	cp := *m
	if ix.Contains(&cp.Filing) || !ix.Contains(&m.Filing) {
		t.Fatal("Contains must recognize the original filing only")
	}
	cp.SetPos(40, 0, 0)
	if !ix.Holds(&m.Filing, world.ChunkPos{}) || ix.Len() != 1 {
		t.Fatal("a copy's SetPos moved the original")
	}
	checkChunkIndexUUID(t, &ix, id, m)
	cp = *m
	cp.Unfile()
	if !ix.Holds(&m.Filing, world.ChunkPos{}) || ix.Len() != 1 {
		t.Fatal("a copy's Unfile removed the original")
	}
	checkChunkIndexUUID(t, &ix, id, m)
}

func TestChunkIndexReplacedUUID(t *testing.T) {
	var ix ChunkIndex[*Mob]
	first, second := &Mob{}, &Mob{}
	id := uuid.UUID{1}
	ix.Add(first, &first.Filing, world.ChunkPos{}, id)
	ix.Add(second, &second.Filing, world.ChunkPos{}, id)
	if !ix.Contains(&first.Filing) || !ix.Contains(&second.Filing) {
		t.Fatal("both filings must be present despite sharing a UUID")
	}
	checkChunkIndexUUID(t, &ix, id, second)
	first.SetPos(40, 0, 0)
	checkChunkIndexUUID(t, &ix, id, second)
	first.Unfile()
	if ix.Contains(&first.Filing) || !ix.Contains(&second.Filing) {
		t.Fatal("Contains must reject the unfiled body and retain the replacement")
	}
	checkChunkIndexUUID(t, &ix, id, second)
	second.Unfile()
	checkChunkIndexUUID(t, &ix, id, nil)
}
