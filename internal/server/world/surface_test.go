package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestScanSurfaceMatchesAColumnWalk checks the section-skipping surface scan
// against a walk of every block of every column: over generated ground, over
// an empty chunk, and over lone blocks at the top and the bottom of the world.
func TestScanSurfaceMatchesAColumnWalk(t *testing.T) {
	g := NewOverworldGenerator(5)
	var chunks []*Chunk
	for i := range int32(4) {
		c := newChunk(i*5, -i*3)
		g.Generate(c)
		chunks = append(chunks, c)
	}
	stone := registry.BlockStates["minecraft:stone"]
	sparse := newChunk(0, 0)
	sparse.SetBlock(3, worldMinY+worldHeight-1, 4, stone)
	sparse.SetBlock(9, worldMinY, 2, stone)
	chunks = append(chunks, sparse, newChunk(1, 1))

	const empty = worldMinY - 1
	for i, c := range chunks {
		got := scanSurface(c, empty)
		for lx := range int32(16) {
			for lz := range int32(16) {
				want := int32(empty)
				for y := int32(worldMinY + worldHeight - 1); y >= worldMinY; y-- {
					if c.BlockAt(lx, y, lz) != airState {
						want = y
						break
					}
				}
				if got[lx][lz] != want {
					t.Errorf("chunk %d column (%d, %d): surface %d, want %d", i, lx, lz, got[lx][lz], want)
				}
			}
		}
	}
}
