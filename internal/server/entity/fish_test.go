package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
)

func TestSalmonBoxScalesWithItsSize(t *testing.T) {
	for size, want := range map[int]float64{SalmonSmall: 0.35, SalmonMedium: 0.7, SalmonLarge: 1.05} {
		m := NewMob(TypeSalmon, 1, 0, 0, 0, 1)
		m.SalmonSize = size
		if got := m.BbWidth(); got < want-1e-9 || got > want+1e-9 {
			t.Errorf("size %d salmon is %.3f wide, want %.3f", size, got, want)
		}
	}
}

func TestFinalizedSalmonRollsEverySize(t *testing.T) {
	seen := map[int]int{}
	for seed := range int64(400) {
		m := NewMob(TypeSalmon, 1, 0, 0, 0, seed)
		m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:river"), nil, rules.LocalDifficulty{})
		seen[m.SalmonSize]++
	}
	// 30, 50 and 15 of 95
	if seen[SalmonSmall] < 80 || seen[SalmonMedium] < 120 || seen[SalmonLarge] < 30 || len(seen) != 3 {
		t.Errorf("400 salmon rolled sizes %v, want about 126, 210 and 63", seen)
	}
}

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

// TestANamedOrBucketedFishNeverDespawns: AbstractFish.removeWhenFarAway.
func TestANamedOrBucketedFishNeverDespawns(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	far := []Player{survivor(7, 200.5)}
	named := NewMob(TypeSalmon, 1, 0.5, 64, 0.5, 3)
	named.Named = true
	if named.despawnStep(w, far) {
		t.Error("a named salmon was despawned")
	}
	bucketed := NewMob(TypeCod, 2, 0.5, 64, 0.5, 3)
	bucketed.FromBucket = true
	if bucketed.despawnStep(w, far) {
		t.Error("a bucketed cod was despawned")
	}
}

// TestFishAvoidPaceFollowsTheFledPlayer: the sprint pace is measured against
// the player the goal fled from, wherever the snapshots now sit.
func TestFishAvoidPaceFollowsTheFledPlayer(t *testing.T) {
	m := NewMob(TypeCod, 1, 0, 61, 0, 1)
	g := &fishAvoidGoal{m: m, from: 7}
	near := Player{EntityID: 7, X: 3, Y: 61}
	far := Player{EntityID: 8, X: 30, Y: 61}
	for _, players := range [][]Player{{near, far}, {far, near}} {
		g.Tick(&tickView{players: players})
		if got := m.navOf().nav.speedFactor; got != fishAvoidSprint {
			t.Errorf("pace %v with the fled player 3 blocks away, want %v", got, fishAvoidSprint)
		}
	}
	g.Tick(&tickView{players: []Player{far}})
	if got := m.navOf().nav.speedFactor; got != fishAvoidWalk {
		t.Errorf("pace %v with the fled player gone, want %v", got, fishAvoidWalk)
	}
}
