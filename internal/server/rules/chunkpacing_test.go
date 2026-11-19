package rules_test

import (
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
