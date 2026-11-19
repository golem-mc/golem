package raid

import "testing"

func TestPreparingIsBeforeTheFirstWaveAndDuringItsCountdown(t *testing.T) {
	r := &Raid{groups: map[int][]Raider{}}
	if !r.Preparing() {
		t.Fatal("raid before first wave is not preparing")
	}
	r.groupsSpawned = 1
	r.groups[1] = []Raider{&fakeRaider{alive: true}}
	r.cooldown = 300
	if r.Preparing() {
		t.Fatal("live wave is preparing")
	}
	r.groups[1] = nil
	if !r.Preparing() {
		t.Fatal("between-wave countdown is not preparing")
	}
	r.cooldown = 0
	if r.Preparing() {
		t.Fatal("no countdown is preparing")
	}
}
