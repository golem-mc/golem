package server

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// TestPlayerStoreRoundTrip checks that what is saved comes back, and that it is
// written where and how a vanilla server writes it.
func TestPlayerStoreRoundTrip(t *testing.T) {
	t.Parallel()
	level := t.TempDir()
	store := newPlayerStore(level)
	id := uuid.MustParse("3f0ca0c8-9f16-3b87-92fb-b626b9a1bb99")

	// Nobody has played here, which is not an error.
	root, err := store.load(id)
	if err != nil {
		t.Fatalf("load an unknown player: %v", err)
	}
	if root != nil {
		t.Errorf("load an unknown player = %v, want nothing", root)
	}

	saved := nbt.Compound{"Health": nbt.Float(12.5), "Pos": nbt.List{
		Elem:  nbt.TypeDouble,
		Items: []nbt.Tag{nbt.Double(103.5), nbt.Double(-60), nbt.Double(5.5)},
	}}
	if err := saveTestPlayer(store, id, saved); err != nil {
		t.Fatalf("save: %v", err)
	}

	// The path is the one the game uses, so that a world can be moved between
	// this server and a vanilla one.
	want := filepath.Join(level, "players", "data", id.String()+".dat")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("player file was not written to %s: %v", want, err)
	}

	// And it is gzipped, which is what the game reads.
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("read player file: %v", err)
	}
	if len(data) < 2 || data[0] != 0x1F || data[1] != 0x8B {
		t.Errorf("player file starts % X, want a gzip header (1F 8B)", data[:min(len(data), 2)])
	}

	loaded, err := store.load(id)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if health, ok := nbt.Get[nbt.Float](loaded, "Health"); !ok || health != 12.5 {
		t.Errorf("Health = %v, %v; want 12.5, true", health, ok)
	}
	if pos, ok := storedPosition(loaded); !ok || pos.X != 103.5 || pos.Y != -60 || pos.Z != 5.5 {
		t.Errorf("position = %+v, %v; want (103.5, -60, 5.5), true", pos, ok)
	}
}

// saveTestPlayer writes root as a player's file, after every save before it.
func saveTestPlayer(store *playerStore, id uuid.UUID, root nbt.Compound) error {
	data, err := nbt.Encode("", root)
	if err != nil {
		return err
	}
	return store.write(playerWrite{id: id, seq: store.seq.Add(1), state: data})
}

// TestPlayerStoreWriteKeepsTheLaterSave checks that a save read before the one
// on disk does not overwrite any of the player's three files, and that a later
// one replaces all three.
func TestPlayerStoreWriteKeepsTheLaterSave(t *testing.T) {
	t.Parallel()
	store := newPlayerStore(t.TempDir())
	id := uuid.New()
	save := func(seq uint64, level int32, stats, advancements string) {
		t.Helper()
		data, err := nbt.Encode("", nbt.Compound{"XpLevel": nbt.Int(level)})
		if err != nil {
			t.Fatal(err)
		}
		w := playerWrite{id: id, seq: seq, state: data, stats: []byte(stats), advancements: []byte(advancements)}
		if err := store.write(w); err != nil {
			t.Fatalf("write seq %d: %v", seq, err)
		}
	}
	check := func(when string, level int32, stats, advancements string) {
		t.Helper()
		root, err := store.load(id)
		if err != nil {
			t.Fatalf("%s: load: %v", when, err)
		}
		if got, ok := nbt.Get[nbt.Int](root, "XpLevel"); !ok || got != nbt.Int(level) {
			t.Errorf("%s: XpLevel = %v, %v; want %d, true", when, got, ok, level)
		}
		for path, want := range map[string]string{store.statsPath(id): stats, store.advancementsPath(id): advancements} {
			got, err := os.ReadFile(path)
			if err != nil || string(got) != want {
				t.Errorf("%s: %s = %q, %v; want %q", when, filepath.Base(filepath.Dir(path)), got, err, want)
			}
		}
	}

	save(2, 2, `{"stats":2}`, `{"advancements":2}`)
	save(1, 1, `{"stats":1}`, `{"advancements":1}`)
	check("after an earlier save", 2, `{"stats":2}`, `{"advancements":2}`)
	save(3, 3, `{"stats":3}`, `{"advancements":3}`)
	check("after a later save", 3, `{"stats":3}`, `{"advancements":3}`)
}

