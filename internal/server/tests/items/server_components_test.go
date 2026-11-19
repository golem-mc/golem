package items

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The data components a stack carries, as they travel: what a vanilla player
// file's enchanted stacks read back as, what a creative client may put in a
// slot, what refuses to merge with a plain stack, and what /give hands out
// in vanilla's own syntax.

// TestVanillaEnchantedFileIsRead checks this server against a player file a
// real vanilla server wrote: the sword it enchanted five ways and the
// book it filled with stored enchantments arrive in the player's inventory
// with every component on them, and the file they are saved back into holds
// the compounds the vanilla one did.
func TestVanillaEnchantedFileIsRead(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	// The fixture is the file a vanilla server wrote for a player given a
	// diamond sword carrying minecraft:enchantments (sharpness 5),
	// minecraft:custom_name ("Sting"), minecraft:unbreakable,
	// minecraft:repair_cost (3) and minecraft:enchantment_glint_override
	// (true), and an enchanted book carrying minecraft:stored_enchantments
	// (sharpness 3, unbreaking 2).
	const name = "Enchanter"
	before := servertest.InstallPlayerFixture(t, cfg, name, "vanilla-enchanted.dat")
	saved := servertest.PlayerFile(cfg.LevelName, name)

	_, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login(name)
	c.Configure()

	// The join's window content is the wire's first sight of the stacks,
	// components and all.
	slots := c.Inventory()
	sharpness := servertest.SharpnessID(t)
	sword, ok := registry.ItemID("minecraft:diamond_sword")
	if !ok {
		t.Fatal("the registry has no diamond_sword")
	}
	sting := protocol.Text{Text: "Sting"}
	wantSword := protocol.ItemStack{
		ItemID:        sword,
		Count:         1,
		Enchantments:  map[int32]int32{sharpness: 5},
		CustomName:    &sting,
		RepairCost:    3,
		GlintOverride: protocol.GlintShown,
		Unbreakable:   true,
	}
	if !slots[36].Equal(wantSword) {
		t.Errorf("slot 36 = %+v, want the vanilla file's sword %+v", slots[36], wantSword)
	}
	book, ok := registry.ItemID("minecraft:enchanted_book")
	if !ok {
		t.Fatal("the registry has no enchanted_book")
	}
	unbreaking, ok := registry.EnchantmentID("minecraft:unbreaking")
	if !ok {
		t.Fatal("the registry has no unbreaking")
	}
	wantBook := protocol.ItemStack{
		ItemID:             book,
		Count:              1,
		StoredEnchantments: map[int32]int32{sharpness: 3, unbreaking: 2},
	}
	if !slots[37].Equal(wantBook) {
		t.Errorf("slot 37 = %+v, want the vanilla file's book %+v", slots[37], wantBook)
	}

	c.EnterWorld(name)
	c.Disconnect()

	// And the file they are saved back into is the vanilla one, compound
	// for compound: the components under the keys and in the shapes the
	// vanilla stack codec writes.
	after := servertest.ReadPlayerFile(t, saved)
	want, _ := nbt.Get[nbt.List](before, "Inventory")
	got, ok := nbt.Get[nbt.List](after, "Inventory")
	if !ok || len(got.Items) != len(want.Items) {
		t.Fatalf("Inventory holds %v, want the %d stacks it was loaded with", got, len(want.Items))
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Inventory =\n%v\nwant\n%v", got, want)
	}
}

