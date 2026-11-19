package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func namedTable() protocol.ItemStack {
	name := protocol.Text{Text: "Altar"}
	return protocol.ItemStack{ItemID: int32(registry.ItemEnchantingTable), Count: 1, CustomName: &name}
}

// A placed table keeps its item's name and titles its window with it.
// vanilla: EnchantingTableBlockEntity.applyImplicitComponents, EnchantingTableBlock.getMenuProvider
func TestANamedEnchantingTableTitlesItsWindow(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	w.SetCreativeSlot(36, namedTable())
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	_, title := w.ExpectOpenTitle(protocol.MenuEnchantment, -1, 4, 64, 0)
	if title.Plain() != "Altar" {
		t.Errorf("window title = %q, want Altar", title.Plain())
	}
}

// The name survives a save and reload, and the broken table drops named.
// vanilla: EnchantingTableBlockEntity.saveAdditional, blocks/enchanting_table loot table
func TestANamedEnchantingTableSavesAndDropsItsName(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.AutoSaveTicks = 0
	srv, c := servertest.Join(t, cfg, "Breaker", servertest.OpsListing(t, "Breaker"))
	servertest.HoldTicks(t, srv)
	c.MoveToGround(3.5, 64, 3.5, true)
	c.Settle()
	srv.GiveSlot("Breaker", player.HotbarSlot, namedTable())
	c.PlaceBlock(3, 63, 0, protocol.FaceTop)
	c.Settle()
	if tag, ok := srv.BlockEntityTagAt(3, 64, 0); !ok || tag["CustomName"] == nil {
		t.Fatalf("placed table tag = %v, want a CustomName", tag)
	}

	servertest.AutoSaveEveryTick(srv)
	servertest.RunTicks(t, srv, servertest.AutoSaveEvery)
	reopened, err := world.NewWorld(cfg.LevelName, world.NewFlatGenerator())
	if err != nil {
		t.Fatalf("reopen world: %v", err)
	}
	pos := world.ChunkPos{X: 0, Z: 0}
	reopened.Acquire(pos)
	tag, ok := reopened.BlockEntityTag(3, 64, 0)
	reopened.Release(pos)
	reopened.Close()
	if !ok || tag["CustomName"] == nil {
		t.Fatalf("reloaded table tag = %v, want a CustomName", tag)
	}

	c.ChatCommand("setblock 3 64 0 air destroy")
	c.ExpectCommandFeedback("commands.setblock.success", "3", "64", "0")
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 || registry.Item(items[0].Stack.ItemID) != registry.ItemEnchantingTable {
		t.Fatalf("drops = %+v, want one enchanting table", items)
	}
	if name := items[0].Stack.CustomName; name == nil || name.Plain() != "Altar" {
		t.Errorf("drop custom name = %v, want Altar", name)
	}
}
