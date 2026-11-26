package containers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// shulkerTagPackConfig is a config whose pack replaces #minecraft:shulker_boxes
// with stone. The tables are process-wide, so callers are not parallel.
func shulkerTagPackConfig(t *testing.T) *config.Config {
	t.Helper()
	t.Cleanup(func() { registry.ApplyPackTags(nil); registry.ApplyPackRegistries(nil) })
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "p")
	for file, body := range map[string]string{
		"pack.mcmeta": `{"pack":{"description":"p"}}`,
		"data/minecraft/tags/item/shulker_boxes.json": `{"replace":true,"values":["minecraft:stone"]}`,
	} {
		path := filepath.Join(pack, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return cfg
}

// A pack that replaces #minecraft:shulker_boxes changes neither which items a
// shulker box window refuses (shulker boxes) nor which a bundle refuses.
// vanilla: BlockItem.canFitInsideContainerItems, ShulkerBoxSlot.mayPlace
func TestAPackShulkerBoxesTagKeepsContainerRestrictions(t *testing.T) {
	srv, w := servertest.OpenWindowed(t, shulkerTagPackConfig(t))
	if !registry.Item(servertest.ItemID(t, "minecraft:stone")).InTag(registry.ItemTagIDShulkerBoxes) {
		t.Fatal("the pack did not replace the tag")
	}
	box := servertest.ItemStack(t, "minecraft:shulker_box", 1)
	stone := servertest.ItemStack(t, "minecraft:stone", 1)

	srv.SetBlock(4, 64, 0, registry.MustState("minecraft:shulker_box", nil))
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuShulkerBox, true, 4, 64, 0)
	for _, tc := range []struct {
		name    string
		carried protocol.ItemStack
		in      bool
	}{{"shulker box", box, false}, {"stone", stone, true}} {
		// pick takes the stack from the first hotbar slot to the cursor.
		pick := func() {
			srv.GiveSlot("Testificate", player.HotbarSlot, tc.carried)
			w.Click(54, 0, protocol.ClickModePickup)
			w.ExpectClickResult(map[int16]protocol.ItemStack{54: {}}, tc.carried)
		}

		pick()
		w.Click(0, 0, protocol.ClickModePickup)
		if tc.in {
			w.ExpectClickResult(map[int16]protocol.ItemStack{0: tc.carried}, protocol.ItemStack{})
		} else {
			w.ExpectClickResult(map[int16]protocol.ItemStack{}, tc.carried)
		}

		// The same stack dropped on a bundle in the second hotbar slot.
		if !tc.in {
			// the shulker box is still on the cursor
		} else {
			pick()
		}
		srv.GiveSlot("Testificate", player.HotbarSlot+1, rawBundle(t))
		w.Click(55, 0, protocol.ClickModePickup)
		if tc.in {
			w.ExpectClickResult(map[int16]protocol.ItemStack{55: rawBundle(t, tc.carried)}, protocol.ItemStack{})
		} else {
			w.ExpectClickResult(map[int16]protocol.ItemStack{}, tc.carried)
			// Put the refused box back so the next case starts with an empty cursor.
			w.Click(54, 0, protocol.ClickModePickup)
			w.ExpectClickResult(map[int16]protocol.ItemStack{54: tc.carried}, protocol.ItemStack{})
		}
	}
}
