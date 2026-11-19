package commands

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestGiveItemPropertyOverrides: /give takes a stack's own max_stack_size and
// the other prototype components, and refuses a damageable stackable one with
// the malformed-item message.
// vanilla: ItemInput.createItemStack, ItemStack.validateComponents
func TestGiveItemPropertyOverrides(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)

	c.ChatCommand("give @s stone[max_stack_size=4] 6")
	c.ChatCommand("give @s stick[enchantable={value:5},dye=red,damage_type=arrow,minimum_attack_charge=0.5f]")
	c.ChatCommand("give @s diamond_sword[max_damage=10]")
	c.Settle()
	stone := srv.InventorySlot("Tester", player.HotbarSlot)
	if n, ok := stone.MaxStackSizeOf(); registry.Item(stone.ItemID) != registry.ItemStone || !ok || n != 4 || stone.Count != 4 {
		t.Fatalf("first stack = %+v, want 4 stone with max_stack_size 4", stone)
	}
	if rest := srv.InventorySlot("Tester", player.HotbarSlot+1); rest.Count != 2 {
		t.Fatalf("second stack = %+v, want the other 2", rest)
	}
	stick := srv.InventorySlot("Tester", player.HotbarSlot+2)
	if registry.Item(stick.ItemID) != registry.ItemStick || registry.StackEnchantable(stick) != 5 {
		t.Fatalf("stick = %+v, want enchantable 5", stick)
	}
	sword := srv.InventorySlot("Tester", player.HotbarSlot+3)
	if registry.Item(sword.ItemID) != registry.ItemDiamondSword || registry.StackMaxDamage(sword) != 10 {
		t.Fatalf("sword = %+v, want max_damage 10", sword)
	}

	input := "give @s stick[max_damage=10]"
	want := servertest.CommandErrorMessageForTest("arguments.item.malformed", "Item cannot be both damageable and stackable")
	c.ChatCommand(input)
	got := false
	c.ReadTicks(srv, 5, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySystemChat && bytes.Equal(data, want) {
			got = true
		}
		return got
	})
	if !got {
		t.Fatal("no malformed-item refusal for a damageable stackable stick")
	}
	c.Disconnect()
}
