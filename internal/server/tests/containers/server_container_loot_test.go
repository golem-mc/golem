package containers

import (
	"bytes"
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
)

const (
	lootTable = "minecraft:chests/village/village_plains_house"
	lootChest = `minecraft:chest{LootTable:"` + lootTable + `",LootTableSeed:7L}`
)

// rolledLoot is what the table rolls into an empty chest under the seed.
func rolledLoot(t *testing.T) []protocol.ItemStack {
	t.Helper()
	slots := make([]protocol.ItemStack, 27)
	if !rules.FillLoot(lootTable, &rules.LootContext{}, rules.NewJavaRandom(7), slots) {
		t.Fatal("the table is unknown")
	}
	return slots
}

// TestAChestRollsItsLootTableOnFirstOpen: a chest loaded with a table and a
// seed opens with the stacks the table rolls, and its next save writes Items
// and no table.
// vanilla: RandomizableContainerBlockEntity.createMenu, RandomizableContainer.unpackLootTable
func TestAChestRollsItsLootTableOnFirstOpen(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	servertest.Setblock(t, w.Client, 4, 64, 0, lootChest)

	w.UseBlock(4, 64, 0)
	slots := w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	want := rolledLoot(t)
	if len(slots) != 27+36 {
		t.Fatalf("the window has %d slots, want 63", len(slots))
	}
	rolled := 0
	for i := range want {
		if !slots[i].Equal(want[i]) {
			t.Errorf("slot %d = %+v, want %+v", i, slots[i], want[i])
		}
		if !want[i].Empty() {
			rolled++
		}
	}
	if rolled == 0 {
		t.Fatal("the table rolled nothing")
	}
	tag, ok := srv.BlockEntityTagAt(4, 64, 0)
	if !ok {
		t.Fatal("the chest has no block entity tag")
	}
	if _, ok := tag["LootTable"]; ok {
		t.Error("the opened chest still saves its loot table")
	}
	if _, ok := nbt.Get[nbt.List](tag, "Items"); !ok {
		t.Error("the opened chest saves no Items")
	}
}

// TestAHopperDrawsFromAFreshTableChest: a hopper under a chest still holding
// its loot table takes an item on its first transfer.
// vanilla: HopperBlockEntity.getSlots, RandomizableContainerBlockEntity.getItem
func TestAHopperDrawsFromAFreshTableChest(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.Setblock(t, c, 4, 64, 0, lootChest)
	if !srv.PlaceContainer(4, 63, 0, "minecraft:hopper") {
		t.Fatal("could not place the hopper under the chest")
	}
	srv.StopTick()
	servertest.TickBlockEntitiesByHand(srv, 10)
	drawn := 0
	for _, s := range srv.InventoryAt(4, 63, 0) {
		if !s.Empty() {
			drawn++
		}
	}
	if drawn == 0 {
		t.Error("the hopper drew nothing from the fresh table chest")
	}
}

// TestBreakingAnUnopenedTableChestDropsTheRolledContents: the break rolls the
// table first, so the spill is the table's stacks.
// vanilla: Containers.dropContents
func TestBreakingAnUnopenedTableChestDropsTheRolledContents(t *testing.T) {
	t.Parallel()
	_, c := servertest.AttachActor(t)
	servertest.Setblock(t, c, 4, 64, 0, lootChest)
	c.ChatCommand("setblock 4 64 0 air destroy")
	got := servertest.AwaitDroppedStack(t, c)
	found := false
	for _, s := range rolledLoot(t) {
		if !s.Empty() && s.ItemID == got {
			found = true
		}
	}
	if !found {
		t.Errorf("the broken chest dropped item %d, which the table did not roll", got)
	}
}

// TestItemReplaceRollsAnUnopenedTableChest: /item replace on a chest still
// holding its table rolls the table first, so the other slots keep the
// rolled stacks and the chest saves no table.
// vanilla: RandomizableContainerBlockEntity.setItem
func TestItemReplaceRollsAnUnopenedTableChest(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.ItemCreativeConfig(), servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	c.SpawnedAt()
	c.SkipJoin()
	servertest.Setblock(t, c, 2, 64, 3, lootChest)

	c.ChatCommand("item replace block 2 64 3 container.0 with minecraft:stone 4")
	c.Settle()
	got := srv.InventoryAt(2, 64, 3)
	want := rolledLoot(t)
	if got[0].ItemID != servertest.MustItem("minecraft:stone") || got[0].Count != 4 {
		t.Errorf("slot 0 = %+v, want the four stone set", got[0])
	}
	rolled := 0
	for i := 1; i < len(want); i++ {
		if !got[i].Equal(want[i]) {
			t.Errorf("slot %d = %+v, want the rolled %+v", i, got[i], want[i])
		}
		if !want[i].Empty() {
			rolled++
		}
	}
	if rolled == 0 {
		t.Fatal("the table rolled nothing past slot 0")
	}
	tag, _ := srv.BlockEntityTagAt(2, 64, 3)
	if _, ok := tag["LootTable"]; ok {
		t.Error("the chest still saves its loot table")
	}
	c.Disconnect()
}

