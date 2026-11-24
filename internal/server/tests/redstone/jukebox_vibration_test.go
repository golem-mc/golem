package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// A record entering or leaving a jukebox is a block_change vibration
// (frequency 11) heard by a sensor.
// vanilla: JukeboxBlockEntity.notifyItemChangedInJukebox, JukeboxPlayable.tryInsertIntoJukebox
func TestJukeboxDiscChangeVibration(t *testing.T) {
	t.Parallel()
	srv, c := survivalScene(t)
	srv.SetBlock(2, 64, 4, servertest.MustState(t, "minecraft:jukebox", map[string]string{"has_record": "false"}))
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:music_disc_cat"), Count: 1})
	c.Settle()
	placeSensor(t, srv)

	heard := func(step string) {
		t.Helper()
		servertest.UntilTicks(t, srv, 10, "the sensor to hear the "+step, func() bool {
			return registry.StateValue(srv.BlockAt(4, 64, 4), registry.PropSculkSensorPhase) == "active"
		})
		tag, _ := srv.BlockEntityTagAt(4, 64, 4)
		if got, _ := nbt.IntField(tag, "last_vibration_frequency"); got != 11 {
			t.Errorf("%s gave frequency %d, want 11", step, got)
		}
		// Active 30 ticks, then cooldown 10.
		servertest.RunTicks(t, srv, 45)
		expectSensor(t, srv, 4, 64, 4, "inactive", "0")
	}

	c.PlaceBlock(2, 64, 4, protocol.FaceTop)
	c.Settle()
	heard("insertion")

	c.PlaceBlock(2, 64, 4, protocol.FaceTop)
	c.Settle()
	heard("ejection")
}
