package advancements

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
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

// TestPackRecipeAdvancementUnlocksItsRecipe: a pack's recipes/ advancement is
// loaded and fired like vanilla's, so holding its ingredient adds the pack
// recipe to the recipe book, and a pack overriding a vanilla recipe
// advancement with an impossible criterion stops it unlocking.
// vanilla: ServerAdvancementManager.apply, AdvancementRewards.grant
func TestPackRecipeAdvancementUnlocksItsRecipe(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	cfg, data := packConfig(t)
	writePackFile(t, filepath.Join(data, "t", "recipe", "wand.json"),
		`{"type":"minecraft:crafting_shaped","pattern":["a","b"],"key":{"a":"minecraft:stick","b":"minecraft:diamond"},`+
			`"result":{"id":"minecraft:diamond","count":1}}`)
	writePackFile(t, filepath.Join(data, "t", "advancement", "recipes", "misc", "wand.json"),
		`{"criteria":{"x":{"trigger":"minecraft:inventory_changed","conditions":{"items":[{"items":"minecraft:diamond"}]}}},`+
			`"rewards":{"recipes":["t:wand"]}}`)
	writePackFile(t, filepath.Join(data, "minecraft", "advancement", "recipes", "misc", "stick.json"),
		`{"criteria":{"x":{"trigger":"minecraft:impossible"}},"rewards":{"recipes":["minecraft:stick"]}}`)
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Packer"))
	c := connect()
	c.WatchBook = true
	c.Login("Packer")
	c.Configure()
	c.ReadJoin(func(int32, []byte) {})
	c.MarkLoaded()
	c.Settle()

	give(srv, t, "Packer", "oak_planks")
	servertest.RunTicks(t, srv, 3)
	c.Settle()
	if srv.RecipeKnown("Packer", "minecraft:stick") {
		t.Error("oak planks unlocked the stick recipe despite the impossible override")
	}
	give(srv, t, "Packer", "diamond")
	servertest.RunTicks(t, srv, 3)
	c.Settle()
	if !srv.RecipeKnown("Packer", "t:wand") {
		t.Error("holding a diamond did not unlock the pack recipe")
	}
}

// TestPackAdvancementStyledTitleAndIconComponents: a data-pack advancement
// with a styled text component title/description and icon components is loaded,
// sent with its full text components and icon patch in Update Advancements, and
// announced in chat with the styled title and tooltip.
// vanilla: DisplayInfo.CODEC, DisplayInfo.serializeToNetwork, Advancement.decorateName
func TestPackAdvancementStyledTitleAndIconComponents(t *testing.T) {
	cfg, data := packConfig(t)
	writePackFile(t, filepath.Join(data, "pk", "advancement", "gold.json"), `{
		"display": {
			"icon": {
				"id": "minecraft:stone",
				"components": {"minecraft:enchantment_glint_override": true}
			},
			"title": {"text": "Gold", "color": "gold", "bold": true},
			"description": {"translate": "pk.desc", "with": ["stone"], "fallback": "Find stone"},
			"background": "minecraft:gui/advancements/backgrounds/stone"
		},
		"criteria": {
			"got": {
				"trigger": "minecraft:inventory_changed",
				"conditions": {"items": [{"items": "minecraft:stone"}]}
			}
		}
	}`)
	srv, c := joinOp(t, cfg, "Packer")

	give(srv, t, "Packer", "stone")
	var update *sentUpdate
	var announceTag nbt.Tag
	c.ReadTicks(srv, 40, func(id int32, payload []byte) bool {
		switch id {
		case protocol.IDPlayUpdateAdvancements:
			u := parseUpdate(t, payload)
			if _, ok := u.added["pk:gold"]; ok {
				update = &u
			}
		case protocol.IDPlaySystemChat:
			tag, _, err := nbt.DecodeNetwork(payload)
			if err != nil {
				t.Fatalf("system chat: %v", err)
			}
			if fields, ok := nbt.CompoundView(tag); ok && fields["translate"] == nbt.String("chat.type.advancement.task") {
				announceTag = tag
			}
		}
		return update != nil && announceTag != nil
	})
	if update == nil {
		t.Fatal("Update Advancements for pk:gold did not arrive")
	}
	added := update.added["pk:gold"]
	titleFields, ok := nbt.CompoundView(added.titleTag)
	if !ok || titleFields["text"] != nbt.String("Gold") || titleFields["color"] != nbt.String("gold") || !protocol.FlagOn(titleFields["bold"]) {
		t.Errorf("Update Advancements title = %v, want text Gold, color gold, bold true", added.titleTag)
	}
	descFields, ok := nbt.CompoundView(added.descTag)
	if !ok || descFields["translate"] != nbt.String("pk.desc") || descFields["fallback"] != nbt.String("Find stone") {
		t.Errorf("Update Advancements description = %v, want translate pk.desc with fallback", added.descTag)
	}
	if added.icon != servertest.ItemIDOrDie(t, "minecraft:stone") || added.iconStack.GlintOverride != protocol.GlintShown {
		t.Errorf("Update Advancements icon = %d (glint=%v), want stone with GlintShown", added.icon, added.iconStack.GlintOverride)
	}

	if announceTag == nil {
		t.Fatal("completion chat announcement did not arrive")
	}
	msgFields, _ := nbt.CompoundView(announceTag)
	with, _ := msgFields["with"].(nbt.List)
	if len(with.Items) != 2 {
		t.Fatalf("announcement with = %v, want [player, advancement]", with)
	}
	brackets, ok := nbt.CompoundView(nbt.ListElement(with.Items[1]))
	if !ok || brackets["translate"] != nbt.String("chat.square_brackets") || brackets["color"] != nbt.String("green") {
		t.Fatalf("announcement advancement = %v, want green chat.square_brackets", with.Items[1])
	}
	bracketWith, _ := brackets["with"].(nbt.List)
	if len(bracketWith.Items) != 1 {
		t.Fatalf("chat.square_brackets with = %v, want 1 item", bracketWith)
	}
	chatTitle, ok := nbt.CompoundView(nbt.ListElement(bracketWith.Items[0]))
	if !ok || chatTitle["text"] != nbt.String("Gold") || chatTitle["color"] != nbt.String("gold") || !protocol.FlagOn(chatTitle["bold"]) {
		t.Errorf("chat title = %v, want text Gold, color gold, bold true", bracketWith.Items[0])
	}
	hover, ok := nbt.CompoundView(chatTitle["hover_event"])
	if !ok || hover["action"] != nbt.String("show_text") {
		t.Fatalf("chat title hover_event = %v, want show_text", chatTitle["hover_event"])
	}
	tip, ok := nbt.CompoundView(hover["value"])
	if !ok || tip["text"] != nbt.String("Gold") || tip["color"] != nbt.String("gold") || !protocol.FlagOn(tip["bold"]) {
		t.Errorf("tooltip = %v, want Gold in gold bold", hover["value"])
	}
	extra, _ := tip["extra"].(nbt.List)
	if len(extra.Items) != 2 || nbt.ListElement(extra.Items[0]) != nbt.String("\n") {
		t.Errorf("tooltip extra = %v, want newline and description", extra)
	}
}