// TestVanillaLoadedMultishotCrossbowIsRead checks that a crossbow vanilla
// loaded with multishot, whose arrows after the first carry
// minecraft:intangible_projectile, arrives in the inventory with both arrows
// and is saved back with the component on the second.
// vanilla: ProjectileWeaponItem.useAmmo
func TestVanillaLoadedMultishotCrossbowIsRead(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	const name = "Crossbowman"
	root := servertest.InstallPlayerFixture(t, cfg, name, "vanilla-enchanted.dat")
	saved := servertest.PlayerFile(cfg.LevelName, name)
	arrow := func(intangible bool) nbt.Compound {
		stack := nbt.Compound{"id": nbt.String("minecraft:arrow"), "count": nbt.Int(1)}
		if intangible {
			stack["components"] = nbt.Compound{"minecraft:intangible_projectile": nbt.Compound{}}
		}
		return stack
	}
	inventory, _ := nbt.Get[nbt.List](root, "Inventory")
	inventory.Items = append(inventory.Items, nbt.Compound{
		"Slot":  nbt.Byte(2),
		"id":    nbt.String("minecraft:crossbow"),
		"count": nbt.Int(1),
		"components": nbt.Compound{"minecraft:charged_projectiles": nbt.List{
			Elem: nbt.TypeCompound, Items: []nbt.Tag{arrow(false), arrow(true)},
		}},
	})
	root["Inventory"] = inventory
	servertest.WritePlayerFile(t, saved, root)

	_, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login(name)
	c.Configure()
	slots := c.Inventory()
	if registry.Item(slots[38].ItemID) != registry.ItemCrossbow {
		t.Fatalf("slot 38 = %+v, want the vanilla file's crossbow", slots[38])
	}
	if got := len(slots[38].ChargedProjectiles()); got != 2 {
		t.Errorf("the crossbow holds %d charged projectiles, want 2", got)
	}
	c.EnterWorld(name)
	c.Disconnect()

	after := servertest.ReadPlayerFile(t, saved)
	got, _ := nbt.Get[nbt.List](after, "Inventory")
	var crossbow nbt.Compound
	for _, item := range got.Items {
		if stack, ok := item.(nbt.Compound); ok && stack["Slot"] == nbt.Byte(2) {
			crossbow = stack
		}
	}
	components, _ := nbt.Get[nbt.Compound](crossbow, "components")
	loaded, _ := nbt.Get[nbt.List](components, "minecraft:charged_projectiles")
	if len(loaded.Items) != 2 {
		t.Fatalf("saved crossbow = %v, want two charged projectiles", crossbow)
	}
	// A count of 1 is left out of a stack saved in charged_projectiles.
	wantSecond := arrow(true)
	delete(wantSecond, "count")
	if !reflect.DeepEqual(loaded.Items[1], wantSecond) {
		t.Errorf("saved second arrow = %v, want %v", loaded.Items[1], wantSecond)
	}
}

// TestCreativeMaySetAnEnchantedItem checks the one packet a creative client
// invents stacks with: a slot set to an enchanted sword is an enchanted
// sword, on the wire and in the inventory, whatever the wear and the rest of
// its components say.
func TestCreativeMaySetAnEnchantedItem(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	// A watcher joins first, so that what it hears of the conjuror is the
	// join's own traffic: the player info and spawn, and the equipment
	// change the set is.
	watcher := connect().Join("Watcher")

	c := connect()
	c.Login("Conjuror")
	c.Configure()
	c.Inventory()
	c.EnterWorld("Conjuror")

	sword, ok := registry.ItemID("minecraft:diamond_sword")
	if !ok {
		t.Fatal("the registry has no diamond_sword")
	}
	sharpness := servertest.SharpnessID(t)
	name := protocol.Text{Text: "Sting"}
	enchanted := protocol.ItemStack{
		ItemID:        sword,
		Count:         1,
		Damage:        11,
		Enchantments:  map[int32]int32{sharpness: 5},
		CustomName:    &name,
		GlintOverride: protocol.GlintHidden,
	}
	c.SetCreativeSlot(36, enchanted)
	servertest.WaitFor(t, srv, 100, func() bool {
		return srv.InventorySlot("Conjuror", player.HotbarSlot).Equal(enchanted)
	})
	if got := srv.InventorySlot("Conjuror", player.HotbarSlot); !got.Equal(enchanted) {
		t.Fatalf("the held item = %+v, want the enchanted sword the client set", got)
	}
	entityID := watcher.ExpectPlayerAdded("Conjuror")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Conjuror")

	// And the next set is heard as the equipment the others see, the sword
	// whole with its components on it.
	c.SetCreativeSlot(36, enchanted)
	entries := watcher.ExpectEquipment(entityID)
	if len(entries) == 0 || !servertest.EquipEntry(entries, protocol.EquipMainHand).Equal(enchanted) {
		t.Errorf("the equipment = %v, want the enchanted sword in the main hand", entries)
	}
	watcher.Disconnect()
	c.Disconnect()
}

