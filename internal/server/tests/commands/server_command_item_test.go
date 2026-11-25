package commands

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"strings"
	"testing"

	"strconv"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The translation keys /item answers with.
const (
	itemBlockSetKey            = "commands.item.block.replace.success.known_item"
	itemEntitySetSingleKey     = "commands.item.entity.replace.success.single.known_item"
	itemEntitySetMultipleKey   = "commands.item.entity.replace.success.multiple.known_item"
	itemBlockReplaceKey        = "commands.item.block.replace.success"
	itemBlockModifyKey         = "commands.item.block.modify.success"
	itemEntitySingleReplaceKey = "commands.item.entity.replace.success.single"
	itemEntitySingleModifyKey  = "commands.item.entity.modify.success.single"
	itemTargetNoContainerKey   = "commands.item.target.not_a_container"
	itemSourceNoContainerKey   = "commands.item.source.not_a_container"
	itemTargetNoSlotKey        = "commands.item.target.no_such_slot"
	itemSourceNoSlotKey        = "commands.item.source.no_such_slot"
	itemNoChangedKnownItem     = "commands.item.target.failed.known_item"
	itemOverstackedKey         = "arguments.item.overstacked"
	slotUnknownKey             = "slot.unknown"
	itemPosUnloadedKey         = "argument.pos.unloaded"
	itemPosOutOfWorldKey       = "argument.pos.outofworld"
)

// itemFailureText builds the expected System Chat payload of a bare red
// failure carrying arguments, which is the shape the /item run failures
// arrive in.
func itemFailureText(key string, with ...protocol.Text) []byte {
	var b protocol.Buffer
	inner := protocol.Text{Translate: key, With: with}
	b.PutText(protocol.Text{Color: protocol.ColorRed, Extra: []protocol.Text{inner}})
	b.PutBool(false)
	return b.Bytes()
}

// awaitItemFailure reads packets until the bare red failure the key and its
// arguments make arrives, stepping over the slot updates and success lines
// on the way.
func awaitItemFailure(t *testing.T, c *servertest.Client, key string, with ...protocol.Text) {
	t.Helper()
	want := itemFailureText(key, with...)
	for packetID, data := range c.Stream() {
		if packetID == protocol.IDPlaySystemChat && bytes.Equal(data, want) {
			return
		}
	}
	t.Fatalf("no bare red failure for %s arrived", key)
}

// dialItem is a creative-mode operator's connection under that config.
func dialItem(t *testing.T, name string) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.ItemCreativeConfig(), name, servertest.OpsListing(t, name))
	return srv, c
}

// TestItemReplaceEntitySetsSlots checks /item replace entity with: every
// slot word the player's own things answer puts the stack and its count in
// the window slot the word addresses, and the feedback names the target and
// the item, with the hover previewing the count the stack carried.
func TestItemReplaceEntitySetsSlots(t *testing.T) {
	t.Parallel()
	_, c := dialItem(t, "Testificate")

	for _, tc := range []struct {
		name  string
		word  string // the slot word
		count int32  // the count given
		slot  int16  // the window slot the stack lands in
		item  string // the item given, by its registry name
	}{
		{"main hand follows the selection", "weapon.mainhand", 3, 36, "minecraft:diamond"},
		{"the bare weapon is the main hand", "weapon", 2, 36, "minecraft:diamond"},
		{"off hand", "weapon.offhand", 1, 45, "minecraft:torch"},
		{"armor head", "armor.head", 1, 5, "minecraft:diamond_helmet"},
		{"armor feet", "armor.feet", 1, 8, "minecraft:diamond_boots"},
		{"hotbar", "hotbar.2", 7, 38, "minecraft:stone"},
		{"main inventory", "inventory.3", 4, 12, "minecraft:dirt"},
		{"a machine slot past the hotbar is the main inventory", "container.13", 5, 13, "minecraft:apple"},
		{"a machine slot inside the hotbar", "container.0", 6, 36, "minecraft:gold_ingot"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, tc.item), Count: tc.count}
			c.ChatCommand("item replace entity @s " + tc.word + " with " + tc.item + " " + strconv.Itoa(int(tc.count)))
			updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
			if updates[0].Slot != tc.slot || updates[0].Stack.ItemID != want.ItemID || updates[0].Stack.Count != want.Count {
				t.Errorf("item slot update = slot %d holding %+v, want slot %d holding %+v",
					updates[0].Slot, updates[0].Stack, tc.slot, want)
			}
			servertest.ExpectFeedbackSoon(t, c, chats, itemEntitySetSingleKey, servertest.ItemNumber(1),
				servertest.PlayerDisplayComponent("Testificate"),
				servertest.ItemFeedbackText(tc.item, servertest.ItemDisplayKey(t, tc.item), tc.count))
		})
	}
	c.Disconnect()
}

