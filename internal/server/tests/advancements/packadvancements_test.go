package advancements

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The pack layer is process-wide, so these tests are not parallel with each
// other and put the vanilla tree back when they finish.

// packAdvancement is a displayed root advancement, with its own tab, that is
// done by holding the item: its one criterion is "got".
func packAdvancement(item string) string { return packAdvancementFor(item, item) }

// packAdvancementFor is packAdvancement with an icon other than the item.
func packAdvancementFor(icon, item string) string {
	return `{"display":{"icon":{"id":"minecraft:` + icon + `"},"title":{"translate":"pk.title"},` +
		`"description":{"translate":"pk.desc"},"background":"minecraft:gui/advancements/backgrounds/stone"},` +
		`"criteria":{"got":{"trigger":"minecraft:inventory_changed","conditions":{"items":[{"items":"minecraft:` + item + `"}]}}}}`
}

func writePackFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// packConfig is a survival world with one data pack, whose advancement
// folder is returned.
func packConfig(t *testing.T) (*config.Config, string) {
	t.Helper()
	t.Cleanup(func() { advancement.ApplyPack(nil) })
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "a")
	writePackFile(t, filepath.Join(pack, "pack.mcmeta"), `{"pack":{"description":"a"}}`)
	return cfg, filepath.Join(pack, "data")
}

func joinOp(t *testing.T, cfg *config.Config, name string) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, name))
	c, _ := join(t, connect, name)
	return srv, c
}

func give(srv *server.Server, t *testing.T, name, item string) {
	t.Helper()
	srv.GiveSlot(name, player.HotbarSlot, servertest.ItemStack(t, "minecraft:"+item, 1))
}

// TestPackAdvancementIsGrantedByItsTrigger: an advancement a data pack adds
// is awarded when its trigger fires and its tab is sent to the player.
// vanilla: ServerAdvancementManager.apply, InventoryChangeTrigger
func TestPackAdvancementIsGrantedByItsTrigger(t *testing.T) {
	cfg, data := packConfig(t)
	writePackFile(t, filepath.Join(data, "pk", "advancement", "stick.json"), packAdvancement("stick"))
	// A file that does not decode is skipped, not the whole load.
	writePackFile(t, filepath.Join(data, "pk", "advancement", "broken.json"), `{"criteria":`)
	srv, c := joinOp(t, cfg, "Packer")

	give(srv, t, "Packer", "dirt")
	c.Settle()
	if srv.AdvancementObtained("Packer", "pk:stick", "got") {
		t.Fatal("holding dirt obtained the stick advancement")
	}
	give(srv, t, "Packer", "stick")
	u := awaitUpdate(t, srv, c)
	if !srv.AdvancementObtained("Packer", "pk:stick", "got") {
		t.Error("holding a stick did not obtain pk:stick")
	}
	added, ok := u.added["pk:stick"]
	if !ok || added.title != "pk.title" || added.background == "" {
		t.Errorf("the update added %+v, want pk:stick with its title and tab background", u.added)
	}
	if u.progress["pk:stick"]["got"] == nil {
		t.Errorf("the update's progress for pk:stick = %v, want got obtained", u.progress["pk:stick"])
	}
}

// TestPackTickAndBareLocationCriteriaAreGranted: tick fires every tick and
// location every second, with no condition to pass.
// vanilla: ServerPlayer.tick, doTick
func TestPackTickAndBareLocationCriteriaAreGranted(t *testing.T) {
	cfg, data := packConfig(t)
	writePackFile(t, filepath.Join(data, "pk", "advancement", "joined.json"), `{"criteria":{"t":{"trigger":"minecraft:tick"}}}`)
	writePackFile(t, filepath.Join(data, "pk", "advancement", "here.json"), `{"criteria":{"l":{"trigger":"minecraft:location"}}}`)
	srv, _ := joinOp(t, cfg, "Packer")

	servertest.UntilTicks(t, srv, 5, "tick advancements granted", func() bool {
		return srv.AdvancementObtained("Packer", "pk:joined", "t") &&
			srv.AdvancementObtained("Packer", "minecraft:recipes/decorations/crafting_table", "unlock_right_away")
	})
	servertest.UntilTicks(t, srv, 40, "bare location advancement granted", func() bool {
		return srv.AdvancementObtained("Packer", "pk:here", "l")
	})
}

// TestPackAdvancementOverridesTheBuiltInOne: a pack's advancement of a
// vanilla ID replaces the vanilla one whole.
// vanilla: ServerAdvancementManager.apply
func TestPackAdvancementOverridesTheBuiltInOne(t *testing.T) {
	cfg, data := packConfig(t)
	writePackFile(t, filepath.Join(data, "minecraft", "advancement", "story", "root.json"), packAdvancementFor("crafting_table", "stick"))
	srv, c := joinOp(t, cfg, "Packer")

	give(srv, t, "Packer", "crafting_table")
	c.Settle()
	if srv.AdvancementObtained("Packer", "minecraft:story/root", "got") {
		t.Fatal("the vanilla crafting table criterion still fires")
	}
	give(srv, t, "Packer", "stick")
	awaitUpdate(t, srv, c)
	if !srv.AdvancementObtained("Packer", "minecraft:story/root", "got") {
		t.Error("the pack's stick criterion did not obtain story/root")
	}
}