// TestPackTickAdvancementTestsItsPlayerConditions: a tick criterion with a
// player predicate is obtained only while the predicate holds of the player.
// vanilla: SimpleCriterionTrigger.SimpleInstance.player
func TestPackTickAdvancementTestsItsPlayerConditions(t *testing.T) {
	cfg, data := packConfig(t)
	dir := filepath.Join(data, "pk", "advancement")
	tick := func(player string) string {
		return `{"criteria":{"c":{"trigger":"minecraft:tick","conditions":{"player":[` + player + `]}}}}`
	}
	writePackFile(t, filepath.Join(dir, "sneak.json"), tick(
		`{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:flags":{"is_sneaking":true}}}`))
	writePackFile(t, filepath.Join(dir, "score.json"), tick(
		`{"condition":"minecraft:entity_scores","entity":"this","scores":{"x":1}}`))
	srv, c := joinOp(t, cfg, "Packer")
	c.ChatCommand("scoreboard objectives add x dummy")
	c.Settle()

	servertest.RunTicks(t, srv, 5)
	if srv.AdvancementObtained("Packer", "pk:sneak", "c") || srv.AdvancementObtained("Packer", "pk:score", "c") {
		t.Fatal("a tick advancement was obtained while its player condition fails")
	}
	c.SendSneak(true)
	servertest.UntilTicks(t, srv, 20, "the sneak advancement", func() bool { return srv.AdvancementObtained("Packer", "pk:sneak", "c") })
	if srv.AdvancementObtained("Packer", "pk:score", "c") {
		t.Fatal("the score advancement was obtained with the score unset")
	}
	c.ChatCommand("scoreboard players set Packer x 1")
	servertest.UntilTicks(t, srv, 20, "the score advancement", func() bool { return srv.AdvancementObtained("Packer", "pk:score", "c") })
}

// stackWithData is an item carrying custom_data {a:1b}.
func stackWithData(t *testing.T, item string) protocol.ItemStack {
	t.Helper()
	stack := servertest.ItemStack(t, "minecraft:"+item, 1)
	stack.SetCustomData(nbt.Compound{"a": nbt.Byte(1)})
	return stack
}

// TestPackInventoryChangedTestsCustomData: a criterion whose item predicate
// asks for custom_data is obtained by the marked stick and not a plain one.
// vanilla: InventoryChangeTrigger.TriggerInstance.matches, ItemPredicate.test
func TestPackInventoryChangedTestsCustomData(t *testing.T) {
	cfg, data := packConfig(t)
	writePackFile(t, filepath.Join(data, "pk", "advancement", "marked.json"),
		`{"criteria":{"got":{"trigger":"minecraft:inventory_changed","conditions":{"items":[`+
			`{"items":"minecraft:stick","predicates":{"minecraft:custom_data":"{a:1b}"}}]}}}}`)
	srv, c := joinOp(t, cfg, "Packer")

	give(srv, t, "Packer", "stick")
	c.Settle()
	if srv.AdvancementObtained("Packer", "pk:marked", "got") {
		t.Fatal("a plain stick obtained the custom_data advancement")
	}
	srv.GiveSlot("Packer", player.HotbarSlot, stackWithData(t, "stick"))
	c.Settle()
	servertest.UntilTicks(t, srv, 40, "the marked stick obtains it", func() bool {
		return srv.AdvancementObtained("Packer", "pk:marked", "got")
	})
}

// TestPackConsumeItemTestsCustomData: eating a bread with custom_data obtains
// the criterion, eating a plain one does not.
// vanilla: ConsumeItemTrigger.TriggerInstance.matches
func TestPackConsumeItemTestsCustomData(t *testing.T) {
	cfg, data := packConfig(t)
	writePackFile(t, filepath.Join(data, "pk", "advancement", "meal.json"),
		`{"criteria":{"ate":{"trigger":"minecraft:consume_item","conditions":{"item":`+
			`{"items":"minecraft:bread","predicates":{"minecraft:custom_data":"{a:1b}"}}}}}}`)
	srv, c := joinOp(t, cfg, "Packer")
	servertest.HoldTicks(t, srv)

	eat := func(stack protocol.ItemStack) {
		srv.SetFood("Packer", 10)
		srv.GiveSlot("Packer", player.HotbarSlot, stack)
		c.UseItemRightClicks(protocol.HandMain)
		c.Settle()
		if !srv.Eating("Packer") {
			t.Fatal("the meal begins")
		}
		servertest.UntilTicks(t, srv, 100, "the meal is eaten", func() bool { return !srv.Eating("Packer") })
	}
	eat(servertest.ItemStack(t, "minecraft:bread", 1))
	if srv.AdvancementObtained("Packer", "pk:meal", "ate") {
		t.Fatal("a plain bread obtained the custom_data advancement")
	}
	eat(stackWithData(t, "bread"))
	if !srv.AdvancementObtained("Packer", "pk:meal", "ate") {
		t.Error("the marked bread did not obtain the advancement")
	}
}

