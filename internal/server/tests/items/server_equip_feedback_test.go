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
