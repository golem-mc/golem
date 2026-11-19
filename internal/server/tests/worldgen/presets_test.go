package worldgen

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/internal/server/world/terrain"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// startPreset starts a server on seed 12345 with the level-type given, in dir.
func startPreset(t *testing.T, dir, levelType string) *server.Server {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelType = levelType
	cfg.LevelSeed = "12345"
	cfg.LevelName = dir
	srv, _ := servertest.Start(t, cfg)
	servertest.StopTicking(t, srv)
	return srv
}

// storedOverworldGenerator reads the overworld generator's noise settings and
// biome source preset from the world_gen_settings.dat in dir.
func storedOverworldGenerator(t *testing.T, dir string) (settings, preset string) {
	t.Helper()
	s, source := storedOverworldSource(t, dir)
	p, _ := nbt.Get[nbt.String](source, "preset")
	return s, string(p)
}

// storedOverworldSource reads the overworld generator's noise settings and
// biome source compound from the world_gen_settings.dat in dir.
func storedOverworldSource(t *testing.T, dir string) (settings string, source nbt.Compound) {
	t.Helper()
	data := servertest.ReadSavedFile(t, filepath.Join(dir, "data", "minecraft", "world_gen_settings.dat"), "data")
	dims, _ := nbt.Get[nbt.Compound](data, "dimensions")
	overworld, _ := nbt.Get[nbt.Compound](dims, world.DimensionName)
	gen, _ := nbt.Get[nbt.Compound](overworld, "generator")
	s, _ := nbt.Get[nbt.String](gen, "settings")
	source, _ = nbt.Get[nbt.Compound](gen, "biome_source")
	return string(s), source
}

// TestAmplifiedLevelTypeIsKeptAcrossARestart creates an amplified world and
// reopens it asking for the normal one: the stored preset wins, so chunk
// (0, 0), never saved, regenerates as the same amplified terrain. Chunk (0, 0)
// is ocean, where the two presets agree, so a land column of chunk
// (-125, -110), 86 high amplified and 57 normal, is read as well.
func TestAmplifiedLevelTypeIsKeptAcrossARestart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	srv := startPreset(t, dir, "minecraft:amplified")
	if settings, preset := storedOverworldGenerator(t, dir); settings != "minecraft:amplified" || preset != "minecraft:overworld" {
		t.Errorf("stored generator settings %q, biome source preset %q; want minecraft:amplified, minecraft:overworld", settings, preset)
	}
	columns := [][2]int32{{0, 0}, {5, 9}, {15, 15}, {-2000, -1750}}
	first := map[[3]int32]uint16{}
	for _, c := range columns {
		for y := int32(-64); y < 320; y += 3 {
			first[[3]int32{c[0], y, c[1]}] = srv.LoadBlockAt(c[0], y, c[1])
		}
	}
	srv.Shutdown()

	// The unprefixed "normal": the harness swaps the default spelling of it
	// for its flat test ground.
	back := startPreset(t, dir, "normal")
	if settings, _ := storedOverworldGenerator(t, dir); settings != "minecraft:amplified" {
		t.Errorf("after a restart as normal the stored settings are %q, want minecraft:amplified", settings)
	}
	var differ int
	for pos, want := range first {
		if got := back.LoadBlockAt(pos[0], pos[1], pos[2]); got != want {
			if differ++; differ <= 4 {
				t.Errorf("block at %v is %d after the restart, %d before", pos, got, want)
			}
		}
	}
	if differ > 0 {
		t.Errorf("%d of %d blocks changed across the restart", differ, len(first))
	}
}

// TestLargeBiomesLevelTypeReadsTheLargeClimate starts a world asked for
// LARGE_BIOMES, which vanilla lowercases and puts in the minecraft namespace,
// and compares chunk (0, 0)'s surface biomes with the large-biomes noise's
// biome source.
func TestLargeBiomesLevelTypeReadsTheLargeClimate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	srv := startPreset(t, dir, "LARGE_BIOMES")
	if settings, preset := storedOverworldGenerator(t, dir); settings != "minecraft:large_biomes" || preset != "minecraft:overworld" {
		t.Errorf("stored generator settings %q, biome source preset %q; want minecraft:large_biomes, minecraft:overworld", settings, preset)
	}

	var cells [4][96][4]terrain.OverworldBiome
	source := terrain.NewOverworldBiomeSource(terrain.NewRandomState(terrain.OverworldLarge, 12345))
	source.FillChunk(0, 0, func(x, y, z int32, b terrain.OverworldBiome) { cells[x][y][z] = b })
	var columns, mismatches int
	srv.ScanOverworldSurfaces(0, 0, 15, 15, 319, func(x, y, z int32, _ uint16) bool {
		columns++
		got := srv.OverworldBiomeAt(x, y, z)
		if want := "minecraft:" + cells[x>>2][(y>>2)+16][z>>2].Name(); got != want {
			if mismatches++; mismatches <= 8 {
				t.Errorf("the surface at (%d, %d, %d) reads %s, the large biomes source says %s", x, y, z, got, want)
			}
		}
		return false
	})
	if columns == 0 {
		t.Fatal("chunk (0, 0) carries no terrain at all")
	}
	if mismatches > 0 {
		t.Errorf("%d of %d surface columns read a biome other than the large biomes source's", mismatches, columns)
	}
}