// TestPlayerStoreKeepsABackup checks that the file a save replaces is kept, and
// that a player whose current file is unreadable is loaded from it rather than
// starting again from nothing.
func TestPlayerStoreKeepsABackup(t *testing.T) {
	t.Parallel()
	store := newPlayerStore(t.TempDir())
	id := uuid.New()

	if err := saveTestPlayer(store, id, nbt.Compound{"XpLevel": nbt.Int(30)}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := saveTestPlayer(store, id, nbt.Compound{"XpLevel": nbt.Int(31)}); err != nil {
		t.Fatalf("save again: %v", err)
	}

	backup := store.path(id, playerBackupSuffix)
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("the replaced file was not kept as %s: %v", playerBackupSuffix, err)
	}

	// A save cut short by the server dying leaves a file that is not gzip.
	if err := os.WriteFile(store.path(id, playerFileSuffix), []byte("half a file"), 0o644); err != nil {
		t.Fatalf("corrupt the player file: %v", err)
	}

	loaded, err := store.load(id)
	if err == nil {
		t.Error("loading a corrupt file was not reported")
	}
	if level, ok := nbt.Get[nbt.Int](loaded, "XpLevel"); !ok || level != 30 {
		t.Errorf("XpLevel = %v, %v; want 30, true (the backup)", level, ok)
	}

	// With neither file readable there is nothing to fall back to, and the
	// caller has to be told rather than handed an empty player.
	if err := os.Remove(backup); err != nil {
		t.Fatalf("remove the backup: %v", err)
	}
	if _, err := store.load(id); err == nil {
		t.Error("loading a corrupt file with no backup was not reported")
	}
}

