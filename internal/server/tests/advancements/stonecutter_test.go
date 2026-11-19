package advancements

import (
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestTakingAStonecutterResultFiresRecipeCrafted(t *testing.T) {
	t.Cleanup(func() { advancement.ApplyPack(nil) })
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "a")
	writePackFile(t, filepath.Join(pack, "pack.mcmeta"), `{"pack":{"description":"a"}}`)
	criterion := func(item string) string {
		return `{"trigger":"minecraft:recipe_crafted","conditions":{` +
			`"recipe_id":"minecraft:chiseled_stone_bricks_from_stone_stonecutting",` +
			`"ingredients":[{"items":"minecraft:` + item + `"}]}}`
	}
	writePackFile(t, filepath.Join(pack, "data", "pk", "advancement", "cut.json"),
		`{"criteria":{"stone":`+criterion("stone")+`,"andesite":`+criterion("andesite")+`},"requirements":[["stone"],["andesite"]]}`)
	srv, w := servertest.OpenWindowed(t, cfg)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:stonecutter", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuStonecutter, false, 4, 64, 0)
	servertest.HoldTicks(t, srv)
	stone := servertest.ItemStack(t, "minecraft:stone", 1)
	w.SetCreativeSlot(36, stone)
	w.Click(29, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{29: {}}, stone)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: stone}, protocol.ItemStack{})
	w.ClickButton(0)
	w.SettleTrackingState()
	if srv.AdvancementObtained("Testificate", "pk:cut", "stone") {
		t.Fatal("selecting a cut fired recipe_crafted")
	}
	w.Click(1, 0, protocol.ClickModePickup)
	w.ReadTicks(srv, 0, func(int32, []byte) bool { return false })
	if !srv.AdvancementObtained("Testificate", "pk:cut", "stone") {
		t.Error("taking the cut did not fire recipe_crafted with the spent stone")
	}
	if srv.AdvancementObtained("Testificate", "pk:cut", "andesite") {
		t.Error("recipe_crafted reported an unspent ingredient")
	}
}