// heldPotion is the trusted slot a potion named Bob with the given
// potion_contents value is shown as: one item, two components added and none
// removed, the name's tag and the contents' bytes unprefixed.
func heldPotion(potion int32, contents []byte) []byte {
	var b protocol.Buffer
	b.PutVarInt(1)
	b.PutVarInt(potion)
	b.PutVarInt(2)
	b.PutVarInt(0)
	b.PutVarInt(protocol.DataComponentCustomName)
	b.PutU8(0x08) // a string tag
	b.PutI16(3)
	b.PutRaw([]byte("Bob"))
	b.PutVarInt(protocol.DataComponentPotionContents)
	b.PutRaw(contents)
	return b.Bytes()
}

// TestCreativeKeepsComponentsItDoesNotModel checks what a creative player
// holding something this server has no model for gets: the stack is kept
// with the unmodelled bytes on it, and everyone it is shown to sees those
// bytes again — a potion from the creative menu is a potion wherever it
// goes, not a bottle the server emptied.
func TestCreativeKeepsComponentsItDoesNotModel(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	watcher := connect().Join("Watcher")

	c := connect()
	c.Login("Alchemist")
	c.Configure()
	c.Inventory()
	c.EnterWorld("Alchemist")
	entityID := watcher.ExpectPlayerAdded("Alchemist")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Alchemist")

	potion, ok := registry.ItemID("minecraft:potion")
	if !ok {
		t.Fatal("the registry has no potion")
	}
	// The stack a vanilla client sends for a potion off the creative menu:
	// a custom name this server models, and the potion's contents, whose
	// value this server has no shape for. The value
	// travels length-prefixed, the untrusted slot shape's way, and reads
	// as the potion_contents the wiki documents — a potion ID, then three
	// absent optionals — which is the check it is kept against: the bytes
	// ride because the client's own codec reads them.
	name := protocol.Text{Text: "Bob"}
	contents := []byte{0x01, 0x05, 0x00, 0x00, 0x00}
	setPotion := func(slot int16) {
		c.Send(protocol.IDPlayCreativeSlot, func(b *protocol.Buffer) {
			b.PutI16(slot)
			b.PutVarInt(1)      // count
			b.PutVarInt(potion) // minecraft:potion
			b.PutVarInt(2)      // components to add
			b.PutVarInt(0)      // none to remove
			b.PutVarInt(protocol.DataComponentCustomName)
			b.PutVarInt(6) // the value's byte length
			b.PutU8(0x08)  // a string tag
			b.PutI16(3)
			b.PutRaw([]byte("Bob"))
			b.PutVarInt(protocol.DataComponentPotionContents)
			b.PutVarInt(int32(len(contents)))
			b.PutRaw(contents)
		})
	}

	setPotion(36)
	servertest.WaitFor(t, srv, 100, func() bool {
		got := srv.InventorySlot("Alchemist", player.HotbarSlot)
		return got.ItemID == potion && got.Count == 1
	})
	got := srv.InventorySlot("Alchemist", player.HotbarSlot)
	if got.ItemID != potion || got.Count != 1 {
		t.Fatalf("the held item = %+v, want the potion the client set", got)
	}
	if got.CustomName == nil || got.CustomName.Text != name.Text {
		t.Errorf("the held item's name = %v, want the Bob the client named it", got.CustomName)
	}

	// And the stack rides back out whole: the equipment the others see
	// carries the unmodelled bytes unchanged, the way the client sent them.
	setPotion(36)
	held := heldPotion(potion, contents)
	var equipment protocol.Buffer
	equipment.PutVarInt(entityID)
	equipment.PutU8(0x80 | protocol.EquipMainHand) // more to follow
	equipment.PutRaw(held)
	for slot := protocol.EquipOffHand; slot < protocol.EquipHead; slot++ {
		equipment.PutU8(0x80 | uint8(slot))
		equipment.PutSlot(protocol.ItemStack{})
	}
	equipment.PutU8(protocol.EquipHead)
	equipment.PutSlot(protocol.ItemStack{})
	if got := watcher.ExpectPlay(protocol.IDPlayEntityEquipment); !bytes.Equal(got, equipment.Bytes()) {
		t.Errorf("the equipment = % X, want the potion's components re-sent whole: % X",
			got, equipment.Bytes())
	}
	watcher.Disconnect()
	c.Disconnect()
}

