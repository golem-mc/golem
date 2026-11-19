package server

import "testing"

func TestSpawnDiceKeepsSequence(t *testing.T) {
	s := &Server{}
	s.seedDice(6459)
	locked := newDice(s.seedFor("spawn", 0))
	for _, seed := range []int64{s.seedFor("spawn", 0), 1234} {
		s.spawnRng.Seed(seed)
		locked.Seed(seed)
		for i := range 1024 {
			if got, want := s.spawnRng.Intn(i+1), locked.Intn(i+1); got != want {
				t.Fatalf("draw %d: Intn = %d, want %d", i, got, want)
			}
			if got, want := s.spawnRng.Float32(), locked.Float32(); got != want {
				t.Fatalf("draw %d: Float32 = %v, want %v", i, got, want)
			}
			if got, want := s.spawnRng.Float64(), locked.Float64(); got != want {
				t.Fatalf("draw %d: Float64 = %v, want %v", i, got, want)
			}
			if got, want := s.spawnRng.Int63(), locked.Int63(); got != want {
				t.Fatalf("draw %d: Int63 = %d, want %d", i, got, want)
			}
		}
	}
}
