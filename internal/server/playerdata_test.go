package server

import (
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
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
	state := playerState(loaded, p, false, "minecraft:overworld", 12345, nil, nil, nil, nil, 0)

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
	state := playerState(nil, p, false, "minecraft:overworld", -42, nil, nil, nil, nil, 0)

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

func TestPlayerStateSavesDeathTimeFromVitals(t *testing.T) {
	p := &player.Player{UUID: uuid.MustParse("3f0ca0c8-9f16-3b87-92fb-b626b9a1bb99")}
	p.SetVitals(player.Vitals{Dead: true, DeathTime: 15})
	state := playerState(nil, p, false, "minecraft:overworld", 0, nil, nil, nil, nil, 0)
	if got, ok := nbt.Get[nbt.Short](state, "DeathTime"); !ok || got != 15 {
		t.Errorf("DeathTime = %v, %v; want 15, true", got, ok)
	}
	p.SetVitals(rules.FreshVitals(0))
	state = playerState(nil, p, false, "minecraft:overworld", 0, nil, nil, nil, nil, 0)
	if got, ok := nbt.Get[nbt.Short](state, "DeathTime"); !ok || got != 0 {
		t.Errorf("DeathTime after respawn = %v, %v; want 0, true", got, ok)
	}
}