// TestReloadKeepsProgressAndDropsRemovedAdvancements: /reload rebuilds the
// tree from the packs and every player's progress over it, then resets the
// client's tree: progress of an advancement that remains is kept, a new
// advancement works, and a removed one is gone from the tree sent.
// vanilla: PlayerAdvancements.reload
func TestReloadKeepsProgressAndDropsRemovedAdvancements(t *testing.T) {
	cfg, data := packConfig(t)
	dir := filepath.Join(data, "pk", "advancement")
	writePackFile(t, filepath.Join(dir, "stick.json"), packAdvancement("stick"))
	writePackFile(t, filepath.Join(dir, "keep.json"), packAdvancement("bone"))
	srv, c := joinOp(t, cfg, "Packer")
	give(srv, t, "Packer", "stick")
	awaitUpdate(t, srv, c)
	srv.GiveSlot("Packer", player.HotbarSlot+1, servertest.ItemStack(t, "minecraft:bone", 1))
	awaitUpdate(t, srv, c)

	writePackFile(t, filepath.Join(dir, "new.json"), packAdvancement("clay_ball"))
	if err := os.Remove(filepath.Join(dir, "stick.json")); err != nil {
		t.Fatal(err)
	}
	out := run(t, srv, c, "reload")
	var reset *sentUpdate
	for i := range out.updates {
		if out.updates[i].reset {
			reset = &out.updates[i]
		}
	}
	if reset == nil {
		t.Fatalf("/reload sent no resetting update: %+v", out.updates)
	}
	if _, ok := reset.added["pk:stick"]; ok || reset.progress["pk:stick"] != nil {
		t.Errorf("the reset update still carries the removed pk:stick: %+v", reset)
	}
	if reset.added["pk:keep"].title != "pk.title" || reset.progress["pk:keep"]["got"] == nil {
		t.Errorf("the reset update = %+v, want pk:keep shown with got obtained", reset)
	}
	if !srv.AdvancementObtained("Packer", "pk:keep", "got") || srv.AdvancementObtained("Packer", "pk:stick", "got") {
		t.Error("progress after the reload: pk:keep lost, or pk:stick kept")
	}

	srv.GiveSlot("Packer", player.HotbarSlot+2, servertest.ItemStack(t, "minecraft:clay_ball", 1))
	awaitUpdate(t, srv, c)
	if !srv.AdvancementObtained("Packer", "pk:new", "got") {
		t.Error("the advancement added by the reload is not awarded")
	}
}

// TestPackAdvancementThatVanillaRefusesIsDroppedAlone: an advancement naming
// an unknown trigger or an invalid parent id is left out whole, while the
// others in the pack load; one with keys vanilla ignores, or with conditions
// the server does not model, loads and never fires.
// vanilla: Criterion.CODEC, ServerAdvancementManager.apply
func TestPackAdvancementThatVanillaRefusesIsDroppedAlone(t *testing.T) {
	cfg, data := packConfig(t)
	dir := filepath.Join(data, "pk", "advancement")
	good := packAdvancement("stick")
	writePackFile(t, filepath.Join(dir, "good.json"), good)
	writePackFile(t, filepath.Join(dir, "trigger.json"), strings.Replace(good, "minecraft:inventory_changed", "mod:x", 1))
	writePackFile(t, filepath.Join(dir, "parent.json"), strings.Replace(good, `{"display"`, `{"parent":"Bad Id","display"`, 1))
	writePackFile(t, filepath.Join(dir, "extra.json"), strings.Replace(good, `{"display"`, `{"extra":1,"display"`, 1))
	writePackFile(t, filepath.Join(dir, "slots.json"),
		strings.Replace(good, `"conditions":{`, `"conditions":{"player":[],"slots":{"empty":1},`, 1))
	srv, c := joinOp(t, cfg, "Packer")

	give(srv, t, "Packer", "stick")
	awaitUpdate(t, srv, c)
	tree := advancement.Current()
	for _, id := range []string{"pk:good", "pk:extra", "pk:slots"} {
		if tree.Get(id) == nil {
			t.Errorf("%s is not in the tree", id)
		}
	}
	for _, id := range []string{"pk:trigger", "pk:parent"} {
		if tree.Get(id) != nil {
			t.Errorf("%s was loaded, vanilla refuses it", id)
		}
	}
	if !srv.AdvancementObtained("Packer", "pk:good", "got") || !srv.AdvancementObtained("Packer", "pk:extra", "got") {
		t.Error("an advancement that loads was not obtained")
	}
	if srv.AdvancementObtained("Packer", "pk:slots", "got") {
		t.Error("an inventory_changed with slots the server does not model fired")
	}
}

// TestUsingAnInteractionEntityGrantsItsAdvancement: a use of an interaction
// entity fires player_interacted_with_entity.
// vanilla: Interaction.interact, ServerGamePacketListenerImpl.handleInteract
func TestUsingAnInteractionEntityGrantsItsAdvancement(t *testing.T) {
	cfg, data := packConfig(t)
	writePackFile(t, filepath.Join(data, "pk", "advancement", "use.json"),
		`{"criteria":{"used":{"trigger":"minecraft:player_interacted_with_entity","conditions":{"entity":[`+
			`{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:entity_type":"minecraft:interaction"}}]}}}}`)
	srv, c := joinOp(t, cfg, "Packer")

	c.ChatCommand(`summon minecraft:interaction ~ ~ ~ {width:4f,height:4f}`)
	typ, _ := registry.EntityTypeID("minecraft:interaction")
	spawn := c.AwaitSpawn(typ)
	if srv.AdvancementObtained("Packer", "pk:use", "used") {
		t.Fatal("the advancement was obtained before the use")
	}
	c.UseOn(spawn.EntityID)
	servertest.UntilTicks(t, srv, 40, "pk:use obtained", func() bool {
		return srv.AdvancementObtained("Packer", "pk:use", "used")
	})
}
