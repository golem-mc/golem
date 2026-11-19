package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The tests below settle after the packets they send rather than count
// ticks: the loop is held, and a ping coming back is what says the server
// handled what was sent before it.

// givenWindowed joins an operator in survival with the smallest view, has
// them /give themselves item, which lands in the selected hotbar slot, and
// holds the loop.
func givenWindowed(t *testing.T, cfg *config.Config, item string) (*server.Server, *servertest.WindowedClient) {
	t.Helper()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.ViewAsk = config.MinDistance
	w := servertest.WindowedOf(t, "Testificate", c)
	w.ChatCommand("give @s " + item)
	servertest.AwaitSlotUpdates(t, c, 1)
	// The give's pickup sound and the rest of its traffic.
	w.Settle()
	if srv.InventorySlot("Testificate", player.HotbarSlot).Empty() {
		t.Fatalf("/give @s %s left the hand empty", item)
	}
	servertest.HoldTicks(t, srv)
	return srv, w
}

// expectSlot fails unless the window slot holds one of the item, given by
// name, or is empty for "".
func expectSlot(t *testing.T, srv *server.Server, slot int, item string) {
	t.Helper()
	got := srv.InventorySlot("Testificate", slot)
	if item == "" {
		if !got.Empty() {
			t.Errorf("slot %d = %+v, want empty", slot, got)
		}
		return
	}
	if got.ItemID != servertest.MustItemID(t, item) || got.Count != 1 {
		t.Errorf("slot %d = %+v, want one %s", slot, got, item)
	}
}

// TestGivenEquippableStickIsWornByUse: a stick given a head equippable goes
// on the head when used, with the component's equip sound.
// vanilla: Item.use, Equippable.swapWithEquipmentSlot
func TestGivenEquippableStickIsWornByUse(t *testing.T) {
	t.Parallel()
	srv, w := givenWindowed(t, servertest.SurvivalTestConfig(), `stick[equippable={slot:"head"}]`)

	w.UseItemRightClicks(protocol.HandMain)
	expectEquipSound(t, srv, w, "minecraft:item.armor.equip_generic")
	w.Settle()
	expectSlot(t, srv, player.ArmorSlot, "minecraft:stick")
	expectSlot(t, srv, player.HotbarSlot, "")
}

// TestGivenEquippableStickFitsTheHelmetSlot: a click puts the stick with a
// head equippable into the helmet slot, where a plain stick is refused.
// vanilla: ArmorSlot.mayPlace, LivingEntity.isEquippableInSlot
func TestGivenEquippableStickFitsTheHelmetSlot(t *testing.T) {
	t.Parallel()
	srv, w := givenWindowed(t, servertest.SurvivalTestConfig(), `stick[equippable={slot:"head"}]`)
	srv.GiveSlot("Testificate", player.MainSlot, servertest.ItemStack(t, "minecraft:stick", 1))
	w.Settle()

	w.Click(player.MainSlot, 0, protocol.ClickModePickup)
	w.Click(player.ArmorSlot, 0, protocol.ClickModePickup)
	w.Settle()
	expectSlot(t, srv, player.ArmorSlot, "")
	w.Click(player.MainSlot, 0, protocol.ClickModePickup)

	w.Click(player.HotbarSlot, 0, protocol.ClickModePickup)
	w.Click(player.ArmorSlot, 0, protocol.ClickModePickup)
	w.Settle()
	expectSlot(t, srv, player.ArmorSlot, "minecraft:stick")
	expectSlot(t, srv, player.HotbarSlot, "")
	if _, own := srv.InventorySlot("Testificate", player.ArmorSlot).EquippableOf(); !own {
		t.Error("the worn stick is not the given one")
	}
}

