package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestAStacksOwnMaxDamageEndsItsTool: a pickaxe given max_damage=3 breaks on
// the third stone it digs, and one whose max_damage is removed never wears.
// vanilla: ItemStack.getMaxDamage, ItemStack.isDamageableItem
func TestAStacksOwnMaxDamageEndsItsTool(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, durabilityConfig("survival"), "Digger")
	servertest.GroundThePlayer(c)
	stone := registry.BlockStates["minecraft:stone"]
	pick := protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:diamond_pickaxe"), Count: 1}
	short := pick
	var b protocol.Buffer
	b.PutVarInt(3)
	short.SetRaw(protocol.RawComponent{Type: protocol.DataComponentMaxDamage, Data: b.Bytes()})
	srv.GiveSlot("Digger", player.HotbarSlot, short)
	for i := range int32(3) {
		srv.SetBlock(1+i, 63, 1, stone)
		c.BreakBlock(1+i, 63, 1)
		c.WaitBlockChanged()
		got := srv.InventorySlot("Digger", player.HotbarSlot)
		if i < 2 && got.Damage != i+1 {
			t.Fatalf("after stone %d the pickaxe's wear = %d, want %d", i+1, got.Damage, i+1)
		}
		if i == 2 && !got.Empty() {
			t.Fatalf("the pickaxe survived its third stone: %+v", got)
		}
	}

	endless := pick
	endless.Remove(protocol.DataComponentMaxDamage)
	srv.GiveSlot("Digger", player.HotbarSlot, endless)
	srv.SetBlock(5, 63, 1, stone)
	c.BreakBlock(5, 63, 1)
	c.WaitBlockChanged()
	if got := srv.InventorySlot("Digger", player.HotbarSlot); got.Empty() || got.Damage != 0 {
		t.Fatalf("a pickaxe without max_damage wore: %+v", got)
	}
}
