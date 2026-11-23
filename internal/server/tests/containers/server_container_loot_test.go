package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
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
	w.ChatCommand("random reset *")
	w.ExpectCommandFeedbackText("commands.random.reset.all.success", servertest.ItemNumber(0))
}

func TestUnseededChestFillsFromItsLootSequence(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelSeed = "12345"
	_, w := servertest.OpenWindowedOps(t, cfg)
	defer w.Disconnect()
	servertest.Setblock(t, w.Client, 4, 64, 0, `minecraft:chest{LootTable:"`+lootTable+`"}`)
	w.UseBlock(4, 64, 0)
	got := w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	want := make([]protocol.ItemStack, 27)
	if !rules.FillLoot(lootTable, &rules.LootContext{}, world.NewRandomSequence(12345, lootTable), want) {
		t.Fatal("the table is unknown")
	}
	for i, stack := range want {
		if !got[i].Equal(stack) {
			t.Errorf("slot %d = %+v, want %+v from the named sequence", i, got[i], stack)
		}
	}
	w.ChatCommand("random reset *")
	w.ExpectCommandFeedbackText("commands.random.reset.all.success", servertest.ItemNumber(1))
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