// TestGivenEquippableSlotOverridesTheItems: a leather helmet given a chest
// equippable goes on the chest when used.
func TestGivenEquippableSlotOverridesTheItems(t *testing.T) {
	t.Parallel()
	srv, w := givenWindowed(t, servertest.SurvivalTestConfig(), `leather_helmet[equippable={slot:"chest"}]`)

	w.UseItemRightClicks(protocol.HandMain)
	w.Settle()
	expectSlot(t, srv, player.ArmorSlot+1, "minecraft:leather_helmet")
	expectSlot(t, srv, player.ArmorSlot, "")
	expectSlot(t, srv, player.HotbarSlot, "")
}

// TestGivenUnswappableEquippableStaysInHand: swappable:false keeps the stick
// in hand when used.
// vanilla: Item.use
func TestGivenUnswappableEquippableStaysInHand(t *testing.T) {
	t.Parallel()
	srv, w := givenWindowed(t, servertest.SurvivalTestConfig(), `stick[equippable={slot:"head",swappable:false}]`)

	w.UseItemRightClicks(protocol.HandMain)
	w.Settle()
	expectSlot(t, srv, player.ArmorSlot, "")
	expectSlot(t, srv, player.HotbarSlot, "minecraft:stick")
}

// TestGivenEquippableSurvivesARestart: the stack's own equippable reaches the
// player file and comes back, so the stick still goes on the head with its
// own sound.
// vanilla: Equippable.CODEC
func TestGivenEquippableSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	srv, _ := givenWindowed(t, cfg, `stick[equippable={slot:"head",equip_sound:"item.armor.equip_gold",dispensable:false,allowed_entities:["player","zombie"]}]`)
	given := srv.InventorySlot("Testificate", player.HotbarSlot)
	srv.Shutdown()

	restarted := *cfg
	srv, again := servertest.Dial(t, &restarted, servertest.OpsListing(t, "Testificate"))
	w := servertest.WindowedOf(t, "Testificate", again)
	kept := srv.InventorySlot("Testificate", player.HotbarSlot)
	if !kept.Same(given) {
		t.Fatalf("the rejoined hand holds %+v, want %+v", kept, given)
	}
	if eq, ok := registry.StackEquippable(kept); !ok || eq.Dispensable || eq.Entities != "minecraft:player,minecraft:zombie" {
		t.Errorf("the rejoined equippable = %+v, %v", eq, ok)
	}
	servertest.HoldTicks(t, srv)
	w.UseItemRightClicks(protocol.HandMain)
	expectEquipSound(t, srv, w, "minecraft:item.armor.equip_gold")
	w.Settle()
	expectSlot(t, srv, player.ArmorSlot, "minecraft:stick")
}

// TestGivenEquippableStackWearsOneAndStowsTheOldPiece: using a stack of
// equippable sticks over a worn piece wears one, keeps the rest in hand,
// returns the old piece to the inventory and plays the equip sound.
// vanilla: Equippable.swapWithEquipmentSlot, ItemStack.consumeAndReturn
func TestGivenEquippableStackWearsOneAndStowsTheOldPiece(t *testing.T) {
	t.Parallel()
	srv, w := givenWindowed(t, servertest.SurvivalTestConfig(), `stick[equippable={slot:"head"}] 5`)
	srv.GiveSlot("Testificate", player.ArmorSlot, servertest.ItemStack(t, "minecraft:golden_helmet", 1))
	w.Settle()

	w.UseItemRightClicks(protocol.HandMain)
	expectEquipSound(t, srv, w, "minecraft:item.armor.equip_generic")
	w.Settle()
	expectSlot(t, srv, player.ArmorSlot, "minecraft:stick")
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); got.ItemID != servertest.MustItemID(t, "minecraft:stick") || got.Count != 4 {
		t.Errorf("hand = %+v, want four sticks", got)
	}
	helmet := servertest.MustItemID(t, "minecraft:golden_helmet")
	found := false
	for slot := player.MainSlot; slot <= player.HotbarSlot+8; slot++ {
		if srv.InventorySlot("Testificate", slot).ItemID == helmet {
			found = true
		}
	}
	if !found {
		t.Error("the old helmet did not return to the inventory")
	}
}
