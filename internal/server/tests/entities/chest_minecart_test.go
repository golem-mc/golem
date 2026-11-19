package entities

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

const chestCart = "minecraft:chest_minecart"

// chestCartPlayer joins a player of the game mode given, named Testificate,
// holding count chest minecarts.
func chestCartPlayer(t *testing.T, gameMode string, count int32) (*server.Server, *servertest.WindowedClient) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	cfg.GameMode = gameMode
	srv, w := servertest.OpenWindowed(t, cfg)
	w.MoveToGround(0.5, 64, 0.5, true)
	w.Settle()
	if count > 0 {
		srv.GiveSlot("Testificate", player.HotbarSlot,
			protocol.ItemStack{ItemID: servertest.MustItemID(t, chestCart), Count: count})
	}
	return srv, w
}

// summonChestCart summons a chest minecart on the test rails with the tick
// loop held, so the cart stays where it stands, and returns its entity ID.
func summonChestCart(t *testing.T, srv *server.Server, tag string) int32 {
	t.Helper()
	srv.StopTick()
	servertest.CartRails(t, srv, 3, 6)
	// The dispatcher refuses input left behind the last argument, so
	// trim the space an empty tag leaves.
	srv.RunLine(strings.TrimSpace("summon minecraft:chest_minecart 4.5 64.0625 1.5 " + tag))
	for _, cart := range srv.MinecartsInDimension(world.DimensionName) {
		return cart.ID
	}
	t.Fatal("the summon stood no cart")
	return 0
}

func itemOf(t *testing.T, name string, count int32) protocol.ItemStack {
	t.Helper()
	return protocol.ItemStack{ItemID: servertest.MustItemID(t, name), Count: count}
}

// TestAChestMinecartItemStandsAChestCartOnARail: the item used on a rail
// stands a chest minecart there, spends the item, and shows watchers the
// chest cart's entity type; an item that is no cart stands nothing.
// vanilla: MinecartItem.useOn
func TestAChestMinecartItemStandsAChestCartOnARail(t *testing.T) {
	t.Parallel()
	srv, c := chestCartPlayer(t, "survival", 2)
	servertest.CartRails(t, srv, 2, 5)
	c.MoveTo(2.5, 64, 2.9, 0, 60)
	c.Settle()

	c.PlaceBlock(2, 64, 1, protocol.FaceTop)
	typeID, _ := registry.EntityTypeID(chestCart)
	spawned := servertest.AwaitBoatSpawn(c.Client, typeID)
	if got := srv.MinecartKind(spawned.EntityID); got != chestCart {
		t.Fatalf("the laid cart is a %q, want %s", got, chestCart)
	}
	servertest.PollUntil(t, 10*time.Second, func() bool {
		return srv.InventorySlot("Testificate", player.HotbarSlot).Count == 1
	})

	// A diamond used on the same rail lays no cart.
	srv.GiveSlot("Testificate", player.HotbarSlot, itemOf(t, "minecraft:diamond", 1))
	before := len(srv.MinecartsInDimension(world.DimensionName))
	c.PlaceBlock(3, 64, 1, protocol.FaceTop)
	c.Settle()
	if after := len(srv.MinecartsInDimension(world.DimensionName)); after != before {
		t.Errorf("a diamond used on a rail left %d carts, want the %d it began with", after, before)
	}
}

// TestAChestMinecartOpensA27SlotWindowAndTakesItems: a click on the cart
// opens the 9x3 window, and a shift-click of a hotbar stack moves it into
// the cart's own slots.
// vanilla: MinecartChest.createMenu, AbstractMinecartContainer.interact
func TestAChestMinecartOpensA27SlotWindowAndTakesItems(t *testing.T) {
	t.Parallel()
	srv, w := chestCartPlayer(t, "survival", 0)
	cartID := summonChestCart(t, srv, "")
	srv.GiveSlot("Testificate", player.HotbarSlot, itemOf(t, "minecraft:diamond", 5))
	w.MoveTo(4.5, 64, 2.7, 0, 60)
	w.Settle()

	w.UseOn(cartID)
	content := w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 0, 0, 0)
	if len(content) != 27+36 {
		t.Fatalf("the window shows %d slots, want the cart's 27 and the inventory's 36", len(content))
	}
	// Window slots 54..62 are the hotbar.
	w.Click(54, 0, 1)
	diamond := servertest.MustItemID(t, "minecraft:diamond")
	servertest.PollUntil(t, 10*time.Second, func() bool {
		return srv.MinecartCargoAt(cartID, 0).ItemID == diamond
	})
	if got := srv.MinecartCargoAt(cartID, 0); got.Count != 5 {
		t.Errorf("the cart's first slot holds %+v, want the five diamonds", got)
	}
}

