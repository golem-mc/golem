package commands

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestItemCommandsRemoveDefaultComponents(t *testing.T) {
	t.Parallel()
	const name = "Tester"
	srv, c := servertest.Join(t, servertest.CombatTestConfig(), name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	c.FacePlayer(5.5, 64, 5.5, 0, 0)
	c.Settle()

	for _, tc := range []struct {
		line, feedback string
		slot           int16
		item           registry.Item
		removed        []string
	}{
		{"give @s stone[!custom_name] 1", "commands.give.success.single", 36, registry.ItemStone, nil},
		{"give @s diamond_sword[!attribute_modifiers] 1", "commands.give.success.single", 37, registry.ItemDiamondSword, []string{"minecraft:attribute_modifiers"}},
		{"item replace entity @s hotbar.2 with diamond_sword[!enchantments,!lore,!rarity,!item_name,!tooltip_display,!item_model,!break_sound,!repair_cost,!damage]",
			itemEntitySetSingleKey, 38, registry.ItemDiamondSword, []string{
				"minecraft:break_sound", "minecraft:damage", "minecraft:enchantments", "minecraft:item_model",
				"minecraft:item_name", "minecraft:lore", "minecraft:rarity", "minecraft:repair_cost", "minecraft:tooltip_display",
			}},
		{"item replace entity @s hotbar.3 with stone[!damage,!container]", itemEntitySetSingleKey, 39, registry.ItemStone, nil},
	} {
		c.ChatCommand(tc.line)
		updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
		if !slices.ContainsFunc(chats, func(chat []byte) bool { return bytes.Contains(chat, []byte(tc.feedback)) }) {
			servertest.AwaitSystemChatSaying(t, c, tc.feedback)
		}
		received := updates[0].Stack
		if updates[0].Slot != tc.slot {
			t.Errorf("%s: slot = %d, want %d", tc.line, updates[0].Slot, tc.slot)
		}
		removed := received.RemovedNames()
		slices.Sort(removed)
		if received.ItemID != int32(tc.item) || received.Count != 1 || !slices.Equal(removed, tc.removed) {
			t.Errorf("%s: slot %d item/count/removals = %d/%d/%v, want %d/1/%v",
				tc.line, tc.slot, received.ItemID, received.Count, removed, tc.item, tc.removed)
		}
	}
	c.ChatCommand("data get entity @s Inventory")
	line := servertest.FeedbackComponent(t, c)
	if line.Translate != "commands.data.entity.query" || len(line.With) != 2 ||
		!strings.Contains(line.With[1].Plain(), `"!minecraft:attribute_modifiers": {}`) {
		t.Fatalf("Inventory feedback = %s, want the sword's attribute_modifiers removal", line.Plain())
	}

	c.SwapHotbarSlot(1)
	// Set armor to zero to measure the full attack before damage reduction.
	c.ChatCommand(`summon zombie ~ ~ ~2 {NoAI:1b,attributes:[{id:"minecraft:armor",base:0.0d}]}`)
	zombie := c.ExpectMobSpawn(protocol.EntityTypeZombie)
	c.Settle()
	servertest.AwaitFullSwing(t, srv, name)
	c.Attack(zombie.EntityID)
	c.Settle()
	if health, ok := srv.MobHealth(zombie.EntityID); !ok || health != 19 {
		t.Errorf("zombie health after a full hit = %v (present %t), want 19", health, ok)
	}

	c.ChatCommand("attribute @s attack_speed get")
	attribute := servertest.FeedbackComponent(t, c)
	if attribute.Translate != "commands.attribute.value.get.success" ||
		len(attribute.With) != 3 || attribute.With[2].Plain() != "4.0" {
		t.Errorf("attack speed feedback = %s, want base attack speed 4.0", attribute.Plain())
	}

	c.SwapHotbarSlot(2)
	c.ChatCommand("enchant @s sharpness 1")
	servertest.AwaitSystemChatSaying(t, c, "commands.enchant.success.single")
	c.ChatCommand(`summon zombie ~2 ~ ~ {NoAI:1b,attributes:[{id:"minecraft:armor",base:0.0d}]}`)
	zombie = c.ExpectMobSpawn(protocol.EntityTypeZombie)
	c.Settle()
	servertest.AwaitFullSwing(t, srv, name)
	c.Attack(zombie.EntityID)
	c.Settle()
	if health, ok := srv.MobHealth(zombie.EntityID); !ok || health != 13 {
		t.Errorf("zombie health after a hit with the non-enchanted sword = %v (present %t), want 13", health, ok)
	}
	c.ChatCommand("data get entity @s Inventory[{Slot:2b}].components")
	components := servertest.FeedbackComponent(t, c)
	if components.Translate != "commands.data.entity.query" || len(components.With) != 2 {
		t.Fatalf("held sword data = %s", components.Plain())
	}
	for _, removal := range []string{`"!minecraft:damage": {}`, `"!minecraft:enchantments": {}`} {
		if !strings.Contains(components.With[1].Plain(), removal) {
			t.Errorf("held sword components = %s, want %s", components.With[1].Plain(), removal)
		}
	}
}

func TestItemCommandsRemoveAllRemainingDefaults(t *testing.T) {
	t.Parallel()
	const name = "Tester"
	srv, c := servertest.Join(t, servertest.CombatTestConfig(), name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	c.FacePlayer(5.5, 64, 5.5, 0, 0)
	c.Settle()

	for _, tc := range []struct {
		spec    string
		item    registry.Item
		removed []string
	}{
		{"shulker_box[!container]", registry.ItemShulkerBox, []string{"minecraft:container"}},
		{"bundle[!bundle_contents]", registry.ItemBundle, []string{"minecraft:bundle_contents"}},
		{"white_banner[!banner_patterns]", registry.ItemWhiteBanner, []string{"minecraft:banner_patterns"}},
		{"potion[!potion_contents]", registry.ItemPotion, []string{"minecraft:potion_contents"}},
		{"light[!block_state]", registry.ItemLight, []string{"minecraft:block_state"}},
		{"chicken_spawn_egg[!entity_data]", registry.ItemChickenSpawnEgg, []string{"minecraft:entity_data"}},
		{"beehive[!bees]", registry.ItemBeehive, []string{"minecraft:bees"}},
		{"cod_bucket[!bucket_entity_data]", registry.ItemCodBucket, []string{"minecraft:bucket_entity_data"}},
		{"crossbow[!charged_projectiles]", registry.ItemCrossbow, []string{"minecraft:charged_projectiles"}},
		{"blue_egg[!chicken/variant]", registry.ItemBlueEgg, []string{"minecraft:chicken/variant"}},
		{"red_cushion[!cushion/color]", registry.ItemRedCushion, []string{"minecraft:cushion/color"}},
		{"debug_stick[!debug_stick_state]", registry.ItemDebugStick, []string{"minecraft:debug_stick_state"}},
		{"enchanted_golden_apple[!enchantment_glint_override]", registry.ItemEnchantedGoldenApple, []string{"minecraft:enchantment_glint_override"}},
		{"firework_rocket[!fireworks]", registry.ItemFireworkRocket, []string{"minecraft:fireworks"}},
		{"goat_horn[!instrument]", registry.ItemGoatHorn, []string{"minecraft:instrument"}},
		{"filled_map[!map_decorations]", registry.ItemFilledMap, []string{"minecraft:map_decorations"}},
		{"decorated_pot[!pot_decorations]", registry.ItemDecoratedPot, []string{"minecraft:pot_decorations"}},
		{"tipped_arrow[!potion_duration_scale]", registry.ItemTippedArrow, []string{"minecraft:potion_duration_scale"}},
		{"angler_pottery_sherd[!provides_pottery_pattern]", registry.ItemAnglerPotterySherd, []string{"minecraft:provides_pottery_pattern"}},
		{"knowledge_book[!recipes]", registry.ItemKnowledgeBook, []string{"minecraft:recipes"}},
		{"oak_sign[!sign_text_front,!sign_text_back]", registry.ItemOakSign, []string{"minecraft:sign_text_back", "minecraft:sign_text_front"}},
		{"enchanted_book[!stored_enchantments]", registry.ItemEnchantedBook, []string{"minecraft:stored_enchantments"}},
		{"suspicious_stew[!suspicious_stew_effects]", registry.ItemSuspiciousStew, []string{"minecraft:suspicious_stew_effects"}},
		{"writable_book[!writable_book_content]", registry.ItemWritableBook, []string{"minecraft:writable_book_content"}},
	} {
		line := "item replace entity @s hotbar.0 with " + tc.spec
		c.ChatCommand(line)
		updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
		if !slices.ContainsFunc(chats, func(chat []byte) bool { return bytes.Contains(chat, []byte(itemEntitySetSingleKey)) }) {
			servertest.AwaitSystemChatSaying(t, c, itemEntitySetSingleKey)
		}
		got := updates[0].Stack
		removed := got.RemovedNames()
		slices.Sort(removed)
		if updates[0].Slot != 36 || got.ItemID != int32(tc.item) || got.Count != 1 || !slices.Equal(removed, tc.removed) {
			t.Errorf("%s: slot=%d item=%d count=%d removed=%v, want 36/%d/1/%v",
				tc.spec, updates[0].Slot, got.ItemID, got.Count, removed, tc.item, tc.removed)
		}
	}

	// Verify saved inventory NBT carries a removal from word 1 (bit 68).
	c.ChatCommand("data get entity @s Inventory[{Slot:0b}].components")
	components := servertest.FeedbackComponent(t, c)
	if components.Translate != "commands.data.entity.query" || len(components.With) != 2 ||
		!strings.Contains(components.With[1].Plain(), `"!minecraft:writable_book_content": {}`) {
		t.Fatalf("writable book components = %s, want !minecraft:writable_book_content", components.Plain())
	}

	// Verify a spawn egg with !entity_data spawns nothing when used on the ground.
	c.ChatCommand("item replace entity @s hotbar.0 with chicken_spawn_egg[!entity_data]")
	servertest.AwaitSlotUpdates(t, c, 1)
	servertest.AwaitSystemChatSaying(t, c, itemEntitySetSingleKey)
	c.PlaceBlock(5, 63, 5, protocol.FaceTop)
	c.Settle()
	if got := srv.InventorySlot(name, 36); got.ItemID != int32(registry.ItemChickenSpawnEgg) || got.Count != 1 {
		t.Errorf("spawn egg after use = %+v, want unconsumed chicken_spawn_egg", got)
	}
	if len(srv.Mobs()) != 0 {
		t.Errorf("mobs spawned by chicken_spawn_egg[!entity_data] = %d, want 0", len(srv.Mobs()))
	}
}

func TestRemovedDefaultConsumers(t *testing.T) {
	t.Parallel()
	const name = "Tester"
	srv, c := servertest.Join(t, servertest.CombatTestConfig(), name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	c.FacePlayer(5.5, 64, 5.5, 0, 90)
	c.Settle()

	replaceSlot := func(slot, spec string) protocol.ItemStack {
		t.Helper()
		c.ChatCommand("item replace entity @s " + slot + " with " + spec)
		updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
		if !slices.ContainsFunc(chats, func(chat []byte) bool { return bytes.Contains(chat, []byte(itemEntitySetSingleKey)) }) {
			servertest.AwaitSystemChatSaying(t, c, itemEntitySetSingleKey)
		}
		return updates[0].Stack
	}
	queryData := func(cmd string) string {
		t.Helper()
		c.ChatCommand(cmd)
		line := servertest.FeedbackComponent(t, c)
		if line.Translate != "commands.data.entity.query" || len(line.With) != 2 {
			t.Fatalf("%s = %s, want commands.data.entity.query", cmd, line.Plain())
		}
		return line.With[1].Plain()
	}

	// 1. Thrown blue_egg[!chicken/variant] keeps its removal on the projectile.
	replaceSlot("hotbar.0", "blue_egg[!chicken/variant]")
	servertest.UseHeld(c, 0, 0)
	c.Settle()
	eggComps := queryData("data get entity @e[type=egg,limit=1] Item.components")
	if !strings.Contains(eggComps, `"!minecraft:chicken/variant": {}`) {
		t.Errorf("thrown egg Item.components = %s, want !minecraft:chicken/variant", eggComps)
	}
	c.ChatCommand("kill @e[type=egg]")
	servertest.AwaitSystemChatSaying(t, c, "commands.kill.success.single")

	// 2. Thrown lingering potion preserves both removed (!potion_duration_scale -> 1.0,
	// omitted from cloud NBT) and explicit (0.5f) duration scales in the spawned cloud.
	replaceSlot("hotbar.0", `lingering_potion[potion_contents={potion:"minecraft:slowness"},!potion_duration_scale]`)
	servertest.UseHeld(c, 0, 0)
	c.Settle()
	servertest.RunTicks(t, srv, 15)
	cloudData := queryData("data get entity @e[type=area_effect_cloud,limit=1]")
	if strings.Contains(cloudData, "potion_duration_scale") {
		t.Errorf("stripped lingering potion cloud NBT = %s, want potion_duration_scale omitted (default 1.0)", cloudData)
	}
	c.ChatCommand("kill @e[type=area_effect_cloud]")
	servertest.AwaitSystemChatSaying(t, c, "commands.kill.success.single")

	replaceSlot("hotbar.0", `lingering_potion[potion_contents={potion:"minecraft:slowness"},potion_duration_scale=0.5f]`)
	servertest.UseHeld(c, 0, 0)
	c.Settle()
	servertest.RunTicks(t, srv, 15)
	cloudScale := queryData("data get entity @e[type=area_effect_cloud,limit=1] potion_duration_scale")
	if cloudScale != "0.5f" {
		t.Errorf("explicit lingering potion cloud potion_duration_scale = %s, want 0.5f", cloudScale)
	}
	c.ChatCommand("kill @e[type=area_effect_cloud]")
	servertest.AwaitSystemChatSaying(t, c, "commands.kill.success.single")

	// 3. set_contents restores minecraft:container on shulker_box[!container].
	replaceSlot("hotbar.0", "shulker_box[!container]")
	c.ChatCommand(`item modify entity @s hotbar.0 {type:"minecraft:set_contents",component:"minecraft:container",entries:[{type:"minecraft:item",name:"minecraft:stick"}]}`)
	updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
	if !slices.ContainsFunc(chats, func(chat []byte) bool { return bytes.Contains(chat, []byte(itemEntitySingleModifyKey)) }) {
		servertest.AwaitSystemChatSaying(t, c, itemEntitySingleModifyKey)
	}
	if got := updates[0].Stack; got.Removes(protocol.DataComponentContainer) || len(got.Container) != 1 || got.Container[0].ItemID != int32(registry.ItemStick) {
		t.Errorf("restored shulker_box = %+v, want stick in container with no removal", got)
	}
	shulkerComps := queryData("data get entity @s Inventory[{Slot:0b}].components")
	if !strings.Contains(shulkerComps, `"minecraft:container"`) || !strings.Contains(shulkerComps, `"minecraft:stick"`) ||
		strings.Contains(shulkerComps, `"!minecraft:container"`) {
		t.Errorf("restored shulker_box components = %s, want minecraft:container with stick and no removal", shulkerComps)
	}

	// 4. set_fireworks on firework_rocket[!fireworks] starts from flight 0, not 1.
	replaceSlot("hotbar.0", "firework_rocket[!fireworks]")
	c.ChatCommand(`item modify entity @s hotbar.0 {type:"minecraft:set_fireworks"}`)
	updates, chats = servertest.AwaitSlotUpdates(t, c, 1)
	if !slices.ContainsFunc(chats, func(chat []byte) bool { return bytes.Contains(chat, []byte(itemEntitySingleModifyKey)) }) {
		servertest.AwaitSystemChatSaying(t, c, itemEntitySingleModifyKey)
	}
	if got := updates[0].Stack; got.Removes(protocol.DataComponentFireworks) {
		t.Errorf("modified firework_rocket still removes fireworks: %+v", got)
	}
	rocketComps := queryData("data get entity @s Inventory[{Slot:0b}].components")
	if !strings.Contains(rocketComps, `"minecraft:fireworks": {}`) || strings.Contains(rocketComps, "flight_duration") {
		t.Errorf("modified firework_rocket components = %s, want empty minecraft:fireworks (flight 0)", rocketComps)
	}

	// 5. Bare and typed /clear predicates match ordinary defaults and skip stripped ones.
	for _, tc := range []struct {
		ordinary, stripped string
		predicates         []string
	}{
		{"shulker_box", "shulker_box[!container]", []string{"shulker_box[container]", "shulker_box[container~{}]"}},
		{"bundle", "bundle[!bundle_contents]", []string{"bundle[bundle_contents]", "bundle[bundle_contents~{}]"}},
		{"enchanted_book", "enchanted_book[!stored_enchantments]", []string{"enchanted_book[stored_enchantments]", "enchanted_book[stored_enchantments~[]]"}},
	} {
		replaceSlot("hotbar.0", tc.stripped)
		for _, pred := range tc.predicates {
			c.ChatCommand("clear @s " + pred + " 0")
			servertest.AwaitCommandErrorText(t, c, "clear.failed.single", servertest.PlayerDisplayComponent(name))
		}
		replaceSlot("hotbar.0", tc.ordinary)
		for _, pred := range tc.predicates {
			c.ChatCommand("clear @s " + pred + " 0")
			if line := servertest.FeedbackComponent(t, c); line.Translate != "commands.clear.test.single" {
				t.Errorf("clear %s on %s = %s, want commands.clear.test.single", pred, tc.ordinary, line.Plain())
			}
		}
	}

	// 6. Clicking a stripped bundle with stone on the cursor swaps instead of inserting.
	replaceSlot("hotbar.0", "bundle[!bundle_contents]")
	replaceSlot("hotbar.1", "stone")
	w := &servertest.WindowedClient{Client: c}
	w.Click(37, 0, protocol.ClickModePickup)
	c.Settle()
	w.Click(36, 0, protocol.ClickModePickup)
	c.Settle()
	w.Click(37, 0, protocol.ClickModePickup)
	c.Settle()
	if got := srv.InventorySlot(name, 36); got.ItemID != int32(registry.ItemStone) || got.Count != 1 {
		t.Errorf("slot 36 after clicking stripped bundle = %+v, want stone", got)
	}
	if got := srv.InventorySlot(name, 37); got.ItemID != int32(registry.ItemBundle) || !got.Removes(protocol.DataComponentBundleContents) || len(got.BundleContents()) != 0 {
		t.Errorf("slot 37 after clicking stripped bundle = %+v, want empty bundle[!bundle_contents]", got)
	}

	// 7. Using goat_horn[!instrument] triggers neither horn sound nor cooldown.
	replaceSlot("hotbar.0", "goat_horn[!instrument]")
	servertest.UseHeld(c, 0, 0)
	var sounds, cooldowns int
	c.SettleAndCollect(func(id int32, _ []byte) {
		switch id {
		case protocol.IDPlayEntitySoundEffect:
			sounds++
		case protocol.IDPlayCooldown:
			cooldowns++
		}
	})
	if sounds != 0 || cooldowns != 0 {
		t.Errorf("goat_horn[!instrument] use sent sounds=%d cooldowns=%d, want 0/0", sounds, cooldowns)
	}
}

func TestItemReplaceExplicitEmptyBundleContents(t *testing.T) {
	t.Parallel()
	const name = "Tester"
	srv, c := servertest.Join(t, servertest.CombatTestConfig(), name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)

	c.ChatCommand("item replace entity @s hotbar.0 with stone[bundle_contents=[]]")
	updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
	if !slices.ContainsFunc(chats, func(chat []byte) bool { return bytes.Contains(chat, []byte(itemEntitySetSingleKey)) }) {
		servertest.AwaitSystemChatSaying(t, c, itemEntitySetSingleKey)
	}
	got := updates[0].Stack
	if updates[0].Slot != 36 || got.ItemID != int32(registry.ItemStone) || got.Count != 1 {
		t.Fatalf("slot update = %+v, want 1x stone in slot 36", updates[0])
	}
	if !slices.ContainsFunc(got.Raw, func(raw protocol.RawComponent) bool {
		return raw.Type == protocol.DataComponentBundleContents
	}) || len(got.BundleContents()) != 0 {
		t.Fatalf("wire stack = %+v, want explicit empty bundle_contents component", got)
	}

	c.ChatCommand("data get entity @s Inventory[{Slot:0b}].components")
	components := servertest.FeedbackComponent(t, c)
	if components.Translate != "commands.data.entity.query" || len(components.With) != 2 ||
		!strings.Contains(components.With[1].Plain(), `"minecraft:bundle_contents": []`) {
		t.Errorf("stone components = %s, want minecraft:bundle_contents: []", components.Plain())
	}

	for _, pred := range []string{"stone[bundle_contents]", "stone[bundle_contents=[]]", "stone[bundle_contents~{}]"} {
		c.ChatCommand("clear @s " + pred + " 0")
		if line := servertest.FeedbackComponent(t, c); line.Translate != "commands.clear.test.single" {
			t.Errorf("clear %s = %s, want commands.clear.test.single", pred, line.Plain())
		}
	}

	// Empty bundle_contents on a bundle matches its prototype default and stays unpatched.
	c.ChatCommand("item replace entity @s hotbar.0 with bundle[bundle_contents=[]]")
	updates, chats = servertest.AwaitSlotUpdates(t, c, 1)
	if !slices.ContainsFunc(chats, func(chat []byte) bool { return bytes.Contains(chat, []byte(itemEntitySetSingleKey)) }) {
		servertest.AwaitSystemChatSaying(t, c, itemEntitySetSingleKey)
	}
	bundle := updates[0].Stack
	if bundle.ItemID != int32(registry.ItemBundle) || bundle.Removes(protocol.DataComponentBundleContents) ||
		slices.ContainsFunc(bundle.Raw, func(raw protocol.RawComponent) bool {
			return raw.Type == protocol.DataComponentBundleContents
		}) {
		t.Errorf("bundle[bundle_contents=[]] = %+v, want unpatched default bundle", bundle)
	}

	// Non-compound list entries remain malformed.
	c.ChatCommand("item replace entity @s hotbar.0 with stone[bundle_contents=[5]]")
	if line := servertest.FeedbackComponent(t, c); !strings.Contains(line.Plain(), "Malformed 'minecraft:bundle_contents' component") {
		t.Errorf("stone[bundle_contents=[5]] feedback = %s, want malformed bundle_contents error", line.Plain())
	}
}
