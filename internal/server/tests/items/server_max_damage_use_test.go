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

// TestMendingRepairsToAStacksOwnMaxDamage: an orb mends a max_damage=20
// pickaxe at 19 wear down to no wear.
// vanilla: ExperienceOrb.repairPlayerItems
func TestMendingRepairsToAStacksOwnMaxDamage(t *testing.T) {
	t.Parallel()
	cfg, _ := servertest.ExperienceSetup(t)
	srv, c, _ := servertest.JoinAs(t, cfg, "Mender")

	pick := servertest.EnchantedStack(t, "minecraft:diamond_pickaxe", "minecraft:mending", 1)
	pick = withMaxDamage(pick, 20)
	pick.Damage = 19
	srv.GiveSlot("Mender", player.HotbarSlot, pick)

	w := servertest.NewOrbWatcher(t, c)
	srv.SpawnOrb(0.5, 64, 0.5, 10)
	w.ReadUntilTicks(srv, 240, func() bool {
		_, told := w.Slots[player.HotbarSlot]
		return len(w.Takes) > 0 && told
	})
	if got := srv.InventorySlot("Mender", player.HotbarSlot); got.Damage != 0 {
		t.Errorf("the pickaxe's wear after mending = %d, want 0", got.Damage)
	}
}
