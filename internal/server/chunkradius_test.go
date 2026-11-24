package server

import "testing"

func TestChunkRadiusClampsClientDistance(t *testing.T) {
	srv := &Server{}
	srv.viewDistance.Store(4)
	for asked, want := range map[int32]int32{0: 4, 1: 2, 2: 2, 3: 3, 8: 4} {
		c := &client{srv: srv}
		c.viewDistance.Store(asked)
		if got := c.chunkRadius(); got != want {
			t.Errorf("asked %d: radius %d, want %d", asked, got, want)
		}
	}
}
