package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

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
