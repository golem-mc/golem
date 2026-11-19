package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// A stack whose patch removes its item's glider, death_protection or
// damage_resistant keeps the removal through a rejoin and acts as if the
// item had none. vanilla: ItemStack.get returns null for a removed component

// TestAnElytraWithoutGliderStartsNoGlide: a worn elytra[!glider] does not
// open. vanilla: LivingEntity.canGlideUsing
func TestAnElytraWithoutGliderStartsNoGlide(t *testing.T) {
	t.Parallel()
	srv, c := rejoinedWith(t, "elytra[!glider]")
	if stack := srv.InventorySlot("Eater", player.HotbarSlot); !stack.Removes(protocol.DataComponentGlider) {
		t.Fatalf("the reloaded elytra %+v lost its glider removal", stack)
	}
	servertest.UseHeld(c, -90, 0)
	c.Settle()
	servertest.UntilTicks(t, srv, 20, "the elytra worn", func() bool {
		return !srv.InventorySlot("Eater", player.ArmorSlot+1).Empty()
	})

	c.MoveToGrounded(0.5, 66, 0.5, -90, 10, false)
	servertest.HoldTicks(t, srv)
	servertest.StartFallFlying(c)
	servertest.RunTicks(t, srv, 10)
	if srv.PlayerFallFlying("Eater") {
		t.Error("an elytra without glider started a glide")
	}
}

// TestATotemWithoutDeathProtectionDoesNotSave: a held
// totem_of_undying[!death_protection] lets a lethal /damage kill.
// vanilla: LivingEntity.checkTotemDeathProtection
func TestATotemWithoutDeathProtectionDoesNotSave(t *testing.T) {
	t.Parallel()
	srv, c := rejoinedWith(t, "totem_of_undying[!death_protection]")
	if stack := srv.InventorySlot("Eater", player.HotbarSlot); !stack.Removes(protocol.DataComponentDeathProtection) {
		t.Fatalf("the reloaded totem %+v lost its death_protection removal", stack)
	}
	playerID := c.PlayerEntityID

	c.ChatCommand("damage @s 1000 minecraft:generic")
	saved := c.ReadTicks(srv, 20, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityEvent {
			return false
		}
		r := protocol.NewReader(data)
		return r.ReadI32() == playerID && r.ReadU8() == protocol.EntityStatusTotem
	})
	if saved {
		t.Fatal("a totem without death_protection played the totem animation")
	}
	if got := srv.PlayerHealth("Eater"); got > 0 {
		t.Errorf("health after the lethal blow = %v, want 0", got)
	}
}

// TestANetheriteIngotWithoutDamageResistantBurnsInLava: a dropped
// netherite_ingot[!damage_resistant] burns in lava.
// vanilla: ItemStack.canBeHurtBy
func TestANetheriteIngotWithoutDamageResistantBurnsInLava(t *testing.T) {
	t.Parallel()
	srv, _ := rejoinedWith(t, "netherite_ingot[!damage_resistant]")
	stack := srv.InventorySlot("Eater", player.HotbarSlot)
	if !stack.Removes(protocol.DataComponentDamageResistant) {
		t.Fatalf("the reloaded ingot %+v lost its damage_resistant removal", stack)
	}
	if !srv.SetBlock(4, 64, 4, registry.MustState("minecraft:lava", nil)) {
		t.Fatal("the world did not take the lava")
	}
	burned := srv.SpawnItemStack(4.5, 64.5, 4.5, stack)
	servertest.RunTicks(t, srv, 40)
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.ID == burned {
			t.Fatal("the ingot without damage_resistant survived 40 ticks of lava")
		}
	}
}