// TestPlayerStateKeepsWhatItDoesNotUnderstand checks that saving a player who
// was loaded from a vanilla world writes everything this server does not
// model back out again. Dropping them would empty a player's pockets the
// first time they logged in here. The fields it does model - survival state,
// the experience and the ender chest among them - are its own, and this
// checks they won through rather than the stale values the file held.
func TestPlayerStateKeepsWhatItDoesNotUnderstand(t *testing.T) {
	t.Parallel()
	loaded := nbt.Compound{
		"Health":         nbt.Float(12.5),
		"playerGameType": nbt.Int(1),
		"XpLevel":        nbt.Int(30),
		"EnderItems":     nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"Slot": nbt.Byte(0)}}},
		"Pos":            nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(0), nbt.Double(0), nbt.Double(0)}},
	}

	p := &player.Player{UUID: uuid.MustParse("3f0ca0c8-9f16-3b87-92fb-b626b9a1bb99")}
	p.SetPosition(player.Position{X: 103.5, Y: -60, Z: 5.5, Yaw: 90, Pitch: -45, OnGround: true})
	// Half starved and halfway dead, in adventure mode; the file they were
	// "loaded" from disagrees about all of it.
	p.SetVitals(player.Vitals{
		GameMode:   2,       // adventure
		Health:     20 - 13, // as the loaded file would have them: 7.5 half-hearts
		Food:       3,
		Saturation: 0.5,
		Exhaustion: 1.25,
		Score:      7,
	})
	state := playerState(loaded, p, false, "minecraft:overworld", 12345, nil, nil, nil, nil, 0, 0)

	// The survival state the player actually is in won, not the 12.5 the
	// file they were loaded with claimed.
	if health, ok := nbt.Get[nbt.Float](state, "Health"); !ok || health != 7 {
		t.Errorf("Health = %v, %v; want the player's own 7, true", health, ok)
	}
	if score, ok := nbt.Get[nbt.Int](state, "Score"); !ok || score != 7 {
		t.Errorf("Score = %v, %v; want 7, true", score, ok)
	}
	if mode, ok := nbt.Get[nbt.Int](state, "playerGameType"); !ok || mode != 2 {
		t.Errorf("playerGameType = %v, %v; want 2, true", mode, ok)
	}
	if food, ok := nbt.Get[nbt.Int](state, "foodLevel"); !ok || food != 3 {
		t.Errorf("foodLevel = %v, %v; want 3, true", food, ok)
	}

	// The ender chest is modelled since ender chests exist, so the player's
	// own ender items won - this player was never given the one the file
	// they were loaded with held, the way the stale experience lost above.
	if ender, ok := nbt.Get[nbt.List](state, "EnderItems"); !ok || len(ender.Items) != 0 {
		t.Errorf("EnderItems = %v, %v; want the player's own empty ender chest", ender, ok)
	}
	// The experience is modelled now, so the file's stale level lost to the
	// empty one the player was carrying.
	if level, ok := nbt.Get[nbt.Int](state, "XpLevel"); !ok || level != 0 {
		t.Errorf("XpLevel = %v, %v; want the player's own 0, true", level, ok)
	}

	if version, ok := nbt.Get[nbt.Int](state, "DataVersion"); !ok || version != dataVersion {
		t.Errorf("DataVersion = %v, %v; want %d, true", version, ok, dataVersion)
	}
	if got, ok := nbt.Get[nbt.String](state, "Dimension"); !ok || string(got) != world.DimensionName {
		t.Errorf("Dimension = %v, %v; want %q, true", got, ok, world.DimensionName)
	}
	// The UUID as four big-endian integers, which is how the game stores one:
	// 3f0ca0c8 9f163b87 92fbb626 b9a1bb99, read as signed.
	want := nbt.IntArray{1057792200, -1625932921, -1828997594, -1180583015}
	if got, ok := nbt.Get[nbt.IntArray](state, "UUID"); !ok || !slices.Equal(got, want) {
		t.Errorf("UUID = %v, %v; want %v, true", got, ok, want)
	}

	pos, ok := storedPosition(state)
	if !ok {
		t.Fatal("the position that was just written cannot be read back")
	}
	if pos != (player.Position{X: 103.5, Y: -60, Z: 5.5, Yaw: 90, Pitch: -45, OnGround: true}) {
		t.Errorf("position = %+v, want where the player was standing", pos)
	}

	// Written to a copy, so that a shutdown saving a player at the same moment
	// as their own connection cannot see the compound half changed.
	if _, ok := loaded["DataVersion"]; ok {
		t.Error("playerState wrote through to the compound the player was loaded with")
	}
}

// TestPlayerStateKeepsTheClaimedDataVersion checks the DataVersion rule for a
// save: the claim a file rode in with stands, older and newer alike — an
// older claim's unread data rides through in that version's shape and must
// keep asking for the game's fixers, and a newer claim's is no more this
// server's to rewrite — and only what carries no claim, which is what this
// server writes for a player with no file, takes the current stamp.
func TestPlayerStateKeepsTheClaimedDataVersion(t *testing.T) {
	t.Parallel()
	p := &player.Player{UUID: uuid.MustParse("3f0ca0c8-9f16-3b87-92fb-b626b9a1bb99")}
	for _, tc := range []struct {
		name   string
		loaded nbt.Compound
		want   nbt.Int
	}{
		{"no file at all", nil, dataVersion},
		{"a file without the field", nbt.Compound{}, dataVersion},
		{"a file claiming zero, which reads as claiming nothing", nbt.Compound{"DataVersion": nbt.Int(0)}, dataVersion},
		{"a current file", nbt.Compound{"DataVersion": nbt.Int(dataVersion)}, dataVersion},
		{"an older file", nbt.Compound{"DataVersion": nbt.Int(3700)}, 3700},
		{"a file of a newer version", nbt.Compound{"DataVersion": nbt.Int(4999)}, 4999},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := playerState(tc.loaded, p, false, world.DimensionName, 0, nil, nil, nil, nil, 0, 0)
			if version, ok := nbt.Get[nbt.Int](state, "DataVersion"); !ok || version != tc.want {
				t.Errorf("DataVersion = %v, %v; want %v", version, ok, tc.want)
			}
		})
	}
}