// TestSingleBiomeSurfaceLevelTypeIsPlainsEverywhere starts a single_biome_surface
// world and reads chunk (0, 0)'s surface biomes: all plains, where the normal
// world on this seed has ocean.
func TestSingleBiomeSurfaceLevelTypeIsPlainsEverywhere(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	srv := startPreset(t, dir, "single_biome_surface")
	settings, source := storedOverworldSource(t, dir)
	typ, _ := nbt.Get[nbt.String](source, "type")
	biome, _ := nbt.Get[nbt.String](source, "biome")
	if settings != "minecraft:overworld" || typ != "minecraft:fixed" || biome != "minecraft:plains" {
		t.Errorf("stored generator settings %q, biome source %q of %q; want minecraft:overworld, minecraft:fixed of minecraft:plains", settings, typ, biome)
	}

	var columns, mismatches int
	srv.ScanOverworldSurfaces(0, 0, 15, 15, 319, func(x, y, z int32, _ uint16) bool {
		columns++
		if got := srv.OverworldBiomeAt(x, y, z); got != "minecraft:plains" {
			if mismatches++; mismatches <= 8 {
				t.Errorf("the surface at (%d, %d, %d) reads %s, want minecraft:plains", x, y, z, got)
			}
		}
		return false
	})
	if columns == 0 {
		t.Fatal("chunk (0, 0) carries no terrain at all")
	}
	if mismatches > 0 {
		t.Errorf("%d of %d surface columns read a biome other than plains", mismatches, columns)
	}
}

// TestAnUnknownLevelTypeIsTheNormalOverworld starts worlds asked for a name
// vanilla's preset registry does not hold: the legacy largebiomes is one, as
// vanilla only consults its legacy names for a value that is no identifier.
func TestAnUnknownLevelTypeIsTheNormalOverworld(t *testing.T) {
	t.Parallel()
	for _, levelType := range []string{"largebiomes", "nonsense"} {
		t.Run(levelType, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			startPreset(t, dir, levelType)
			if settings, _ := storedOverworldGenerator(t, dir); settings != "minecraft:overworld" {
				t.Errorf("level-type %s stored settings %q, want minecraft:overworld", levelType, settings)
			}
		})
	}
}

// TestDebugLevelTypeBuildsTheBlockStateGrid starts debug_all_block_states and
// checks the barrier floor, the state grid, the immutable blocks and the fixed
// spawn, game mode and difficulty.
// vanilla: DebugLevelSource, MinecraftServer.setupDebugLevel
func TestDebugLevelTypeBuildsTheBlockStateGrid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelType = "minecraft:debug_all_block_states"
	cfg.LevelName = dir
	cfg.Difficulty = "hard"
	const name = "Digger"
	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, name))
	c.Login(name)
	c.Configure()
	var isDebug bool
	c.ReadJoin(func(id int32, data []byte) {
		if id != protocol.IDPlayLogin {
			return
		}
		r := protocol.NewReader(data)
		r.ReadI32()
		r.ReadBool()
		for range r.ReadVarInt() {
			r.ReadString(32767)
		}
		r.ReadVarInt()
		r.ReadVarInt()
		r.ReadVarInt()
		r.ReadBool()
		r.ReadBool()
		r.ReadBool()
		r.ReadVarInt()
		r.ReadString(32767)
		r.ReadI64()
		r.ReadVarInt()
		r.ReadVarInt()
		isDebug = r.ReadBool()
	})
	if !isDebug {
		t.Error("the login packet's is-debug flag is false")
	}

	barrier := registry.BlockBarrier.DefaultState()
	for _, y := range []int32{60, 70} {
		want := barrier
		if y == 70 {
			want = 0 // state 0 of the registry, at the grid's first cell
		}
		if got := srv.BlockAt(1, y, 1); got != want {
			t.Errorf("block at (1, %d, 1) = %d, want %d", y, got, want)
		}
	}
	// Grid columns advance z first: x/2 * width + z/2, width ceil(sqrt(states)).
	width := int32(math.Ceil(math.Sqrt(float64(registry.StateCount))))
	for _, cell := range []struct {
		x, z  int32
		state uint16
	}{{1, 3, 1}, {3, 1, uint16(width)}, {0, 0, 0}, {2, 2, 0}, {-1, 1, 0}} {
		if got := srv.BlockAt(cell.x, 70, cell.z); got != cell.state {
			t.Errorf("block at (%d, 70, %d) = %d, want %d", cell.x, cell.z, got, cell.state)
		}
	}

	level := servertest.ReadSavedFile(t, filepath.Join(dir, "level.dat"), "Data")
	spawn, _ := nbt.Get[nbt.Compound](level, "spawn")
	if pos, _ := nbt.Get[nbt.IntArray](spawn, "pos"); len(pos) != 3 || pos[0] != 0 || pos[1] != 80 || pos[2] != 0 {
		t.Errorf("level.dat spawn %v, want (0, 80, 0)", pos)
	}
	if d := srv.Difficulty(); d != 0 || !srv.DifficultyLocked() {
		t.Errorf("difficulty %d locked %v, want peaceful and locked", d, srv.DifficultyLocked())
	}
	if m := srv.PlayerGameMode(name); m != 3 {
		t.Errorf("game mode %d, want spectator", m)
	}

	srv.SetPlayerGameMode(name, 1)
	c.MarkLoaded()
	c.BreakBlock(1, 60, 1)
	if got := srv.BlockAt(1, 60, 1); got != barrier {
		t.Errorf("the barrier was broken: block %d", got)
	}
	if srv.SetBlock(1, 60, 1, 0) {
		t.Error("SetBlock changed a block of a debug level")
	}
	c.ChatCommand("setblock 1 65 1 stone")
	servertest.AwaitCommandFailure(t, c, "commands.setblock.failed")
	if got := srv.BlockAt(1, 65, 1); got != 0 {
		t.Errorf("/setblock placed block %d", got)
	}
	c.ChatCommand("fill 1 65 1 2 65 2 stone")
	servertest.AwaitCommandFailure(t, c, "commands.fill.failed")
}