// TestExecuteIfItemsRollsAnUnopenedTableChest: the item test reads the
// rolled stacks of a chest still holding its table.
// vanilla: RandomizableContainerBlockEntity.getItem
func TestExecuteIfItemsRollsAnUnopenedTableChest(t *testing.T) {
	t.Parallel()
	_, c := servertest.Dial(t, servertest.ItemCreativeConfig(), servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	c.SpawnedAt()
	c.SkipJoin()
	servertest.Setblock(t, c, 2, 64, 3, lootChest)

	total := 0
	for _, s := range rolledLoot(t) {
		if !s.Empty() {
			total += int(s.Count)
		}
	}
	c.ChatCommand("execute if items block 2 64 3 container.* *")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, servertest.ConditionalPassCount(int64(total))) {
		t.Errorf("the block container test answered % X, want %d items counted", got, total)
	}
	c.Disconnect()
}

// TestAPoweredCrafterLeavesItsLootTablePending: a crafter fired with a pending
// table sees an empty grid, plays the fail event and keeps the table; only
// opening the menu rolls it.
// vanilla: CrafterBlockEntity.getItem, CrafterBlock.dispenseFrom
func TestAPoweredCrafterLeavesItsLootTablePending(t *testing.T) {
	t.Parallel()
	srv, w := crafterFacingSouth(t, true)
	servertest.Setblock(t, w.Client, 4, 64, -1,
		`minecraft:crafter[orientation=south_up]{LootTable:"`+lootTable+`",LootTableSeed:7L}`)
	w.DrainRecorded()
	w.Events = nil
	powerCrafter(t, srv, w)
	w.DrainRecorded()
	if !slices.Contains(w.Events, protocol.WorldEventCrafterFail) {
		t.Errorf("no fail event played; events were %v", w.Events)
	}
	tag, ok := srv.BlockEntityTagAt(4, 64, -1)
	if !ok {
		t.Fatal("the crafter has no block entity tag")
	}
	if _, ok := tag["LootTable"]; !ok {
		t.Error("the fired crafter rolled its loot table")
	}
}

// TestASimpleDungeonChestFillsOnOpen: a table that uses enchant_randomly
// is generated, so a chest holding it opens with the stacks it rolls.
// vanilla: RandomizableContainerBlockEntity.createMenu
func TestASimpleDungeonChestFillsOnOpen(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	servertest.Setblock(t, w.Client, 4, 64, 0, `minecraft:chest{LootTable:"minecraft:chests/simple_dungeon",LootTableSeed:7L}`)

	w.UseBlock(4, 64, 0)
	slots := w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	rolled := 0
	for _, s := range slots[:27] {
		if !s.Empty() {
			rolled++
		}
	}
	if rolled == 0 {
		t.Fatal("the simple_dungeon chest opened empty")
	}
}

// TestChestLootCarriesTheEnchantmentsItsTableRolls: an ancient_city chest
// enchants its tools and books, so across seeds some stack arrives enchanted.
func TestChestLootCarriesTheEnchantmentsItsTableRolls(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	enchanted := false
	for seed := 1; seed <= 8 && !enchanted; seed++ {
		servertest.Setblock(t, w.Client, 4, 64, 0, fmt.Sprintf(`minecraft:chest{LootTable:"minecraft:chests/ancient_city",LootTableSeed:%dL}`, seed))
		w.UseBlock(4, 64, 0)
		slots := w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
		for _, s := range slots[:27] {
			if len(s.Enchantments) > 0 || len(s.StoredEnchantments) > 0 {
				enchanted = true
			}
		}
		w.CloseWindow()
	}
	if !enchanted {
		t.Fatal("no ancient_city stack carried an enchantment")
	}
}

// TestAPlacedChestRollsItsStacksContainerLoot places a chest whose stack
// carries container_loot: a data pick copies the table back onto the item,
// and opening the chest fills it from the table.
// vanilla: RandomizableContainerBlockEntity.applyImplicitComponents,
// RandomizableContainerBlockEntity.collectImplicitComponents
func TestAPlacedChestRollsItsStacksContainerLoot(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	want := protocol.ContainerLoot{Table: "minecraft:chests/simple_dungeon", Seed: 7}
	servertest.GiveHeldItem(w.Client, "minecraft:chest")
	w.ChatCommand(`item modify entity @s weapon.mainhand {type:"set_loot_table",loot_table_id:"minecraft:chests/simple_dungeon",seed:7L}`)
	servertest.UntilTicks(t, srv, 200, "the held chest's container_loot", func() bool {
		return srv.InventorySlot("Testificate", player.HotbarSlot).ContainerLoot == want
	})
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	servertest.UntilTicks(t, srv, 200, "the placed chest's loot table", func() bool {
		tag, ok := srv.BlockEntityTagAt(4, 64, 0)
		return ok && tag["LootTable"] == nbt.String(want.Table)
	})
	w.Settle()
	w.Drain()

	servertest.GiveHeldItem(w.Client, "minecraft:stick")
	servertest.PickedStack(t, w.Client, 4, 64, 0, true)
	picked := false
	for slot := player.HotbarSlot; slot < player.HotbarSlot+9; slot++ {
		if srv.InventorySlot("Testificate", slot).ContainerLoot == want {
			picked = true
		}
	}
	if !picked {
		t.Error("the data pick did not carry the chest's container_loot")
	}

	servertest.GiveHeldItem(w.Client, "minecraft:stick")
	w.Drain()
	w.UseBlock(4, 64, 0)
	slots := w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	rolled := 0
	for _, s := range slots[:27] {
		if !s.Empty() {
			rolled++
		}
	}
	if rolled == 0 {
		t.Fatal("the placed chest opened empty")
	}
}