// TestPlayerStateWritesVitals checks that the survival state is saved under
// the keys a vanilla server keeps it, so a player who comes back - here or on
// a vanilla server - comes back as hungry and as hurt as they left.
func TestPlayerStateWritesVitals(t *testing.T) {
	t.Parallel()
	p := &player.Player{UUID: uuid.MustParse("3f0ca0c8-9f16-3b87-92fb-b626b9a1bb99")}
	p.SetVitals(player.Vitals{
		GameMode:   2,   // adventure
		Health:     6.5, // hurt
		Food:       1,   // hungry
		Saturation: 0,
		Exhaustion: 3.5,

		// Two levels up and a third of the way into the third, of the
		// sixteen points the orb pickups banked.
		ExperienceLevel:    2,
		ExperienceProgress: 7.0 / 9.0,
		ExperienceTotal:    16,
	})
	state := playerState(nil, p, false, "minecraft:overworld", -42, nil, nil, nil, nil, 0, 0)

	if mode, ok := nbt.Get[nbt.Int](state, "playerGameType"); !ok || mode != 2 {
		t.Errorf("playerGameType = %v, %v; want 2, true", mode, ok)
	}
	if health, ok := nbt.Get[nbt.Float](state, "Health"); !ok || health != 6.5 {
		t.Errorf("Health = %v, %v; want 6.5, true", health, ok)
	}
	if food, ok := nbt.Get[nbt.Int](state, "foodLevel"); !ok || food != 1 {
		t.Errorf("foodLevel = %v, %v; want 1, true", food, ok)
	}
	if sat, ok := nbt.Get[nbt.Float](state, "foodSaturationLevel"); !ok || sat != 0 {
		t.Errorf("foodSaturationLevel = %v, %v; want 0, true", sat, ok)
	}
	if ex, ok := nbt.Get[nbt.Float](state, "foodExhaustionLevel"); !ok || ex != 3.5 {
		t.Errorf("foodExhaustionLevel = %v, %v; want 3.5, true", ex, ok)
	}
	// The experience under the keys the game writes it with, and the
	// enchantment seed beside them.
	if level, ok := nbt.Get[nbt.Int](state, "XpLevel"); !ok || level != 2 {
		t.Errorf("XpLevel = %v, %v; want 2, true", level, ok)
	}
	if progress, ok := nbt.Get[nbt.Float](state, "XpP"); !ok || float64(progress) != float64(float32(7.0/9.0)) {
		t.Errorf("XpP = %v, %v; want %g, true", progress, ok, float32(7.0/9.0))
	}
	if total, ok := nbt.Get[nbt.Int](state, "XpTotal"); !ok || total != 16 {
		t.Errorf("XpTotal = %v, %v; want 16, true", total, ok)
	}
	if seed, ok := nbt.Get[nbt.Int](state, "XpSeed"); !ok || seed != -42 {
		t.Errorf("XpSeed = %v, %v; want -42, true", seed, ok)
	}
}

// TestPlayerStateWritesSeenCredits checks the end-credits flag is saved
// under the key a vanilla server keeps it, shown and unshown alike, so the
// next exit from the end reads whether to hold the credits.
func TestPlayerStateWritesSeenCredits(t *testing.T) {
	t.Parallel()
	p := &player.Player{UUID: uuid.MustParse("3f0ca0c8-9f16-3b87-92fb-b626b9a1bb99")}
	loaded := nbt.Compound{"seenCredits": nbt.Byte(1)}

	// Unshown overwrites whatever the file claimed, as the flag is written
	// every save rather than left out.
	state := playerState(loaded, p, false, "minecraft:overworld", 0, nil, nil, nil, nil, 0, 0)
	if got, ok := nbt.Get[nbt.Byte](state, "seenCredits"); !ok || got != 0 {
		t.Errorf("seenCredits = %v, %v; want 0, true", got, ok)
	}

	p.SetSeenCredits(true)
	state = playerState(nil, p, false, "minecraft:overworld", 0, nil, nil, nil, nil, 0, 0)
	if got, ok := nbt.Get[nbt.Byte](state, "seenCredits"); !ok || got != 1 {
		t.Errorf("seenCredits = %v, %v; want 1, true", got, ok)
	}
}

