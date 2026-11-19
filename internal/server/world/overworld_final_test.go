package world

import "testing"

// TestOverworldChunkFinalWhenAcquired holds an acquired overworld chunk's
// blocks unchanged by the later acquisition of its eight neighbours: a chunk
// is handed out only once every neighbour's features have landed in it.
// Acquiring the 3x3 is enough because only chunks within radius 1 write into
// the centre.
//
// vanilla: ChunkPyramid.GENERATION_PYRAMID (SPAWN after radius-1 FEATURES)
func TestOverworldChunkFinalWhenAcquired(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewOverworldGenerator(0))
	// A forest chunk whose trees depend on the order its neighbours decorate.
	centre := ChunkPos{-2, -3}
	minY, maxY := w.MinY(), w.MinY()+w.Height()
	snapshot := func() []uint16 {
		blocks := make([]uint16, 0, 16*16*int(w.Height()))
		for y := minY; y < maxY; y++ {
			for z := range int32(16) {
				for x := range int32(16) {
					blocks = append(blocks, w.BlockAt(centre.X*16+x, y, centre.Z*16+z))
				}
			}
		}
		return blocks
	}

	w.Acquire(centre)
	before := snapshot()
	var neighbours []ChunkPos
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			if dx != 0 || dz != 0 {
				neighbours = append(neighbours, ChunkPos{centre.X + dx, centre.Z + dz})
			}
		}
	}
	for _, pos := range neighbours {
		w.Acquire(pos)
	}
	after := snapshot()

	diffs := 0
	for i := range before {
		if before[i] == after[i] {
			continue
		}
		diffs++
		if diffs <= 5 {
			x, z := int32(i%16), int32(i/16%16)
			y := minY + int32(i/256)
			t.Errorf("block (%d,%d,%d) changed from %d to %d after the neighbours were acquired",
				centre.X*16+x, y, centre.Z*16+z, before[i], after[i])
		}
	}
	if diffs > 5 {
		t.Errorf("%d blocks changed in all", diffs)
	}
	for _, pos := range neighbours {
		w.Release(pos)
	}
	w.Release(centre)
}