// TestAChestMinecartIsTitledWithItsName: the window carries the entity's
// display name.
// vanilla: Entity.getDisplayName
func TestAChestMinecartIsTitledWithItsName(t *testing.T) {
	t.Parallel()
	srv, c := chestCartPlayer(t, "survival", 0)
	cartID := summonChestCart(t, srv, "")
	c.MoveTo(4.5, 64, 2.7, 0, 60)
	c.Settle()
	c.UseOn(cartID)
	title, opened := awaitBoatCargoTitle(t, srv, c.Client)
	if !opened {
		t.Fatal("the cart opened nothing")
	}
	if title.Translate != "entity.minecraft.chest_minecart" {
		t.Errorf("title translates %q, want entity.minecraft.chest_minecart", title.Translate)
	}
}

// TestAChestMinecartIsTitledWithItsCustomName: a named cart's window
// carries the name.
// vanilla: AbstractMinecartContainer, Entity.getDisplayName
func TestAChestMinecartIsTitledWithItsCustomName(t *testing.T) {
	t.Parallel()
	srv, c := chestCartPlayer(t, "survival", 0)
	cartID := summonChestCart(t, srv, `{CustomName:"Bob"}`)
	c.MoveTo(4.5, 64, 2.7, 0, 60)
	c.Settle()
	c.UseOn(cartID)
	title, opened := awaitBoatCargoTitle(t, srv, c.Client)
	if !opened {
		t.Fatal("the cart opened nothing")
	}
	if title.Text != "Bob" || title.Translate != "" {
		t.Errorf("title is %+v, want the literal Bob", title)
	}
}

// TestABrokenChestMinecartDropsItselfAndItsContents: a blow that breaks the
// cart leaves the cart item and every stack it held; the contents also spill
// when entity_drops is off, which gates the cart item alone.
// vanilla: VehicleEntity.destroy, AbstractMinecartContainer.remove
func TestABrokenChestMinecartDropsItselfAndItsContents(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		rule      string
		wantCarts int32
	}{
		{"drops on", "true", 1},
		{"drops off", "false", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := chestCartPlayer(t, "survival", 0)
			srv.GiveSlot("Testificate", player.HotbarSlot, itemOf(t, "minecraft:diamond_sword", 1))
			servertest.AwaitFullSwing(t, srv, "Testificate")
			cartID := summonChestCart(t, srv, "")
			srv.RunLine("gamerule entity_drops " + tc.rule)
			srv.StowMinecartCargo(cartID, 0, itemOf(t, "minecraft:diamond", 3))
			srv.StowMinecartCargo(cartID, 26, itemOf(t, "minecraft:cobblestone", 40))
			c.MoveTo(4.5, 64, 2.7, 0, 60)
			c.Settle()

			c.Attack(cartID)
			// The blow marks the cart dead on the connection's goroutine;
			// only the next cart pass takes it out and lays its drops, so
			// the pass runs after the blow is known to have landed.
			c.Settle()
			for range 200 {
				srv.TickMinecarts(1)
				if srv.MinecartKind(cartID) == "" {
					break
				}
			}
			if srv.MinecartKind(cartID) != "" {
				t.Fatal("the cart survived 200 passes after the blow")
			}
			items := srv.ItemsInDimension(world.DimensionName)
			if got := servertest.ItemCountOf(items, "minecraft:diamond"); got != 3 {
				t.Errorf("%d diamonds spilled, want the 3 the cart held", got)
			}
			if got := servertest.ItemCountOf(items, "minecraft:cobblestone"); got != 40 {
				t.Errorf("%d cobblestone spilled, want the 40 the cart held", got)
			}
			if got := servertest.ItemCountOf(items, chestCart); got != tc.wantCarts {
				t.Errorf("%d chest minecart items dropped, want %d", got, tc.wantCarts)
			}
		})
	}
}