// TestItemEntityRefusals checks the refusals the entity form answers with:
// the slot words the grammar knows but the player keeps no slot for, and a
// count past what the item stacks up to.
func TestItemEntityRefusals(t *testing.T) {
	t.Parallel()
	_, c := dialItem(t, "Testificate")

	// An item the body slot does not take fails the set; a slot the player
	// lacks (the mob inventory band, machine slots past the player's own
	// containers) is refused by its word.
	c.ChatCommand("item replace entity @s armor.body with minecraft:dirt 1")
	awaitItemFailure(t, c, itemNoChangedKnownItem,
		protocol.Text{Text: "1 minecraft:dirt"})
	c.ChatCommand("item replace entity @s mob.inventory.0 with minecraft:dirt 1")
	awaitItemFailure(t, c, itemTargetNoSlotKey, protocol.Text{Text: "mob.inventory.0"})
	c.ChatCommand("item replace entity @s container.36 with minecraft:dirt 1")
	awaitItemFailure(t, c, itemTargetNoSlotKey, protocol.Text{Text: "container.36"})

	// A count of more than an unstackable item stacks up to: the vanilla
	// item argument refuses the stack before anything is written.
	c.ChatCommand("item replace entity @s weapon.mainhand with minecraft:diamond_sword 5")
	awaitItemFailure(t, c, itemOverstackedKey,
		protocol.Text{Text: "minecraft:diamond_sword"},
		servertest.ItemNumber(1))
	c.Disconnect()
}

