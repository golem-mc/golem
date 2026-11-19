package blocks

import (
	"math"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// putPortalAt fills the blocks at (x, y..y+1, z) of dimension with a portal.
func putPortalAt(t *testing.T, srv *server.Server, dimension string, x, y, z int32) {
	t.Helper()
	portal := registry.MustState("minecraft:nether_portal", map[string]string{"axis": "x"})
	for _, dy := range []int32{0, 1} {
		if !srv.SetBlockInDimension(dimension, x, y+dy, z, portal) &&
			srv.LoadBlockInDimension(dimension, x, y+dy, z) != portal {
			t.Fatalf("placing the portal block at y=%d failed", y+dy)
		}
	}
}

// clearPortalAt empties the cells putPortalAt filled.
func clearPortalAt(t *testing.T, srv *server.Server, dimension string, x, y, z int32) {
	t.Helper()
	for _, dy := range []int32{0, 1} {
		if !srv.SetBlockInDimension(dimension, x, y+dy, z, 0) {
			t.Fatalf("clearing the portal block at y=%d failed", y+dy)
		}
	}
}

// TestAllowEnteringNetherUsingPortalsRefusesTheNether checks that with the
// rule off a player in an overworld portal stays put and one in a nether
// portal still arrives in the overworld, and that turning the rule back on
// lets the first one through.
// vanilla: ServerLevel.isAllowedToEnterPortal
func TestAllowEnteringNetherUsingPortalsRefusesTheNether(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Voyager")
	servertest.HoldTicks(t, srv)
	srv.SetGameRule(world.RuleAllowEnteringNether.String(), false)

	pos := srv.PlayerPosition("Voyager")
	putPortalAt(t, srv, world.DimensionName, int32(math.Floor(pos.X)), int32(math.Floor(pos.Y)), int32(math.Floor(pos.Z)))
	servertest.RunTicks(t, srv, 5)
	// The player's trip runs on its own goroutine, off the tick: wait for
	// the server to have decided it.
	servertest.PollUntil(t, 20*time.Second, func() bool { return srv.PortalTripsDecided() == 1 })
	if got := srv.PlayerDimension("Voyager"); got != world.DimensionName {
		t.Fatalf("with the rule off the player is in %s, want to stay in the overworld", got)
	}

	// A refused trip still spends the portal cooldown, which standing in the
	// portal keeps restarting: step out for the 300 ticks it lasts, then in.
	srv.SetGameRule(world.RuleAllowEnteringNether.String(), true)
	clearPortalAt(t, srv, world.DimensionName, int32(math.Floor(pos.X)), int32(math.Floor(pos.Y)), int32(math.Floor(pos.Z)))
	servertest.RunTicks(t, srv, 320)
	putPortalAt(t, srv, world.DimensionName, int32(math.Floor(pos.X)), int32(math.Floor(pos.Y)), int32(math.Floor(pos.Z)))
	servertest.RunTicks(t, srv, 5)
	if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn); !ok {
		t.Fatal("no respawn arrived for the nether crossing")
	}
	if got := srv.PlayerDimension("Voyager"); got != world.NetherName {
		t.Fatalf("the player is in %s, want the nether", got)
	}
}

// TestAllowEnteringNetherUsingPortalsLetsAPlayerLeaveTheNether checks the
// rule off does not hold a player in the nether.
// vanilla: ServerLevel.isAllowedToEnterPortal
func TestAllowEnteringNetherUsingPortalsLetsAPlayerLeaveTheNether(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Voyager")
	servertest.HoldTicks(t, srv)
	srv.SetGameRule(world.RuleAllowEnteringNether.String(), false)

	srv.MovePlayerToDimension("Voyager", world.NetherName, 80.5, 70, -39.5)
	// Nether chunks generate on the chunk sender's wall clock, not the held
	// tick clock, so counting ticks for them is load-dependent.
	servertest.PollUntil(t, 20*time.Second, func() bool {
		return srv.ChunkLoadedInDimension(world.NetherName, 80, -40)
	})
	if got := srv.PlayerDimension("Voyager"); got != world.NetherName {
		t.Fatalf("player is in %s, want the nether", got)
	}
	if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn); !ok {
		t.Fatal("no respawn arrived for the move to the nether")
	}
	putPortalAt(t, srv, world.NetherName, 80, 70, -40)
	servertest.RunTicks(t, srv, 5)
	if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn); !ok {
		t.Fatal("no respawn arrived for the way back")
	}
	if got := srv.PlayerDimension("Voyager"); got != world.DimensionName {
		t.Fatalf("the player is in %s, want the overworld", got)
	}
}