// TestCreativeThrowsComponentsItDoesNotModel checks the stack dropped out of
// the window: the item entity it becomes carries the unmodelled bytes the
// client sent, so what lands on the ground is what was thrown.
func TestCreativeThrowsComponentsItDoesNotModel(t *testing.T) {
	t.Parallel()
	_, connect := servertest.Start(t, servertest.BlockTestConfig())
	c := connect()
	c.Login("Alchemist")
	c.Configure()
	c.Inventory()
	c.EnterWorld("Alchemist")

	potion, ok := registry.ItemID("minecraft:potion")
	if !ok {
		t.Fatal("the registry has no potion")
	}
	contents := []byte{0x01, 0x05, 0x00, 0x00, 0x00}
	c.Send(protocol.IDPlayCreativeSlot, func(b *protocol.Buffer) {
		b.PutI16(-1) // the stack dropped out of the window
		b.PutVarInt(1)
		b.PutVarInt(potion)
		b.PutVarInt(2)
		b.PutVarInt(0)
		b.PutVarInt(protocol.DataComponentCustomName)
		b.PutVarInt(6)
		b.PutU8(0x08)
		b.PutI16(3)
		b.PutRaw([]byte("Bob"))
		b.PutVarInt(protocol.DataComponentPotionContents)
		b.PutVarInt(int32(len(contents)))
		b.PutRaw(contents)
	})

	// The stack the world is shown: the spawn of the item entity, then the
	// metadata naming what it holds, byte for byte the stack that was set.
	r := protocol.NewReader(c.ExpectItemPacket(protocol.IDPlaySpawnEntity))
	entityID := r.ReadVarInt()
	r.ReadUUID()
	if got := r.ReadVarInt(); got != protocol.EntityTypeItem {
		t.Fatalf("spawned entity type = %d, want %d (minecraft:item)", got, protocol.EntityTypeItem)
	}

	held := heldPotion(potion, contents)
	var metadata protocol.Buffer
	metadata.PutVarInt(entityID)
	metadata.PutMetadata(protocol.MetadataIndexItemSlot, protocol.MetadataSlot)
	metadata.PutRaw(held)
	metadata.EndMetadata()
	if got := c.ExpectItemPacket(protocol.IDPlayEntityMetadata); !bytes.Equal(got, metadata.Bytes()) {
		t.Errorf("the item's metadata = % X, want the components re-sent whole: % X",
			got, metadata.Bytes())
	}
	c.Disconnect()
}

