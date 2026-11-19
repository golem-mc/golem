package world

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestALevelDatHardcoreFlagPinsHard checks a level.dat with hardcore=1 loads
// hardcore and hard, refuses a lower difficulty, and keeps the flag in a save.
// vanilla: MinecraftServer.setDifficulty
func TestALevelDatHardcoreFlagPinsHard(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeLevelDat(t, dir, nbt.Compound{
		"DataVersion": nbt.Int(dataVersion),
		"difficulty_settings": nbt.Compound{
			"difficulty": nbt.String("easy"), "hardcore": nbt.Byte(1), "locked": nbt.Byte(0),
		},
	})
	w, _ := NewWorld(dir, &OverworldGenerator{deferred: true})
	defer w.Close()
	if !w.Hardcore() || w.Difficulty() != DifficultyHard {
		t.Errorf("hardcore %v difficulty %d, want hardcore and hard", w.Hardcore(), w.Difficulty())
	}
	w.SetDifficulty(DifficultyPeaceful)
	if w.Difficulty() != DifficultyHard {
		t.Errorf("difficulty after SetDifficulty(peaceful) = %d, want hard", w.Difficulty())
	}
	w.Save()
	level, err := LoadLevel(dir)
	if err != nil || !level.Hardcore {
		t.Errorf("reloaded level hardcore = %v, err %v; want true", level != nil && level.Hardcore, err)
	}
}

// TestAnOlderLevelDatHardcoreByteLoadsHardcore checks the top-level hardcore
// byte of an older level.dat is read next to Difficulty.
// vanilla: LevelDatDifficultyFix
func TestAnOlderLevelDatHardcoreByteLoadsHardcore(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeLevelDat(t, dir, nbt.Compound{
		"DataVersion": nbt.Int(dataVersion),
		"Difficulty":  nbt.Byte(1),
		"hardcore":    nbt.Byte(1),
	})
	level, err := LoadLevel(dir)
	if err != nil || !level.Hardcore || level.Difficulty != DifficultyHard {
		t.Errorf("level = %+v, err %v; want hardcore and hard", level, err)
	}
}
