package entity

import (
	"math/rand"
	"testing"
)

// TestADirectedOrbPopLeavesAlongTheDirection: whatever the roll, the push
// never points against the direction and the spawn point sits a quarter
// block along it.
// vanilla: ExperienceOrb.<init>(Level, Vec3, Vec3, int)
func TestADirectedOrbPopLeavesAlongTheDirection(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for range 200 {
		vx, vy, vz, ox, oy, oz := DirectedOrbPop(r, 0, 2, 0)
		if vy < 0 || vx*0+vy*2+vz*0 < 0 {
			t.Fatalf("push (%v,%v,%v) points against the direction", vx, vy, vz)
		}
		if ox != 0 || oz != 0 || oy != 0.25 {
			t.Fatalf("offset = (%v,%v,%v), want a quarter block up", ox, oy, oz)
		}
	}
	vx, vy, vz := 0.0, 0.0, 0.0
	for range 200 {
		vx, vy, vz, _, _, _ = DirectedOrbPop(r, 1, 0, 0)
		if vx < 0 {
			t.Fatalf("push (%v,%v,%v) points against +x", vx, vy, vz)
		}
	}
	if _, _, _, ox, oy, oz := DirectedOrbPop(r, 0, 0, 0); ox != 0 || oy != 0 || oz != 0 {
		t.Errorf("zero direction offset = (%v,%v,%v), want none", ox, oy, oz)
	}
}

// TestABottleOEnchantingFallsAtItsOwnGravity pins the 0.07 pull.
// vanilla: ThrownExperienceBottle.getDefaultGravity
func TestABottleOEnchantingFallsAtItsOwnGravity(t *testing.T) {
	if got := ThrownExperienceBottle.Gravity(); got != 0.07 {
		t.Errorf("gravity = %v, want 0.07", got)
	}
}
