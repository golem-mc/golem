package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestMannequinDyingHasTheSleepingBox checks that a dying mannequin has the
// 0.2 x 0.2 box of a sleeping one, not the standing box.
// vanilla: Avatar.POSES
func TestMannequinDyingHasTheSleepingBox(t *testing.T) {
	w, h := MannequinDimensions(protocol.PoseDying)
	if w != 0.2 || h != 0.2 {
		t.Errorf("dying box = %v x %v, want 0.2 x 0.2", w, h)
	}
}
