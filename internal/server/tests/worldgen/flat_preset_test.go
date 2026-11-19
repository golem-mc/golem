package worldgen

import (
	"fmt"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestFlatLevelTypeIsTheVanillaDefaultPreset joins a server asked for
// level-type minecraft:flat and reads the world it was given: the vanilla
// default flat preset — bedrock, two dirt and a grass_block up from the
// bottom of the world, open sky above — and a fresh player set down on the
// grass at spawn. The stone ground the rest of this suite builds on comes
// from the harness's own level-type, not from this one.
func TestFlatLevelTypeIsTheVanillaDefaultPreset(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelType = "minecraft:flat"
	cfg.LevelName = t.TempDir()

	srv, connect := servertest.Start(t, cfg)

	name := func(x, y, z int32) string {
		n, _, ok := registry.BlockStateName(srv.LoadBlockAt(x, y, z))
		if !ok {
			return ""
		}
		return n
	}
	// The ground is flat, so the spawn column and one away from it answer
	// the same, and the sky over the grass stays open.
	for _, x := range []int32{0, 5} {
		for _, at := range []struct {
			y    int32
			want string
		}{
			{-64, "minecraft:bedrock"},
			{-63, "minecraft:dirt"},
			{-62, "minecraft:dirt"},
			{-61, "minecraft:grass_block"},
			{-60, "minecraft:air"},
			{0, "minecraft:air"},
		} {
			if got := name(x, at.y, x); got != at.want {
				t.Errorf("block at (%d, %d, %d) = %q, want %q", x, at.y, x, got, at.want)
			}
		}
	}

	c := connect()
	c.Login("Testificate")
	c.Configure()
	if pos := c.SpawnedAt(); pos.X != 0.5 || pos.Y != -60 || pos.Z != 0.5 {
		t.Errorf("fresh join put the player at (%g, %g, %g), want (0.5, -60, 0.5)", pos.X, pos.Y, pos.Z)
	}
	c.EnterWorld("Testificate")
}

// TestFlatWorldIsAnnouncedFlatToTheClient reads the Login (play) packet a
// flat world's join opens the play phase with: its Is Flat flag and sea
// level are the flat world's own — the flag true, the level one above the
// world's floor — where a noise world's are false and 63.
func TestFlatWorldIsAnnouncedFlatToTheClient(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelType = "minecraft:flat"
	cfg.LevelName = t.TempDir()

	_, connect := servertest.Start(t, cfg)

	c := connect()
	c.Login("Testificate")
	c.Configure()
	tail := servertest.ReadLoginTail(t, c.ExpectNext(protocol.IDPlayLogin))
	if !tail.Superflat {
		t.Error("the login packet said the world is not flat")
	}
	if tail.HasDeath {
		t.Error("the login packet carried a death location for a living player")
	}
	if tail.SeaLevel != -63 {
		t.Errorf("the login packet's sea level = %d, want -63", tail.SeaLevel)
	}
	c.EnterWorld("Testificate")
}

// TestTheStoredFlatPresetReopensWithItsGround joins a server whose world
// records a flat preset of its own — the stone up 128 a world this server
// built before the vanilla default preset was kept does — and reads the
// world back: the stored ground and its spawn above it, not the vanilla
// default the level-type alone would make.
func TestTheStoredFlatPresetReopensWithItsGround(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// The file a vanilla server leaves behind, or an older build of this
	// one did: the flat generator its layers make, without which the
	// level-type's own default preset would open the world.
	data, err := nbt.Encode("", nbt.Compound{
		// Any data version: the reader takes the data compound.
		"DataVersion": nbt.Int(4440),
		"data": nbt.Compound{
			"seed":                nbt.Long(0),
			"generate_structures": nbt.Byte(0),
			"dimensions": nbt.Compound{
				world.DimensionName: nbt.Compound{
					"type": nbt.String(world.DimensionType),
					"generator": nbt.Compound{
						"type": nbt.String("minecraft:flat"),
						"settings": nbt.Compound{
							"biome": nbt.String("minecraft:plains"),
							"layers": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
								nbt.Compound{"block": nbt.String("minecraft:stone"), "height": nbt.Int(128)},
							}},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "data", "minecraft", "world_gen_settings.dat")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := servertest.WriteGzip(path, data); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelType = "minecraft:flat"
	cfg.LevelName = dir
	srv, connect := servertest.Start(t, cfg)

	// The ground is flat, so one column stands for all of them: stone to
	// the top of the world's ground and sky above.
	if got := srv.LoadBlockAt(0, 63, 0); got != registry.BlockStates["minecraft:stone"] {
		t.Errorf("the ground under spawn is %d, want stone", got)
	}
	if got := srv.LoadBlockAt(0, 64, 0); got != registry.BlockStates["minecraft:air"] {
		t.Errorf("above the ground is %d, want air", got)
	}

	c := connect()
	c.Login("Testificate")
	c.Configure()
	if pos := c.SpawnedAt(); pos.X != 0.5 || pos.Y != 64 || pos.Z != 0.5 {
		t.Errorf("fresh join put the player at (%g, %g, %g), want (0.5, 64, 0.5)", pos.X, pos.Y, pos.Z)
	}
	c.EnterWorld("Testificate")
}

// startFlatWith starts a server on level-type levelType with generator-settings
// settings in a fresh directory, and joins a player if join is set.
func startFlatWith(t *testing.T, levelType, settings string, join bool) (*server.Server, string) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelType = levelType
	cfg.LevelSeed = "12345"
	cfg.GeneratorSettings = settings
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg)
	servertest.StopTicking(t, srv)
	if !join {
		return srv, cfg.LevelName
	}
	// A joined player keeps the spawn chunks loaded for the biome read.
	c := connect()
	c.Login("Testificate")
	c.Configure()
	c.EnterWorld("Testificate")
	return srv, cfg.LevelName
}

// storedGenerator reads the overworld generator compound of the
// world_gen_settings.dat in dir.
func storedGenerator(t *testing.T, dir string) nbt.Compound {
	t.Helper()
	data := servertest.ReadSavedFile(t, filepath.Join(dir, "data", "minecraft", "world_gen_settings.dat"), "data")
	dims, _ := nbt.Get[nbt.Compound](data, "dimensions")
	overworld, _ := nbt.Get[nbt.Compound](dims, world.DimensionName)
	gen, _ := nbt.Get[nbt.Compound](overworld, "generator")
	return gen
}

func TestFlatForestLakeStoneIsNotReplacedByOre(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelType = "minecraft:flat"
	cfg.LevelSeed = "0"
	cfg.LevelName = t.TempDir()
	cfg.GeneratorSettings = `{"biome":"minecraft:forest","features":true,"lakes":true,"layers":[{"block":"minecraft:bedrock","height":1},{"block":"minecraft:dirt","height":3},{"block":"minecraft:grass_block","height":1}]}`
	srv, _ := servertest.Start(t, cfg)
	servertest.StopTicking(t, srv)
	// Vanilla 26.3 leaves the lava lake's stone rim intact in both chunks.
	for _, p := range [][3]int32{{65, -61, -33}, {66, -62, -31}} {
		if got := registry.BlockOf(srv.LoadBlockAt(p[0], p[1], p[2])); got != registry.BlockStone {
			t.Errorf("lake rim at %v = %s, want stone", p, got.Name())
		}
	}
}

// TestFlatGeneratorSettingsBuildTheLayersAndBiome starts a flat world whose
// generator-settings name layers and a biome, and reads them back from the
// world and from world_gen_settings.dat.
func TestFlatGeneratorSettingsBuildTheLayersAndBiome(t *testing.T) {
	t.Parallel()
	srv, dir := startFlatWith(t, "minecraft:flat",
		`{"layers":[{"block":"minecraft:bedrock","height":1},{"block":"stone","height":3},{"block":"minecraft:sand","height":1}],"biome":"minecraft:desert"}`, true)
	for y, want := range map[int32]string{
		-64: "minecraft:bedrock", -63: "minecraft:stone", -62: "minecraft:stone",
		-61: "minecraft:stone", -60: "minecraft:sand", -59: "minecraft:air",
	} {
		n, _, _ := registry.BlockStateName(srv.LoadBlockAt(0, y, 0))
		if n != want {
			t.Errorf("block at y=%d = %q, want %q", y, n, want)
		}
	}
	if got := srv.OverworldBiomeAt(0, -60, 0); got != "minecraft:desert" {
		t.Errorf("biome = %q, want minecraft:desert", got)
	}
	gen := storedGenerator(t, dir)
	settings, _ := nbt.Get[nbt.Compound](gen, "settings")
	if b, _ := nbt.Get[nbt.String](settings, "biome"); b != "minecraft:desert" {
		t.Errorf("stored biome = %q, want minecraft:desert", b)
	}
	layers, _ := nbt.Get[nbt.List](settings, "layers")
	var got []string
	for _, item := range layers.Items {
		l, _ := item.(nbt.Compound)
		b, _ := nbt.Get[nbt.String](l, "block")
		h, _ := nbt.Get[nbt.Int](l, "height")
		got = append(got, fmt.Sprintf("%s*%d", b, h))
	}
	want := []string{"minecraft:bedrock*1", "minecraft:stone*3", "minecraft:sand*1"}
	if !slices.Equal(got, want) {
		t.Errorf("stored layers = %v, want %v", got, want)
	}
}

// assertColumn checks the blocks at x=0, z=0 against want, keyed by y.
func assertColumn(t *testing.T, srv *server.Server, label string, want map[int32]string) {
	t.Helper()
	for y, w := range want {
		n, _, _ := registry.BlockStateName(srv.LoadBlockAt(0, y, 0))
		if n != w {
			t.Errorf("%s: block at y=%d = %q, want %q", label, y, n, w)
		}
	}
}

// storedBiome reads the biome world_gen_settings.dat in dir records.
func storedBiome(t *testing.T, dir string) nbt.String {
	t.Helper()
	settings, _ := nbt.Get[nbt.Compound](storedGenerator(t, dir), "settings")
	b, _ := nbt.Get[nbt.String](settings, "biome")
	return b
}

// TestFlatGeneratorSettingsWithoutLayersKeepTheDefaultPreset checks that a
// value with no layers list leaves the default bedrock, dirt and grass.
// vanilla: DedicatedServerProperties (resultOrPartial)
func TestFlatGeneratorSettingsWithoutLayersKeepTheDefaultPreset(t *testing.T) {
	t.Parallel()
	for _, settings := range []string{`{}`, `{"layers":"stone"}`} {
		srv, _ := startFlatWith(t, "minecraft:flat", settings, false)
		assertColumn(t, srv, settings, map[int32]string{
			-64: "minecraft:bedrock", -63: "minecraft:dirt", -62: "minecraft:dirt",
			-61: "minecraft:grass_block", -60: "minecraft:air",
		})
	}
}

// TestBadFlatGeneratorSettingsLayersAreDropped checks that an unknown block, a
// height out of range or a layer missing a field drops that layer only, and
// that the remaining layers are kept.
// vanilla: ListCodec (partial result), FlatLayerInfo.CODEC
func TestBadFlatGeneratorSettingsLayersAreDropped(t *testing.T) {
	t.Parallel()
	srv, _ := startFlatWith(t, "minecraft:flat",
		`{"layers":[{"block":"minecraft:nope","height":1},{"block":"stone","height":2},{"block":"dirt","height":-1},{"block":"dirt"},7,{"block":"sand","height":1}]}`, false)
	assertColumn(t, srv, "mixed", map[int32]string{
		-64: "minecraft:stone", -63: "minecraft:stone", -62: "minecraft:sand", -61: "minecraft:air",
	})
	srv, _ = startFlatWith(t, "minecraft:flat", `{"layers":[{"block":"minecraft:nope","height":1}]}`, false)
	assertColumn(t, srv, "only unknown", map[int32]string{-64: "minecraft:air"})
}

// TestFlatGeneratorSettingsBiomeIsLenient checks that a biome without a
// namespace resolves and that a non-string biome is plains with the layers
// kept.
// vanilla: Biome.CODEC, Identifier.CODEC, FlatLevelGeneratorSettings.CODEC (lenientOptionalFieldOf)
func TestFlatGeneratorSettingsBiomeIsLenient(t *testing.T) {
	t.Parallel()
	_, dir := startFlatWith(t, "minecraft:flat", `{"layers":[{"block":"stone","height":1}],"biome":"desert"}`, false)
	if b := storedBiome(t, dir); b != "minecraft:desert" {
		t.Errorf("biome desert: stored biome = %q, want minecraft:desert", b)
	}
	srv, dir := startFlatWith(t, "minecraft:flat", `{"layers":[{"block":"stone","height":1}],"biome":5}`, false)
	if b := storedBiome(t, dir); b != "minecraft:plains" {
		t.Errorf("biome 5: stored biome = %q, want minecraft:plains", b)
	}
	assertColumn(t, srv, "biome 5", map[int32]string{-64: "minecraft:stone", -63: "minecraft:air"})
}

// TestFlatGeneratorSettingsWithAnUnknownBiomeUsePlains checks the biome
// fallback while the layers are kept.
func TestFlatGeneratorSettingsWithAnUnknownBiomeUsePlains(t *testing.T) {
	t.Parallel()
	srv, _ := startFlatWith(t, "minecraft:flat",
		`{"layers":[{"block":"minecraft:stone","height":2}],"biome":"minecraft:nope"}`, true)
	for y, want := range map[int32]string{-64: "minecraft:stone", -63: "minecraft:stone", -62: "minecraft:air"} {
		n, _, _ := registry.BlockStateName(srv.LoadBlockAt(0, y, 0))
		if n != want {
			t.Errorf("block at y=%d = %q, want %q", y, n, want)
		}
	}
	if got := srv.OverworldBiomeAt(0, -60, 0); got != "minecraft:plains" {
		t.Errorf("biome = %q, want minecraft:plains", got)
	}
}

// TestGeneratorSettingsDoNotAffectANormalWorld checks that a seeded world
// ignores generator-settings.
func TestGeneratorSettingsDoNotAffectANormalWorld(t *testing.T) {
	t.Parallel()
	_, dir := startFlatWith(t, "normal",
		`{"layers":[{"block":"minecraft:stone","height":2}],"biome":"minecraft:desert"}`, false)
	if kind, _ := nbt.Get[nbt.String](storedGenerator(t, dir), "type"); kind != "minecraft:noise" {
		t.Errorf("generator type = %q, want minecraft:noise", kind)
	}
}

// TestADefaultFlatWorldHasItsVillagesWhenStructuresGenerate joins a default
// flat server with generate-structures on and off and asks /locate for a
// plains village: found with it on, not found with it off.
// vanilla: FlatLevelGeneratorSettings.getDefault, WorldOptions.generateStructures
func TestADefaultFlatWorldHasItsVillagesWhenStructuresGenerate(t *testing.T) {
	t.Parallel()
	for _, generate := range []bool{true, false} {
		t.Run(fmt.Sprint(generate), func(t *testing.T) {
			t.Parallel()
			cfg := servertest.BlockTestConfig()
			cfg.LevelType = "minecraft:flat"
			cfg.LevelSeed = "0"
			cfg.GenerateStructures = generate
			srv, c := servertest.Join(t, cfg, "Locator", servertest.OpsListing(t, "Locator"))
			srv.StopTick()
			c.ChatCommand("locate structure minecraft:village_plains")
			if generate {
				servertest.AwaitSystemChatSaying(t, c, "commands.locate.structure.success")
			} else {
				servertest.AwaitCommandErrorText(t, c, "commands.locate.structure.not_found", protocol.Text{Text: "minecraft:village_plains"})
			}
		})
	}
}

// TestDefaultFlatWorldGrowsNoFirstGenerationAnimals grows the first-generation
// animals of the chunks around spawn in a default flat world, whose village
// and stronghold structure sets make it a flat-mode overworld generator: none
// grow.
// vanilla: FlatLevelSource.spawnOriginalMobs is empty
func TestDefaultFlatWorldGrowsNoFirstGenerationAnimals(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelType = "minecraft:flat"
	cfg.LevelSeed = "0"
	cfg.LevelName = t.TempDir()
	srv, _ := servertest.Start(t, cfg)
	servertest.StopTicking(t, srv)

	for x := int32(-4); x <= 4; x++ {
		for z := int32(-4); z <= 4; z++ {
			if mobs := srv.GrowNaturalMobsForTest(world.ChunkPos{X: x, Z: z}); len(mobs) != 0 {
				t.Errorf("chunk (%d, %d) grew %d first-generation mobs, want none", x, z, len(mobs))
			}
		}
	}
}
