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

// sensorBeside puts a sculk sensor 5 blocks from the player and returns its
// cell.
func sensorBeside(t *testing.T, srv *server.Server, name string) [3]int32 {
	t.Helper()
	at := srv.PlayerPosition(name)
	cell := [3]int32{int32(at.X) + 5, int32(at.Y), int32(at.Z)}
	srv.SetBlock(cell[0], cell[1], cell[2], servertest.MustState(t, "minecraft:sculk_sensor", nil))
	return cell
}

// smallWindowed joins a survival player with the smallest view and simulation
// distance, which the equip tests need no more than.
func smallWindowed(t *testing.T) (*server.Server, *servertest.WindowedClient) {
	t.Helper()
	cfg := servertest.SurvivalTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Dial(t, cfg)
	c.ViewAsk = config.MinDistance
	w := servertest.WindowedOf(t, "Testificate", c)
	servertest.HoldTicks(t, srv)
	return srv, w
}

// expectEquipSound reads the sound sent in the current tick and checks its id
// and that it plays in the players category.
func expectEquipSound(t *testing.T, srv *server.Server, w *servertest.WindowedClient, sound string) {
	t.Helper()
	want, ok := registry.SoundEventID(sound)
	if !ok {
		t.Fatalf("no sound event %s", sound)
	}
	r := protocol.NewReader(w.ExpectWithinTicks(srv, 0, protocol.IDPlaySoundEffect))
	if got := r.ReadVarInt() - 1; got != want {
		t.Errorf("sound id = %d, want %d (%s)", got, want, sound)
	}
	if got := r.ReadVarInt(); got != protocol.SoundCategoryPlayer {
		t.Errorf("sound category = %d, want %d", got, protocol.SoundCategoryPlayer)
	}
}

// TestRightClickEquipPlaysSoundAndPostsEvent: putting on armour from the hand
// plays the piece's equip sound for the wearer and posts the equip event.
// vanilla: LivingEntity.onEquipItem
func TestRightClickEquipPlaysSoundAndPostsEvent(t *testing.T) {
	t.Parallel()
	srv, w := smallWindowed(t)
	cell := sensorBeside(t, srv, "Testificate")
	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:diamond_helmet", 1))

	w.UseItemRightClicks(protocol.HandMain)
	expectEquipSound(t, srv, w, "minecraft:item.armor.equip_diamond")
	servertest.UntilTicks(t, srv, 8, "the sensor to hear the equip", func() bool {
		_, props := servertest.StatePropsOf(t, srv, cell[0], cell[1], cell[2])
		return props["sculk_sensor_phase"] != "inactive"
	})
}

// TestWindowClickEquipPlaysSound: moving a piece into its armour slot by a
// click plays its equip sound too.
func TestWindowClickEquipPlaysSound(t *testing.T) {
	t.Parallel()
	srv, w := smallWindowed(t)
	srv.GiveSlot("Testificate", player.MainSlot, servertest.ItemStack(t, "minecraft:iron_chestplate", 1))

	w.Click(player.MainSlot, 0, protocol.ClickModePickup)
	w.Click(player.ArmorSlot+1, 0, protocol.ClickModePickup)
	expectEquipSound(t, srv, w, "minecraft:item.armor.equip_iron")
}

// TestDragEquipPlaysSound: a single-slot drag onto an armour slot is a pickup
// in vanilla, so it plays the equip sound as a click does.
// vanilla: AbstractContainerMenu.doClick
func TestDragEquipPlaysSound(t *testing.T) {
	t.Parallel()
	srv, w := smallWindowed(t)
	srv.GiveSlot("Testificate", player.MainSlot, servertest.ItemStack(t, "minecraft:iron_helmet", 1))

	w.Click(player.MainSlot, 0, protocol.ClickModePickup)
	w.Click(-999, 0, protocol.ClickModeDrag)
	w.Click(player.ArmorSlot, 1, protocol.ClickModeDrag)
	w.Click(-999, 2, protocol.ClickModeDrag)
	expectEquipSound(t, srv, w, "minecraft:item.armor.equip_iron")
}

// TestRightClickEquipFromOffHand: using a helmet held in the off hand puts it
// on the head.
// vanilla: Equippable.swapWithEquipmentSlot
func TestRightClickEquipFromOffHand(t *testing.T) {
	t.Parallel()
	srv, w := smallWindowed(t)
	srv.GiveSlot("Testificate", player.OffHandSlot, servertest.ItemStack(t, "minecraft:iron_helmet", 1))

	w.UseItemRightClicks(protocol.HandOff)
	expectEquipSound(t, srv, w, "minecraft:item.armor.equip_iron")
	servertest.UntilTicks(t, srv, 8, "the helmet on the head", func() bool {
		return srv.InventorySlot("Testificate", player.ArmorSlot).Count == 1 &&
			srv.InventorySlot("Testificate", player.OffHandSlot).Empty()
	})
}

// TestCreativeEquipKeepsHandStack: in creative the head slot gets a copy of
// a helmet and the hand keeps it.
// vanilla: Equippable.swapWithEquipmentSlot
func TestCreativeEquipKeepsHandStack(t *testing.T) {
	t.Parallel()
	srv, w := smallWindowed(t)
	srv.SetPlayerGameMode("Testificate", protocol.GameModeCreative)
	servertest.RunTicks(t, srv, 2)
	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:iron_helmet", 1))

	w.UseItemRightClicks(protocol.HandMain)
	w.ExpectWithinTicks(srv, 8, protocol.IDPlayContainerSlot)
	if got := srv.InventorySlot("Testificate", player.ArmorSlot); got.Count != 1 {
		t.Errorf("head = %+v, want the helmet", got)
	}
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); got.Count != 1 {
		t.Errorf("hand = %+v, want the helmet kept", got)
	}
}

// TestEquipIdenticalPieceChangesNothing: using a helmet identical to the
// worn one swaps nothing.
// vanilla: Equippable.swapWithEquipmentSlot
func TestEquipIdenticalPieceChangesNothing(t *testing.T) {
	t.Parallel()
	srv, w := smallWindowed(t)
	helmet := servertest.ItemStack(t, "minecraft:iron_helmet", 1)
	srv.GiveSlot("Testificate", player.HotbarSlot, helmet)
	srv.GiveSlot("Testificate", player.ArmorSlot, helmet)

	w.UseItemRightClicks(protocol.HandMain)
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 3)
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); got.Count != 1 {
		t.Errorf("hand = %+v, want the helmet kept", got)
	}
	if got := srv.InventorySlot("Testificate", player.ArmorSlot); got.Count != 1 {
		t.Errorf("head = %+v, want the helmet kept", got)
	}
}