// TestAnEnchantmentCountTheStackDoesNotBackIsRefused sends the one count a
// wire-supplied size a stack allocation from: an enchantments component
// claiming 0x7fffffff entries out of a value with none behind them. The
// count the bytes do not back fails the read, the refused stack leaves the
// slot as it was, and neither the connection nor the server pays for the
// claim.
// vanilla presizes the levels map at Math.min(count, 65536)
func TestAnEnchantmentCountTheStackDoesNotBackIsRefused(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Hoarder")

	sword, ok := registry.ItemID("minecraft:diamond_sword")
	if !ok {
		t.Fatal("the registry has no diamond_sword")
	}
	// One enchanted pair is what the count is read from; nothing behind
	// it has to reach the refusal, the read failing on the first entry.
	c.Send(protocol.IDPlayCreativeSlot, func(b *protocol.Buffer) {
		b.PutI16(36)
		b.PutVarInt(1) // count
		b.PutVarInt(sword)
		b.PutVarInt(1) // one component added
		b.PutVarInt(0) // none removed
		b.PutVarInt(protocol.DataComponentEnchantments)
		b.PutVarInt(5) // the value's byte length
		b.PutVarInt(0x7fffffff)
	})

	if got := srv.InventorySlot("Hoarder", player.HotbarSlot); !got.Empty() {
		t.Errorf("the held item = %+v, want the claim refused", got)
	}
	// The slot is put back the way it was, the client's belief corrected.
	slot := c.AwaitPlay(protocol.IDPlayContainerSlot)
	r := protocol.NewReader(slot)
	r.ReadVarInt() // window ID
	r.ReadVarInt() // state ID
	if got := r.ReadI16(); got != 36 {
		t.Errorf("the correction named slot %d, want 36", got)
	}
	if stack, ok := r.ReadSlot(); !ok || !stack.Empty() {
		t.Errorf("the correction put %+v in the slot, want nothing", stack)
	}
	if got := srv.PlayerCount(); got != 1 {
		t.Errorf("player count = %d, want the one still connected", got)
	}
	c.Chat("still here")
	c.ExpectChat("Hoarder", "still here")
}

// TestEnchantedStacksDoNotMerge checks the equality the merges run on: two
// stacks of one item carrying different components are as far apart as two
// different items, in the windows and in the world.
func TestEnchantedStacksDoNotMerge(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	sword, ok := registry.ItemID("minecraft:diamond_sword")
	if !ok {
		t.Fatal("the registry has no diamond_sword")
	}
	sharpness := servertest.SharpnessID(t)
	plain := protocol.ItemStack{ItemID: sword, Count: 1}
	enchanted := protocol.ItemStack{ItemID: sword, Count: 1,
		Enchantments: map[int32]int32{sharpness: 5}}

	w.SetCreativeSlot(36, plain)
	w.SetCreativeSlot(37, enchanted)

	// Picking the enchanted one up and clicking the plain slot swaps the
	// two rather than pouring one into the other, the way the vanilla
	// click's own branch runs on isSameItemSameComponents.
	w.Click(37, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{37: {}}, enchanted)
	w.Click(36, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{36: enchanted}, plain)
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Equal(enchanted) {
		t.Errorf("the held slot = %+v, want the enchanted sword it was swapped to", got)
	}
}

