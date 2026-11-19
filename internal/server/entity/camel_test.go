package entity

import "testing"

// TestACamelStandsUpOnlyWhereItsStandingBoxFits: a sitting camel under a
// one-block ceiling may not change pose, and may with the ceiling gone.
// vanilla: Camel.canCamelChangePose, LivingEntity.wouldNotSuffocateAtTargetPose
func TestACamelStandsUpOnlyWhereItsStandingBoxFits(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -4, -4, 4, 4, 63, stone)
	floor(w, -4, -4, 4, 4, 65, stone)
	m := NewMob(TypeCamel, 1, 0.5, 64, 0.5, 1)
	m.OnGround = true
	m.camel.lastPose = -1
	if m.camelCanChangePose(w) {
		t.Error("a sitting camel may stand with a ceiling a block above its feet")
	}
	m.camel.lastPose = 1
	if !m.camelCanChangePose(w) {
		t.Error("a standing camel may not sit with a ceiling its sitting box clears")
	}
	m.camel.lastPose = -1
	floor(w, -4, -4, 4, 4, 65, 0)
	if !m.camelCanChangePose(w) {
		t.Error("a sitting camel may not stand under open sky")
	}
}
