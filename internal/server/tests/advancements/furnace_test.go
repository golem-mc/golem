package advancements

import (
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestTakingASmeltedResultFiresRecipeCrafted smelts one raw iron and breaks
// the furnace with the ingot inside, which fires nothing, then smelts another
// and takes the ingot: recipe_crafted fires for the smelting recipe, its
// ingredients the furnace's slots after the take (the second coal, no raw
// iron). vanilla: AbstractFurnaceBlockEntity.awardUsedRecipesAndPopExperience
func TestTakingASmeltedResultFiresRecipeCrafted(t *testing.T) {
	t.Cleanup(func() { advancement.ApplyPack(nil) })
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "a")
	writePackFile(t, filepath.Join(pack, "pack.mcmeta"), `{"pack":{"description":"a"}}`)
	criterion := func(ingredient string) string {
		c := `{"trigger":"minecraft:recipe_crafted","conditions":{"recipe_id":"minecraft:iron_ingot_from_smelting_raw_iron"`
		if ingredient != "" {
			c += `,"ingredients":[{"items":"minecraft:` + ingredient + `"}]`
		}
		return c + `}}`
	}
	writePackFile(t, filepath.Join(pack, "data", "pk", "advancement", "smelt.json"),
		`{"criteria":{"smelted":`+criterion("")+`,"coal":`+criterion("coal")+`,"raw":`+criterion("raw_iron")+`},`+
			`"requirements":[["smelted"],["coal"],["raw"]]}`)
	srv, w := servertest.OpenWindowed(t, cfg)
	servertest.StopTheLoop(t, srv)
	obtained := func() []string {
		var got []string
		for _, name := range []string{"smelted", "coal", "raw"} {
			if srv.AdvancementObtained("Testificate", "pk:smelt", name) {
				got = append(got, name)
			}
		}
		return got
	}
	smeltOne := func() {
		servertest.LoadFurnace(t, srv, w, "minecraft:raw_iron", 1, 2)
		// One smelt of 200 ticks, and two over to leave the furnace idle.
		for range 200 + 2 {
			srv.TickBlockEntities()
		}
	}

	smeltOne()
	w.BreakBlock(4, 64, 0)
	w.Settle()
	if got := obtained(); len(got) != 0 {
		t.Errorf("breaking the furnace obtained %v, want nothing", got)
	}

	smeltOne()
	w.UseBlock(4, 64, 0)
	slots := w.ExpectContainerOpen(protocol.MenuFurnace, true, 4, 64, 0)
	if got := slots[2]; !got.Equal(servertest.ItemStack(t, "minecraft:iron_ingot", 1)) {
		t.Fatalf("the result slot = %+v, want the ingot", got)
	}
	w.Click(2, 0, protocol.ClickModePickup)
	// The loop is stopped; the click itself runs the payout before the barrier.
	w.ReadTicks(srv, 0, func(int32, []byte) bool { return false })
	if got := obtained(); len(got) != 2 || got[0] != "smelted" || got[1] != "coal" {
		t.Errorf("taking the ingot obtained %v, want [smelted coal]", got)
	}
}