// TestPlayerStateWritesTheBodyEnvironment checks the environment fields
// Entity.saveWithoutId writes for every entity are on the player file too:
// the fall, breath and burn the body carries, and a motion of none, the
// client owning a player's velocity.
func TestPlayerStateWritesTheBodyEnvironment(t *testing.T) {
	t.Parallel()
	p := &player.Player{UUID: uuid.MustParse("3f0ca0c8-9f16-3b87-92fb-b626b9a1bb99")}
	p.SetVitals(player.Vitals{
		FallDistance:      12.5,
		AirSupply:         40,
		FireTicks:         200,
		EnteredNether:     &[3]float64{1.5, 64, -2.5},
		ImpulseImpact:     &[3]float64{0.5, 72, 0.5},
		ImpulseGraceTicks: 30,
	})
	state := playerState(nil, p, false, "minecraft:overworld", 0, nil, nil, nil, nil, 0, 0)
	if got, ok := storedVec3(state, "entered_nether_pos"); !ok || got != [3]float64{1.5, 64, -2.5} {
		t.Errorf("entered_nether_pos = %v, %v; want (1.5, 64, -2.5)", got, ok)
	}
	if got, ok := storedVec3(state, "current_explosion_impact_pos"); !ok || got != [3]float64{0.5, 72, 0.5} {
		t.Errorf("current_explosion_impact_pos = %v, %v; want (0.5, 72, 0.5)", got, ok)
	}
	if grace, ok := nbt.Get[nbt.Int](state, "current_impulse_context_reset_grace_time"); !ok || grace != 30 {
		t.Errorf("current_impulse_context_reset_grace_time = %v, %v; want 30, true", grace, ok)
	}
	p.UpdateVitals(func(v *player.Vitals) { v.EnteredNether, v.ImpulseImpact = nil, nil })
	cleared := playerState(state, p, false, "minecraft:overworld", 0, nil, nil, nil, nil, 0, 0)
	if _, ok := cleared["entered_nether_pos"]; ok {
		t.Error("entered_nether_pos outlived the player leaving the nether")
	}
	if _, ok := cleared["current_explosion_impact_pos"]; ok {
		t.Error("current_explosion_impact_pos outlived its reset")
	}

	motion, ok := nbt.Get[nbt.List](state, "Motion")
	if !ok || len(motion.Items) != 3 {
		t.Fatalf("Motion = %v, %v; want three doubles", motion, ok)
	}
	for i, item := range motion.Items {
		if v, ok := item.(nbt.Double); !ok || v != 0 {
			t.Errorf("Motion[%d] = %v, want 0", i, item)
		}
	}
	if fall, ok := nbt.Get[nbt.Double](state, "fall_distance"); !ok || fall != 12.5 {
		t.Errorf("fall_distance = %v, %v; want 12.5, true", fall, ok)
	}
	if air, ok := nbt.Get[nbt.Short](state, "Air"); !ok || air != 40 {
		t.Errorf("Air = %v, %v; want 40, true", air, ok)
	}
	if fire, ok := nbt.Get[nbt.Short](state, "Fire"); !ok || fire != 200 {
		t.Errorf("Fire = %v, %v; want 200, true", fire, ok)
	}
}

