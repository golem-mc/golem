package world

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// spawnProtected stands a digger with shears and stone at servertest.DigTarget, an oak
// leaves block there, and the world spawn dx blocks west of it; ops.json
// names the given players.
func spawnProtected(t *testing.T, cfg *config.Config, dx int32, ops ...string) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, cfg, "Digger", servertest.OpsListing(t, ops...), servertest.SpawnProtectionRadius(cfg.SpawnProtection))
	c.MoveToGround(0.5, 64, 0.5, true)
	leaves, ok := registry.FindState("minecraft:oak_leaves", map[string]string{"persistent": "true"})
	if !ok || !srv.SetBlock(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2], leaves) {
		t.Fatal("could not stand the leaves")
	}
	srv.SetWorldSpawn(world.SpawnPos{X: servertest.DigTarget[0] - dx, Y: 64, Z: servertest.DigTarget[2]})
	c.Settle()
	return srv, c
}

// clickTally is what the server sent in the ticks after a click.
type clickTally struct {
	overlays, blockUpdates, acks int
}

// tallyAfterClick counts the overlay, Block Update and ack packets of the
// next three ticks.
func tallyAfterClick(t *testing.T, srv *server.Server, c *servertest.Client) clickTally {
	t.Helper()
	var tally clickTally
	c.ExpectNoneWithinTicks(srv, 3, func(id int32, data []byte) bool {
		switch {
		case id == protocol.IDPlaySystemChat && bytes.Contains(data, []byte("build.spawn_protection")):
			tally.overlays++
		case id == protocol.IDPlayBlockUpdate:
			tally.blockUpdates++
		case id == protocol.IDPlayAckBlockChange:
			tally.acks++
		}
		return false
	})
	return tally
}

// TestSpawnProtectionBreak: a non-op's break at or inside the radius is
// refused with the overlay and the ack, no Block Update; past it, or by an
// op, or with an empty ops.json, it goes through without the overlay.
func TestSpawnProtectionBreak(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		cfg    func() *config.Config
		dx     int32
		ops    []string
		breaks bool
	}{
		{"inside", servertest.SurvivalConfig, 10, []string{"Boss"}, false},
		{"on the edge", servertest.SurvivalConfig, 16, []string{"Boss"}, false},
		{"outside", servertest.SurvivalConfig, 17, []string{"Boss"}, true},
		{"op inside", servertest.SurvivalConfig, 10, []string{"Digger"}, true},
		{"no ops", servertest.SurvivalConfig, 10, nil, true},
		{"creative inside", servertest.BlockTestConfig, 10, []string{"Boss"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := spawnProtected(t, tc.cfg(), tc.dx, tc.ops...)
			srv.GiveSlot("Digger", player.HotbarSlot, servertest.ItemStack(t, "minecraft:shears", 1))
			c.Settle()
			c.StartDigging(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
			got := tallyAfterClick(t, srv, c)
			if tc.breaks {
				if got.overlays != 0 {
					t.Errorf("overlays = %d, want none", got.overlays)
				}
			} else if got.overlays != 1 || got.blockUpdates != 0 || got.acks != 1 {
				t.Errorf("refused break sent %+v, want 1 overlay, 0 block updates, 1 ack", got)
			}
			broken := srv.BlockAt(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2]) == 0
			if broken != tc.breaks {
				t.Errorf("block broken = %v, want %v", broken, tc.breaks)
			}
		})
	}
}

// TestSpawnProtectionPlace: a non-op's placement inside the radius is
// refused with the overlay, the two Block Updates of the clicked cell and the
// cell beside it, and the ack; nothing is set. Outside, for an op and in the
// nether it is placed.
func TestSpawnProtectionPlace(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		dx     int32
		ops    []string
		nether bool
		places bool
	}{
		{"inside", 10, []string{"Boss"}, false, false},
		{"outside", 17, []string{"Boss"}, false, true},
		{"op inside", 10, []string{"Digger"}, false, true},
		{"nether", 10, []string{"Boss"}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := spawnProtected(t, servertest.SurvivalConfig(), tc.dx, tc.ops...)
			dim := world.DimensionName
			if tc.nether {
				dim = world.NetherName
				stone, _ := registry.FindState("minecraft:stone", nil)
				srv.SetBlockInDimension(dim, servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2], stone)
				srv.MovePlayerToDimension("Digger", dim, 0.5, 64, 0.5)
				if !c.Pump(servertest.JoinTimeout, nil, func(int32, []byte) bool {
					return srv.PlayerDimension("Digger") == dim &&
						srv.ChunkLoadedInDimension(dim, servertest.DigTarget[0], servertest.DigTarget[2])
				}) {
					t.Fatal("the nether never arrived")
				}
				c.Settle()
			}
			srv.GiveSlot("Digger", player.HotbarSlot, servertest.ItemStack(t, "minecraft:stone", 1))
			c.Settle()
			c.PlaceBlock(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2], 1)
			got := tallyAfterClick(t, srv, c)
			placed := srv.BlockInDimension(dim, servertest.DigTarget[0], servertest.DigTarget[1]+1, servertest.DigTarget[2]) != 0
			if placed != tc.places {
				t.Errorf("block placed = %v, want %v", placed, tc.places)
			}
			if !tc.places && (got.overlays != 1 || got.blockUpdates != 2 || got.acks != 1) {
				t.Errorf("refused placement sent %+v, want 1 overlay, 2 block updates, 1 ack", got)
			}
			if tc.places && got.overlays != 0 {
				t.Errorf("overlays = %d, want none", got.overlays)
			}
		})
	}
}