// TestPackItemDurabilityTestsRemainingUses: durability {max:1} is obtained
// when a hit leaves the sword one use from breaking, not before.
// vanilla: ItemDurabilityTrigger.TriggerInstance.matches
func TestPackItemDurabilityTestsRemainingUses(t *testing.T) {
	cfg, data := packConfig(t)
	writePackFile(t, filepath.Join(data, "pk", "advancement", "worn.json"),
		`{"criteria":{"worn":{"trigger":"minecraft:item_durability_changed","conditions":{"durability":{"max":1}}}}}`)
	srv, c := joinOp(t, cfg, "Packer")
	c.ChatCommand("summon minecraft:zombie 1.5 64 2.5 {NoAI:1b}")
	mob := c.ExpectMobSpawn(protocol.EntityTypeZombie)
	servertest.HoldTicks(t, srv)

	hit := func(damage int32) {
		sword := servertest.ItemStack(t, "minecraft:iron_sword", 1)
		sword.Damage = damage
		srv.GiveSlot("Packer", player.HotbarSlot, sword)
		servertest.AwaitFullSwing(t, srv, "Packer")
		c.Attack(mob.EntityID)
		c.Settle()
	}
	hit(100)
	if srv.AdvancementObtained("Packer", "pk:worn", "worn") {
		t.Fatal("wear leaving 149 uses obtained durability max 1")
	}
	hit(248)
	servertest.UntilTicks(t, srv, 40, "the hit leaving one use obtains it", func() bool {
		return srv.AdvancementObtained("Packer", "pk:worn", "worn")
	})
}

// packCriterion writes an advancement of one criterion of the trigger with
// conditions, in the pack at dir.
func packCriterion(t *testing.T, dir, name, trigger, conditions string) {
	t.Helper()
	writePackFile(t, filepath.Join(dir, name+".json"),
		`{"criteria":{"c":{"trigger":"minecraft:`+trigger+`","conditions":`+conditions+`}}}`)
}

// TestPackInteractionTestsItsEntityConditions: a player_interacted_with_entity
// criterion with an entity nbt predicate fires for the tagged villager only.
// vanilla: PlayerInteractTrigger.TriggerInstance.matches, Villager.mobInteract
func TestPackInteractionTestsItsEntityConditions(t *testing.T) {
	cfg, data := packConfig(t)
	packCriterion(t, filepath.Join(data, "pk", "advancement"), "shop", "player_interacted_with_entity",
		`{"entity":[{"condition":"minecraft:entity_properties","entity":"this","predicate":{`+
			`"minecraft:entity_type":"minecraft:villager","minecraft:nbt":"{Tags:[\"shop\"]}"}}]}`)
	srv, c := joinOp(t, cfg, "Packer")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	plain := srv.SpawnMob("minecraft:villager", 1.5, 64, 0.5)
	tagged := srv.SpawnMob("minecraft:villager", -1.5, 64, 0.5)
	c.Settle()

	c.UseOn(plain)
	c.Settle()
	if srv.AdvancementObtained("Packer", "pk:shop", "c") {
		t.Fatal("an untagged villager fired the criterion")
	}
	c.ChatCommand("tag @e[type=minecraft:villager,x=-1.5,y=64,z=0.5,distance=..0.5] add shop")
	c.Settle()
	c.UseOn(tagged)
	servertest.UntilTicks(t, srv, 20, "the tagged villager's criterion", func() bool { return srv.AdvancementObtained("Packer", "pk:shop", "c") })
}

// TestPackInteractionTestsItsItemPredicate: a player_interacted_with_entity
// criterion whose item asks for custom_data fires when the villager is used
// with the marked stick and not a plain one.
// vanilla: PlayerInteractTrigger.TriggerInstance.matches, ItemPredicate.test
func TestPackInteractionTestsItsItemPredicate(t *testing.T) {
	cfg, data := packConfig(t)
	packCriterion(t, filepath.Join(data, "pk", "advancement"), "marked", "player_interacted_with_entity",
		`{"item":{"items":"minecraft:stick","predicates":{"minecraft:custom_data":"{a:1b}"}}}`)
	srv, c := joinOp(t, cfg, "Packer")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	villager := srv.SpawnMob("minecraft:villager", 1.5, 64, 0.5)
	c.Settle()

	srv.GiveSlot("Packer", player.HotbarSlot, servertest.ItemStack(t, "minecraft:stick", 1))
	c.Settle()
	c.UseOn(villager)
	c.Settle()
	if srv.AdvancementObtained("Packer", "pk:marked", "c") {
		t.Fatal("a plain stick fired the custom_data criterion")
	}
	srv.GiveSlot("Packer", player.HotbarSlot, stackWithData(t, "stick"))
	c.Settle()
	c.UseOn(villager)
	servertest.UntilTicks(t, srv, 20, "the marked stick's criterion", func() bool { return srv.AdvancementObtained("Packer", "pk:marked", "c") })
}

// TestPackBreedingTestsItsChildCondition: a bred_animals criterion whose
// child is a baby fires for two cows breeding.
// vanilla: BredAnimalsTrigger.TriggerInstance.matches
func TestPackBreedingTestsItsChildCondition(t *testing.T) {
	cfg, data := packConfig(t)
	dir := filepath.Join(data, "pk", "advancement")
	packCriterion(t, dir, "baby", "bred_animals",
		`{"child":[{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:flags":{"is_baby":true}}}]}`)
	packCriterion(t, dir, "adult", "bred_animals",
		`{"child":[{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:flags":{"is_baby":false}}}]}`)
	srv, c := joinOp(t, cfg, "Packer")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	srv.GiveSlot("Packer", player.HotbarSlot, servertest.ItemStack(t, "minecraft:wheat", 4))
	srv.SpawnMob("minecraft:cow", 1.5, 64, 2.5)
	srv.SpawnMob("minecraft:cow", 2.5, 64, 2.5)
	cow1 := c.ExpectMobSpawn(protocol.EntityTypeCow)
	cow2 := c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(cow1.EntityID)
	c.AwaitEntityEvent(cow1.EntityID, 18)
	c.UseOn(cow2.EntityID)
	c.AwaitEntityEvent(cow2.EntityID, 18)
	servertest.UntilTicks(t, srv, 700, "the cows breed", func() bool { return srv.AdvancementObtained("Packer", "pk:baby", "c") })
	if srv.AdvancementObtained("Packer", "pk:adult", "c") {
		t.Error("the child is not a baby")
	}
}

