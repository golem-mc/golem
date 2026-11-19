package commands

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"strings"
	"testing"

	"strconv"

	"io"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/console"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
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

// vanilla: AbstractChestBoat.getSlot, ContainerEntity.getChestVehicleSlot
func TestItemChestBoatCargoSlots(t *testing.T) {
	t.Parallel()
	srv, c := dialItem(t, "Testificate")
	servertest.HoldTicks(t, srv)
	for _, tc := range []struct {
		kind string
		id   int32
	}{
		{"minecraft:oak_chest_boat", protocol.EntityTypeOakChestBoat},
		{"minecraft:bamboo_chest_raft", protocol.EntityTypeBambooChestRaft},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			c.ChatCommand("summon " + tc.kind + ` 2.5 64 2.5 {NoGravity:1b}`)
			boat := c.AwaitSpawn(tc.id)
			c.Settle()
			target := boat.Uuid.String()
			for _, slot := range []int{0, 26} {
				c.ChatCommand("item replace entity " + target + " container." + strconv.Itoa(slot) + " with minecraft:cobblestone 5")
				c.ExpectCommandFeedbackText(itemEntitySetSingleKey, servertest.ItemNumber(1),
					servertest.EntityDisplayComponent(tc.kind, "entity."+strings.ReplaceAll(tc.kind, ":", "."), boat.Uuid),
					servertest.ItemFeedbackText("minecraft:cobblestone", "block.minecraft.cobblestone", 5))
				if got := srv.BoatCargoAt(boat.EntityID, slot); got.ItemID != int32(registry.ItemCobblestone) || got.Count != 5 {
					t.Fatalf("cargo slot %d = %+v, want five cobblestone", slot, got)
				}
			}
			c.ChatCommand("item replace entity " + target + " container.27 with minecraft:dirt 1")
			awaitItemFailure(t, c, itemTargetNoSlotKey, protocol.Text{Text: "container.27"})

			c.ChatCommand("item replace entity @s hotbar.0 from entity " + target + " container.26")
			updates, _ := servertest.AwaitSlotUpdates(t, c, 1)
			if got := updates[0]; got.Slot != 36 || got.Stack.ItemID != int32(registry.ItemCobblestone) || got.Stack.Count != 5 {
				t.Fatalf("copied cargo = %+v, want five cobblestone in hotbar.0", got)
			}
			c.Settle()
			w := &servertest.WindowedClient{Client: c, WantBlockEntityType: -1}
			c.UseOnSneaking(boat.EntityID)
			w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 0, 0, 0)
			c.ChatCommand("item replace entity " + target + " container.0 with minecraft:dirt 2")
			w.AwaitWindowSlot(0, servertest.ItemStack(t, "minecraft:dirt", 2))
			c.ChatCommand("item modify entity " + target + ` container.0 {type:"discard"}`)
			w.AwaitWindowSlot(0, protocol.ItemStack{})
			if got := srv.BoatCargoAt(boat.EntityID, 0); !got.Empty() {
				t.Fatalf("discarded cargo = %+v, want empty", got)
			}
			w.CloseWindow()
			c.Settle()
		})
	}
	c.ChatCommand(`summon minecraft:oak_boat 2.5 64 2.5 {NoGravity:1b}`)
	boat := c.AwaitSpawn(protocol.EntityTypeOakBoat)
	c.Settle()
	c.ChatCommand("item replace entity " + boat.Uuid.String() + " container.0 with minecraft:cobblestone 5")
	awaitItemFailure(t, c, itemTargetNoSlotKey, protocol.Text{Text: "container.0"})
}

