package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func withMaxDamage(stack protocol.ItemStack, n int32) protocol.ItemStack {
	var b protocol.Buffer
	b.PutVarInt(n)
	stack.SetRaw(protocol.RawComponent{Type: protocol.DataComponentMaxDamage, Data: b.Bytes()})
	return stack
}

// TestASwordWithItsOwnMaxDamageBreaksOnItsLastHit: a sword at max_damage=10
// with nine wear breaks on the next blow instead of lasting its prototype's
// 1561 uses.
// vanilla: ItemStack.hurtAndBreak, ItemStack.getMaxDamage
func TestASwordWithItsOwnMaxDamageBreaksOnItsLastHit(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, durabilityConfig("survival"), "Fencer")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	sword := withMaxDamage(protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:diamond_sword"), Count: 1, Damage: 9}, 10)
	srv.GiveSlot("Fencer", player.HotbarSlot, sword)
	servertest.AwaitFullSwing(t, srv, "Fencer")
	cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.Attack(cow)
	c.Settle()

	if got := srv.InventorySlot("Fencer", player.HotbarSlot); !got.Empty() {
		t.Fatalf("the sword survived its tenth wear: %+v", got)
	}
}
