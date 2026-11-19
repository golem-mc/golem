package entity

import (
	"math"
	"testing"
)

func TestRandomLookAroundCoversFullCircleAtEyeLevel(t *testing.T) {
	var widest float64
	for seed := int64(1); seed <= 500; seed++ {
		cow := NewMob(TypeCow, 1, 0.5, 66, 0.5, seed)
		g := &randomLookAround{m: cow}
		g.Start(nil)
		d := math.Abs(math.Mod(yawToward(g.relX, g.relZ)-float64(cow.HeadYaw)+540, 360) - 180)
		widest = math.Max(widest, d)
	}
	if widest <= 90 {
		t.Errorf("widest yaw turn = %v, want more than 90", widest)
	}
}