// TestPackEffectsChangedTestsAmplifier: an effects_changed criterion with an
// amplifier range is obtained for Speed II and not Speed I.
// vanilla: MobEffectsPredicate.MobEffectInstancePredicate.matches
func TestPackEffectsChangedTestsAmplifier(t *testing.T) {
	cfg, data := packConfig(t)
	packCriterion(t, filepath.Join(data, "pk", "advancement"), "fast", "effects_changed",
		`{"effects":{"minecraft:speed":{"amplifier":{"min":1}}}}`)
	srv, c := joinOp(t, cfg, "Packer")
	c.ChatCommand("effect give Packer minecraft:speed 100 0")
	c.Settle()
	servertest.RunTicks(t, srv, 5)
	if srv.AdvancementObtained("Packer", "pk:fast", "c") {
		t.Fatal("Speed I obtained a criterion that wants amplifier 1 or more")
	}
	c.ChatCommand("effect give Packer minecraft:speed 100 1")
	servertest.UntilTicks(t, srv, 20, "Speed II", func() bool { return srv.AdvancementObtained("Packer", "pk:fast", "c") })
}

// meleeHit stands the player by a fresh mob, tagged "boss" or not, and lands
// one full-strength blow with the held item.
func meleeHit(t *testing.T, srv *server.Server, c *servertest.Client, name, kind string, boss bool) {
	t.Helper()
	var id int32
	if boss {
		id = srv.SpawnMobTagged(kind, 0.5, 64, 2.0, nbt.Compound{"Tags": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("boss")}}})
	} else {
		id = srv.SpawnMob(kind, 0.5, 64, 2.0)
	}
	c.Settle()
	servertest.AwaitFullSwing(t, srv, name)
	c.AttackLanded(srv, name, id)
}

// TestPackHurtEntityFiresOnAMeleeHitAndTestsDealtDamage: a player_hurt_entity
// criterion with no conditions fires for a melee hit, and one with
// damage.dealt.min 5 only for a hit of that much before armor.
// vanilla: LivingEntity.hurtServer, DamagePredicate.matches
func TestPackHurtEntityFiresOnAMeleeHitAndTestsDealtDamage(t *testing.T) {
	cfg, data := packConfig(t)
	dir := filepath.Join(data, "pk", "advancement")
	packCriterion(t, dir, "any", "player_hurt_entity", `{}`)
	packCriterion(t, dir, "five", "player_hurt_entity", `{"damage":{"dealt":{"min":5}}}`)
	srv, c := joinOp(t, cfg, "Packer")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	meleeHit(t, srv, c, "Packer", "minecraft:zombie", false)
	servertest.UntilTicks(t, srv, 20, "the unconditioned criterion", func() bool { return srv.AdvancementObtained("Packer", "pk:any", "c") })
	if srv.AdvancementObtained("Packer", "pk:five", "c") {
		t.Fatal("a bare hand's hit of 1 fired the dealt.min 5 criterion")
	}
	give(srv, t, "Packer", "diamond_sword")
	c.Settle()
	meleeHit(t, srv, c, "Packer", "minecraft:zombie", false)
	servertest.UntilTicks(t, srv, 20, "the dealt.min 5 criterion", func() bool { return srv.AdvancementObtained("Packer", "pk:five", "c") })
}

// TestPackKilledEntityTestsEntityTags: a player_killed_entity criterion whose
// victim must carry a /tag tag fires for the tagged chicken only.
// vanilla: KilledTrigger.TriggerInstance.matches, EntityTypePredicate, EntityPredicate
func TestPackKilledEntityTestsEntityTags(t *testing.T) {
	cfg, data := packConfig(t)
	packCriterion(t, filepath.Join(data, "pk", "advancement"), "boss", "player_killed_entity",
		`{"entity":[{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:entity_tags":{"all_of":["boss"]}}}]}`)
	srv, c := joinOp(t, cfg, "Packer")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	give(srv, t, "Packer", "netherite_sword")
	c.Settle()
	meleeHit(t, srv, c, "Packer", "minecraft:chicken", false)
	servertest.RunTicks(t, srv, 40)
	if srv.AdvancementObtained("Packer", "pk:boss", "c") {
		t.Fatal("an untagged chicken's kill fired the entity_tags criterion")
	}
	meleeHit(t, srv, c, "Packer", "minecraft:chicken", true)
	servertest.UntilTicks(t, srv, 40, "the tagged chicken's kill", func() bool { return srv.AdvancementObtained("Packer", "pk:boss", "c") })
}

// TestPackEntityKilledPlayerTestsEntityType: an entity_killed_player
// criterion with entity_type zombie fires when a zombie kills the player, and
// one for a skeleton does not.
// vanilla: Entity.awardKillScore, KilledTrigger.TriggerInstance.matches
func TestPackEntityKilledPlayerTestsEntityType(t *testing.T) {
	cfg, data := packConfig(t)
	dir := filepath.Join(data, "pk", "advancement")
	for _, kind := range []string{"zombie", "skeleton"} {
		packCriterion(t, dir, kind, "entity_killed_player",
			`{"entity":[{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:entity_type":"minecraft:`+kind+`"}}]}`)
	}
	srv, c := joinOp(t, cfg, "Packer")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	srv.SetHealth("Packer", 1)
	id := srv.SpawnMob("minecraft:zombie", 0.5, 64, 1.0)
	srv.SetMobTargetPlayer(id, "Packer")
	servertest.UntilTicks(t, srv, 400, "the zombie's kill", func() bool { return srv.AdvancementObtained("Packer", "pk:zombie", "c") })
	if srv.AdvancementObtained("Packer", "pk:skeleton", "c") {
		t.Error("a zombie's kill fired the skeleton criterion")
	}
}