// TestItemReplaceBlock checks /item replace block with and from over a
// container block: the stack lands in the slot the window and the block
// entity number the same, the feedback names the place and the item, a
// block that keeps no container and a slot the container does not have are
// refused, and the from form copies a slot - an empty one as air.
func TestItemReplaceBlock(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.ItemCreativeConfig(), servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	c.SpawnedAt()
	c.SkipJoin()

	c.ChatCommand("setblock 2 64 3 minecraft:barrel")
	barrel, ok := registry.FindState("minecraft:barrel", nil)
	if !ok {
		t.Fatal("the registry has no barrel")
	}
	servertest.AwaitBlockUpdate(t, c, 2, 64, 3, barrel)
	// The setblock's own feedback is drained before the /item forms below,
	// so their answers are the next thing the client reads.
	servertest.DrainBlockUpdates(t, c, 8)

	// The with form: the stack, its count carried, into the slot named.
	c.ChatCommand("item replace block 2 64 3 container.13 with minecraft:stone 4")
	c.ExpectCommandFeedbackText(itemBlockSetKey, servertest.ItemNumber(1),
		servertest.ItemNumber(2), servertest.ItemNumber(64), servertest.ItemNumber(3),
		servertest.ItemFeedbackText("minecraft:stone", servertest.ItemDisplayKey(t, "minecraft:stone"), 4))
	if got := srv.InventoryAt(2, 64, 3)[13]; got.ItemID != servertest.MustItem("minecraft:stone") || got.Count != 4 {
		t.Errorf("barrel slot 13 = %+v, want four stone", got)
	}

	// A slot the container does not have is refused by its number, and the
	// barrel is left alone.
	c.ChatCommand("item replace block 2 64 3 container.27 with minecraft:dirt 1")
	awaitItemFailure(t, c, itemTargetNoSlotKey, protocol.Text{Text: "container.27"})
	if got := srv.InventoryAt(2, 64, 3)[26]; !got.Empty() {
		t.Errorf("barrel slot 26 = %+v, want it untouched by the refused set", got)
	}

	// The other block refusal: a place that keeps no container at all.
	c.ChatCommand("item replace block 3 64 4 container.0 with minecraft:dirt 1")
	awaitItemFailure(t, c, itemTargetNoContainerKey,
		servertest.ItemNumber(3), servertest.ItemNumber(64), servertest.ItemNumber(4))

	// The from form copies a slot whole, an empty one as an empty stack.
	c.ChatCommand("item replace block 2 64 3 container.1 from block 2 64 3 container.13")
	c.ExpectCommandFeedbackText(itemBlockReplaceKey, servertest.ItemNumber(1),
		servertest.ItemNumber(2), servertest.ItemNumber(64), servertest.ItemNumber(3))
	if got := srv.InventoryAt(2, 64, 3)[1]; got.ItemID != servertest.MustItem("minecraft:stone") || got.Count != 4 {
		t.Errorf("barrel slot 1 = %+v, want the four stone copied into it", got)
	}
	c.ChatCommand("item replace block 2 64 3 container.2 from block 2 64 3 container.0")
	c.ExpectCommandFeedbackText(itemBlockReplaceKey, servertest.ItemNumber(1),
		servertest.ItemNumber(2), servertest.ItemNumber(64), servertest.ItemNumber(3))
	if got := srv.InventoryAt(2, 64, 3)[2]; !got.Empty() {
		t.Errorf("barrel slot 2 = %+v, want the empty source slot copied as nothing", got)
	}

	// The from form's source refusal: a slot beyond the source's own.
	c.ChatCommand("item replace block 2 64 3 container.3 from block 2 64 3 container.30")
	awaitItemFailure(t, c, itemSourceNoSlotKey, protocol.Text{Text: "container.30"})
	c.Disconnect()
}

