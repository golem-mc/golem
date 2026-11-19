package world

import "testing"

func TestSpawnEnemiesRefreshAcrossDimensions(t *testing.T) {
	t.Parallel()
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	dim := NewDimensionWorld(t.TempDir(), true, NewFlatGenerator())
	t.Cleanup(func() { dim.Close() })
	dim.SetTimeSource(w)
	check := func(want bool) {
		t.Helper()
		for _, level := range []*World{w, dim} {
			if got := level.SpawnEnemies(); got != want {
				t.Fatalf("%s SpawnEnemies = %v, want %v", level.Name(), got, want)
			}
		}
	}
	check(true)
	w.SetBoolRule(RuleSpawnMobs, false)
	check(true)
	w.SetDifficulty(DifficultyNormal)
	check(false)
	w.SetBoolRule(RuleSpawnMobs, true)
	w.SetBoolRule(RuleSpawnPhantoms, true)
	check(false)
	w.SetDifficultyLocked(true)
	if w.ChangeDifficulty(DifficultyEasy) {
		t.Fatal("locked difficulty changed")
	}
	check(false)
	w.SetBoolRule(RuleSpawnMonsters, true)
	check(true)
	w.SetBoolRule(RuleSpawnMonsters, false)
	check(false)
	w.SetBoolRule(RuleSpawnMonsters, true)
	check(true)
	w.SetBoolRule(RuleSpawnMobs, false)
	w.SetDifficulty(DifficultyNormal)
	w.SetBoolRule(RuleSpawnMobs, true)
	w.SetDifficultyLocked(false)
	if !w.ChangeDifficulty(DifficultyEasy) {
		t.Fatal("unlocked difficulty refused")
	}
	check(true)
}
