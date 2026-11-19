package containers

// The copper chests: a right-click opens the chest's own 27- or 54-slot
// window, with the lid action and the conductor-above check, and each
// weathering stage plays its own open and close sound.

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// copperChestNames are the eight copper chest blocks, unwaxed before waxed.
var copperChestNames = []string{
	"minecraft:copper_chest",
	"minecraft:exposed_copper_chest",
	"minecraft:weathered_copper_chest",
	"minecraft:oxidized_copper_chest",
	"minecraft:waxed_copper_chest",
	"minecraft:waxed_exposed_copper_chest",
	"minecraft:waxed_weathered_copper_chest",
	"minecraft:waxed_oxidized_copper_chest",
}

// copperChestState is the block state of a single copper chest facing north.
func copperChestState(t *testing.T, name string) uint16 {
	t.Helper()
	return registry.MustState(name, map[string]string{"facing": "north", "type": "single", "waterlogged": "false"})
}

// TestACopperChestOpensLikeAChest: a single copper chest opens the chest's
// 27-slot window, ahead of which its own open sound plays, and tells the lid
// to rise naming its own block.
func TestACopperChestOpensLikeAChest(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	if !srv.SetBlock(4, 64, 0, copperChestState(t, "minecraft:copper_chest")) {
		t.Fatal("the copper chest's chunk is not loaded")
	}

	w.UseBlock(4, 64, 0)
	if name, _, _, _ := w.AwaitChestSound(); name != "minecraft:block.copper_chest.open" {
		t.Errorf("the copper chest opened to the sound %q, want minecraft:block.copper_chest.open", name)
	}
	if slots := w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0); len(slots) != 27+36 {
		t.Fatalf("the copper chest window has %d slots, want the chest's 27 and the player's 36", len(slots))
	}
	w.ExpectLidOf(4, 64, 0, 1, protocol.BlockCopperChestIDs["copper_chest"])
}

// TestACopperChestsHingeSoundFollowsItsStage: each weathering stage has its
// own open and close sound, the exposed stage sharing the unaffected one and
// the waxed variants sharing their unwaxed stage's.
// vanilla: CopperChestBlock's open and close sounds
func TestACopperChestsHingeSoundFollowsItsStage(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	open := map[string]string{
		"minecraft:copper_chest":                 "minecraft:block.copper_chest.open",
		"minecraft:exposed_copper_chest":         "minecraft:block.copper_chest.open",
		"minecraft:weathered_copper_chest":       "minecraft:block.copper_chest_weathered.open",
		"minecraft:oxidized_copper_chest":        "minecraft:block.copper_chest_oxidized.open",
		"minecraft:waxed_copper_chest":           "minecraft:block.copper_chest.open",
		"minecraft:waxed_exposed_copper_chest":   "minecraft:block.copper_chest.open",
		"minecraft:waxed_weathered_copper_chest": "minecraft:block.copper_chest_weathered.open",
		"minecraft:waxed_oxidized_copper_chest":  "minecraft:block.copper_chest_oxidized.open",
	}
	for i, name := range copperChestNames {
		x := int32(4 + i)
		if !srv.SetBlock(x, 64, 0, copperChestState(t, name)) {
			t.Fatalf("%s's chunk is not loaded", name)
		}
		// The blocks share a chunk but spread past arm's reach, so the
		// player steps in front of each before using it.
		w.FacePlayer(float64(x)+0.5, 64, 3.5, 0, 0)
		w.UseBlock(x, 64, 0)
		if got, _, _, _ := w.AwaitChestSound(); got != open[name] {
			t.Errorf("%s opened to the sound %q, want %q", name, got, open[name])
		}
		w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, x, 64, 0)
		w.CloseWindow()
		w.ExpectWindowZero()
		wantClose := open[name][:len(open[name])-len(".open")] + ".close"
		if got, _, _, _ := w.AwaitChestSound(); got != wantClose {
			t.Errorf("%s closed to the sound %q, want %q", name, got, wantClose)
		}
	}
}
