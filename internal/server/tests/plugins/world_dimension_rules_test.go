package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// TestPluginWorldDimensionRules checks that plugin worlds created with
// minecraft:the_nether or minecraft:overworld follow their dimension type's
// rules rather than defaulting to the overworld or lacking sky light.
// vanilla: BucketItem.emptyContents, RespawnAnchorBlock.canSetSpawn,
// ServerPlayer.findRespawnAndUseSpawnBlock, PlayerList.respawn
func TestPluginWorldDimensionRules(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, h, run := worldPlugin(t, cfg)
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Join("Tester")

	netherKey := plugin.Key{Namespace: "test", Path: "nether"}
	overKey := plugin.Key{Namespace: "test", Path: "over"}

	var (
		netherWorld, overWorld plugin.World
		respawns               int
		respawnBed             bool
		respawnAnchor          bool
		respawnWorld           plugin.Key
	)
	run(func() {
		var err error
		netherWorld, err = h.CreateWorld(plugin.WorldOptions{
			Key:       netherKey,
			Dimension: plugin.MinecraftKey("the_nether"),
			Generator: &flatGen{},
		})
		if err != nil {
			t.Errorf("CreateWorld(test:nether): %v", err)
		}
		overWorld, err = h.CreateWorld(plugin.WorldOptions{
			Key:       overKey,
			Dimension: plugin.MinecraftKey("overworld"),
			Generator: &flatGen{},
		})
		if err != nil {
			t.Errorf("CreateWorld(test:over): %v", err)
		}
		plugin.Listen(h, func(e *plugin.PlayerRespawnEvent) {
			respawns++
			respawnBed = e.Bed
			respawnAnchor = e.Anchor
			if e.Location.World != nil {
				respawnWorld = e.Location.World.Key()
			}
		})
	})
	if netherWorld == nil || overWorld == nil {
		t.FailNow()
	}

	moveTo := func(dim string, x, y, z float64) {
		t.Helper()
		srv.MovePlayerToDimension("Tester", dim, x, y, z)
		if !c.Pump(servertest.ExpectTimeout, nil, func(int32, []byte) bool {
			return srv.PlayerDimension("Tester") == dim &&
				srv.ChunkLoadedInDimension(dim, int32(x), int32(z))
		}) {
			t.Fatalf("did not arrive in %s", dim)
		}
		c.StandAt(x, y, z, 0)
	}

	// Move player into test:nether (type minecraft:the_nether).
	moveTo("test:nether", 8.5, 4, 8.5)

	// 1. Emptying a water bucket evaporates instead of placing water.
	srv.GiveSlot("Tester", player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.ItemIDOrDie(t, "minecraft:water_bucket"),
		Count:  1,
	})
	c.PlaceBlock(8, 3, 9, protocol.FaceTop)
	c.Settle()
	if got := srv.BlockInDimension("test:nether", 8, 4, 9); got != 0 {
		name, _ := servertest.StateProps(got)
		t.Errorf("water bucket in test:nether left %q at (8,4,9), want air", name)
	}

	// 2. Respawn anchor charged with glowstone sets spawn instead of exploding,
	//    and respawning after death returns to test:nether spending one charge.
	anchorEmpty := registry.MustState("minecraft:respawn_anchor", map[string]string{"charges": "0"})
	srv.SetBlockInDimension("test:nether", 9, 4, 8, anchorEmpty)
	srv.GiveSlot("Tester", player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.ItemIDOrDie(t, "minecraft:glowstone"),
		Count:  1,
	})
	c.PlaceBlock(9, 4, 8, protocol.FaceTop)
	c.Settle()
	if name, props := servertest.StateProps(srv.BlockInDimension("test:nether", 9, 4, 8)); name != "minecraft:respawn_anchor" || props["charges"] != "1" {
		t.Fatalf("after glowstone charge: block = %q charges=%q, want respawn_anchor charges=1", name, props["charges"])
	}

	// Bare-hand click on the charged anchor sets spawn and does not explode.
	c.UseBlock(9, 4, 8)
	c.ExpectSystemChat("block.minecraft.set_spawn", false)
	c.Settle()
	if name, props := servertest.StateProps(srv.BlockInDimension("test:nether", 9, 4, 8)); name != "minecraft:respawn_anchor" || props["charges"] != "1" {
		t.Fatalf("charged anchor in test:nether exploded or changed: %q charges=%q", name, props["charges"])
	}
	if spawn, forced, ok := srv.SpawnPointOf("Tester"); !ok || forced || spawn != (world.SpawnPos{X: 9, Y: 4, Z: 8}) {
		t.Errorf("spawn point = (%d,%d,%d) forced=%v ok=%v, want (9,4,8) unforced", spawn.X, spawn.Y, spawn.Z, forced, ok)
	}

	if !srv.Hurt("Tester", 50) {
		t.Fatal("failed to kill player")
	}
	c.Settle()
	// Release ticks across respawn so the PlayerRespawnEvent listener runs on
	// the tick loop while PerformRespawn waits for it.
	servertest.ReleaseTicks(t, srv)
	c.PerformRespawn()
	_, noBlock := c.ExpectRespawnThroughTeleport()
	if noBlock {
		t.Error("respawn in test:nether reported no respawn block")
	}
	if got := srv.PlayerDimension("Tester"); got != "test:nether" {
		t.Errorf("respawned in %q, want test:nether", got)
	}
	if name, props := servertest.StateProps(srv.BlockInDimension("test:nether", 9, 4, 8)); name != "minecraft:respawn_anchor" || props["charges"] != "0" {
		t.Errorf("after respawn: anchor = %q charges=%q, want charges=0", name, props["charges"])
	}
	c.MarkLoaded()
	servertest.HoldTicks(t, srv)

	run(func() {
		if respawns != 1 || !respawnAnchor || respawnBed || respawnWorld != netherKey {
			t.Errorf("PlayerRespawnEvent: count=%d anchor=%v bed=%v world=%v, want 1 true false %v",
				respawns, respawnAnchor, respawnBed, respawnWorld, netherKey)
		}
		if sky, _ := netherWorld.Light(plugin.BlockPos{X: 8, Y: 5, Z: 8}); sky != 0 {
			t.Errorf("test:nether sky light = %d, want 0", sky)
		}
	})

	// 3. Move player into test:over (type minecraft:overworld) and verify sky light.
	moveTo("test:over", 8.5, 4, 8.5)
	run(func() {
		if sky, _ := overWorld.Light(plugin.BlockPos{X: 8, Y: 5, Z: 8}); sky != 15 {
			t.Errorf("test:over sky light = %d, want 15", sky)
		}
	})
}
