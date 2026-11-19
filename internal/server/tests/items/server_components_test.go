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