// TestPlayerStateWritesTheRespawn checks the respawn point is saved under the
// compound and keys a vanilla server keeps it in, with the angles and
// dimension the point carries and vanilla's forced flag as its setter left
// it - written only where vanilla writes one - and that a player without one
// does not carry the point of the file they were loaded from out with them.
func TestPlayerStateWritesTheRespawn(t *testing.T) {
	t.Parallel()
	p := &player.Player{UUID: uuid.MustParse("3f0ca0c8-9f16-3b87-92fb-b626b9a1bb99")}
	loaded := nbt.Compound{"respawn": nbt.Compound{"pos": nbt.IntArray{9, 9, 9}}}

	state := playerState(loaded, p, false, "minecraft:overworld", 0, nil, nil, nil, nil, 0, 0)
	if _, ok := nbt.Get[nbt.Compound](state, "respawn"); ok {
		t.Error("a player with no respawn point saved the one their file held")
	}
	if _, ok := loaded["respawn"]; !ok {
		t.Error("the delete wrote through to the compound the player was loaded with")
	}

	for _, tc := range []struct {
		name  string
		spawn respawnPos
		want  nbt.Compound
	}{
		{"a bed's, not forced", respawnPos{
			pos: world.SpawnPos{X: 5, Y: 64, Z: 1}, dim: world.DimensionName,
			yaw: 137.5, pitch: -12.5,
		}, nbt.Compound{
			"pos":       nbt.IntArray{5, 64, 1},
			"dimension": nbt.String(world.DimensionName),
			"yaw":       nbt.Float(137.5),
			"pitch":     nbt.Float(-12.5),
		}},
		{"one /spawnpoint set, forced", respawnPos{
			pos: world.SpawnPos{X: 5, Y: 64, Z: 1}, dim: world.DimensionName,
			yaw: 90, pitch: 30, forced: true,
		}, nbt.Compound{
			"pos":       nbt.IntArray{5, 64, 1},
			"dimension": nbt.String(world.DimensionName),
			"yaw":       nbt.Float(90),
			"pitch":     nbt.Float(30),
			"forced":    nbt.Byte(1),
		}},
	} {
		wrote := playerState(loaded, p, false, "minecraft:overworld", 0, &tc.spawn, nil, nil, nil, 0, 0)
		if got, ok := nbt.Get[nbt.Compound](wrote, "respawn"); !ok || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: respawn = %v, %v; want %v, true", tc.name, got, ok, tc.want)
		}
	}
}

// TestPlayerStateWritesTheRecipeBook checks the recipe book is saved as the
// compound a vanilla server keeps it in: the recipes the player has and the
// ones never looked at yet as the name lists vanilla writes, the switches
// that are on as the bytes vanilla names them and the ones that are off
// left out, and anything else the loaded file's compound held carried
// through. A player whose book this server never read saves none.
func TestPlayerStateWritesTheRecipeBook(t *testing.T) {
	t.Parallel()
	p := &player.Player{UUID: uuid.MustParse("3f0ca0c8-9f16-3b87-92fb-b626b9a1bb99")}
	loaded := nbt.Compound{"recipeBook": nbt.Compound{
		"recipes":       nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("minecraft:crafting_table")}},
		"toBeDisplayed": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("minecraft:crafting_table"), nbt.String("minecraft:an_old_recipe")}},
	}}
	book := &inventory.RecipeBook{
		Known:         []string{"minecraft:crafting_table", "minecraft:stonecutter"},
		ToBeDisplayed: []string{"minecraft:crafting_table"},
	}
	book.Settings.Open[0] = true
	book.Settings.Filtering[1] = true

	wrote := playerState(loaded, p, false, "minecraft:overworld", 0, nil, nil, nil, book, 0, 0)
	got, ok := nbt.Get[nbt.Compound](wrote, "recipeBook")
	if !ok {
		t.Fatal("the recipe book was not saved")
	}
	want := nbt.Compound{
		"recipes":                     nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("minecraft:crafting_table"), nbt.String("minecraft:stonecutter")}},
		"toBeDisplayed":               nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("minecraft:crafting_table")}},
		"isGuiOpen":                   nbt.Byte(1),
		"isFurnaceFilteringCraftable": nbt.Byte(1),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("recipeBook = %v, want %v", got, want)
	}
	// The compound the player was loaded with is not written through to.
	kept, _ := nbt.Get[nbt.Compound](loaded, "recipeBook")
	if len(kept["toBeDisplayed"].(nbt.List).Items) != 2 {
		t.Error("the save rewrote the compound the player was loaded with")
	}

	// The off switches are left out rather than written false, and a player
	// whose book this server never read saves no book at all.
	book = &inventory.RecipeBook{Known: book.Known, ToBeDisplayed: nil}
	book.Settings.Open[0] = false
	book.Settings.Filtering[1] = false
	wrote = playerState(loaded, p, false, "minecraft:overworld", 0, nil, nil, nil, book, 0, 0)
	if got, _ = nbt.Get[nbt.Compound](wrote, "recipeBook"); got == nil {
		t.Fatal("the recipe book was not saved")
	}
	for _, key := range []string{"isGuiOpen", "isFurnaceFilteringCraftable"} {
		if _, ok := got[key]; ok {
			t.Errorf("recipeBook[%q] = %v, want it left out", key, got[key])
		}
	}
	if got := playerState(loaded, p, false, "minecraft:overworld", 0, nil, nil, nil, nil, 0, 0); got == nil {
		t.Fatal("no state was saved")
	}
	if _, ok := nbt.Get[nbt.Compound](got, "recipeBook"); ok {
		t.Error("a player whose book was never read saved the one their file held")
	}
}

