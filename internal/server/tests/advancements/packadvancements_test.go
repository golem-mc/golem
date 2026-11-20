package advancements

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/advancement"
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
