package world

import (
	"testing"
)

func checkRandomTickCount(t *testing.T, s *section) {
	t.Helper()
	count := 0
	for i := range blocksPerSection {
		if randomlyTicking(s.at(i)) {
			count++
		}
	}
	if count != s.ticking {
		t.Fatalf("count=%d actual=%d", s.ticking, count)
	}
}
