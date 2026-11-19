package world

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestLegacyRulesNameKnownRules checks every rule GameRuleRegistryFix renames
// to is one this server's table has.
func TestLegacyRulesNameKnownRules(t *testing.T) {
	t.Parallel()
	for _, l := range legacyRules {
		if ruleByID(l.id) == nil {
			t.Errorf("%s renames to %s, which the table has no row for", l.old, l.id)
		}
	}
}

// TestAnOlderWorldsSpawnCompoundFollowsSetSpawn checks a level.dat from
// 1.21.9 to 1.21.11, which keeps the spawn only in its spawn compound: a
// spawn set here is written into it with the rotation it was given, or 0/0
// where none was, so vanilla reads it after its upgrade.
func TestAnOlderWorldsSpawnCompoundFollowsSetSpawn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeLevelDat(t, dir, nbt.Compound{
		"DataVersion": nbt.Int(4671),
		"spawn": nbt.Compound{
			"dimension": nbt.String(DimensionName),
			"pos":       nbt.IntArray{96, 258, -32},
			"yaw":       nbt.Float(45),
			"pitch":     nbt.Float(0),
		},
	})
	w, _ := NewWorld(dir, NewFlatGenerator())
	if got := w.Spawn(); got != (SpawnPos{X: 96, Y: 258, Z: -32, Yaw: 45}) {
		t.Fatalf("spawn = %+v, want the compound's", got)
	}
	w.SetSpawn(SpawnPos{X: 7, Y: 70, Z: 7})
	w.Save()
	w.Close()

	level, err := LoadLevel(dir)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := nbt.Get[nbt.Int](level.raw, "DataVersion"); v != 4671 {
		t.Errorf("DataVersion = %d, want the world's own 4671", v)
	}
	spawn, _ := nbt.Get[nbt.Compound](level.raw, "spawn")
	pos, _ := nbt.Get[nbt.IntArray](spawn, "pos")
	if len(pos) != 3 || pos[0] != 7 || pos[1] != 70 || pos[2] != 7 {
		t.Errorf("spawn.pos = %v, want [7 70 7]", pos)
	}
	if yaw, _ := nbt.Get[nbt.Float](spawn, "yaw"); yaw != 0 {
		t.Errorf("spawn.yaw = %v, want 0, no rotation was given", yaw)
	}
	if pitch, _ := nbt.Get[nbt.Float](spawn, "pitch"); pitch != 0 {
		t.Errorf("spawn.pitch = %v, want 0, no rotation was given", pitch)
	}

	// A rotation that is given is the one kept.
	w, _ = NewWorld(dir, NewFlatGenerator())
	w.SetSpawn(SpawnPos{X: 8, Y: 71, Z: 8, Yaw: 90, Pitch: -15})
	w.Save()
	w.Close()
	level, err = LoadLevel(dir)
	if err != nil {
		t.Fatal(err)
	}
	spawn, _ = nbt.Get[nbt.Compound](level.raw, "spawn")
	if yaw, _ := nbt.Get[nbt.Float](spawn, "yaw"); yaw != 90 {
		t.Errorf("spawn.yaw = %v, want the given 90", yaw)
	}
	if pitch, _ := nbt.Get[nbt.Float](spawn, "pitch"); pitch != -15 {
		t.Errorf("spawn.pitch = %v, want the given -15", pitch)
	}
}

// TestAnOlderWorldsLegacyRulesAreReadAndKept checks a level.dat from before
// 1.21.11, whose GameRules hold strings under the old names: they are read
// through GameRuleRegistryFix's renames, and written back the same way.
func TestAnOlderWorldsLegacyRulesAreReadAndKept(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeLevelDat(t, dir, nbt.Compound{
		"DataVersion": nbt.Int(4440),
		"GameRules": nbt.Compound{
			"keepInventory":    nbt.String("true"),
			"disableRaids":     nbt.String("true"),
			"doFireTick":       nbt.String("false"),
			"randomTickSpeed":  nbt.String("10"),
			"doInsomnia":       nbt.String("true"),
			"spawnChunkRadius": nbt.String("2"),
		},
	})
	w, _ := NewWorld(dir, NewFlatGenerator())
	for id, want := range map[GameRuleIndex]bool{RuleKeepInventory: true, RuleRaids: false, RuleSpawnPhantoms: true} {
		if got := w.BoolRule(id); got != want {
			t.Errorf("%s = %v, want %v", id, got, want)
		}
	}
	if w.IntRule(RuleFireSpread) != 0 || w.IntRule(RuleRandomTickSpeed) != 10 {
		t.Errorf("fire spread %d random tick speed %d, want 0 and 10",
			w.IntRule(RuleFireSpread), w.IntRule(RuleRandomTickSpeed))
	}
	w.SetBoolRule(RuleSpawnPhantoms, false)
	w.SetIntRule(RuleFireSpread, -1)
	w.Save()
	w.Close()

	level, err := LoadLevel(dir)
	if err != nil {
		t.Fatal(err)
	}
	rules, _ := nbt.Get[nbt.Compound](level.raw, "GameRules")
	for key, want := range map[string]string{
		"keepInventory": "true", "disableRaids": "true", "doInsomnia": "false",
		"randomTickSpeed": "10", "doFireTick": "true", "allowFireTicksAwayFromPlayer": "true",
		"spawnChunkRadius": "2",
	} {
		if got, _ := nbt.Get[nbt.String](rules, key); string(got) != want {
			t.Errorf("GameRules %s = %q, want %q", key, got, want)
		}
	}
	if _, ok := level.raw["game_rules"]; ok {
		t.Error("a game_rules compound was added to a level.dat from before 1.21.11")
	}
}

// TestA26_1WorldsRulesStayInGameRulesDat checks a world 26.1 has already
// upgraded, whose rules live in game_rules.dat alone: a save here must not
// write a game_rules compound back into level.dat, where the next open would
// read it over the file.
func TestA26_1WorldsRulesStayInGameRulesDat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeLevelDat(t, dir, nbt.Compound{"DataVersion": nbt.Int(4790)})
	rules := ruleTable()
	rules[RuleKeepInventory] = ruleValue{b: true}
	if err := writeGameRules(dir, rules); err != nil {
		t.Fatal(err)
	}
	w, _ := NewWorld(dir, NewFlatGenerator())
	if !w.BoolRule(RuleKeepInventory) {
		t.Fatalf("keep_inventory = %v, want the file's true", w.BoolRule(RuleKeepInventory))
	}
	w.Save()
	w.Close()

	level, err := LoadLevel(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := level.raw["game_rules"]; ok {
		t.Error("a game_rules compound was written into a 26.1 world's level.dat")
	}

	// The file is what the world's rules are now: rewriting it takes.
	rules[RuleKeepInventory] = ruleValue{b: false}
	if err := writeGameRules(dir, rules); err != nil {
		t.Fatal(err)
	}
	w, _ = NewWorld(dir, NewFlatGenerator())
	if w.BoolRule(RuleKeepInventory) {
		t.Error("keep_inventory = true, want the file's false")
	}
	w.Close()
}
