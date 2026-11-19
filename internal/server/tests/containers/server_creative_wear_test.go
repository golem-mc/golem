package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestCreativeSlotWearLimitSparesUnbreakableStacks checks that a creative
// client's stack worn to its max damage is refused, while an unbreakable one
// is kept: only a damageable item counts as broken.
func TestCreativeSlotWearLimitSparesUnbreakableStacks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder")

	pickaxe := servertest.ItemStack(t, "minecraft:iron_pickaxe", 1)
	pickaxe.Damage = registry.MaxDamage(pickaxe.ItemID)

	c.SetCreativeSlot(player.HotbarSlot, pickaxe)
	c.Settle()
	if got := srv.InventorySlot("Builder", player.HotbarSlot); !got.Empty() {
		t.Errorf("hotbar after a broken pickaxe = %+v, want empty", got)
	}

	pickaxe.Unbreakable = true
	c.SetCreativeSlot(player.HotbarSlot, pickaxe)
	c.Settle()
	got := srv.InventorySlot("Builder", player.HotbarSlot)
	if got.ItemID != pickaxe.ItemID || got.Damage != pickaxe.Damage || !got.Unbreakable {
		t.Errorf("hotbar after an unbreakable worn pickaxe = %+v, want it kept", got)
	}
}
