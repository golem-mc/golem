package world

import (
	"slices"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: EnderDragonFight.tick, scanState, spawnExitPortal
func TestEndPodiumWaitsForFirstFightScan(t *testing.T) {
	for _, tc := range []struct {
		seed string
		y    int32
	}{{"0", 62}, {"12345", 64}} {
		t.Run(tc.seed, func(t *testing.T) {
			cfg := servertest.CrossingTestConfig(t)
			cfg.LevelSeed = tc.seed
			srv, c := servertest.Join(t, cfg, "DragonWatcher", servertest.OpsListing(t, "DragonWatcher"), func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
			// The untouched surface heights come from the vanilla recordings.
			if got := srv.LoadBlockInDimension(world.EndName, 0, tc.y, 0); got != registry.BlockEndStone.DefaultState() {
				t.Fatalf("unvisited End origin = %d, want end stone before the fight scan", got)
			}
			servertest.RunTicks(t, srv, 20)
			if data := srv.SavedDragonFight(); !nbt.BoolField(data, "needs_state_scanning") || data["exit_portal_location"] != nil {
				t.Fatal("unwatched End scanned its fight")
			}
			srv.MovePlayerToDimension("DragonWatcher", world.EndName, 0, 70, 0)
			if _, ok := servertest.ReadUntilViewSent(t, c); !ok {
				t.Fatal("no End view")
			}
			servertest.HoldTicks(t, srv)
			if got := srv.BlockInDimension(world.EndName, 0, tc.y+3, 0); got != registry.BlockAir.DefaultState() {
				t.Fatalf("frozen End origin = %d, want air before the fight scan", got)
			}
			servertest.RunTicks(t, srv, 20)
			servertest.PollUntil(t, 20*time.Second, srv.DragonArenaSettled)
			servertest.RunTicks(t, srv, 1)
			data := srv.SavedDragonFight()
			pos, _ := nbt.Get[nbt.IntArray](data, "exit_portal_location")
			if !slices.Equal(pos, nbt.IntArray{0, tc.y, 0}) || nbt.BoolField(data, "needs_state_scanning") {
				t.Fatalf("first scan saved location %v, needs scanning %v", pos, data["needs_state_scanning"])
			}
			for _, p := range [][3]int32{{0, tc.y, 0}, {0, tc.y + 3, 0}, {-3, tc.y, 0}, {0, tc.y, -3}, {3, tc.y, 0}, {0, tc.y, 3}} {
				if got := srv.BlockInDimension(world.EndName, p[0], p[1], p[2]); got != registry.BlockBedrock.DefaultState() {
					t.Errorf("podium at %v = %d, want bedrock", p, got)
				}
			}
			if got := srv.BlockInDimension(world.EndName, 1, tc.y, 0); got != registry.BlockAir.DefaultState() {
				t.Errorf("inactive portal ring = %d, want air", got)
			}
			torch := registry.MustState("minecraft:wall_torch", map[string]string{"facing": "east"})
			want := []servertest.BlockChange{{X: 0, Y: tc.y + 3, Z: 0, State: registry.BlockBedrock.DefaultState()}, {X: 1, Y: tc.y + 2, Z: 0, State: torch}}
			c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
				for _, cell := range servertest.BlockChangesIn(t, id, data) {
					want = slices.DeleteFunc(want, func(expected servertest.BlockChange) bool { return expected == cell })
				}
				return len(want) == 0
			})
			if len(want) > 0 {
				t.Errorf("missing podium updates after scan: %v", want)
			}
		})
	}
}

// vanilla: EnderDragonFight.findExitPortal, spawnExitPortal
func TestEndLoneBedrockDoesNotSuppressFirstPodium(t *testing.T) {
	srv, c := servertest.Join(t, servertest.CrossingTestConfig(t), "DragonWatcher", servertest.OpsListing(t, "DragonWatcher"), func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
	srv.MovePlayerToDimension("DragonWatcher", world.EndName, 0, 85, 0)
	if _, ok := servertest.ReadUntilViewSent(t, c); !ok {
		t.Fatal("no End view")
	}
	servertest.HoldTicks(t, srv)
	srv.SetBlockInDimension(world.EndName, 0, 80, 0, registry.BlockBedrock.DefaultState())
	servertest.RunTicks(t, srv, 20)
	servertest.PollUntil(t, 20*time.Second, srv.DragonArenaSettled)
	servertest.RunTicks(t, srv, 1)
	data := srv.SavedDragonFight()
	pos, _ := nbt.Get[nbt.IntArray](data, "exit_portal_location")
	if !slices.Equal(pos, nbt.IntArray{0, 79, 0}) || nbt.BoolField(data, "needs_state_scanning") {
		t.Errorf("first scan saved location %v, needs scanning %v", pos, data["needs_state_scanning"])
	}
	for _, p := range [][3]int32{{0, 82, 0}, {-3, 79, 0}, {0, 78, 0}} {
		if got := srv.BlockInDimension(world.EndName, p[0], p[1], p[2]); got != registry.BlockBedrock.DefaultState() {
			t.Errorf("podium at %v = %d, want bedrock", p, got)
		}
	}
}
