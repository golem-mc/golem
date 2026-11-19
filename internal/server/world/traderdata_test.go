package world

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWanderingTraderDataSurvivesARestart: a fresh world has vanilla's 24000
// and 25, an unchanged state writes no file, and a changed one reads back.
func TestWanderingTraderDataSurvivesARestart(t *testing.T) {
	root := t.TempDir()
	d := NewWanderingTraderData(root)
	var got TraderSpawnState
	d.Update(func(st *TraderSpawnState) { got = *st })
	if got != (TraderSpawnState{SpawnDelay: 24000, SpawnChance: 25}) {
		t.Fatalf("fresh state %+v, want 24000 and 25", got)
	}
	d.Save()
	if _, err := os.Stat(filepath.Join(root, wanderingTraderFile)); err == nil {
		t.Error("an unchanged state wrote wandering_trader.dat")
	}
	d.Update(func(st *TraderSpawnState) { st.SpawnDelay, st.SpawnChance = 22800, 50 })
	d.Save()
	NewWanderingTraderData(root).Update(func(st *TraderSpawnState) { got = *st })
	if got != (TraderSpawnState{SpawnDelay: 22800, SpawnChance: 50}) {
		t.Errorf("restarted state %+v, want 22800 and 50", got)
	}
}
