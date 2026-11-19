package entity

import "math/rand"

// ReplaySource is a seeded rand source that counts its draws, so a body's
// stream can be wound back to an earlier point when a held step is undone.
type ReplaySource struct {
	src  rand.Source64
	seed int64
	n    uint64
}

// Rolls is a Rand with the ReplaySource that feeds it.
type Rolls struct {
	*rand.Rand
	Src *ReplaySource
}

// NewRolls returns Rolls seeded with seed; the sequence is the one
// rand.NewSource(seed) gives.
func NewRolls(seed int64) *Rolls {
	s := &ReplaySource{src: rand.NewSource(seed).(rand.Source64), seed: seed}
	return &Rolls{Rand: rand.New(s), Src: s}
}

// Int63 implements rand.Source.
func (s *ReplaySource) Int63() int64 { s.n++; return s.src.Int63() }

// Uint64 implements rand.Source64.
func (s *ReplaySource) Uint64() uint64 { s.n++; return s.src.Uint64() }

// Seed implements rand.Source.
func (s *ReplaySource) Seed(seed int64) { s.src.Seed(seed); s.seed, s.n = seed, 0 }

// Mark is the number of draws so far; 0 on a nil source.
func (s *ReplaySource) Mark() uint64 {
	if s == nil {
		return 0
	}
	return s.n
}

// Rewind winds the stream back to a Mark taken earlier.
func (s *ReplaySource) Rewind(mark uint64) {
	if s == nil || s.n == mark {
		return
	}
	s.src.Seed(s.seed)
	for s.n = 0; s.n < mark; s.n++ {
		s.src.Uint64()
	}
}
