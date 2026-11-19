package containers

// The copper chests: a right-click opens the chest's own 27- or 54-slot
// window, with the lid action and the conductor-above check, and each
// weathering stage plays its own open and close sound.

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
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

// TestAWeatheredCopperChestPairOpens54Slots: two weathered copper chests
// placed side by side pair, and either half opens the pair's 54-slot window.
// vanilla: CopperChestBlock.getStateForPlacement, ChestPair
func TestAWeatheredCopperChestPairOpens54Slots(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	servertest.PlaceChestPair(t, w, "minecraft:weathered_copper_chest", 4, 64, 0)
	if props := chestPropsOf(t, srv, 4, 64, 0, "minecraft:weathered_copper_chest"); props["type"] != "left" {
		t.Errorf("the first weathered copper chest stands %v, want the left half", props)
	}
	if props := chestPropsOf(t, srv, 5, 64, 0, "minecraft:weathered_copper_chest"); props["type"] != "right" {
		t.Errorf("the second weathered copper chest stands %v, want the right half", props)
	}

	w.UseBlock(4, 64, 0)
	if name, _, _, _ := w.AwaitChestSound(); name != "minecraft:block.copper_chest_weathered.open" {
		t.Errorf("the weathered pair opened to the sound %q, want minecraft:block.copper_chest_weathered.open", name)
	}
	if slots := w.ExpectContainerOpen(protocol.MenuGeneric9x6, true, 4, 64, 0); len(slots) != 54+36 {
		t.Fatalf("the weathered pair window has %d slots, want the pair's 54 and the player's 36", len(slots))
	}
}

// TestAMixedCopperChestPairOpens54Slots: an oxidized and an exposed copper
// chest placed side by side pair, and both settle on the exposed stage.
// vanilla: CopperChestBlock.getLeastOxidizedChestOfConnectedBlocks
func TestAMixedCopperChestPairOpens54Slots(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	servertest.PlaceCopperChestPair(t, w, "minecraft:oxidized_copper_chest", "minecraft:exposed_copper_chest", 4, 64, 0)
	chestPropsOf(t, srv, 4, 64, 0, "minecraft:exposed_copper_chest")
	chestPropsOf(t, srv, 5, 64, 0, "minecraft:exposed_copper_chest")

	w.UseBlock(4, 64, 0)
	if slots := w.ExpectContainerOpen(protocol.MenuGeneric9x6, true, 4, 64, 0); len(slots) != 54+36 {
		t.Fatalf("the mixed pair window has %d slots, want the pair's 54 and the player's 36", len(slots))
	}
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

// TestACopperChestUnderAConductorStaysShut: a full block above a copper chest
// refuses its window as it does a chest's, while the click still counts as
// used. vanilla: ChestBlock.isChestBlockedAt
func TestACopperChestUnderAConductorStaysShut(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	if !srv.SetBlock(4, 64, 0, copperChestState(t, "minecraft:oxidized_copper_chest")) {
		t.Fatal("the copper chest's chunk is not loaded")
	}
	if !srv.SetBlock(4, 65, 0, registry.BlockStates["minecraft:stone"]) {
		t.Fatal("the stone's chunk is not loaded")
	}

	w.UseBlock(4, 64, 0)
	if opened := servertest.ChestWindowOpens(t, w.Client); opened != 0 {
		t.Errorf("a copper chest with stone above opened %d windows, want none", opened)
	}
	// Glass is not a conductor, so the same chest under it opens.
	if !srv.SetBlock(4, 65, 0, registry.BlockStates["minecraft:glass"]) {
		t.Fatal("the glass's chunk is not loaded")
	}
	w.UseBlock(4, 64, 0)
	if opened := servertest.ChestWindowOpens(t, w.Client); opened != 1 {
		t.Errorf("a copper chest under glass opened %d windows, want one", opened)
	}
}

// TestACopperChestKeepsItsContents: every one of the eight copper chest
// blocks gets the chest block entity a placed chest keeps, and what it holds
// survives a save and reload.
func TestACopperChestKeepsItsContents(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.GameMode = "creative"
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	srv, w := servertest.OpenWindowed(t, cfg)
	stack := servertest.ItemStack(t, "minecraft:diamond", 1)
	for i, name := range copperChestNames {
		x := int32(4 + i)
		w.FacePlayer(float64(x)+0.5, 64, 3.5, 0, 0)
		w.SetCreativeSlot(36, servertest.ItemStack(t, name, 1))
		w.PlaceBlock(x, 63, 0, protocol.FaceTop)
		w.ExpectBlockAck()
		if !srv.SetInventoryAt(x, 64, 0, 0, stack) {
			t.Fatalf("%s has no chest block entity after placement", name)
		}
		if got := srv.InventoryAt(x, 64, 0); len(got) != 27 || !got[0].Equal(stack) {
			t.Fatalf("%s holds %d slots (%+v), want the chest's 27 with the diamond", name, len(got), got)
		}
	}

	srv.FlushSaves()
	srv.Shutdown()
	restarted := *cfg
	srv2, connect := servertest.Start(t, &restarted)
	c := connect()
	c.Login("Testificate")
	c.Configure()
	c.Inventory()
	c.EnterWorld("Testificate")
	for i, name := range copperChestNames {
		x := int32(4 + i)
		if got := srv2.InventoryAt(x, 64, 0); len(got) != 27 || !got[0].Equal(stack) {
			t.Errorf("%s after the reload holds %d slots (%+v), want the diamond", name, len(got), got)
		}
	}
	c.Disconnect()
}