// TestAKilledChestMinecartSpillsItsContentsAlone: /kill leaves no cart item
// but the contents still spill.
// vanilla: Entity.kill, AbstractMinecartContainer.remove
func TestAKilledChestMinecartSpillsItsContentsAlone(t *testing.T) {
	t.Parallel()
	srv, _ := chestCartPlayer(t, "survival", 0)
	cartID := summonChestCart(t, srv, "")
	srv.StowMinecartCargo(cartID, 4, itemOf(t, "minecraft:emerald", 7))

	srv.RunLine("kill @e[type=minecraft:chest_minecart]")
	servertest.PollUntil(t, 10*time.Second, func() bool {
		srv.TickMinecarts(1)
		return srv.MinecartKind(cartID) == ""
	})
	items := srv.ItemsInDimension(world.DimensionName)
	if got := servertest.ItemCountOf(items, "minecraft:emerald"); got != 7 {
		t.Errorf("%d emeralds spilled, want the 7 the cart held", got)
	}
	if got := servertest.ItemCountOf(items, chestCart); got != 0 {
		t.Errorf("%d chest minecart items dropped by a /kill, want none", got)
	}
}

// TestAChestMinecartRollsAndKeepsItsLootTable: a summoned cart saves its
// pending table and seed, the first open rolls it into the slots, and a break
// of an unopened cart rolls it too.
// vanilla: ContainerEntity.unpackChestVehicleLootTable, addChestVehicleSaveData
func TestAChestMinecartRollsAndKeepsItsLootTable(t *testing.T) {
	t.Parallel()
	const table = "minecraft:chests/buried_treasure"
	srv, c := chestCartPlayer(t, "survival", 0)
	cartID := summonChestCart(t, srv, `{LootTable:"`+table+`",LootTableSeed:7L}`)
	tag, _ := srv.MinecartSaveTag(cartID)
	if got, _ := nbt.Get[nbt.String](tag, "LootTable"); string(got) != table {
		t.Errorf("saved LootTable = %q, want %q", got, table)
	}
	if _, ok := tag["Items"]; ok {
		t.Error("the unrolled cart saves Items beside its table")
	}

	c.MoveTo(4.5, 64, 2.7, 0, 60)
	c.Settle()
	c.UseOn(cartID)
	c.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 0, 0, 0)
	heart := servertest.MustItemID(t, "minecraft:heart_of_the_sea")
	var hearts int32
	for slot := range 27 {
		if stack := srv.MinecartCargoAt(cartID, slot); stack.ItemID == heart {
			hearts += stack.Count
		}
	}
	if hearts != 1 {
		t.Errorf("the opened cart holds %d hearts of the sea, want the table's one", hearts)
	}
	tag, _ = srv.MinecartSaveTag(cartID)
	if _, ok := tag["LootTable"]; ok {
		t.Error("the opened cart still saves its loot table")
	}
}

// TestOpeningALootChestMinecartObtainsItsCriterion: the open that rolls a
// chest cart's bastion table is a player_generates_container_loot for that
// table alone.
// vanilla: ContainerEntity.unpackChestVehicleLootTable, GenerateContainerLootTrigger
func TestOpeningALootChestMinecartObtainsItsCriterion(t *testing.T) {
	t.Parallel()
	const adv = "minecraft:nether/loot_bastion"
	srv, c := chestCartPlayer(t, "survival", 0)
	cartID := summonChestCart(t, srv, `{LootTable:"minecraft:chests/bastion_bridge"}`)
	if srv.AdvancementObtained("Testificate", adv, "loot_bastion_bridge") {
		t.Fatal("loot_bastion_bridge was obtained before the cart was opened")
	}

	c.MoveTo(4.5, 64, 2.7, 0, 60)
	c.Settle()
	c.UseOn(cartID)
	c.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 0, 0, 0)
	if !srv.AdvancementObtained("Testificate", adv, "loot_bastion_bridge") {
		t.Error("opening the loot cart did not obtain loot_bastion_bridge")
	}
	if srv.AdvancementObtained("Testificate", adv, "loot_bastion_treasure") {
		t.Error("opening the loot cart obtained loot_bastion_treasure")
	}
}

