package entity

import (
	"testing"
)

func TestBucketTagRoundTrips(t *testing.T) {
	m := NewMob(TypeCod, 1, 0, 0, 0, 1)
	m.NoAI, m.Silent, m.Health = true, true, 2
	tag := m.BucketTag()
	out := NewMob(TypeCod, 2, 0, 0, 0, 2)
	out.LoadBucketTag(tag)
	if !out.NoAI || !out.Silent || out.Glowing || out.Health != 2 || !out.FromBucket {
		t.Errorf("the released cod = NoAI %v Silent %v Glowing %v Health %v FromBucket %v", out.NoAI, out.Silent, out.Glowing, out.Health, out.FromBucket)
	}
	if out.removeWhenFarAway() {
		t.Error("a bucketed fish despawns when far away")
	}
}

// TestFishDespawnBeyondSixtyFourButASquidDoesNot: the water_ambient category
// despawns at 64 blocks, the water_creature one at 128.
// vanilla: MobCategory.WATER_AMBIENT
func TestFishDespawnBeyondSixtyFourButASquidDoesNot(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	at := func(x float64) []Player { return []Player{survivor(7, x)} }
	if !NewMob(TypeCod, 1, 0.5, 64, 0.5, 3).despawnStep(w, at(100.5)) {
		t.Error("a cod 100 blocks from the player stands, want it discarded past 64")
	}
	if NewMob(TypeCod, 1, 0.5, 64, 0.5, 3).despawnStep(w, at(50.5)) {
		t.Error("a cod 50 blocks from the player was discarded")
	}
	if NewMob(TypeSquid, 2, 0.5, 64, 0.5, 3).despawnStep(w, at(100.5)) {
		t.Error("a squid 100 blocks from the player was discarded, want the 128 of its category")
	}
	if !NewMob(TypeSquid, 2, 0.5, 64, 0.5, 3).despawnStep(w, at(150.5)) {
		t.Error("a squid 150 blocks from the player stands")
	}
}