// TestPackEntityHurtPlayerTestsSourceEntityAndType: an entity_hurt_player
// criterion fires for a zombie's hit when its damage.source_entity is a
// zombie, not when it is a skeleton or when the damage must be a projectile.
// A burning arrow a skeleton shot afterwards fires the direct arrow and source
// skeleton criteria, with the arrow's fire flag and flight speed, and neither reversed pair.
// vanilla: EntityHurtPlayerTrigger.TriggerInstance.matches, DamagePredicate.matches
func TestPackEntityHurtPlayerTestsSourceEntityAndType(t *testing.T) {
	cfg, data := packConfig(t)
	dir := filepath.Join(data, "pk", "advancement")
	packCriterion(t, dir, "zombie", "entity_hurt_player", `{"damage":{"source_entity":{"minecraft:entity_type":"minecraft:zombie"}}}`)
	packCriterion(t, dir, "skeleton", "entity_hurt_player", `{"damage":{"source_entity":{"minecraft:entity_type":"minecraft:skeleton"}}}`)
	packCriterion(t, dir, "shot", "entity_hurt_player", `{"damage":{"type":{"tags":[{"id":"minecraft:is_projectile","expected":true}]}}}`)
	for _, key := range []string{"direct_entity", "source_entity"} {
		for _, kind := range []string{"zombie", "skeleton"} {
			packCriterion(t, dir, key+"_"+kind, "entity_hurt_player",
				`{"damage":{"type":{"`+key+`":{"minecraft:entity_type":"minecraft:`+kind+`"}}}}`)
		}
	}
	for name, damage := range map[string]string{
		"shot_direct_arrow":      `"direct_entity":{"minecraft:entity_type":"minecraft:arrow"}`,
		"shot_source_skeleton":   `"source_entity":{"minecraft:entity_type":"minecraft:skeleton"}`,
		"shot_direct_skeleton":   `"direct_entity":{"minecraft:entity_type":"minecraft:skeleton"}`,
		"shot_source_arrow":      `"source_entity":{"minecraft:entity_type":"minecraft:arrow"}`,
		"shot_direct_burning":    `"direct_entity":{"minecraft:entity_type":"minecraft:arrow","minecraft:flags":{"is_on_fire":true}}`,
		"shot_direct_unlit":      `"direct_entity":{"minecraft:entity_type":"minecraft:arrow","minecraft:flags":{"is_on_fire":false}}`,
		"shot_direct_moving":     `"direct_entity":{"minecraft:entity_type":"minecraft:arrow","minecraft:movement":{"speed":{"min":1}}}`,
		"shot_direct_stationary": `"direct_entity":{"minecraft:entity_type":"minecraft:arrow","minecraft:movement":{"speed":{"max":0}}}`,
	} {
		packCriterion(t, dir, name, "entity_hurt_player", `{"damage":{"type":{`+damage+`}}}`)
	}
	srv, c := joinOp(t, cfg, "Packer")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob("minecraft:zombie", 0.5, 64, 1.0)
	srv.SetMobTargetPlayer(id, "Packer")
	servertest.UntilTicks(t, srv, 400, "the zombie's hit", func() bool { return srv.AdvancementObtained("Packer", "pk:zombie", "c") })
	for _, key := range []string{"direct_entity", "source_entity"} {
		if !srv.AdvancementObtained("Packer", "pk:"+key+"_zombie", "c") {
			t.Errorf("a zombie's hit did not fire the damage.type.%s zombie criterion", key)
		}
		if srv.AdvancementObtained("Packer", "pk:"+key+"_skeleton", "c") {
			t.Errorf("a zombie's hit fired the damage.type.%s skeleton criterion", key)
		}
	}
	if srv.AdvancementObtained("Packer", "pk:skeleton", "c") {
		t.Error("a zombie's hit fired the skeleton source_entity criterion")
	}
	if srv.AdvancementObtained("Packer", "pk:shot", "c") {
		t.Error("a zombie's melee hit fired the is_projectile criterion")
	}

	srv.KillMob(id)
	skeleton := srv.SpawnMob("minecraft:skeleton", 8.5, 64, 0.5)
	shaft := srv.ShootMobArrowFrom(skeleton, 3.5, 65.2, 0.5, -3, 0, 0)
	if shaft == 0 {
		t.Fatal("the skeleton's arrow never flew")
	}
	srv.IgniteArrow(shaft, 100)
	servertest.RunTicks(t, srv, 8)
	for name, want := range map[string]bool{
		"shot_direct_arrow": true, "shot_source_skeleton": true, "shot_direct_burning": true, "shot_direct_moving": true,
		"shot_direct_skeleton": false, "shot_source_arrow": false, "shot_direct_unlit": false, "shot_direct_stationary": false,
	} {
		if got := srv.AdvancementObtained("Packer", "pk:"+name, "c"); got != want {
			t.Errorf("a skeleton's burning arrow: %s obtained = %v, want %v", name, got, want)
		}
	}
}

// TestPackKilledEntityTestsKillingBlow: a player_killed_entity criterion on
// the killing blow's source entity fires for the player's melee kill when it
// asks for a player, not when it asks for a zombie.
// vanilla: KilledTrigger.TriggerInstance.matches, DamageSourcePredicate.matches
func TestPackKilledEntityTestsKillingBlow(t *testing.T) {
	cfg, data := packConfig(t)
	dir := filepath.Join(data, "pk", "advancement")
	packCriterion(t, dir, "player", "player_killed_entity", `{"killing_blow":{"source_entity":{"minecraft:entity_type":"minecraft:player"}}}`)
	packCriterion(t, dir, "zombie", "player_killed_entity", `{"killing_blow":{"source_entity":{"minecraft:entity_type":"minecraft:zombie"}}}`)
	srv, c := joinOp(t, cfg, "Packer")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	give(srv, t, "Packer", "netherite_sword")
	c.Settle()
	meleeHit(t, srv, c, "Packer", "minecraft:chicken", false)
	servertest.UntilTicks(t, srv, 40, "the melee kill", func() bool { return srv.AdvancementObtained("Packer", "pk:player", "c") })
	if srv.AdvancementObtained("Packer", "pk:zombie", "c") {
		t.Error("a player's kill fired the zombie killing_blow criterion")
	}
}

