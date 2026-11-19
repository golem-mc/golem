package rules_test

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
)

func TestChunkPacerHoldsAtOneBatchUntilAcknowledged(t *testing.T) {
	var p rules.ChunkPacer
	p.Start()
	if got := p.Take(100); got != 9 {
		t.Fatalf("first batch = %d, want the start rate 9", got)
	}
	if got := p.Take(100); got != 0 {
		t.Fatalf("second batch before any answer = %d, want 0", got)
	}
}

func TestChunkPacerStartKeepsAStartedPacer(t *testing.T) {
	var p rules.ChunkPacer
	p.Start()
	p.Take(100)
	p.Acknowledge(30)
	p.Start()
	if got := p.Take(100); got != 30 {
		t.Fatalf("batch after a restart = %d, want the named rate 30", got)
	}
}

func TestChunkPacerCapsBatchesInFlightAtTenAfterAnAnswer(t *testing.T) {
	var p rules.ChunkPacer
	p.Start()
	p.Take(100)
	p.Acknowledge(1)
	sent := 0
	for range 20 {
		if p.Take(100) > 0 {
			sent++
		}
	}
	if sent != 10 {
		t.Fatalf("batches in flight = %d, want 10", sent)
	}
}

func TestChunkPacerClampsTheNamedRate(t *testing.T) {
	for _, tc := range []struct {
		rate float32
		want int
	}{
		{1000, 64},
		{float32(math.NaN()), 1},
		{-5, 1},
	} {
		var p rules.ChunkPacer
		p.Start()
		p.Take(100)
		p.Acknowledge(tc.rate)
		if got := p.Take(1000); got != tc.want {
			t.Errorf("rate %v: batch = %d, want %d", tc.rate, got, tc.want)
		}
	}
}

func TestChunkPacerSendsNothingWithoutPendingChunks(t *testing.T) {
	var p rules.ChunkPacer
	p.Start()
	if got := p.Take(0); got != 0 {
		t.Fatalf("batch of an empty queue = %d, want 0", got)
	}
	if got := p.Take(3); got != 3 {
		t.Fatalf("batch of three pending = %d, want 3 and no charge for the empty one", got)
	}
}