// TestGiveWithComponents checks /give's component syntax: the vanilla forms
// of the components this server models give stacks that carry them, the
// feedback names what was given, and a component this server does not model
// gives nothing at all.
func TestGiveWithComponents(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	_, connect := servertest.Start(t, cfg, servertest.WriteOpsFile(t,
		servertest.OpRecord{UUID: protocol.OfflineUUID("Smith").String(), Name: "Smith", Level: 2}))
	c := connect()
	c.Login("Smith")
	c.Configure()
	c.Inventory()
	c.EnterWorld("Smith")

	sword, ok := registry.ItemID("minecraft:diamond_sword")
	if !ok {
		t.Fatal("the registry has no diamond_sword")
	}
	sharpness := servertest.SharpnessID(t)

	// The vanilla syntax: item, then the components in brackets.
	c.ChatCommand("give @s diamond_sword[enchantments={sharpness:5}]")
	updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
	enchanted := protocol.ItemStack{ItemID: sword, Count: 1,
		Enchantments: map[int32]int32{sharpness: 5}}
	if updates[0].Slot != 36 || !updates[0].Stack.Equal(enchanted) {
		t.Errorf("give slot update = %+v, want the enchanted sword", updates[0])
	}
	// The feedback is the vanilla one: the count, the display name in the
	// colour its bumped rarity draws it in, with the show-item hover
	// carrying the components, then who it went to.
	one := int64(1)
	servertest.ExpectFeedbackSoon(t, c, chats, "commands.give.success.single",
		protocol.Text{Number: &one},
		protocol.Text{
			Translate: "chat.square_brackets",
			Color:     "aqua",
			With:      []protocol.Text{{Extra: []protocol.Text{{Translate: "item.minecraft.diamond_sword"}}}},
			HoverItem: &protocol.HoverItem{ID: "minecraft:diamond_sword", Count: 1,
				Components: nbt.Compound{"minecraft:enchantments": nbt.Compound{
					"minecraft:sharpness": nbt.Int(5),
				}}},
		},
		servertest.PlayerDisplayComponent("Smith"))

	// A name, and the rest of the components in vanilla's syntax.
	c.ChatCommand(`give @s diamond[custom_name="Bob",repair_cost=7,enchantment_glint_override=false]`)
	updates, _ = servertest.AwaitSlotUpdates(t, c, 1)
	diamond, ok := registry.ItemID("minecraft:diamond")
	if !ok {
		t.Fatal("the registry has no diamond")
	}
	bob := protocol.Text{Text: "Bob"}
	named := protocol.ItemStack{ItemID: diamond, Count: 1,
		CustomName: &bob, RepairCost: 7, GlintOverride: protocol.GlintHidden}
	if updates[0].Slot != 37 || !updates[0].Stack.Equal(named) {
		t.Errorf("give slot update = %+v, want the named diamond", updates[0])
	}

	// Every field of potion_contents is optional: an empty one is a real
	// patch on a diamond, and a component the codec has no word for gives
	// nothing.
	c.ChatCommand("give @s diamond[potion_contents={}]")
	updates, _ = servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Slot != 38 || len(updates[0].Stack.Raw) != 1 {
		t.Errorf("give slot update = %+v, want a diamond with the potion_contents patch", updates[0])
	}
	// On a potion the empty component is its own default, so no patch.
	c.ChatCommand("give @s potion[potion_contents={}]")
	updates, _ = servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Slot != 39 || len(updates[0].Stack.Raw) != 0 {
		t.Errorf("give slot update = %+v, want a plain potion", updates[0])
	}
	// Keys the codec has no field for are skipped, and a number of any tag
	// type reads as the colour.
	c.ChatCommand("give @s diamond[potion_contents={bogus:1}]")
	updates, _ = servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Slot != 38 || updates[0].Stack.Count != 2 {
		t.Errorf("give slot update = %+v, want the bogus key skipped, so stacked on the patched diamond", updates[0])
	}
	c.ChatCommand("give @s potion[potion_contents={custom_color:5b,x:1b}]")
	updates, _ = servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Slot != 40 || len(updates[0].Stack.Raw) != 1 {
		t.Errorf("give slot update = %+v, want a potion with the colour", updates[0])
	}
	c.Disconnect()
}

// TestEnchantedItemSurvivesAStorage checks the chest's part of the stack
// codec: an enchanted sword clicked into a chest comes back out of the
// chunk's block entity with its components, after the chunk has been saved
// and loaded again.
func TestEnchantedItemSurvivesAStorage(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	sword, ok := registry.ItemID("minecraft:diamond_sword")
	if !ok {
		t.Fatal("the registry has no diamond_sword")
	}
	sharpness := servertest.SharpnessID(t)
	enchanted := protocol.ItemStack{ItemID: sword, Count: 1,
		Enchantments: map[int32]int32{sharpness: 5}}

	srv, w := servertest.OpenWindowed(t, cfg)
	chest, ok := registry.ItemID("minecraft:chest")
	if !ok {
		t.Fatal("the registry has no chest")
	}
	w.SetCreativeSlot(36, protocol.ItemStack{ItemID: chest, Count: 1})
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)

	w.SetCreativeSlot(36, enchanted)
	w.Click(54, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{54: {}}, enchanted)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: enchanted}, protocol.ItemStack{})
	w.CloseWindow()
	w.ExpectWindowZero()

	// The chunk is saved with the chest's block entity whole, and a
	// restarted server reads the stack back with its components.
	srv.FlushSaves()
	srv.Shutdown()
	restarted := *cfg
	srv2, connect := servertest.Start(t, &restarted)
	c := connect()
	c.Login("Testificate")
	c.Configure()
	c.Inventory()
	c.EnterWorld("Testificate")

	got := srv2.InventoryAt(4, 64, 0)
	if len(got) == 0 || !got[0].Equal(enchanted) {
		t.Errorf("the reloaded chest = %v, want the enchanted sword", got)
	}
	c.Disconnect()
}