// entityCond is an entity_properties condition on this with the predicate.
func entityCond(predicate string) string {
	return `{"condition":"minecraft:entity_properties","entity":"this","predicate":` + predicate + `}`
}

// tagCond is an entity nbt predicate that wants the tag.
func tagCond(tag string) string {
	return entityCond(`{"minecraft:nbt":"{Tags:[\"` + tag + `\"]}"}`)
}

// TestPackTameAnimalTestsItsEntityCondition: a tame_animal criterion with an
// entity nbt predicate fires for a wolf carrying the tag, not another tag.
// vanilla: TameAnimalTrigger.TriggerInstance.matches
func TestPackTameAnimalTestsItsEntityCondition(t *testing.T) {
	cfg, data := packConfig(t)
	dir := filepath.Join(data, "pk", "advancement")
	packCriterion(t, dir, "right", "tame_animal", `{"entity":[`+tagCond("pet")+`]}`)
	packCriterion(t, dir, "wrong", "tame_animal", `{"entity":[`+tagCond("other")+`]}`)
	srv, c := joinOp(t, cfg, "Packer")
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 64, 0.5, true)
	srv.GiveSlot("Packer", player.HotbarSlot, servertest.ItemStack(t, "minecraft:bone", 64))
	c.Settle()
	id := srv.SpawnMob("minecraft:wolf", 1.5, 64, 0.5)
	srv.SetMobNoAI(id)
	c.ChatCommand("tag @e[type=minecraft:wolf] add pet")
	c.Settle()
	for range 64 {
		if srv.AdvancementObtained("Packer", "pk:right", "c") {
			break
		}
		c.UseOn(id)
		c.Settle()
	}
	if !srv.AdvancementObtained("Packer", "pk:right", "c") {
		t.Fatal("the tagged wolf's taming did not fire the criterion")
	}
	if srv.AdvancementObtained("Packer", "pk:wrong", "c") {
		t.Error("a criterion wanting another tag fired")
	}
}

// TestPackSummonedEntityTestsItsEntityCondition: a summoned_entity criterion
// with a location predicate fires for a golem built in the overworld, not one
// wanting the nether.
// vanilla: SummonedEntityTrigger.TriggerInstance.matches
func TestPackSummonedEntityTestsItsEntityCondition(t *testing.T) {
	cfg, data := packConfig(t)
	dir := filepath.Join(data, "pk", "advancement")
	packCriterion(t, dir, "here", "summoned_entity",
		`{"entity":[`+entityCond(`{"minecraft:entity_type":"minecraft:iron_golem","minecraft:location":{"dimension":"minecraft:overworld"}}`)+`]}`)
	packCriterion(t, dir, "nether", "summoned_entity",
		`{"entity":[`+entityCond(`{"minecraft:entity_type":"minecraft:iron_golem","minecraft:location":{"dimension":"minecraft:the_nether"}}`)+`]}`)
	cfg.GameMode = "creative"
	srv, c := joinOp(t, cfg, "Packer")
	srv.GiveSlot("Packer", player.HotbarSlot, servertest.ItemStack(t, "minecraft:carved_pumpkin", 1))
	c.Settle()
	iron := servertest.MustState(t, "minecraft:iron_block", nil)
	for _, at := range [][3]int32{{-3, 64, 3}, {-4, 65, 3}, {-3, 65, 3}, {-2, 65, 3}} {
		srv.SetBlock(at[0], at[1], at[2], iron)
	}
	c.PlaceBlock(-3, 65, 3, protocol.FaceTop)
	c.Settle()
	servertest.UntilTicks(t, srv, 20, "the golem's criterion", func() bool { return srv.AdvancementObtained("Packer", "pk:here", "c") })
	if srv.AdvancementObtained("Packer", "pk:nether", "c") {
		t.Error("a nether location fired for an overworld golem")
	}
}

// TestPackCuredZombieVillagerTestsItsConditions: a cured_zombie_villager
// criterion tests the zombie's tag and the villager's profession.
// vanilla: CuredZombieVillagerTrigger.TriggerInstance.matches
func TestPackCuredZombieVillagerTestsItsConditions(t *testing.T) {
	cfg, data := packConfig(t)
	dir := filepath.Join(data, "pk", "advancement")
	profession := func(p string) string {
		return entityCond(`{"minecraft:nbt":"{VillagerData:{profession:\"minecraft:` + p + `\"}}"}`)
	}
	packCriterion(t, dir, "right", "cured_zombie_villager",
		`{"zombie":[`+tagCond("patient")+`],"villager":[`+profession("librarian")+`]}`)
	packCriterion(t, dir, "wrongzombie", "cured_zombie_villager", `{"zombie":[`+tagCond("other")+`]}`)
	packCriterion(t, dir, "wrongvillager", "cured_zombie_villager", `{"villager":[`+profession("farmer")+`]}`)
	srv, c := joinOp(t, cfg, "Packer")
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 64, 0.5, true)
	srv.GiveSlot("Packer", player.HotbarSlot, servertest.ItemStack(t, "minecraft:golden_apple", 1))
	c.ChatCommand(`summon minecraft:zombie_villager 1.5 64 0.5 {NoAI:1b,Tags:["patient"],VillagerData:{profession:"minecraft:librarian",level:1,type:"minecraft:plains"}}`)
	id := c.AwaitSpawn(zombieVillagerType(t)).EntityID
	c.ChatCommand(`effect give @e[type=minecraft:zombie_villager] minecraft:weakness 60`)
	c.Settle()
	c.UseOn(id)
	c.AwaitEntityEvent(id, protocol.EntityStatusZombieVillagerCure)
	c.ChatCommand(`data merge entity @e[type=minecraft:zombie_villager,limit=1] {ConversionTime:2}`)
	c.Settle()
	servertest.UntilTicks(t, srv, 60, "the cure's criterion", func() bool { return srv.AdvancementObtained("Packer", "pk:right", "c") })
	for _, wrong := range []string{"wrongzombie", "wrongvillager"} {
		if srv.AdvancementObtained("Packer", "pk:"+wrong, "c") {
			t.Errorf("%s fired", wrong)
		}
	}
}