// TestItemFromEntity checks the from form's entity source: a stack read out
// of one player's slot lands in another's, and a slot no player keeps
// answers with the source's own refusal.
func TestItemFromEntity(t *testing.T) {
	t.Parallel()
	_, connect := servertest.Start(t, servertest.ItemCreativeConfig(), servertest.OpsListing(t, "Alice", "Bob"))

	alice := connect().Join("Alice")
	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	// Bob's main hand holds emeralds Alice's hotbar copies.
	bob.ChatCommand("item replace entity Bob weapon.mainhand with minecraft:emerald 3")
	updates, _ := servertest.AwaitSlotUpdates(t, bob, 1)
	if updates[0].Slot != 36 || updates[0].Stack.ItemID != servertest.MustItem("minecraft:emerald") {
		t.Fatalf("Bob's main hand = %+v, want emeralds", updates[0])
	}
	bob.Settle()

	alice.ChatCommand("item replace entity Alice hotbar.4 from entity Bob weapon.mainhand")
	updates, chats := servertest.AwaitSlotUpdates(t, alice, 1)
	if updates[0].Slot != 40 || updates[0].Stack.ItemID != servertest.MustItem("minecraft:emerald") || updates[0].Stack.Count != 3 {
		t.Errorf("Alice's copied stack = %+v, want the three emeralds out of Bob's main hand", updates[0])
	}
	servertest.ExpectFeedbackSoon(t, alice, chats, itemEntitySingleReplaceKey, servertest.ItemNumber(1),
		servertest.PlayerDisplayComponent("Alice"))

	// A source slot the player keeps none of answers with the source's
	// refusal, naming the raw id the word parsed to.
	alice.ChatCommand("item replace entity Alice hotbar.4 from entity Bob mob.inventory.0")
	awaitItemFailure(t, alice, itemSourceNoSlotKey, protocol.Text{Text: "mob.inventory.0"})

	// The ender chest and the cursor are the player's own things: writing
	// one ender slot and reading it back into the hotbar, and the same for
	// the cursor.
	alice.ChatCommand("item replace entity Alice enderchest.5 with minecraft:emerald 6")
	alice.ExpectCommandFeedbackText(itemEntitySetSingleKey, servertest.ItemNumber(1),
		servertest.PlayerDisplayComponent("Alice"),
		servertest.ItemFeedbackText("minecraft:emerald", servertest.ItemDisplayKey(t, "minecraft:emerald"), 6))
	alice.ChatCommand("item replace entity Alice hotbar.6 from entity Alice enderchest.5")
	updates, chats = servertest.AwaitSlotUpdates(t, alice, 1)
	if updates[0].Slot != 42 || updates[0].Stack.ItemID != servertest.MustItem("minecraft:emerald") || updates[0].Stack.Count != 6 {
		t.Errorf("Alice's ender copy = %+v, want the six emeralds out of her ender chest", updates[0])
	}
	// The read names the emeralds it copied, which is the same message the
	// ender write itself answered with.
	servertest.ExpectFeedbackSoon(t, alice, chats, itemEntitySingleReplaceKey, servertest.ItemNumber(1),
		servertest.PlayerDisplayComponent("Alice"))
	alice.ChatCommand("item replace entity Alice player.cursor with minecraft:apple 2")
	servertest.AwaitCommandFeedbackText(t, alice, itemEntitySetSingleKey, servertest.ItemNumber(1),
		servertest.PlayerDisplayComponent("Alice"),
		servertest.ItemFeedbackText("minecraft:apple", servertest.ItemDisplayKey(t, "minecraft:apple"), 2))
	alice.ChatCommand("item replace entity Alice hotbar.7 from entity Alice player.cursor")
	updates, _ = servertest.AwaitSlotUpdates(t, alice, 1)
	if updates[0].Slot != 43 || updates[0].Stack.ItemID != servertest.MustItem("minecraft:apple") || updates[0].Stack.Count != 2 {
		t.Errorf("Alice's cursor copy = %+v, want the two apples her cursor held", updates[0])
	}
	alice.Disconnect()
	bob.Disconnect()
}

// TestItemReplaceMobArmour checks a mob's and an armor stand's armour slots:
// the head takes a helmet and its viewers are sent it, and the chest takes
// only a piece worn there. vanilla: LivingEntity.createEquipmentSlotAccess
func TestItemReplaceMobArmour(t *testing.T) {
	t.Parallel()
	srv, c := dialItem(t, "Testificate")

	zombieID := srv.SpawnMob("minecraft:zombie", 4.5, 64, 4.5)
	zombieText, ok := srv.MobUUID(zombieID)
	if !ok {
		t.Fatal("spawning the zombie failed")
	}
	zombieUUID, err := uuid.Parse(zombieText)
	if err != nil {
		t.Fatalf("the zombie's UUID %q does not parse: %v", zombieText, err)
	}

	helmet := servertest.ItemIDOrDie(t, "minecraft:diamond_helmet")
	c.ChatCommand("item replace entity " + zombieText + " armor.head with minecraft:diamond_helmet")
	entries := c.ExpectEquipment(zombieID)
	if got := servertest.EquipEntry(entries, protocol.EquipHead); got.ItemID != helmet {
		t.Errorf("the zombie's head = %+v, want a diamond helmet", got)
	}
	c.ExpectCommandFeedbackText(itemEntitySetSingleKey, servertest.ItemNumber(1),
		servertest.EntityDisplayComponent("minecraft:zombie", "entity.minecraft.zombie", zombieUUID),
		servertest.ItemFeedbackText("minecraft:diamond_helmet", servertest.ItemDisplayKey(t, "minecraft:diamond_helmet"), 1))

	c.ChatCommand("item replace entity " + zombieText + " armor.chest with minecraft:stone")
	awaitItemFailure(t, c, itemNoChangedKnownItem,
		protocol.Text{Text: "1 minecraft:stone"})

	chestplate := servertest.ItemIDOrDie(t, "minecraft:iron_chestplate")
	c.ChatCommand("item replace entity " + zombieText + " armor.chest with minecraft:iron_chestplate")
	entries = c.ExpectEquipment(zombieID)
	if got := servertest.EquipEntry(entries, protocol.EquipChest); got.ItemID != chestplate {
		t.Errorf("the zombie's chest = %+v, want an iron chestplate", got)
	}

	standID := srv.SpawnArmorStand(6.5, 64, 4.5)
	const stand = "@e[type=minecraft:armor_stand,limit=1]"
	c.ChatCommand("item replace entity " + stand + " armor.chest with minecraft:stone")
	awaitItemFailure(t, c, itemNoChangedKnownItem,
		protocol.Text{Text: "1 minecraft:stone"})
	c.ChatCommand("item replace entity " + stand + " armor.chest with minecraft:iron_chestplate")
	entries = c.ExpectEquipment(standID)
	if got := servertest.EquipEntry(entries, protocol.EquipChest); got.ItemID != chestplate {
		t.Errorf("the armor stand's chest = %+v, want an iron chestplate", got)
	}
	if got := srv.ArmorStandStack(standID, protocol.EquipChest); got.ItemID != chestplate {
		t.Errorf("the armor stand holds %+v in its chest slot, want the iron chestplate", got)
	}
	c.Disconnect()
}

