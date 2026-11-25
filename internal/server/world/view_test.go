package world

import "testing"

func TestInSendViewIncludesNeighborRing(t *testing.T) {
	count := func(radius int32) int {
		n := 0
		for x := int32(-10); x <= 10; x++ {
			for z := int32(-10); z <= 10; z++ {
				if InSendView(ChunkPos{X: x, Z: z}, ChunkPos{}, radius) {
					n++
				}
			}
		}
		return n
	}
	// vanilla: ChunkTrackingView.isWithinDistance with includeNeighbors
	if got := count(2); got != 49 {
		t.Errorf("radius 2 sends %d chunks, want 49", got)
	}
	if !InSendView(ChunkPos{X: 3, Z: 3}, ChunkPos{}, 2) || InSendView(ChunkPos{X: 4, Z: 0}, ChunkPos{}, 2) {
		t.Error("radius 2 must reach the corner (3,3) but not (4,0)")
	}
}