// TestPackStartedRidingTestsItsVehicle: a started_riding criterion whose
// player predicate holds a vehicle nbt fires for the tagged vehicle only.
// vanilla: StartRidingTrigger, EntityPredicate vehicle
func TestPackStartedRidingTestsItsVehicle(t *testing.T) {
	cfg, data := packConfig(t)
	dir := filepath.Join(data, "pk", "advancement")
	vehicle := func(tag string) string {
		return `{"player":[` + entityCond(`{"minecraft:vehicle":{"minecraft:nbt":"{Tags:[\"`+tag+`\"]}"}}`) + `]}`
	}
	packCriterion(t, dir, "wrong", "started_riding", vehicle("other"))
	packCriterion(t, dir, "right", "started_riding", vehicle("mount"))
	srv, c := joinOp(t, cfg, "Packer")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	boat := srv.SpawnBoat(2.5, 64, 0.5, "minecraft:oak_boat")
	c.ChatCommand("tag @e[type=minecraft:oak_boat] add mount")
	c.Settle()
	c.UseOn(boat)
	servertest.AwaitPassengersFor(c, boat)
	servertest.UntilTicks(t, srv, 20, "the vehicle's criterion", func() bool { return srv.AdvancementObtained("Packer", "pk:right", "c") })
	if srv.AdvancementObtained("Packer", "pk:wrong", "c") {
		t.Error("a criterion wanting another vehicle tag fired")
	}
}

func zombieVillagerType(t *testing.T) int32 {
	t.Helper()
	id, ok := registry.EntityTypeID("minecraft:zombie_villager")
	if !ok {
		t.Fatal("no zombie_villager entity type")
	}
	return id
}

// TestPackHurtEntityFiresOnAPlayerLitTNTBlast: a player_hurt_entity criterion
// fires for a mob hurt by a TNT the player lit, with damage above zero before
// and after armor; a TNT nobody lit does not, and a criterion wanting far
// more damage does not.
// vanilla: LivingEntity.hurtServer, PlayerHurtEntityTrigger, DamagePredicate
func TestPackHurtEntityFiresOnAPlayerLitTNTBlast(t *testing.T) {
	cfg, data := packConfig(t)
	dir := filepath.Join(data, "pk", "advancement")
	cow := `{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:entity_type":"minecraft:cow"}}`
	packCriterion(t, dir, "any", "player_hurt_entity", `{"entity":[`+cow+`]}`)
	packCriterion(t, dir, "dealt", "player_hurt_entity", `{"entity":[`+cow+`],"damage":{"dealt":{"min":1},"taken":{"min":1}}}`)
	packCriterion(t, dir, "huge", "player_hurt_entity", `{"entity":[`+cow+`],"damage":{"dealt":{"min":1000}}}`)
	srv, c := joinOp(t, cfg, "Packer")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SpawnMob("minecraft:cow", 20.5, 64, 4.5)
	srv.SpawnTNT(20.5, 64, 4.5, 1)
	servertest.RunTicks(t, srv, 3)
	if srv.AdvancementObtained("Packer", "pk:any", "c") {
		t.Fatal("a TNT nobody lit fired the player's player_hurt_entity")
	}
	srv.SpawnMob("minecraft:cow", 20.5, 64, 4.5)
	srv.SpawnPlayerTNT("Packer", 20.5, 64, 4.5, 1)
	servertest.UntilTicks(t, srv, 5, "the player-lit blast's hurt", func() bool { return srv.AdvancementObtained("Packer", "pk:any", "c") })
	if !srv.AdvancementObtained("Packer", "pk:dealt", "c") {
		t.Error("the blast's damage before and after armor did not match a range above zero")
	}
	if srv.AdvancementObtained("Packer", "pk:huge", "c") {
		t.Error("a criterion wanting 1000 damage fired for a TNT blast")
	}
}

// TestPackHurtEntityFiresOnAMeleeHitOfAMannequin: a player_hurt_entity
// criterion fires for a punched mannequin, and one that wants a zombie does not.
// vanilla: LivingEntity.hurtServer
func TestPackHurtEntityFiresOnAMeleeHitOfAMannequin(t *testing.T) {
	cfg, data := packConfig(t)
	dir := filepath.Join(data, "pk", "advancement")
	packCriterion(t, dir, "any", "player_hurt_entity", `{}`)
	packCriterion(t, dir, "zombie", "player_hurt_entity",
		`{"entity":[{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:entity_type":"minecraft:zombie"}}]}`)
	packCriterion(t, dir, "mannequin", "player_hurt_entity",
		`{"entity":[{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:entity_type":"minecraft:mannequin"}}]}`)
	srv, c := joinOp(t, cfg, "Packer")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	id := srv.SpawnMannequinForTest(0.5, 64, 2.0)
	c.Settle()
	servertest.AwaitFullSwing(t, srv, "Packer")
	c.AttackLanded(srv, "Packer", id)
	servertest.UntilTicks(t, srv, 20, "the unconditioned criterion", func() bool { return srv.AdvancementObtained("Packer", "pk:any", "c") })
	servertest.UntilTicks(t, srv, 20, "the mannequin criterion", func() bool { return srv.AdvancementObtained("Packer", "pk:mannequin", "c") })
	if srv.AdvancementObtained("Packer", "pk:zombie", "c") {
		t.Fatal("a mannequin's hit fired the zombie criterion")
	}
}