// TestItemSlotGrammar checks the refusals the walk answers the slot word,
// the count and the item modifier with while the command is still being read.
// The slot and modifier refusals point where their word ends; the integer
// argument's where its number begins.
func TestItemSlotGrammar(t *testing.T) {
	t.Parallel()
	_, c := dialItem(t, "Testificate")

	for _, tc := range []struct {
		name    string
		command string
		word    string // what the walk refused, or what it stopped before
		key     string
		with    []string
		wordEnd bool // the cursor points at the word's end, not its start
	}{
		{"a word outside the slot ranges keeps its identifier prefix",
			"item replace entity @s nonsense.* with minecraft:stone",
			"nonsense.",
			"argument.resource_or_id.no_such_element",
			[]string{"minecraft:nonsense.", "minecraft:slot_source"},
			true},
		{"an unknown identifier in slot source position",
			"item replace entity @s nonsense.3 with minecraft:stone",
			"nonsense.3",
			"argument.resource_or_id.no_such_element",
			[]string{"minecraft:nonsense.3", "minecraft:slot_source"},
			true},
		{"a hotbar slot number past the hotbar is parsed as a slot source id",
			"item replace entity @s hotbar.9 with minecraft:stone",
			"hotbar.9",
			"argument.resource_or_id.no_such_element",
			[]string{"minecraft:hotbar.9", "minecraft:slot_source"},
			true},
		{"a container slot number past the largest container is parsed as a slot source id",
			"item replace entity @s container.54 with minecraft:stone",
			"container.54",
			"argument.resource_or_id.no_such_element",
			[]string{"minecraft:container.54", "minecraft:slot_source"},
			true},
		{"a crafting grid slot past the 2x2 is parsed as a slot source id",
			"item replace entity @s player.crafting.4 with minecraft:stone",
			"player.crafting.4",
			"argument.resource_or_id.no_such_element",
			[]string{"minecraft:player.crafting.4", "minecraft:slot_source"},
			true},
		{"an item modifier, of which this server keeps none",
			"item modify entity @s weapon.mainhand minecraft:foo",
			"minecraft:foo",
			"argument.resource_or_id.no_such_element",
			[]string{"minecraft:foo", "minecraft:item_modifier"},
			true},
		{"a bare item modifier name is the minecraft namespace's",
			"item modify block 2 64 3 container.0 set_count",
			"set_count",
			"argument.resource_or_id.no_such_element",
			[]string{"minecraft:set_count", "minecraft:item_modifier"},
			true},
		{"a count over the ninety-nine cap",
			"item replace entity @s weapon.mainhand with minecraft:stone 100",
			"100",
			"argument.integer.big",
			[]string{"99", "100"},
			false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cursor := strings.Index(tc.command, tc.word)
			if tc.wordEnd {
				cursor += len(tc.word)
			}
			c.ChatCommand(tc.command)
			c.ExpectCommandError(
				servertest.CommandErrorMessageForTest(tc.key, tc.with...),
				servertest.CommandErrorContextForTest(tc.command, cursor))
		})
	}
	c.Disconnect()
}