// vanilla: ContainerEntity.getChestVehicleItem
func TestItemChestBoatUnpacksLoot(t *testing.T) {
	t.Parallel()
	srv, c := dialItem(t, "Testificate")
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon minecraft:oak_chest_boat 2.5 64 2.5 {NoGravity:1b,LootTable:"minecraft:chests/buried_treasure",LootTableSeed:7L}`)
	boat := c.AwaitSpawn(protocol.EntityTypeOakChestBoat)
	c.Settle()
	c.ChatCommand("item replace entity @s hotbar.0 from entity " + boat.Uuid.String() + " container.0")
	servertest.AwaitSlotUpdates(t, c, 1)
	tag, ok := srv.BoatSaveTag(boat.EntityID)
	if !ok || tag["LootTable"] != nil {
		t.Fatalf("cargo read did not unpack the pending loot table: %+v", tag)
	}
	var hearts int32
	for slot := range 27 {
		if stack := srv.BoatCargoAt(boat.EntityID, slot); stack.ItemID == int32(registry.ItemHeartOfTheSea) {
			hearts += stack.Count
		}
	}
	if hearts != 1 {
		t.Fatalf("unpacked cargo holds %d hearts of the sea, want one", hearts)
	}
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

// TestItemReplaceEntityKeepsComponents checks the reason /item was filed:
// the stack it sets carries custom components, the way /give's does.
func TestItemReplaceEntityKeepsComponents(t *testing.T) {
	t.Parallel()
	_, c := dialItem(t, "Testificate")

	c.ChatCommand("item replace entity @s inventory.0 with minecraft:diamond_sword[enchantments={sharpness:5}]")
	updates, _ := servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Slot != 9 || updates[0].Stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:diamond_sword") {
		t.Fatalf("item slot update = %+v, want the diamond sword in the first main inventory slot", updates[0])
	}
	if got := updates[0].Stack.Enchantments[servertest.SharpnessID(t)]; got != 5 {
		t.Errorf("the set sword's sharpness = %d, want the five the stack was given with", got)
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

// vanilla: EntityItemAccessor.RESPONSE_SET_KNOWN_ITEM, ItemStack.toString
func TestItemRejectedStackFeedback(t *testing.T) {
	t.Parallel()
	cfg := servertest.ItemCreativeConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)
	cubeID := srv.SpawnMob("minecraft:sulfur_cube", 4.5, 64, 4.5)
	target, ok := srv.MobUUID(cubeID)
	if !ok {
		t.Fatal("spawning the sulfur cube failed")
	}
	for _, tc := range []struct {
		name, mode, item, argument string
		baby                       bool
	}{
		{"adult stick", "replace", "minecraft:stick", "1 minecraft:stick", false},
		{"count", "fill", "minecraft:stick 5", "5 minecraft:stick", false},
		{"custom name", "override", `minecraft:stick[minecraft:custom_name={text:"Named stick"}] 3`, "3 minecraft:stick", false},
		{"baby horse armor", "replace", "minecraft:iron_horse_armor", "1 minecraft:iron_horse_armor", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv.SetMobBaby(cubeID, tc.baby)
			c.ChatCommand("item " + tc.mode + " entity " + target + " armor.body with " + tc.item)
			want := itemFailureText(itemNoChangedKnownItem, protocol.Text{Text: tc.argument})
			if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, want) {
				t.Errorf("rejected stack feedback = % X, want % X", got, want)
			}
			if got := srv.MobEquipment(cubeID, "body"); !got.Empty() {
				t.Errorf("refused body stack = %+v, want empty", got)
			}
		})
	}
	c.Disconnect()
}

// TestItemPlayerBodyAndSaddle checks that a player keeps armor.body and
// saddle slots: each takes the piece that belongs there, reads back into
// another slot, refuses a stack that does not belong and clears with air.
// vanilla: LivingEntity.getSlot, LivingEntity.createEquipmentSlotAccess
func TestItemPlayerBodyAndSaddle(t *testing.T) {
	t.Parallel()
	_, c := dialItem(t, "Testificate")

	for _, tc := range []struct {
		word, item, wrong string
		raw               int
	}{
		{"armor.body", "minecraft:leather_horse_armor", "minecraft:saddle", 105},
		{"saddle", "minecraft:saddle", "minecraft:leather_horse_armor", 106},
	} {
		want := servertest.ItemIDOrDie(t, tc.item)
		c.ChatCommand("item replace entity @s " + tc.word + " with " + tc.item + " 1")
		c.ExpectCommandFeedbackText(itemEntitySetSingleKey, servertest.ItemNumber(1),
			servertest.PlayerDisplayComponent("Testificate"),
			servertest.ItemFeedbackText(tc.item, servertest.ItemDisplayKey(t, tc.item), 1))

		c.ChatCommand("item replace entity @s hotbar.0 from entity @s " + tc.word)
		updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
		if updates[0].Slot != 36 || updates[0].Stack.ItemID != want {
			t.Errorf("%s read back as %+v, want %s in slot 36", tc.word, updates[0], tc.item)
		}
		servertest.ExpectFeedbackSoon(t, c, chats, itemEntitySingleReplaceKey, servertest.ItemNumber(1),
			servertest.PlayerDisplayComponent("Testificate"))

		c.ChatCommand("item replace entity @s " + tc.word + " with " + tc.wrong + " 1")
		awaitItemFailure(t, c, itemNoChangedKnownItem,
			protocol.Text{Text: "1 " + tc.wrong})

		c.ChatCommand("item replace entity @s " + tc.word + " with minecraft:air")
		c.ExpectCommandFeedbackText(itemEntitySetSingleKey, servertest.ItemNumber(1),
			servertest.PlayerDisplayComponent("Testificate"),
			protocol.Text{
				Translate: "chat.square_brackets",
				With:      []protocol.Text{{Extra: []protocol.Text{{Translate: "block.minecraft.air"}}}},
			})
		c.ChatCommand("item replace entity @s hotbar.1 from entity @s " + tc.word)
		updates, chats = servertest.AwaitSlotUpdates(t, c, 1)
		if updates[0].Slot != 37 || !updates[0].Stack.Empty() {
			t.Errorf("cleared %s read back as %+v, want an empty slot 37", tc.word, updates[0])
		}
		servertest.ExpectFeedbackSoon(t, c, chats, itemEntitySingleReplaceKey, servertest.ItemNumber(1),
			servertest.PlayerDisplayComponent("Testificate"))
	}
	c.Disconnect()
}

// TestClearEmptiesBodyAndSaddle checks that /clear takes the body and saddle
// slots along with the rest of the inventory.
// vanilla: Inventory.clearOrCountMatchingItems
func TestClearEmptiesBodyAndSaddle(t *testing.T) {
	t.Parallel()
	_, c := dialItem(t, "Testificate")
	c.ChatCommand("item replace entity @s armor.body with minecraft:leather_horse_armor 1")
	c.ExpectCommandFeedbackText(itemEntitySetSingleKey, servertest.ItemNumber(1),
		servertest.PlayerDisplayComponent("Testificate"),
		servertest.ItemFeedbackText("minecraft:leather_horse_armor", servertest.ItemDisplayKey(t, "minecraft:leather_horse_armor"), 1))
	c.ChatCommand("clear @s")
	c.ChatCommand("item replace entity @s hotbar.1 from entity @s armor.body")
	updates, _ := servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Slot != 37 || !updates[0].Stack.Empty() {
		t.Errorf("armor.body after /clear read back as %+v, want an empty slot 37", updates[0])
	}
	c.Disconnect()
}

// TestClearCountTakesSaddleBeforeGrid checks that a limited /clear takes the
// saddle slot, which belongs to the inventory container, before the 2x2 grid.
// vanilla: Inventory.clearOrCountMatchingItems
func TestClearCountTakesSaddleBeforeGrid(t *testing.T) {
	t.Parallel()
	srv, c := dialItem(t, "Testificate")
	c.ChatCommand("item replace entity @s saddle with minecraft:saddle 1")
	c.ExpectCommandFeedbackText(itemEntitySetSingleKey, servertest.ItemNumber(1),
		servertest.PlayerDisplayComponent("Testificate"),
		servertest.ItemFeedbackText("minecraft:saddle", servertest.ItemDisplayKey(t, "minecraft:saddle"), 1))
	c.ChatCommand("item replace entity @s player.crafting.0 with minecraft:saddle 1")
	servertest.AwaitSlotUpdates(t, c, 1)
	c.ChatCommand("clear @s minecraft:saddle 1")
	c.ChatCommand("item replace entity @s hotbar.1 from entity @s saddle")
	servertest.ExpectFeedbackSoon(t, c, nil, itemEntitySingleReplaceKey, servertest.ItemNumber(1),
		servertest.PlayerDisplayComponent("Testificate"))
	if got := srv.InventorySlot("Testificate", 37); !got.Empty() {
		t.Errorf("hotbar.1 holds %+v after reading the saddle slot, want empty", got)
	}
	if got := srv.InventorySlot("Testificate", 1); got.ItemID != servertest.MustItem("minecraft:saddle") {
		t.Errorf("the grid slot holds %+v after /clear took one saddle, want it kept", got)
	}
	c.Disconnect()
}

// TestItemAirCount checks air as the with tail's item: an air stack is
// empty, so it stacks to one, a count above that is refused as overstacked
// with the slot left as it was, and air at one clears the slot.
func TestItemAirCount(t *testing.T) {
	t.Parallel()
	srv, c := dialItem(t, "Testificate")

	// A torch in the main hand: the refusal below must leave it and the
	// clear below that must take it.
	c.ChatCommand("item replace entity @s weapon.mainhand with minecraft:torch 1")
	updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Slot != 36 || updates[0].Stack.ItemID != servertest.MustItem("minecraft:torch") {
		t.Fatalf("the main hand was set to %+v, want a torch", updates[0])
	}
	servertest.ExpectFeedbackSoon(t, c, chats, itemEntitySetSingleKey, servertest.ItemNumber(1),
		servertest.PlayerDisplayComponent("Testificate"),
		servertest.ItemFeedbackText("minecraft:torch", servertest.ItemDisplayKey(t, "minecraft:torch"), 1))
	if got := srv.InventorySlot("Testificate", 36); got.ItemID != servertest.MustItem("minecraft:torch") || got.Count != 1 {
		t.Fatalf("after the set the main hand holds %+v, want the torch", got)
	}

	// Air stacks to one, so a count of two is overstacked and the hand
	// keeps its torch.
	c.ChatCommand("item replace entity @s weapon.mainhand with minecraft:air 2")
	awaitItemFailure(t, c, itemOverstackedKey,
		protocol.Text{Text: "minecraft:air"},
		servertest.ItemNumber(1))
	if got := srv.InventorySlot("Testificate", 36); got.ItemID != servertest.MustItem("minecraft:torch") || got.Count != 1 {
		t.Errorf("the refused air 2 left %+v in the main hand, want the torch", got)
	}

	// Air at one is accepted and clears the hand.
	c.ChatCommand("item replace entity @s weapon.mainhand with minecraft:air 1")
	updates, chats = servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Slot != 36 || !updates[0].Stack.Empty() {
		t.Errorf("air 1 left %+v in the main hand, want it cleared", updates[0])
	}
	servertest.ExpectFeedbackSoon(t, c, chats, itemEntitySetSingleKey, servertest.ItemNumber(1),
		servertest.PlayerDisplayComponent("Testificate"),
		protocol.Text{
			Translate: "chat.square_brackets",
			With:      []protocol.Text{{Extra: []protocol.Text{{Translate: "block.minecraft.air"}}}},
		})
	c.Disconnect()
}

// TestItemTargets checks the form that names several: every player's slot
// is replaced, and the feedback counts the targets whose slots were.
func TestItemTargets(t *testing.T) {
	t.Parallel()
	_, connect := servertest.Start(t, servertest.ItemCreativeConfig(), servertest.OpsListing(t, "Alice", "Bob"))

	alice := connect().Join("Alice")
	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	alice.ChatCommand("item replace entity @a weapon.offhand with minecraft:apple 2")
	var aliceChats [][]byte
	for _, c := range []*servertest.Client{alice, bob} {
		updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
		if c == alice {
			aliceChats = chats
		}
		if updates[0].Slot != 45 || updates[0].Stack.ItemID != servertest.MustItem("minecraft:apple") || updates[0].Stack.Count != 2 {
			t.Errorf("an offhand update = %+v, want two apples", updates[0])
		}
	}
	servertest.ExpectFeedbackSoon(t, alice, aliceChats, itemEntitySetMultipleKey,
		servertest.ItemNumber(2),
		servertest.ItemFeedbackText("minecraft:apple", servertest.ItemDisplayKey(t, "minecraft:apple"), 2))
	alice.Disconnect()
	bob.Disconnect()
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

// TestItemReplaceMobHand checks the entity form against a mob: its main hand
// takes the stack, everyone shown the mob is sent the Set Equipment, and the
// hand reads back as a source; a slot a mob does not keep answers as no such
// slot.
func TestItemReplaceMobHand(t *testing.T) {
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

	bow := servertest.ItemIDOrDie(t, "minecraft:bow")
	c.ChatCommand("item replace entity " + zombieText + " weapon.mainhand with minecraft:bow 1")
	entries := c.ExpectEquipment(zombieID)
	if got := servertest.EquipEntry(entries, protocol.EquipMainHand); got.ItemID != bow {
		t.Errorf("the zombie's main hand = %+v, want a bow", got)
	}
	c.ExpectCommandFeedbackText(itemEntitySetSingleKey, servertest.ItemNumber(1),
		servertest.EntityDisplayComponent("minecraft:zombie", "entity.minecraft.zombie", zombieUUID),
		servertest.ItemFeedbackText("minecraft:bow", servertest.ItemDisplayKey(t, "minecraft:bow"), 1))

	// Read the hand back as a source into the player's own hotbar.
	c.ChatCommand("item replace entity @s hotbar.0 from entity " + zombieText + " weapon.mainhand")
	updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Slot != 36 || updates[0].Stack.ItemID != bow || updates[0].Stack.Count != 1 {
		t.Errorf("the copied stack = %+v, want the bow out of the zombie's hand", updates[0])
	}
	servertest.ExpectFeedbackSoon(t, c, chats, itemEntitySingleReplaceKey, servertest.ItemNumber(1),
		servertest.PlayerDisplayComponent("Testificate"))

	torch := servertest.ItemIDOrDie(t, "minecraft:torch")
	c.ChatCommand("item replace entity " + zombieText + " weapon.offhand with minecraft:torch 1")
	entries = c.ExpectEquipment(zombieID)
	if got := servertest.EquipEntry(entries, protocol.EquipOffHand); got.ItemID != torch {
		t.Errorf("the zombie's off hand = %+v, want a torch", got)
	}
	c.ExpectCommandFeedbackText(itemEntitySetSingleKey, servertest.ItemNumber(1),
		servertest.EntityDisplayComponent("minecraft:zombie", "entity.minecraft.zombie", zombieUUID),
		servertest.ItemFeedbackText("minecraft:torch", servertest.ItemDisplayKey(t, "minecraft:torch"), 1))

	// Clearing a hand with air still sends the hand, so a client showing the
	// bow is told to drop it.
	c.ChatCommand("item replace entity " + zombieText + " weapon.mainhand with minecraft:air")
	entries = c.ExpectEquipment(zombieID)
	if len(entries) != 1 || entries[0].Slot != protocol.EquipMainHand || !entries[0].Stack.Empty() {
		t.Errorf("clearing the main hand = %+v, want one empty main-hand entry", entries)
	}
	c.ExpectCommandFeedbackText(itemEntitySetSingleKey, servertest.ItemNumber(1),
		servertest.EntityDisplayComponent("minecraft:zombie", "entity.minecraft.zombie", zombieUUID),
		protocol.Text{
			Translate: "chat.square_brackets",
			With:      []protocol.Text{{Extra: []protocol.Text{{Translate: "block.minecraft.air"}}}},
		})

	// A slot the mob does not keep answers the source's refusal.
	c.ChatCommand("item replace entity @s hotbar.1 from entity " + zombieText + " container.0")
	awaitItemFailure(t, c, itemSourceNoSlotKey, protocol.Text{Text: "container.0"})
	c.Disconnect()
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

// TestItemMobBodyAndSaddle checks a mob's body and saddle slots: a zombie
// takes horse armour in armor.body and /execute if slots counts it, an
// untamed horse takes a saddle, which its viewers are sent, and a stack
// that does not belong in the slot is refused.
// vanilla: LivingEntity.getSlot, LivingEntity.createEquipmentSlotAccess, Horse.canUseSlot
func TestItemMobBodyAndSaddle(t *testing.T) {
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
	c.ChatCommand("item replace entity " + zombieText + " armor.body with minecraft:iron_horse_armor 1")
	c.ExpectCommandFeedbackText(itemEntitySetSingleKey, servertest.ItemNumber(1),
		servertest.EntityDisplayComponent("minecraft:zombie", "entity.minecraft.zombie", zombieUUID),
		servertest.ItemFeedbackText("minecraft:iron_horse_armor", servertest.ItemDisplayKey(t, "minecraft:iron_horse_armor"), 1))
	c.ChatCommand("execute if slots entity " + zombieText + " armor.body")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
		t.Errorf("the zombie's armor.body answered % X, want a pass with count 1", got)
	}

	horseID := srv.SpawnMob("minecraft:horse", 6.5, 64, 4.5)
	horseText, ok := srv.MobUUID(horseID)
	if !ok {
		t.Fatal("spawning the horse failed")
	}
	c.ChatCommand("item replace entity " + horseText + " saddle with minecraft:stone 1")
	awaitItemFailure(t, c, itemNoChangedKnownItem,
		protocol.Text{Text: "1 minecraft:stone"})
	saddle := servertest.ItemIDOrDie(t, "minecraft:saddle")
	c.ChatCommand("item replace entity " + horseText + " saddle with minecraft:saddle 1")
	// The zombie's armour may be sent first: wait for the horse's saddle slot
	// (EquipmentSlot.SADDLE, 7).
	for id, data := range c.Stream() {
		r := protocol.NewReader(data)
		if id != protocol.IDPlayEntityEquipment || r.ReadVarInt() != horseID {
			continue
		}
		if slot := r.ReadU8(); slot != 7 {
			t.Fatalf("the horse was sent equipment slot %d, want its saddle", slot)
		}
		if got, _ := r.ReadSlot(); got.ItemID != saddle {
			t.Errorf("the horse's saddle slot was sent %+v, want a saddle", got)
		}
		break
	}
	if got := srv.MobEquipment(horseID, "saddle"); got.ItemID != saddle {
		t.Errorf("the horse's saddle = %+v, want a saddle", got)
	}
	c.Disconnect()
}

// TestItemNautilusSaddleAndBabySulfurCube checks that a tamed nautilus's
// saddle from /item reaches the players already tracking it, and that a baby
// sulfur cube refuses horse armour in armor.body.
// vanilla: AbstractNautilus.canUseSlot, SulfurCube.canUseSlot
func TestItemNautilusSaddleAndBabySulfurCube(t *testing.T) {
	t.Parallel()
	srv, c := dialItem(t, "Testificate")

	cubeID := srv.SpawnMob("minecraft:sulfur_cube", 4.5, 64, 4.5)
	srv.SetMobBaby(cubeID, true)
	cubeText, ok := srv.MobUUID(cubeID)
	if !ok {
		t.Fatal("spawning the sulfur cube failed")
	}
	c.ChatCommand("item replace entity " + cubeText + " armor.body with minecraft:iron_horse_armor 1")
	awaitItemFailure(t, c, itemNoChangedKnownItem,
		protocol.Text{Text: "1 minecraft:iron_horse_armor"})
	if got := srv.MobEquipment(cubeID, "body"); !got.Empty() {
		t.Errorf("the baby sulfur cube's body = %+v, want empty", got)
	}

	nautilusID := srv.SpawnMob("minecraft:nautilus", 6.5, 64, 4.5)
	nautilusText, ok := srv.MobUUID(nautilusID)
	if !ok {
		t.Fatal("spawning the nautilus failed")
	}
	if !srv.TameMobFor(nautilusID, "Testificate") {
		t.Fatal("taming the nautilus failed")
	}
	saddle := servertest.ItemIDOrDie(t, "minecraft:saddle")
	c.ChatCommand("item replace entity " + nautilusText + " saddle with minecraft:saddle 1")
	// EquipmentSlot.SADDLE is 7.
	for id, data := range c.Stream() {
		r := protocol.NewReader(data)
		if id != protocol.IDPlayEntityEquipment || r.ReadVarInt() != nautilusID {
			continue
		}
		if slot := r.ReadU8(); slot != 7 {
			t.Fatalf("the nautilus was sent equipment slot %d, want its saddle", slot)
		}
		if got, _ := r.ReadSlot(); got.ItemID != saddle {
			t.Errorf("the nautilus's saddle slot was sent %+v, want a saddle", got)
		}
		break
	}
	if got := srv.MobEquipment(nautilusID, "saddle"); got.ItemID != saddle {
		t.Errorf("the nautilus's saddle = %+v, want a saddle", got)
	}
	c.Disconnect()
}

// TestItemFromConsoleIntoAPlayer checks the console's own /item: a stack
// set into an online player's slot, the console reading its success the way
// a player would.
func TestItemFromConsoleIntoAPlayer(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.ItemCreativeConfig(), "Tester", servertest.OpsListing(t, "Tester"))

	var out bytes.Buffer
	con := console.New(strings.NewReader("item replace entity Tester weapon.mainhand with minecraft:stone 4\n"), &out, io.Discard, srv)
	con.Serve()
	if got := srv.InventorySlot("Tester", player.HotbarSlot); got.ItemID != servertest.MustItem("minecraft:stone") || got.Count != 4 {
		t.Errorf("the main hand after the console's item = %+v, want four stone", got)
	}
	if !strings.Contains(out.String(), "Replaced 1 slot(s) on Tester with [Stone]") {
		t.Errorf("output %q does not carry the item feedback", out.String())
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
		{"a word outside the grammar",
			"item replace entity @s nonsense.3 with minecraft:stone",
			"nonsense.3",
			slotUnknownKey,
			[]string{"nonsense.3"},
			true},
		{"a hotbar slot number past the hotbar",
			"item replace entity @s hotbar.9 with minecraft:stone",
			"hotbar.9",
			slotUnknownKey,
			[]string{"hotbar.9"},
			true},
		{"a container slot number past the largest container",
			"item replace entity @s container.54 with minecraft:stone",
			"container.54",
			slotUnknownKey,
			[]string{"container.54"},
			true},
		{"a crafting grid slot past the 2x2",
			"item replace entity @s player.crafting.4 with minecraft:stone",
			"player.crafting.4",
			slotUnknownKey,
			[]string{"player.crafting.4"},
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

// TestItemCopyFromAnUnloadedBlockKeepsTheTarget checks that a from tail whose
// source cannot be read leaves the target slot as it was: the refusal must
// stop the command rather than let an empty read overwrite what is there.
func TestItemCopyFromAnUnloadedBlockKeepsTheTarget(t *testing.T) {
	t.Parallel()
	srv, c := dialItem(t, "Testificate")

	c.ChatCommand("item replace entity @s weapon with minecraft:stone 5")
	servertest.AwaitSlotUpdates(t, c, 1)
	c.ChatCommand("item replace entity @s weapon from block 100000 64 9 container.0")
	awaitItemFailure(t, c, itemPosUnloadedKey)
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); got.ItemID != servertest.MustItem("minecraft:stone") || got.Count != 5 {
		t.Errorf("the main hand after the refused copy = %+v, want the five stone left there", got)
	}
	c.Disconnect()
}

// TestItemSourceNotAContainer checks the source's own not-a-container
// refusal, which the with tail and the read both answer by place.
func TestItemSourceNotAContainer(t *testing.T) {
	t.Parallel()
	_, c := dialItem(t, "Testificate")

	c.ChatCommand("item replace entity @s hotbar.0 from block 3 64 4 container.0")
	awaitItemFailure(t, c, itemSourceNoContainerKey,
		servertest.ItemNumber(3), servertest.ItemNumber(64), servertest.ItemNumber(4))
	c.Disconnect()
}

// TestItemPositionOutOfWorld checks the second check the vanilla block
// position argument runs: a position inside a loaded chunk but outside the
// world's height is out of world, not merely unloaded.
func TestItemPositionOutOfWorld(t *testing.T) {
	t.Parallel()
	_, c := dialItem(t, "Testificate")

	c.ChatCommand("item replace block 0 1000 0 container.0 with minecraft:stone")
	awaitItemFailure(t, c, itemPosOutOfWorldKey)
	c.ChatCommand("item replace entity @s weapon from block 0 1000 0 container.0")
	awaitItemFailure(t, c, itemPosOutOfWorldKey)
	c.Disconnect()
}

// TestItemArmorSlotValidation checks the armour slots that take only the
// piece belonging to them: the chest slot refuses a stone, takes a
// chestplate, and the head takes anything.
func TestItemArmorSlotValidation(t *testing.T) {
	t.Parallel()
	_, c := dialItem(t, "Testificate")

	c.ChatCommand("item replace entity @s armor.chest with minecraft:stone 1")
	awaitItemFailure(t, c, itemNoChangedKnownItem,
		protocol.Text{Text: "1 minecraft:stone"})

	c.ChatCommand("item replace entity @s armor.chest with minecraft:diamond_chestplate 1")
	updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Slot != 6 || updates[0].Stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:diamond_chestplate") {
		t.Errorf("the chest slot = %+v, want the diamond chestplate in window slot 6", updates[0])
	}
	servertest.ExpectFeedbackSoon(t, c, chats, itemEntitySetSingleKey, servertest.ItemNumber(1),
		servertest.PlayerDisplayComponent("Testificate"),
		servertest.ItemFeedbackText("minecraft:diamond_chestplate", servertest.ItemDisplayKey(t, "minecraft:diamond_chestplate"), 1))

	c.ChatCommand("item replace entity @s armor.head with minecraft:stone 1")
	updates, _ = servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Slot != 5 || updates[0].Stack.ItemID != servertest.MustItem("minecraft:stone") {
		t.Errorf("the head slot = %+v, want the stone the head slot takes", updates[0])
	}
	c.Disconnect()
}

// TestItemArmorSlotFollowsEquippableOverride checks that a stack's own
// equippable component decides which armour slot takes it, over its item's.
// vanilla: LivingEntity.getEquipmentSlotForItem
func TestItemArmorSlotFollowsEquippableOverride(t *testing.T) {
	t.Parallel()
	_, c := dialItem(t, "Testificate")

	c.ChatCommand(`item replace entity @s armor.chest with minecraft:stone[equippable={slot:"chest"}] 1`)
	updates, _ := servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Slot != 6 || updates[0].Stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:stone") {
		t.Errorf("the chest slot = %+v, want the overridden stone in window slot 6", updates[0])
	}

	// Refused, so the next update is the diamond chestplate's.
	c.ChatCommand(`item replace entity @s armor.chest with minecraft:golden_chestplate[equippable={slot:"feet"}] 1`)
	c.ChatCommand("item replace entity @s armor.chest with minecraft:diamond_chestplate 1")
	updates, _ = servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:diamond_chestplate") {
		t.Errorf("the chest slot = %+v, want the feet-overridden golden chestplate refused", updates[0])
	}
	c.Disconnect()
}

// TestItemCraftingGridRefreshesResult checks that writing a 2x2 grid slot
// also sends the result slot the grid is now worth.
func TestItemCraftingGridRefreshesResult(t *testing.T) {
	t.Parallel()
	_, c := dialItem(t, "Testificate")

	c.ChatCommand("item replace entity @s player.crafting.0 with minecraft:oak_planks 1")
	updates, _ := servertest.AwaitSlotUpdates(t, c, 2)
	if updates[0].Slot != 1 || updates[1].Slot != 0 {
		t.Errorf("the first write sent slots %d and %d, want the grid slot 1 and the result slot 0", updates[0].Slot, updates[1].Slot)
	}

	c.ChatCommand("item replace entity @s player.crafting.2 with minecraft:oak_planks 1")
	updates, _ = servertest.AwaitSlotUpdates(t, c, 2)
	if updates[0].Slot != 3 {
		t.Errorf("the second write's grid slot = %d, want 3", updates[0].Slot)
	}
	if updates[1].Slot != 0 || updates[1].Stack.ItemID != servertest.MustItem("minecraft:stick") || updates[1].Stack.Count != 4 {
		t.Errorf("the refreshed result = %+v, want four sticks", updates[1].Stack)
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

// TestItemTargetResolvedBeforeSource checks the error order when a from
// source and an entity target both fail: the targets resolve first, so a
// selector that names nobody answers before the source container is read.
func TestItemTargetResolvedBeforeSource(t *testing.T) {
	t.Parallel()
	_, c := dialItem(t, "Testificate")

	c.ChatCommand("item replace entity @e[type=cow] hotbar.0 from block 3 64 4 container.0")
	awaitItemFailure(t, c, "argument.entity.notfound.entity")
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

// TestItemReplaceWritesItemEntityAndFrameContents checks that the contents
// slot of a dropped item and of an item frame is writable: /item replace
// and /item modify change the item's stack, an emptied item is discarded,
// and a frame keeps one of the stack it is given.
// vanilla: ItemEntity.getSlot, ItemFrame.getSlot
func TestItemReplaceWritesItemEntityAndFrameContents(t *testing.T) {
	t.Parallel()
	_, c := dialItem(t, "Testificate")

	c.ChatCommand(`summon minecraft:item 5.5 64 5.5 {Item:{id:"minecraft:stone",count:1},PickupDelay:32767s}`)
	item := c.AwaitSpawn(protocol.EntityTypeItem)
	c.AwaitMetadataFor(item.EntityID)
	c.ChatCommand("item replace entity @e[type=minecraft:item,limit=1] contents with minecraft:diamond 3")
	stack := itemSlotOf(t, c.AwaitMetadataFor(item.EntityID), protocol.MetadataIndexItemSlot)
	if stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:diamond") || stack.Count != 3 {
		t.Errorf("the item's stack after /item replace = %+v, want 3 diamonds", stack)
	}
	c.ChatCommand(`item modify entity @e[type=minecraft:item,limit=1] contents {type:"set_count",count:7}`)
	stack = itemSlotOf(t, c.AwaitMetadataFor(item.EntityID), protocol.MetadataIndexItemSlot)
	if stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:diamond") || stack.Count != 7 {
		t.Errorf("the item's stack after /item modify = %+v, want 7 diamonds", stack)
	}
	c.ChatCommand("item replace entity @e[type=minecraft:item,limit=1] contents with minecraft:air")
	c.AwaitFallingRemoval(item.EntityID)

	c.ChatCommand("summon minecraft:item_frame 8.5 64 5.5 {Facing:1b}")
	frame := c.AwaitSpawn(protocol.EntityTypeItemFrame)
	c.AwaitMetadataFor(frame.EntityID)
	c.ChatCommand("item replace entity @e[type=minecraft:item_frame,limit=1] contents with minecraft:stone 5")
	stack = itemSlotOf(t, c.AwaitMetadataFor(frame.EntityID), protocol.MetadataIndexFrameItem)
	if stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:stone") || stack.Count != 1 {
		t.Errorf("the frame's item after /item replace = %+v, want one stone", stack)
	}
	c.Disconnect()
}

// itemSlotOf is the stack a metadata write carries at index, failing the
// test when the write has none.
func itemSlotOf(t *testing.T, entries []servertest.MetaEntry, index uint8) protocol.ItemStack {
	t.Helper()
	entry := servertest.MetaOf(entries, index)
	if entry == nil {
		t.Fatalf("metadata %+v carries no item at index %d", entries, index)
	}
	return entry.SlotVal
}