// TestPackHurtEntityFiresOnAnArrowHitOfAMannequin: a player's arrow that hits a
// mannequin fires player_hurt_entity with the mannequin as the victim.
// vanilla: AbstractArrow.onHitEntity, LivingEntity.hurtServer
func TestPackHurtEntityFiresOnAnArrowHitOfAMannequin(t *testing.T) {
	cfg, data := packConfig(t)
	dir := filepath.Join(data, "pk", "advancement")
	packCriterion(t, dir, "zombie", "player_hurt_entity",
		`{"entity":[{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:entity_type":"minecraft:zombie"}}]}`)
	packCriterion(t, dir, "mannequin", "player_hurt_entity",
		`{"entity":[{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:entity_type":"minecraft:mannequin"}}]}`)
	srv, c := joinOp(t, cfg, "Packer")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.ChatCommand("summon mannequin 3.5 64 3.5 {immovable:1b}")
	c.Settle()
	owner := servertest.UuidWords(protocol.OfflineUUID("Packer"))
	c.ChatCommand(fmt.Sprintf("summon arrow 3.5 65 2 {Motion:[0d,0d,1.5d],NoGravity:1b,Owner:[I;%d,%d,%d,%d]}", owner[0], owner[1], owner[2], owner[3]))
	servertest.UntilTicks(t, srv, 20, "the mannequin criterion", func() bool { return srv.AdvancementObtained("Packer", "pk:mannequin", "c") })
	if srv.AdvancementObtained("Packer", "pk:zombie", "c") {
		t.Fatal("a mannequin's arrow hit fired the zombie criterion")
	}
}

// TestPackHurtEntityFiresOnASweepsSecondaryTarget: a sword's sweep that hurts a
// pig beside the cow it struck fires player_hurt_entity for the pig too.
// vanilla: Player.doSweepAttack, LivingEntity.hurtServer
func TestPackHurtEntityFiresOnASweepsSecondaryTarget(t *testing.T) {
	cfg, data := packConfig(t)
	dir := filepath.Join(data, "pk", "advancement")
	packCriterion(t, dir, "pig", "player_hurt_entity",
		`{"entity":[{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:entity_type":"minecraft:pig"}}]}`)
	srv, c := joinOp(t, cfg, "Packer")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	give(srv, t, "Packer", "iron_sword")
	cow := srv.SpawnMob("minecraft:cow", 0.5, 64, 2.0)
	srv.SpawnMob("minecraft:pig", 1.5, 64, 2.0)
	c.Settle()
	servertest.AwaitFullSwing(t, srv, "Packer")
	c.AttackLanded(srv, "Packer", cow)
	servertest.UntilTicks(t, srv, 20, "the pig criterion", func() bool { return srv.AdvancementObtained("Packer", "pk:pig", "c") })
}

// TestPackHurtEntityFiresOnASpearJab: a charged jab that hurts a zombie fires
// player_hurt_entity, and one with the wrong victim type does not.
// vanilla: Player.stabAttack, LivingEntity.hurtServer
func TestPackHurtEntityFiresOnASpearJab(t *testing.T) {
	cfg, data := packConfig(t)
	dir := filepath.Join(data, "pk", "advancement")
	packCriterion(t, dir, "zombie", "player_hurt_entity",
		`{"entity":[{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:entity_type":"minecraft:zombie"}}]}`)
	packCriterion(t, dir, "pig", "player_hurt_entity",
		`{"entity":[{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:entity_type":"minecraft:pig"}}]}`)
	srv, c := joinOp(t, cfg, "Packer")
	c.FacePlayer(0.5, 64, 0.5, 0, 0)
	c.Settle()
	srv.StopTick()
	srv.GiveSlot("Packer", player.HotbarSlot, servertest.ItemStack(t, "minecraft:wooden_spear", 1))
	srv.TickSurvival()
	srv.SetMobBaby(srv.SpawnMob("minecraft:zombie", 0.5, 64, 3.5), false)
	for i := 0; srv.AttackTicks("Packer") < 13; i++ {
		if i > 26 {
			t.Fatal("the spear never charged")
		}
		srv.TickSurvival()
	}
	c.Stab()
	c.Settle()
	srv.TickSurvival()
	servertest.UntilTicks(t, srv, 20, "the zombie criterion", func() bool { return srv.AdvancementObtained("Packer", "pk:zombie", "c") })
	if srv.AdvancementObtained("Packer", "pk:pig", "c") {
		t.Fatal("a jab of a zombie fired the pig criterion")
	}
}

// TestPackHurtEntityFiresOnAnInteractionHit: a player's hit on an interaction
// entity fires player_hurt_entity with a generic blow of 1.
// vanilla: Interaction.skipAttackInteraction
func TestPackHurtEntityFiresOnAnInteractionHit(t *testing.T) {
	cfg, data := packConfig(t)
	dir := filepath.Join(data, "pk", "advancement")
	packCriterion(t, dir, "interaction", "player_hurt_entity",
		`{"entity":[{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:entity_type":"minecraft:interaction"}}]}`)
	packCriterion(t, dir, "two", "player_hurt_entity", `{"damage":{"dealt":{"min":2}}}`)
	srv, c := joinOp(t, cfg, "Packer")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.ChatCommand("summon interaction 0.5 64 2 {width:1f,height:1f}")
	c.Settle()
	id := srv.FixtureIDForTest(entity.FixtureInteraction)
	c.Attack(id)
	servertest.UntilTicks(t, srv, 20, "the interaction criterion", func() bool { return srv.AdvancementObtained("Packer", "pk:interaction", "c") })
	if srv.AdvancementObtained("Packer", "pk:two", "c") {
		t.Fatal("an interaction's 1-damage blow fired the dealt.min 2 criterion")
	}
}

// TestPackHurtEntityFiresOnAPlayerLitTNTBlastOfTheEnderDragon: a
// player_hurt_entity criterion fires for the ender dragon hurt by a TNT the
// player lit.
// vanilla: EnderDragon.hurt, reallyHurt, LivingEntity.hurtServer
func TestPackHurtEntityFiresOnAPlayerLitTNTBlastOfTheEnderDragon(t *testing.T) {
	cfg, data := packConfig(t)
	dir := filepath.Join(data, "pk", "advancement")
	packCriterion(t, dir, "dragon", "player_hurt_entity",
		`{"entity":[{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:entity_type":"minecraft:ender_dragon"}}],"damage":{"taken":{"min":0.01}}}`)
	srv, c := joinOp(t, cfg, "Packer")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SpawnMob("minecraft:ender_dragon", 20.5, 64, 4.5)
	srv.SpawnPlayerTNT("Packer", 20.5, 64, 4.5, 1)
	servertest.UntilTicks(t, srv, 5, "the player-lit blast's hurt of the dragon", func() bool { return srv.AdvancementObtained("Packer", "pk:dragon", "c") })
}
