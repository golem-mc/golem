package entity

import "testing"

// TestMagmaCubeKickNeedsLavaBranch: a cube overlapping water and lava takes
// the water jump, which neither kicks nor flags a movement sync.
// vanilla: LivingEntity.jumpInLiquid, MagmaCube.jumpInLiquid
func TestMagmaCubeKickNeedsLavaBranch(t *testing.T) {
	w := newTestWorld()
	m := NewMob(TypeMagmaCube, 1, 0, 64, 0, 1)
	m.waterFluid.height = 0.8
	m.lavaFluid.height = 0.8
	m.jump = true
	m.jumpStep(w)
	if m.Impulse {
		t.Fatal("water-selected jump flagged a movement sync")
	}
	m.waterFluid.height = 0
	m.jumpStep(w)
	if !m.Impulse {
		t.Fatal("lava jump did not flag a movement sync")
	}
}