// TestStoredPositionRejectsNonsense checks that a file holding a position the
// world cannot contain leaves the player at spawn instead of somewhere the rest
// of the server cannot cope with.
func TestStoredPositionRejectsNonsense(t *testing.T) {
	t.Parallel()
	doubles := func(v ...float64) nbt.List {
		items := make([]nbt.Tag, len(v))
		for i, d := range v {
			items[i] = nbt.Double(d)
		}
		return nbt.List{Elem: nbt.TypeDouble, Items: items}
	}

	tests := []struct {
		name string
		root nbt.Compound
	}{
		{"no position at all", nbt.Compound{}},
		{"the wrong number of coordinates", nbt.Compound{"Pos": doubles(1, 2)}},
		{"the wrong type", nbt.Compound{"Pos": nbt.List{
			Elem:  nbt.TypeFloat,
			Items: []nbt.Tag{nbt.Float(1), nbt.Float(2), nbt.Float(3)},
		}}},
		{"not a number", nbt.Compound{"Pos": doubles(math.NaN(), 0, 0)}},
		{"infinite", nbt.Compound{"Pos": doubles(0, math.Inf(1), 0)}},
		{"outside the world", nbt.Compound{"Pos": doubles(1e300, 0, 0)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if pos, ok := storedPosition(tc.root); ok {
				t.Errorf("position = %+v, want none", pos)
			}
		})
	}

	// A rotation that makes no sense is not worth refusing the position over:
	// facing the wrong way is a smaller surprise than being back at spawn.
	root := nbt.Compound{
		"Pos": doubles(1, 2, 3),
		"Rotation": nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{
			nbt.Float(math.NaN()), nbt.Float(0),
		}},
	}
	pos, ok := storedPosition(root)
	if !ok {
		t.Fatal("a bad rotation threw the position away with it")
	}
	if pos.Yaw != 0 || pos.Pitch != 0 {
		t.Errorf("rotation = (%v, %v), want (0, 0)", pos.Yaw, pos.Pitch)
	}
}

// TestStoredRespawnRejectsNonsense checks that a file holding a respawn
// compound the game's codec would refuse leaves the player with the world
// spawn instead.
func TestStoredRespawnRejectsNonsense(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		root nbt.Compound
	}{
		{"no respawn at all", nbt.Compound{}},
		{"no dimension", nbt.Compound{"respawn": nbt.Compound{
			"pos": nbt.IntArray{1, 2, 3},
		}}},
		{"no position", nbt.Compound{"respawn": nbt.Compound{
			"dimension": nbt.String(world.DimensionName),
		}}},
		{"the wrong number of coordinates", nbt.Compound{"respawn": nbt.Compound{
			"dimension": nbt.String(world.DimensionName),
			"pos":       nbt.IntArray{1, 2},
		}}},
		{"no yaw", nbt.Compound{"respawn": nbt.Compound{
			"dimension": nbt.String(world.DimensionName),
			"pos":       nbt.IntArray{1, 2, 3},
			"pitch":     nbt.Float(0),
		}}},
		{"no pitch", nbt.Compound{"respawn": nbt.Compound{
			"dimension": nbt.String(world.DimensionName),
			"pos":       nbt.IntArray{1, 2, 3},
			"yaw":       nbt.Float(0),
		}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if spawn, ok := storedRespawn(tc.root); ok {
				t.Errorf("respawn = %+v, want none", spawn)
			}
		})
	}
}

