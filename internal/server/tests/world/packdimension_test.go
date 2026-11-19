package world

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// writePack writes a data pack named p under the world with the given files.
func writePack(t *testing.T, level string, files map[string]string) {
	t.Helper()
	pack := filepath.Join(level, "datapacks", "p")
	files["pack.mcmeta"] = `{"pack":{"description":"p"}}`
	for name, body := range files {
		path := filepath.Join(pack, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestAPackDimensionIsCreatedSavedAndRestored checks that a pack's flat level
// stem becomes a dimension the Login packet names, the player can travel to,
// whose chunks come from the stem's layers and are saved under
// dimensions/pk/void, and where a restarted world keeps the player; a stem
// with an unsupported generator is skipped and the server starts anyway.
// vanilla: MinecraftServer.createLevels, LevelStem
func TestAPackDimensionIsCreatedSavedAndRestored(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	writePack(t, cfg.LevelName, map[string]string{
		"data/pk/dimension/void.json": `{"type":"minecraft:overworld","generator":{"type":"minecraft:flat",
			"settings":{"biome":"minecraft:plains","layers":[{"block":"minecraft:glass","height":1}]}}}`,
		"data/pk/dimension/fixed.json": `{"type":"minecraft:overworld","generator":{"type":"minecraft:noise","settings":"minecraft:overworld",
			"biome_source":{"type":"minecraft:fixed","biome":"minecraft:plains"}}}`,
		"data/pk/dimension/odd.json": `{"type":"minecraft:overworld","generator":{"type":"minecraft:debug"}}`,
	})

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Voyager"), servertest.VanillaSaves)
	c := connect()
	c.Login("Voyager")
	c.Configure()
	var login []byte
	c.ReadJoin(func(id int32, data []byte) {
		if id == protocol.IDPlayLogin {
			login = data
		}
	})
	c.MarkLoaded()
	r := protocol.NewReader(login)
	r.ReadI32()
	r.ReadBool()
	var names []string
	for range r.ReadVarInt() {
		names = append(names, r.ReadString(32767))
	}
	if !slices.Contains(names, "pk:void") || slices.Contains(names, "pk:odd") || slices.Contains(names, "pk:fixed") {
		t.Errorf("login dimensions = %v, want pk:void and not pk:odd or pk:fixed", names)
	}

	c.ChatCommand("execute in pk:void run tp @s 0 -63 0")
	data := c.ExpectWithinTicks(srv, 200, protocol.IDPlayRespawn)
	r = protocol.NewReader(data)
	if got := r.ReadVarInt(); got != registry.MustID("minecraft:dimension_type", "minecraft:overworld") {
		t.Errorf("respawn dimension type = %d, want the overworld's", got)
	}
	if got := r.ReadString(32767); got != "pk:void" {
		t.Errorf("respawn dimension = %q, want pk:void", got)
	}
	servertest.UntilTicks(t, srv, 200, "the chunk of pk:void", func() bool {
		return srv.BlockInDimension("pk:void", 0, -64, 0) != 0
	})
	if got := srv.BlockInDimension("pk:void", 0, -64, 0); got != registry.BlockStates["minecraft:glass"] {
		t.Errorf("block at y=-64 = %d, want glass", got)
	}
	if got := srv.BlockInDimension("pk:void", 0, -63, 0); got != registry.BlockStates["minecraft:air"] {
		t.Errorf("block at y=-63 = %d, want air", got)
	}

	c.ChatCommand("save-all flush")
	region := filepath.Join(cfg.LevelName, "dimensions", "pk", "void", "region")
	servertest.UntilTicks(t, srv, 200, "the region file", func() bool {
		entries, _ := os.ReadDir(region)
		return len(entries) > 0
	})
	srv.Shutdown()

	restarted := *cfg
	again, reconnect := servertest.Start(t, &restarted, servertest.VanillaSaves)
	reconnect().Join("Voyager")
	if got := again.PlayerDimension("Voyager"); got != "pk:void" {
		t.Errorf("player dimension after restart = %q, want pk:void", got)
	}
}

// vanilla: EnvironmentAttributeSystem.addDynamicLayers, Mob.isSunBurnTick,
// DaylightDetectorBlock.updateSignalStrength
func TestAPackOverworldDimensionBurnsMonstersAndReadsDaylight(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	writePack(t, cfg.LevelName, map[string]string{
		"data/pk/dimension/sky.json": `{"type":"minecraft:overworld","generator":{"type":"minecraft:flat",
			"settings":{"biome":"minecraft:plains","layers":[{"block":"minecraft:stone","height":128}]}}}`,
	})
	srv, c := servertest.Join(t, cfg, "Voyager", servertest.OpsListing(t, "Voyager"))
	servertest.HoldTicks(t, srv)
	srv.SetPaused(true)
	srv.SetDayTime(6000)
	for _, dimension := range []string{"minecraft:overworld", "pk:sky"} {
		c.ChatCommand("execute in " + dimension + " run forceload add 0 0")
		c.ChatCommand("execute in " + dimension + " run setblock 6 64 6 minecraft:daylight_detector")
	}
	c.ChatCommand("execute in pk:sky run tp @s 0 64 0")
	c.ExpectWithinTicks(srv, 200, protocol.IDPlayRespawn)
	c.Settle()
	servertest.UntilTicks(t, srv, 200, "the pack dimension's detector", func() bool {
		return registry.BlockOf(srv.BlockInDimension("pk:sky", 6, 64, 6)) == registry.BlockDaylightDetector
	})
	overworldMob := srv.SpawnMobInDimension("minecraft:zombie", "minecraft:overworld", 3.5, 64, 3.5)
	packMob := srv.SpawnMobInDimension("minecraft:zombie", "pk:sky", 3.5, 64, 3.5)
	if !c.ReadTicks(srv, 200, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityMetadata {
			return false
		}
		m := servertest.ReadZombieMetadata(c, data)
		return m.EntityID == packMob && m.HasFire && m.OnFire
	}) {
		t.Error("the pack dimension's zombie did not catch fire at noon")
	}
	if srv.MobFireTicks(overworldMob) == 0 {
		t.Error("the overworld's zombie did not catch fire at noon")
	}
	// Noon alone cannot catch a missing sun track: both a constant angle of
	// zero and the noon sun give full power. Midnight must read zero too.
	for _, tc := range []struct {
		time  int64
		power int
	}{{6000, 15}, {0, 7}, {18000, 0}} {
		srv.SetDayTime(tc.time)
		servertest.RunTicks(t, srv, 40)
		for _, dimension := range []string{"minecraft:overworld", "pk:sky"} {
			state := srv.BlockInDimension(dimension, 6, 64, 6)
			if got := registry.StatePower(state); got != tc.power {
				t.Errorf("%s detector at time %d = %d, want %d", dimension, tc.time, got, tc.power)
			}
		}
	}
}

// vanilla: AbstractBedBlock.getBedRule, BedBlock.destroyOnUse, StrawBedBlock.destroyBed
func TestAPackDimensionUsesItsTypesBedRule(t *testing.T) {
	for _, dimensionType := range []string{"minecraft:the_nether", "minecraft:the_end"} {
		for _, straw := range []bool{false, true} {
			bed := "minecraft:red_bed"
			if straw {
				bed = "minecraft:straw_bed"
			}
			t.Run(dimensionType+"/"+bed, func(t *testing.T) {
				t.Parallel()
				cfg := servertest.BlockTestConfig()
				cfg.LevelName = t.TempDir()
				writePack(t, cfg.LevelName, map[string]string{
					"data/pk/dimension/hot.json": `{"type":"` + dimensionType + `","generator":{"type":"minecraft:flat",
						"settings":{"biome":"minecraft:plains","layers":[{"block":"minecraft:glass","height":1}]}}}`,
				})
				srv, c := servertest.Join(t, cfg, "Voyager", servertest.OpsListing(t, "Voyager"))
				c.ChatCommand("execute in pk:hot run tp @s 8.5 1 6.5")
				c.ExpectWithinTicks(srv, 200, protocol.IDPlayRespawn)
				servertest.AwaitChunksSettled(t, srv)
				servertest.UntilTicks(t, srv, 200, "the bed's chunk in pk:hot", func() bool {
					return srv.ChunkLoadedInDimension("pk:hot", 8, 8)
				})
				c.Settle()
				servertest.HoldTicks(t, srv)
				for _, half := range []struct {
					part string
					z    int32
				}{{"foot", 8}, {"head", 9}} {
					state := servertest.MustState(t, bed, map[string]string{"part": half.part, "facing": "south"})
					if !srv.SetBlockInDimension("pk:hot", 8, 1, half.z, state) {
						t.Fatal("could not stand the bed in pk:hot")
					}
				}

				c.UseBlock(8, 1, 8)
				exploded := false
				c.SettleAndCollect(func(id int32, _ []byte) {
					exploded = exploded || id == protocol.IDPlayExplosion
				})
				if exploded != !straw {
					t.Errorf("explosion = %v, want %v", exploded, !straw)
				}
				for _, z := range []int32{8, 9} {
					if got := srv.BlockInDimension("pk:hot", 8, 1, z); registry.BlockOf(got).InGroup(registry.GroupBed) {
						t.Errorf("bed half at z=%d survived use: state %d", z, got)
					}
				}
				if srv.Sleeping("Voyager") {
					t.Error("using the bed put the player to sleep")
				}
				if _, _, ok := srv.SpawnPointOf("Voyager"); ok {
					t.Error("using the bed set a spawn point")
				}
			})
		}
	}
}
