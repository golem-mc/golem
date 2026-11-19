package metrics

import (
	"testing"
	"time"
)

// TestProcessCPUTimeIsNonDecreasing reads the process CPU time twice; a
// platform with no reading fails, which is what Windows did before it had an
// implementation.
func TestProcessCPUTimeIsNonDecreasing(t *testing.T) {
	first, ok := processCPU()
	if !ok {
		t.Fatal("process CPU time is unavailable")
	}
	start := time.Now()
	for time.Since(start) < 10*time.Millisecond {
		// Burn CPU so the second read has time to see.
	}
	second, ok := processCPU()
	if !ok {
		t.Fatal("process CPU time is unavailable on the second read")
	}
	if second < first {
		t.Errorf("process CPU time went backwards: %d then %d", first, second)
	}
}