// TestStoredRespawnCarriesWhatItCannotUse checks that a respawn point the
// game's shape names comes back whatever it names, one no respawn here can
// act on included, to ride through to the next save; only an overworld point
// the world can hold reads as one a respawn can use.
func TestStoredRespawnCarriesWhatItCannotUse(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		respawn nbt.Compound
		want    respawnPos
	}{
		{"one the nether names", nbt.Compound{
			"dimension": nbt.String(world.NetherName),
			"pos":       nbt.IntArray{3, 70, 4},
			"yaw":       nbt.Float(90),
			"pitch":     nbt.Float(30),
			"forced":    nbt.Byte(1),
		}, respawnPos{
			pos: world.SpawnPos{X: 3, Y: 70, Z: 4}, dim: world.NetherName,
			yaw: 90, pitch: 30, forced: true,
		}},
		{"one outside the world", nbt.Compound{
			"dimension": nbt.String(world.DimensionName),
			"pos":       nbt.IntArray{2_000_000_000, 0, 0},
			"yaw":       nbt.Float(0),
			"pitch":     nbt.Float(0),
		}, respawnPos{
			pos: world.SpawnPos{X: 2_000_000_000, Y: 0, Z: 0}, dim: world.DimensionName,
		}},
		{"one the overworld holds, the flag a bed leaves out", nbt.Compound{
			"dimension": nbt.String(world.DimensionName),
			"pos":       nbt.IntArray{1, 2, 3},
			"yaw":       nbt.Float(45),
			"pitch":     nbt.Float(-20),
		}, respawnPos{
			pos: world.SpawnPos{X: 1, Y: 2, Z: 3}, dim: world.DimensionName,
			yaw: 45, pitch: -20,
		}},
		{"one the overworld holds, the flag written down", nbt.Compound{
			"dimension": nbt.String(world.DimensionName),
			"pos":       nbt.IntArray{1, 2, 3},
			"yaw":       nbt.Float(45),
			"pitch":     nbt.Float(-20),
			"forced":    nbt.Byte(0),
		}, respawnPos{
			pos: world.SpawnPos{X: 1, Y: 2, Z: 3}, dim: world.DimensionName,
			yaw: 45, pitch: -20,
		}},
	} {
		spawn, ok := storedRespawn(nbt.Compound{"respawn": tc.respawn})
		if !ok || spawn != tc.want {
			t.Errorf("%s: respawn = %+v, %v; want %+v, true", tc.name, spawn, ok, tc.want)
		}
	}
}

// TestPlayerStateWritesAttributes checks a changed base reaches the file's
// attributes list, and is rewritten once it is reset.
func TestPlayerStateWritesAttributes(t *testing.T) {
	t.Parallel()
	p := &player.Player{UUID: uuid.MustParse("3f0ca0c8-9f16-3b87-92fb-b626b9a1bb99")}
	p.Attributes(func(a *player.Attributes) { a.SetBase(registry.AttributeArmor, 5) })
	state := playerState(nil, p, false, "minecraft:overworld", 0, nil, nil, nil, nil, 0, 0)
	if list, ok := nbt.Get[nbt.List](state, "attributes"); !ok || len(list.Items) != 4 {
		t.Fatalf("attributes = %v, %v; want the three starting instances and armor", list, ok)
	}
	p.Attributes(func(a *player.Attributes) { a.ResetBase(registry.AttributeArmor) })
	state = playerState(state, p, false, "minecraft:overworld", 0, nil, nil, nil, nil, 0, 0)
	list, ok := nbt.Get[nbt.List](state, "attributes")
	if !ok || len(list.Items) != 4 {
		t.Fatalf("attributes = %v after the base was reset, want four entries", list)
	}
	entry, ok := nbt.CompoundView(list.Items[0])
	if !ok || entry["id"] != nbt.String("minecraft:armor") || entry["base"] != nbt.Double(0) {
		t.Errorf("attributes = %v after the base was reset, want the entry at its own base", list)
	}
}