// TestCreativeRefusesAStackItCannotDecode checks the line the raw
// components' checks draw against the client: a creative slot holding a
// book whose page is a component the game's own codec refuses is refused
// whole, the slot corrected to what the server has — the stack is relayed,
// and the pages would take the reading client down with it.
func TestCreativeRefusesAStackItCannotDecode(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.GameMode = "creative" // items are handed out through the creative inventory
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	book, ok := registry.ItemID("minecraft:written_book")
	if !ok {
		t.Fatal("the item registry has no written_book")
	}

	_, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login("Testificate")
	c.Configure()
	c.Inventory()
	c.EnterWorld("Testificate")

	// A written book whose one page is an empty compound: the value the
	// game's own component codec refuses, the reading client with it.
	c.Send(protocol.IDPlayCreativeSlot, func(b *protocol.Buffer) {
		b.PutI16(36)
		b.PutVarInt(1)    // count
		b.PutVarInt(book) // minecraft:written_book
		b.PutVarInt(1)    // components to add
		b.PutVarInt(0)    // none to remove
		b.PutVarInt(protocol.DataComponentWrittenBookContent)
		var value protocol.Buffer
		value.PutString("T") // the title
		value.PutBool(false) // no filtered twin
		value.PutString("A") // the author
		value.PutVarInt(0)   // the generation
		value.PutVarInt(1)   // one page
		value.PutU8(0x0a)    // a compound tag
		value.PutI16(0)      // named nothing
		value.PutU8(0)       // holding nothing: the page the codec refuses
		value.PutBool(false) // no filtered twin
		value.PutBool(false) // unresolved
		b.PutVarInt(int32(value.Len()))
		b.PutRaw(value.Bytes())
	})

	// The slot is put back to what the server has, rather than left as the
	// client drew it.
	r := protocol.NewReader(c.ExpectPlay(protocol.IDPlayContainerSlot))
	r.ReadVarInt() // window ID
	r.ReadVarInt() // state ID
	if got := r.ReadI16(); got != 36 {
		t.Errorf("corrected slot = %d, want 36", got)
	}
	got, ok := r.ReadSlot()
	if !ok {
		t.Fatal("the correction did not read back")
	}
	if !got.Empty() {
		t.Errorf("slot 36 = %+v, want nothing", got)
	}

	c.Settle()
	c.Disconnect()

	// And nothing reached the file: the slot was never accepted.
	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Testificate"))
	if items, ok := nbt.Get[nbt.List](root, "Inventory"); ok && len(items.Items) != 0 {
		t.Errorf("Inventory holds %d stacks, want none", len(items.Items))
	}
}

// TestCreativeKeepsAnItemModel checks that a creative stack carrying
// minecraft:item_model, a component the client's codec reads and this server
// holds as its identifier, is accepted whole.
func TestCreativeKeepsAnItemModel(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	c := connect()
	c.Login("Modeller")
	c.Configure()
	c.Inventory()
	c.EnterWorld("Modeller")

	stick, ok := registry.ItemID("minecraft:stick")
	if !ok {
		t.Fatal("the registry has no stick")
	}
	stack := protocol.ItemStack{ItemID: stick, Count: 1}
	stack.SetItemModel("minecraft:diamond_sword")
	c.SetCreativeSlot(36, stack)
	servertest.WaitFor(t, srv, 100, func() bool {
		return srv.InventorySlot("Modeller", player.HotbarSlot).ItemID == stick
	})
	got := srv.InventorySlot("Modeller", player.HotbarSlot)
	if model, ok := got.ItemModelOf(); !ok || model != "minecraft:diamond_sword" {
		t.Errorf("the held item's model = %q, %v, want minecraft:diamond_sword", model, ok)
	}
	if !got.Same(stack) {
		t.Errorf("the held item = %+v, want the stack the client set %+v", got, stack)
	}
	c.Disconnect()
}
