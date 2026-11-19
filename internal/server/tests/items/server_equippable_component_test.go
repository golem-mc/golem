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