// TestAChestMinecartRollsItsTableAtItsOwnBiome: the mineshaft table's
// picture disc is gated on the sulfur caves, and the cart's roll reads the
// biome at the cart's own block, so a cart standing there rolls the disc a
// roll with no origin never would.
// vanilla: ContainerEntity.unpackChestVehicleLootTable
func TestAChestMinecartRollsItsTableAtItsOwnBiome(t *testing.T) {
	t.Parallel()
	const table = "minecraft:chests/abandoned_mineshaft"
	disc := servertest.MustItemID(t, "minecraft:music_disc_bounce")
	holds := func(slots []protocol.ItemStack) bool {
		for _, s := range slots {
			if s.ItemID == disc && !s.Empty() {
				return true
			}
		}
		return false
	}
	// A seed whose roll carries the disc where the biome is the sulfur caves.
	sulfur := &rules.LootContext{BiomeAt: func(dx, dy, dz int32) string { return "minecraft:sulfur_caves" }}
	var seed int64
	for s := int64(1); seed == 0 && s < 500; s++ {
		slots := make([]protocol.ItemStack, 27)
		if rules.FillLoot(table, sulfur, rules.NewJavaRandom(s), slots) && holds(slots) {
			seed = s
		}
	}
	if seed == 0 {
		t.Fatal("no seed rolled the sulfur-caves disc")
	}

	srv, c := chestCartPlayer(t, "survival", 0)
	cartID := summonChestCart(t, srv, `{LootTable:"`+table+`",LootTableSeed:`+strconv.FormatInt(seed, 10)+`L}`)
	srv.RunLine("fillbiome 0 64 0 8 66 4 minecraft:sulfur_caves")
	c.MoveTo(4.5, 64, 2.7, 0, 60)
	c.Settle()
	c.UseOn(cartID)
	c.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 0, 0, 0)
	opened := make([]protocol.ItemStack, 27)
	for slot := range opened {
		opened[slot] = srv.MinecartCargoAt(cartID, slot)
	}
	if !holds(opened) {
		t.Error("the cart in the sulfur caves rolled no picture disc under the seed that rolls one there")
	}
}

// TestASpectatorOpensNothingOnAChestMinecartWithALootTable: the table stays
// unrolled and no window opens; without a table the spectator's click opens.
// vanilla: AbstractMinecartContainer.createMenu
func TestASpectatorOpensNothingOnAChestMinecartWithALootTable(t *testing.T) {
	t.Parallel()
	srv, c := chestCartPlayer(t, "spectator", 0)
	looted := summonChestCart(t, srv, `{LootTable:"minecraft:chests/buried_treasure"}`)
	c.MoveTo(4.5, 64, 2.7, 0, 60)
	c.Settle()
	servertest.HoldTicks(t, srv)
	c.UseOn(looted)
	if _, opened := awaitBoatCargoTitle(t, srv, c.Client); opened {
		t.Error("a spectator opened a chest minecart that still holds a loot table")
	}
	if tag, _ := srv.MinecartSaveTag(looted); tag["LootTable"] == nil {
		t.Error("the spectator's click rolled the loot table")
	}
	srv.RunLine("summon minecraft:chest_minecart 5.5 64.0625 1.5")
	plain := srv.MinecartsInDimension(world.DimensionName)
	for _, cart := range plain {
		if cart.ID != looted {
			c.UseOn(cart.ID)
		}
	}
	if _, opened := awaitBoatCargoTitle(t, srv, c.Client); !opened {
		t.Error("a spectator's click opened nothing on a cart with no table")
	}
}

