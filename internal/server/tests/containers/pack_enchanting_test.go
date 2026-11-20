package containers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestTheEnchantingTableOffersTheLoadedTag checks that the enchanting table
// draws from the loaded minecraft:in_enchanting_table tag: replacing the tag
// with only unbreaking makes every offered clue on a diamond pickaxe with 15
// bookshelves be unbreaking. The tag tables are process-wide, so this test is
// not parallel.
// vanilla: EnchantmentMenu.getEnchantmentList
func TestTheEnchantingTableOffersTheLoadedTag(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTags(nil) })
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "p")
	for file, body := range map[string]string{
		"pack.mcmeta": `{"pack":{"description":"p"}}`,
		"data/minecraft/tags/enchantment/in_enchanting_table.json": `{"replace":true,"values":["minecraft:unbreaking"]}`,
	} {
		path := filepath.Join(pack, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	srv, w := servertest.OpenWindowed(t, cfg)
	srv.SetXpSeed("Testificate", testSeed)

	fillShelves(t, srv, offerRing()[:15]...)
	if got := srv.CountBookshelves(4, 64, 0); got != 15 {
		t.Fatalf("the ring counts %d shelves, want 15", got)
	}

	openTable(t, srv, w, servertest.ItemStack(t, "minecraft:enchanting_table", 1))
	servertest.CollectWindowProps(w)
	servertest.ForgetWindowProps(w)

	pickaxe := servertest.ItemStack(t, "minecraft:diamond_pickaxe", 1)
	w.SetCreativeSlot(36, pickaxe)
	w.Click(enchHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{enchHotbar0: {}}, pickaxe)
	w.Click(enchItemSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{enchItemSlot: pickaxe}, protocol.ItemStack{})

	lapis := servertest.ItemStack(t, "minecraft:lapis_lazuli", 16)
	w.SetCreativeSlot(36, lapis)
	w.Click(enchHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{enchHotbar0: {}}, lapis)
	w.Click(enchLapisSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{enchLapisSlot: lapis}, protocol.ItemStack{})

	unbreaking, ok := registry.EnchantmentID("minecraft:unbreaking")
	if !ok {
		t.Fatal("minecraft:unbreaking not in registry")
	}
	props := servertest.CollectWindowProps(w)
	expectEnchantProps(w, props, map[int32]int32{
		protocol.EnchantPropertyEnchant0: unbreaking,
		protocol.EnchantPropertyEnchant1: unbreaking,
		protocol.EnchantPropertyEnchant2: unbreaking,
	})
	for _, prop := range []int32{
		protocol.EnchantPropertyLevel0,
		protocol.EnchantPropertyLevel1,
		protocol.EnchantPropertyLevel2,
	} {
		if level := props[prop]; level < 1 {
			t.Errorf("property %d = %d, want >= 1", prop, level)
		}
	}
}
