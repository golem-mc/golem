package world

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestGameRuleTable checks the shape of the game rule table: one row per
// rule the vanilla GameRules class registers, with the kind, default and
// bounds each carries — spot-checking the rows the wiring leans on and the
// odd ones, the feature-flagged rule among them.
func TestGameRuleTable(t *testing.T) {
	t.Parallel()
	if len(GameRuleTable) != 59 {
		t.Fatalf("game rule table has %d rows, want 59", len(GameRuleTable))
	}
	byID := make(map[string]GameRule, len(GameRuleTable))
	for _, r := range GameRuleTable {
		if _, dup := byID[r.ID]; dup {
			t.Errorf("game rule %s is in the table twice", r.ID)
		}
		byID[r.ID] = r
	}

	checks := []struct {
		id    string
		kind  RuleKind
		defB  bool
		defI  int32
		min   int32
		max   int32
		gated bool
	}{
		{"advance_time", RuleBool, true, 0, 0, 0, false},
		{"advance_weather", RuleBool, true, 0, 0, 0, false},
		{"fire_spread_radius_around_player", RuleInt, false, 128, -1, int32Max, false},
		{"immediate_respawn", RuleBool, false, 0, 0, 0, false},
		{"keep_inventory", RuleBool, false, 0, 0, 0, false},
		{"max_block_modifications", RuleInt, false, 32768, 1, int32Max, false},
		{"max_command_forks", RuleInt, false, 65536, 0, int32Max, false},
		{"max_entity_cramming", RuleInt, false, 24, 0, int32Max, false},
		{"max_minecart_speed", RuleInt, false, 8, 1, 1000, true},
		{"max_snow_accumulation_height", RuleInt, false, 1, 0, 8, false},
		{"natural_health_regeneration", RuleBool, true, 0, 0, 0, false},
		{"random_tick_speed", RuleInt, false, 3, 0, int32Max, false},
		{"respawn_radius", RuleInt, false, 10, 0, int32Max, false},
		{"send_command_feedback", RuleBool, true, 0, 0, 0, false},
		{"show_death_messages", RuleBool, true, 0, 0, 0, false},
		{"spawn_mobs", RuleBool, true, 0, 0, 0, false},
		{"spawn_monsters", RuleBool, true, 0, 0, 0, false},
		{"tnt_explosion_drop_decay", RuleBool, false, 0, 0, 0, false},
	}
	for _, c := range checks {
		r, ok := byID[c.id]
		if !ok {
			t.Errorf("game rule %s is missing from the table", c.id)
			continue
		}
		if r.Kind != c.kind {
			t.Errorf("game rule %s kind = %d, want %d", c.id, r.Kind, c.kind)
		}
		if r.DefBool != c.defB {
			t.Errorf("game rule %s default = %t, want %t", c.id, r.DefBool, c.defB)
		}
		if r.DefInt != c.defI {
			t.Errorf("game rule %s default = %d, want %d", c.id, r.DefInt, c.defI)
		}
		// An integer rule's bounds are the parser's floor and ceiling; a
		// boolean rule's own range is the parser's whole width, which the
		// table spells the unbounded pair with.
		if c.kind == RuleInt && (r.Min != c.min || r.Max != c.max) {
			t.Errorf("game rule %s range = [%d, %d], want [%d, %d]", c.id, r.Min, r.Max, c.min, c.max)
		}
		if r.FeatureGated != c.gated {
			t.Errorf("game rule %s feature-gated = %t, want %t", c.id, r.FeatureGated, c.gated)
		}
	}
}

// int32Max is the integer game rules' unbounded ceiling, spelled out so the
// table check above can name it.
const int32Max = int32(^uint32(0) >> 1)

// TestGameRulesRoundTrip checks that a rule a world sets survives a save and
// a reopen: the table is written to data/minecraft/game_rules.dat on Save and
// read back on NewWorld, so a /gamerule outlives a restart the way the
// vanilla server's own does.
func TestGameRulesRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, _ := NewWorld(dir, NewFlatGenerator())
	w.SetBoolRule(RuleKeepInventory, true)
	w.SetBoolRule(RuleAdvanceTime, false)
	w.SetIntRule(RuleRespawnRadius, 42)
	w.SetIntRule(RuleFireSpread, -1)
	w.Save()
	w.Close()

	if _, err := os.Stat(filepath.Join(dir, gameRulesFile)); err != nil {
		t.Fatalf("game_rules.dat was not written: %v", err)
	}

	w2, _ := NewWorld(dir, NewFlatGenerator())
	defer w2.Close()
	if !w2.BoolRule(RuleKeepInventory) {
		t.Error("keep_inventory after reload = false, want true")
	}
	if w2.BoolRule(RuleAdvanceTime) {
		t.Error("advance_time after reload = true, want false")
	}
	if got := w2.IntRule(RuleRespawnRadius); got != 42 {
		t.Errorf("respawn_radius after reload = %d, want 42", got)
	}
	if got := w2.IntRule(RuleFireSpread); got != -1 {
		t.Errorf("fire_spread_radius_around_player after reload = %d, want -1", got)
	}
	// A rule this world never set reads at its default after a reload too.
	if !w2.BoolRule(RuleSpawnMobs) {
		t.Error("spawn_mobs after reload = false, want default true")
	}
}

