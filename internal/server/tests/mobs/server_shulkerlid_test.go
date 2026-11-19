package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/registry"
)

// putShulker stands a shulker box facing the given way at (4, 64, 0).
func putShulker(t *testing.T, srvSet func(x, y, z int32, state uint16) bool, facing string) {
	t.Helper()
	state, ok := registry.FindState("minecraft:shulker_box", map[string]string{"facing": facing})
	if !ok || !srvSet(4, 64, 0, state) {
		t.Fatalf("cannot stand a %s shulker box", facing)
	}
}

// TestAShulkerBoxWithItsLidBlockedStaysShut checks ShulkerBoxBlock.canOpen:
// the half block in front of the box's face must be free of collision.
func TestAShulkerBoxWithItsLidBlockedStaysShut(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		facing string
		block  string
		at     [3]int32
		opens  int
	}{
		{"stone above an up box", "up", "minecraft:stone", [3]int32{4, 65, 0}, 0},
		{"air above an up box", "up", "", [3]int32{}, 1},
		{"a bottom slab above an up box", "up", "minecraft:stone_slab", [3]int32{4, 65, 0}, 0},
		{"stone in front of a north box", "north", "minecraft:stone", [3]int32{4, 64, -1}, 0},
		{"stone behind a north box", "north", "minecraft:stone", [3]int32{4, 64, 1}, 1},
		{"the floor below a down box", "down", "", [3]int32{}, 0},
		{"stone east of an east box", "east", "minecraft:stone", [3]int32{5, 64, 0}, 0},
		{"stone above a north box", "north", "minecraft:stone", [3]int32{4, 65, 0}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
			putShulker(t, srv.SetBlock, tc.facing)
			if tc.block != "" && !srv.SetBlock(tc.at[0], tc.at[1], tc.at[2], registry.BlockStates[tc.block]) {
				t.Fatal("the blocker's chunk is not loaded")
			}
			w.UseBlock(4, 64, 0)
			if got := servertest.ShulkerWindowOpens(t, w.Client); got != tc.opens {
				t.Errorf("%s: %d windows opened, want %d", tc.name, got, tc.opens)
			}
		})
	}
}

// TestAShulkerBoxTopSlabLeavesTheLidFree checks the half block: a top slab
// above an up box sits above the lid's reach.
func TestAShulkerBoxTopSlabLeavesTheLidFree(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	putShulker(t, srv.SetBlock, "up")
	slab, _ := registry.FindState("minecraft:stone_slab", map[string]string{"type": "top"})
	if !srv.SetBlock(4, 65, 0, slab) {
		t.Fatal("the slab's chunk is not loaded")
	}
	w.UseBlock(4, 64, 0)
	if got := servertest.ShulkerWindowOpens(t, w.Client); got != 1 {
		t.Errorf("%d windows opened under a top slab, want 1", got)
	}
}

// TestAShulkerBoxAlreadyOpenOpensForASecondPlayer checks that a box with its
// lid up skips the test.
func TestAShulkerBoxAlreadyOpenOpensForASecondPlayer(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	alice := servertest.WindowedOf(t, "Alice", connect())
	bob := servertest.WindowedOf(t, "Bob", connect())
	putShulker(t, srv.SetBlock, "up")

	alice.UseBlock(4, 64, 0)
	if got := servertest.ShulkerWindowOpens(t, alice.Client); got != 1 {
		t.Fatalf("the first player's windows = %d, want 1", got)
	}
	if !srv.SetBlock(4, 65, 0, registry.BlockStates["minecraft:stone"]) {
		t.Fatal("the stone's chunk is not loaded")
	}
	bob.UseBlock(4, 64, 0)
	if got := servertest.ShulkerWindowOpens(t, bob.Client); got != 1 {
		t.Errorf("the second player's windows = %d, want 1", got)
	}
}

// TestAnOpeningShulkerBoxLiftsAnItemOnItsLid checks that the lid shoves what
// rests on it up, an item and a mob alike.
// vanilla: ShulkerBoxBlockEntity.moveCollidedEntities
func TestAnOpeningShulkerBoxLiftsAnItemOnItsLid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		rise func(srv *server.Server) func() float64
	}{
		{"item", func(srv *server.Server) func() float64 {
			srv.SpawnItemEntityMotion(4.5, 65.125, 0.5, "minecraft:stone", 1, 0, 0, 0)
			return func() float64 { return servertest.SoleItemY(t, srv) }
		}},
		{"zombie", func(srv *server.Server) func() float64 {
			id := srv.SpawnMob("minecraft:zombie", 4.5, 65, 0.5)
			srv.SetMobNoAI(id)
			return func() float64 { _, y, _, _ := srv.MobPos(id); return y }
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
			// Clear the terrain the box stands in.
			for y := int32(63); y <= 68; y++ {
				for x := int32(3); x <= 5; x++ {
					for z := int32(-1); z <= 1; z++ {
						srv.SetBlock(x, y, z, registry.MustState("minecraft:air", nil))
					}
				}
			}
			srv.SetBlock(4, 63, 0, registry.MustState("minecraft:stone", nil))
			putShulker(t, srv.SetBlock, "up")
			servertest.HoldTicks(t, srv)
			y := tc.rise(srv)
			servertest.RunTicks(t, srv, 2)
			if got := y(); got > 65.2 {
				t.Fatalf("y before the lid opens = %v, want it resting on the lid", got)
			}
			w.UseBlock(4, 64, 0)
			if got := servertest.ShulkerWindowOpens(t, w.Client); got != 1 {
				t.Fatalf("%d windows opened, want 1", got)
			}
			peak := 0.0
			for range 12 {
				servertest.RunTicks(t, srv, 1)
				peak = max(peak, y())
			}
			if peak <= 65.5 {
				t.Errorf("highest y while the lid opened = %v, want above 65.5", peak)
			}
		})
	}
}
