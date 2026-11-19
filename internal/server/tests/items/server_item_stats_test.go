package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func itemStat(srv *server.Server, statType, item string) int32 {
	return srv.StatValue("Actor", statType, item)
}

// TestAPlacedBlockIsCountedAsUsed: placing a block is a use of its item.
// vanilla: ItemStack.useOn
func TestAPlacedBlockIsCountedAsUsed(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:cobblestone")

	actor.UseBlock(3, 63, 0)
	actor.Settle()

	if got := itemStat(srv, registry.StatUsed, "minecraft:cobblestone"); got != 1 {
		t.Errorf("used cobblestone = %d, want 1", got)
	}
}

// TestAToolThatBreaksIsCountedAsBroken: the hit that wears the last point
// off a pickaxe counts one broken.
// vanilla: ServerPlayer.onEquippedItemBroken
func TestAToolThatBreaksIsCountedAsBroken(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:stone")
	pick := servertest.EnchantedStack(t, "minecraft:netherite_pickaxe", "minecraft:efficiency", 5)
	pick.Damage = registry.MaxDamage(pick.ItemID) - 1
	srv.GiveSlot("Actor", player.HotbarSlot, pick)
	srv.SetBlock(3, 64, 0, registry.MustState("minecraft:stone", nil))
	actor.Settle()

	actor.BreakBlock(3, 64, 0)
	actor.Settle() // the dig packets are read before the ticks below run
	// The dig finishes some ticks after the start; step until the pickaxe is gone.
	for range 200 {
		if srv.InventorySlot("Actor", player.HotbarSlot).Empty() {
			break
		}
		srv.RunTick()
	}
	actor.Settle()

	if got := srv.InventorySlot("Actor", player.HotbarSlot); !got.Empty() {
		t.Fatalf("the pickaxe after the break = %+v, want it gone", got)
	}
	if got := itemStat(srv, registry.StatBroken, "minecraft:netherite_pickaxe"); got != 1 {
		t.Errorf("broken netherite pickaxe = %d, want 1", got)
	}
}