// TestItemCopiesBetweenEntityAndBlock checks both directions of a copy
// across the two source kinds: a stack read out of a player's hand lands in
// a barrel, and one read out of the barrel lands in the hand.
func TestItemCopiesBetweenEntityAndBlock(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.ItemCreativeConfig(), servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	c.SpawnedAt()
	c.SkipJoin()

	c.ChatCommand("setblock 2 64 3 minecraft:barrel")
	barrel, ok := registry.FindState("minecraft:barrel", nil)
	if !ok {
		t.Fatal("the registry has no barrel")
	}
	servertest.AwaitBlockUpdate(t, c, 2, 64, 3, barrel)
	servertest.DrainBlockUpdates(t, c, 8)

	c.ChatCommand("item replace entity @s hotbar.0 with minecraft:emerald 3")
	updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Slot != 36 || updates[0].Stack.ItemID != servertest.MustItem("minecraft:emerald") {
		t.Fatalf("the hand's emeralds = %+v, want them in window slot 36", updates[0])
	}
	servertest.ExpectFeedbackSoon(t, c, chats, itemEntitySetSingleKey, servertest.ItemNumber(1),
		servertest.PlayerDisplayComponent("Testificate"),
		servertest.ItemFeedbackText("minecraft:emerald", servertest.ItemDisplayKey(t, "minecraft:emerald"), 3))

	c.ChatCommand("item replace block 2 64 3 container.5 from entity @s hotbar.0")
	c.ExpectCommandFeedbackText(itemBlockReplaceKey, servertest.ItemNumber(1),
		servertest.ItemNumber(2), servertest.ItemNumber(64), servertest.ItemNumber(3))
	if got := srv.InventoryAt(2, 64, 3)[5]; got.ItemID != servertest.MustItem("minecraft:emerald") || got.Count != 3 {
		t.Errorf("barrel slot 5 = %+v, want the three emeralds copied out of the hand", got)
	}

	c.ChatCommand("item replace entity @s hotbar.1 from block 2 64 3 container.5")
	updates, chats = servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Slot != 37 || updates[0].Stack.ItemID != servertest.MustItem("minecraft:emerald") || updates[0].Stack.Count != 3 {
		t.Errorf("hotbar slot 1 = %+v, want the three emeralds copied out of the barrel", updates[0])
	}
	servertest.ExpectFeedbackSoon(t, c, chats, itemEntitySingleReplaceKey, servertest.ItemNumber(1),
		servertest.PlayerDisplayComponent("Testificate"))
	c.Disconnect()
}

// TestItemRefusedWithoutPermission checks that a player who holds no
// permission level is offered no /item node at all, so the command reads as
// unknown rather than running.
func TestItemRefusedWithoutPermission(t *testing.T) {
	t.Parallel()
	const command = "item replace entity @s weapon.mainhand with minecraft:stone"
	_, c := servertest.Join(t, servertest.ItemCreativeConfig(), "Testificate")

	c.ChatCommand(command)
	c.ExpectCommandError(
		servertest.CommandErrorMessageForTest("command.unknown.command"),
		servertest.CommandErrorContextForTest(command, 0))
	c.Disconnect()
}

