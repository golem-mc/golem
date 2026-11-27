package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
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

// vanilla: LivingEntity.getDimensions, EntityDimensions.scale
func TestMannequinFixedPosesIgnoreScale(t *testing.T) {
	m := NewMannequin(1, 0, 0, 0)
	m.Attributes.SetBase(registry.AttributeScale, 2)
	for _, pose := range []int32{protocol.PoseSleeping, protocol.PoseDying} {
		m.Pose = pose
		if w, h := m.Dimensions(); w != 0.2 || h != 0.2 {
			t.Errorf("pose %d scaled box = %v x %v, want 0.2 x 0.2", pose, w, h)
		}
	}
	m.Pose = protocol.PoseStanding
	if _, h := m.Dimensions(); h != float64(float32(MannequinHeight)*2) {
		t.Errorf("standing height = %v", h)
	}
}