// TestAChestMinecartKeepsItsItemsAcrossASaveAndReload: a chest cart planted
// in a chunk's entity file comes back with its slots, and the save that
// writes it out again carries them.
// vanilla: ContainerEntity.readChestVehicleSaveData
func TestAChestMinecartKeepsItsItemsAcrossASaveAndReload(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.PlantChunkEntities(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, nbt.Compound{
		"id":   nbt.String(chestCart),
		"UUID": servertest.UuidWords(servertest.FixtureUUID(4, 2, 0, 11)),
		"Pos": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
			nbt.Double(5.5), nbt.Double(70), nbt.Double(5.5),
		}},
		"Motion": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(0), nbt.Double(0), nbt.Double(0)}},
		"Items": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"Slot": nbt.Byte(2), "id": nbt.String("minecraft:diamond"), "count": nbt.Int(9)},
			nbt.Compound{"Slot": nbt.Byte(26), "id": nbt.String("minecraft:stick"), "count": nbt.Int(1)},
			nbt.Compound{"Slot": nbt.Byte(27), "id": nbt.String("minecraft:stick"), "count": nbt.Int(1)},
		}},
		"DisplayOffset": nbt.Int(3),
		"DisplayState":  nbt.Compound{"Name": nbt.String("minecraft:stone")},
	})

	srv, _ := servertest.Join(t, cfg, "Keeper")
	var cartID int32
	servertest.WaitFor(t, srv, 200, func() bool {
		for _, cart := range srv.MinecartsInDimension(world.DimensionName) {
			cartID = cart.ID
		}
		return cartID != 0
	})
	srv.StopTick()

	if got := srv.MinecartCargoAt(cartID, 2); got.ItemID != servertest.MustItemID(t, "minecraft:diamond") || got.Count != 9 {
		t.Errorf("slot 2 holds %+v, want the nine diamonds", got)
	}
	if got := srv.MinecartCargoAt(cartID, 26); got.Count != 1 {
		t.Errorf("slot 26 holds %+v, want the stick", got)
	}

	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, chestCart, func(compound nbt.Compound) {
		items, _ := nbt.Get[nbt.List](compound, "Items")
		if len(items.Items) != 2 {
			t.Errorf("the saved cart carries %d stacks, want the 2 whose slots exist", len(items.Items))
		}
		if offset, _ := nbt.Get[nbt.Int](compound, "DisplayOffset"); offset != 3 {
			t.Errorf("saved DisplayOffset = %v, want 3", offset)
		}
		state := compound["DisplayState"]
		if name, _ := registry.BlockStateOf(state); name != "minecraft:stone" {
			t.Errorf("saved DisplayState = %v, want stone", state)
		}
	})
}

// TestAChestMinecartTellsWatchersItsDisplayBlock: a fresh chest cart sends no
// display fields, a custom block state and offset ride the spawn metadata.
// vanilla: AbstractMinecart.defineSynchedData
func TestAChestMinecartTellsWatchersItsDisplayBlock(t *testing.T) {
	t.Parallel()
	srv, c := chestCartPlayer(t, "survival", 0)
	servertest.CartRails(t, srv, 3, 6)
	srv.RunLine(`summon minecraft:chest_minecart 4.5 64.0625 1.5 {DisplayOffset:3,DisplayState:{Name:"minecraft:stone"}}`)
	stone, _ := registry.FindState("minecraft:stone", nil)
	var gotState, gotOffset int32 = -1, -1
	c.ReadTicksSkipping(srv, 200, servertest.BoatSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityMetadata {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadVarInt() // the cart
		for {
			index := r.ReadU8()
			if index == 0xFF || r.Err() != nil {
				break
			}
			typ := r.ReadVarInt()
			switch index {
			case protocol.MetadataIndexMinecartDisplayBlock:
				gotState = r.ReadVarInt()
			case protocol.MetadataIndexMinecartDisplayOffset:
				gotOffset = r.ReadVarInt()
			default:
				c.T.Fatalf("unexpected metadata field %d of type %d on a fresh custom cart", index, typ)
			}
		}
		return gotState != -1
	})
	if gotState != int32(stone) || gotOffset != 3 {
		t.Errorf("display metadata = block %d offset %d, want stone %d and 3", gotState, gotOffset, stone)
	}
}

// TestAnEmptyChestMinecartSavesAnEmptyItemsList: a chest minecart with no loot
// table saves an empty Items list, a minecart without a container none.
// vanilla: ContainerEntity.addChestVehicleSaveData, ContainerHelper.saveAllItems
func TestAnEmptyChestMinecartSavesAnEmptyItemsList(t *testing.T) {
	t.Parallel()
	srv, _ := chestCartPlayer(t, "survival", 0)
	cartID := summonChestCart(t, srv, "")
	tag, _ := srv.MinecartSaveTag(cartID)
	items, ok := nbt.Get[nbt.List](tag, "Items")
	if !ok || len(items.Items) != 0 {
		t.Errorf("empty chest cart saved Items = %v (present %v), want an empty list", items, ok)
	}

	plain := srv.SpawnMinecartOfKind("minecraft:minecart", 4.5, 64.0625, 4.5)
	if tag, _ := srv.MinecartSaveTag(plain); tag["Items"] != nil {
		t.Errorf("a plain minecart saved Items = %v, want none", tag["Items"])
	}
}
