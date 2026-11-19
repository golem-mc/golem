package server

import (
	"math"
	"testing"
)

// TestBurpPitchRange checks the burp's pitch stays in vanilla's [0.9, 1.0).
func TestBurpPitchRange(t *testing.T) {
	s := &Server{}
	s.seedDice(1)
	for range 1000 {
		if p := s.burpPitch(); p < 0.9 || p >= 1.0 {
			t.Fatalf("burpPitch = %v, want in [0.9, 1.0)", p)
		}
	}
}

// TestBurpPitchFloat32Bounds checks burpPitch equals Mth.randomBetween's
// float32 arithmetic across seeds, including draws where the literal 0.1
// rounds differently.
func TestBurpPitchFloat32Bounds(t *testing.T) {
	lo, hi := float32(0.9), float32(1.0)
	for seed := range int64(50) {
		a, b := &Server{}, &Server{}
		a.seedDice(seed)
		b.seedDice(seed)
		for range 40 {
			want := float32(b.soundRng.Float32()*(hi-lo)) + lo
			if got := a.burpPitch(); math.Float32bits(got) != math.Float32bits(want) {
				t.Fatalf("seed %d: burpPitch = %v, want %v", seed, got, want)
			}
		}
	}
}