// TestGameRulesLoadVanillaFile checks that a game_rules.dat in the vanilla
// shape — an unnamed root compound holding the one data compound, each rule
// under its minecraft:-prefixed name, a boolean as a byte and an integer as
// an int — is read as the rules it says, leaving every rule the file does
// not name at its default.
func TestGameRulesLoadVanillaFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, gameRulesFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	encoded, err := nbt.Encode("", nbt.Compound{
		"data": nbt.Compound{
			"minecraft:keep_inventory":    nbt.Byte(1),
			"minecraft:spawn_mobs":        nbt.Byte(0),
			"minecraft:random_tick_speed": nbt.Int(9),
			"minecraft:some_other_rule":   nbt.Byte(1),
		},
	})
	if err != nil {
		t.Fatalf("encode game_rules.dat: %v", err)
	}
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write(encoded); err != nil {
		t.Fatalf("compress game_rules.dat: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("compress game_rules.dat: %v", err)
	}
	if err := os.WriteFile(path, compressed.Bytes(), 0o644); err != nil {
		t.Fatalf("write game_rules.dat: %v", err)
	}

	w, _ := NewWorld(dir, NewFlatGenerator())
	defer w.Close()
	if !w.BoolRule(RuleKeepInventory) {
		t.Error("keep_inventory from vanilla file = false, want true")
	}
	if w.BoolRule(RuleSpawnMobs) {
		t.Error("spawn_mobs from vanilla file = true, want false")
	}
	if got := w.IntRule(RuleRandomTickSpeed); got != 9 {
		t.Errorf("random_tick_speed from vanilla file = %d, want 9", got)
	}
	// A rule the file does not name stands at its default.
	if !w.BoolRule(RuleAdvanceTime) {
		t.Error("advance_time from vanilla file = false, want default true")
	}
	// The feature-gated rule never reaches the file, so a file that names
	// it anyway is not read for it.
	if got := w.IntRule(RuleMaxMinecartSpeed); got != 8 {
		t.Errorf("max_minecart_speed from vanilla file = %d, want default 8", got)
	}
}

// TestTickTimeGatedByAdvanceTime checks that the advance_time rule gates the
// clock and nothing else: a world age that keeps counting whatever the rule
// says, the way the vanilla server's ServerLevel.tickTime does, and a day
// clock that stops while the rule is off and resumes when it is on.
func TestTickTimeGatedByAdvanceTime(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	w.SetIntRule(RuleRandomTickSpeed, 0)

	w.SetBoolRule(RuleAdvanceTime, false)
	for range 20 {
		w.TickTime()
	}
	worldAge, dayTime := w.Time()
	if worldAge != 20 {
		t.Errorf("world age with advance_time off = %d, want 20", worldAge)
	}
	if dayTime != 0 {
		t.Errorf("day time with advance_time off = %d, want 0", dayTime)
	}

	w.SetBoolRule(RuleAdvanceTime, true)
	for range 5 {
		w.TickTime()
	}
	worldAge, dayTime = w.Time()
	if worldAge != 25 {
		t.Errorf("world age with advance_time on = %d, want 25", worldAge)
	}
	if dayTime != 5 {
		t.Errorf("day time with advance_time on = %d, want 5", dayTime)
	}
}

// TestTickWeatherGatedByAdvanceWeather checks that the advance_weather rule
// holds the weather cycle where it is: a sky mid-rain with a countdown
// running does not count down a tick while the rule is off, and carries on
// from where it stood when the rule is on.
func TestTickWeatherGatedByAdvanceWeather(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())

	// A two-tick stretch of rain: with the rule on, the third tick of the
	// cycle turns it off.
	w.SetWeather(true, false, 2, 2, 0)
	w.SetBoolRule(RuleAdvanceWeather, false)
	for range 10 {
		w.TickWeather()
	}
	if !w.Raining() {
		t.Error("raining with advance_weather off = false, want held true")
	}

	w.SetBoolRule(RuleAdvanceWeather, true)
	for range 3 {
		w.TickWeather()
	}
	if w.Raining() {
		t.Error("raining with advance_weather on after three ticks = true, want false")
	}
}

// TestRuleBounds checks the lookup the /gamerule command checks its
// argument against: the kind and range of a rule the table names, and no
// rule for one it does not.
func TestRuleBounds(t *testing.T) {
	t.Parallel()
	kind, min, max, ok := RuleBounds("respawn_radius")
	if !ok || kind != RuleInt || min != 0 || max != int32Max {
		t.Errorf("RuleBounds(respawn_radius) = (%d, %d, %d, %t), want int (0, unbounded)",
			kind, min, max, ok)
	}
	kind, _, _, ok = RuleBounds("keep_inventory")
	if !ok || kind != RuleBool {
		t.Errorf("RuleBounds(keep_inventory) = (%d, %t), want bool", kind, ok)
	}
	if _, _, _, ok := RuleBounds("nonsense"); ok {
		t.Error("RuleBounds(nonsense) = ok, want false")
	}
}
