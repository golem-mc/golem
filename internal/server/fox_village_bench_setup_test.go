package server

import "testing"

func TestFoxVillageBenchmarkNavigates(t *testing.T) {
	s, reset, foxes := foxVillageBenchScene(t, 128)
	for range 2 {
		reset()
		for range 3000 {
			s.raidState.tick++
			s.tickMobs()
		}
		for i := range 128 {
			x, z := int32(64+(i/2)%8)*16, int32(64+(i/2)/8)*16
			y := int32(64 + (i%2)*16)
			if d := s.allDims()[0].villageView.SectionsToVillage(x, y, z, 0); d != 0 {
				t.Fatalf("distant section %d is not occupied: distance %d", i, d)
			}
		}
		var x float64
		for _, ent := range foxes {
			x += ent.mob.X - 0.5
		}
		if x/float64(len(foxes)) < 5 {
			t.Fatalf("mean fox movement toward village = %.2f blocks, want at least 5", x/float64(len(foxes)))
		}
	}
}