// TestItemFillCyclesAndOverrideEmpties checks the 26.3 set commands over the
// nine hotbar slots: fill puts the item in every one, replace only in the
// first, override in the first and empties the rest.
// vanilla: ItemCommands.fillItemProvider, overrideItemProvider
func TestItemFillCyclesAndOverrideEmpties(t *testing.T) {
	t.Parallel()
	_, c := dialItem(t, "Testificate")
	stone := servertest.ItemIDOrDie(t, "minecraft:stone")
	dirt := servertest.ItemIDOrDie(t, "minecraft:dirt")

	c.ChatCommand("item fill entity @s hotbar.* with minecraft:stone")
	updates, chats := servertest.AwaitSlotUpdates(t, c, 9)
	for i, u := range updates {
		if u.Slot != int16(36+i) || u.Stack.ItemID != stone || u.Stack.Count != 1 {
			t.Errorf("fill update %d = slot %d holding %+v, want slot %d holding stone", i, u.Slot, u.Stack, 36+i)
		}
	}
	servertest.ExpectFeedbackSoon(t, c, chats, itemEntitySetSingleKey, servertest.ItemNumber(9),
		servertest.PlayerDisplayComponent("Testificate"),
		servertest.ItemFeedbackText("minecraft:stone", servertest.ItemDisplayKey(t, "minecraft:stone"), 1))

	c.ChatCommand("item override entity @s hotbar.* with minecraft:dirt")
	updates, chats = servertest.AwaitSlotUpdates(t, c, 9)
	for i, u := range updates {
		wantItem := int32(0)
		if i == 0 {
			wantItem = dirt
		}
		if u.Slot != int16(36+i) || u.Stack.ItemID != wantItem {
			t.Errorf("override update %d = slot %d holding %+v, want slot %d holding item %d", i, u.Slot, u.Stack, 36+i, wantItem)
		}
	}
	servertest.ExpectFeedbackSoon(t, c, chats, itemEntitySetSingleKey, servertest.ItemNumber(9),
		servertest.PlayerDisplayComponent("Testificate"),
		servertest.ItemFeedbackText("minecraft:dirt", servertest.ItemDisplayKey(t, "minecraft:dirt"), 1))

	c.ChatCommand("item replace entity @s hotbar.* with minecraft:stone")
	updates, chats = servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Slot != 36 || updates[0].Stack.ItemID != stone {
		t.Errorf("replace update = slot %d holding %+v, want slot 36 holding stone", updates[0].Slot, updates[0].Stack)
	}
	servertest.ExpectFeedbackSoon(t, c, chats, itemEntitySetSingleKey, servertest.ItemNumber(1),
		servertest.PlayerDisplayComponent("Testificate"),
		servertest.ItemFeedbackText("minecraft:stone", servertest.ItemDisplayKey(t, "minecraft:stone"), 1))
	c.Disconnect()
}

// TestItemCommandTree checks the /item tree is the 26.3 report's: replace,
// fill and override share one shape, modify takes the slots and a modifier.
func TestItemCommandTree(t *testing.T) {
	t.Parallel()
	_, c := servertest.Dial(t, servertest.SurvivalTestConfig(), servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()

	nodes := servertest.DecodeCommands(t, c.AwaitPlay(protocol.IDPlayCommands))
	var shape func(servertest.TestCommandNode) string
	shape = func(n servertest.TestCommandNode) string {
		var parts []string
		for _, i := range n.Children {
			parts = append(parts, shape(nodes[i]))
		}
		if len(parts) == 0 {
			return n.Name
		}
		return n.Name + "(" + strings.Join(parts, " ") + ")"
	}
	var item servertest.TestCommandNode
	for _, i := range nodes[0].Children {
		if nodes[i].Name == "item" {
			item = nodes[i]
		}
	}
	set := func(name string) string {
		return name + "(entity(target(slots(from(entity(source(sourceSlots(modifier))) block(source(sourceSlots(modifier))))" +
			" with(item(count))))) block(target(slots(from(entity(source(sourceSlots(modifier))) block(source(sourceSlots(modifier))))" +
			" with(item(count))))))"
	}
	want := "item(" + set("replace") + " " + set("fill") + " " + set("override") +
		" modify(entity(target(slots(modifier))) block(target(slots(modifier)))))"
	if got := shape(item); got != want {
		t.Errorf("item tree = %s\nwant %s", got, want)
	}
	c.SkipJoin()
	c.Disconnect()
}