// TestAllowEnteringNetherUsingPortalsKeepsAnItemOut checks an item lying in
// an overworld portal stays in the overworld with the rule off and crosses
// with it on.
// vanilla: Entity.handlePortal
func TestAllowEnteringNetherUsingPortalsKeepsAnItemOut(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Voyager")
	servertest.HoldTicks(t, srv)
	srv.SetGameRule(world.RuleAllowEnteringNether.String(), false)

	// The portal cells are edited across 500 ticks: keep their chunk loaded
	// so an unload between the edits cannot swallow one.
	defer srv.HoldChunkInDimension(world.DimensionName, 40, 40)()
	// The item settles first, so the portal goes where it rests.
	srv.SpawnItemEntityDelay(40.5, 64, 40.5, "minecraft:stick", 1, 32767)
	servertest.RunTicks(t, srv, 20)
	rest := srv.ItemsInDimension(world.DimensionName)
	if len(rest) != 1 {
		t.Fatalf("%d items in the overworld, want 1", len(rest))
	}
	x, y, z := int32(math.Floor(rest[0].X)), int32(math.Floor(rest[0].Y)), int32(math.Floor(rest[0].Z))
	putPortalAt(t, srv, world.DimensionName, x, y, z)
	servertest.RunTicks(t, srv, 200)
	if n := len(srv.ItemsInDimension(world.NetherName)); n != 0 {
		t.Fatalf("with the rule off %d items reached the nether", n)
	}
	if n := len(srv.ItemsInDimension(world.DimensionName)); n != 1 {
		t.Fatalf("%d items in the overworld, want the one that stayed", n)
	}

	srv.SetGameRule(world.RuleAllowEnteringNether.String(), true)
	clearPortalAt(t, srv, world.DimensionName, x, y, z)
	servertest.RunTicks(t, srv, 320)
	putPortalAt(t, srv, world.DimensionName, x, y, z)
	servertest.UntilTicks(t, srv, 400, "the item crossing once allowed", func() bool {
		return len(srv.ItemsInDimension(world.NetherName)) == 1
	})
}

// TestSpectatorsGenerateChunksOffLoadsNothingNew checks a spectator sent far
// away generates no chunks with the rule off, a survival player does, and
// switching the rule back on loads the spectator's view.
// vanilla: ChunkMap.skipPlayer
func TestSpectatorsGenerateChunksOffLoadsNothingNew(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		mode int32
		want bool
	}{
		{"survival", protocol.GameModeSurvival, true},
		{"spectator", protocol.GameModeSpectator, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Ghost", servertest.OpsListing(t, "Ghost"))
			c.SendChangeGameMode(tc.mode)
			servertest.UntilTicks(t, srv, 100, "game mode", func() bool { return srv.PlayerGameMode("Ghost") == tc.mode })
			srv.SetGameRule(world.RuleSpectatorsGenerateChunks.String(), false)

			c.ChatCommand("tp @s 3000 100 3000")
			if tc.want {
				servertest.UntilTicks(t, srv, 400, "far chunk generated", func() bool {
					return srv.ChunkLoadedInDimension(world.DimensionName, 3000, 3000)
				})
				return
			}
			servertest.UntilTicks(t, srv, 400, "teleport", func() bool { return srv.PlayerPosition("Ghost").X > 2000 })
			servertest.RunTicks(t, srv, 40)
			if srv.ChunkLoadedInDimension(world.DimensionName, 3000, 3000) {
				t.Fatal("a spectator with the rule off generated the chunk it flew to")
			}
			srv.SetGameRule(world.RuleSpectatorsGenerateChunks.String(), true)
			servertest.UntilTicks(t, srv, 400, "far chunk generated once allowed", func() bool {
				return srv.ChunkLoadedInDimension(world.DimensionName, 3000, 3000)
			})
		})
	}
}
