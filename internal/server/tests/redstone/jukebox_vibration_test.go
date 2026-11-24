package redstone

import (
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// A record entering or leaving a jukebox is a block_change vibration
// (frequency 11) heard by a calibrated sensor tuned to 11. Insertion posts a
// source-free event for the state change and a player-attributed one for the
// use; ejection posts only the source-free one, and an empty slot none.
// vanilla: JukeboxBlockEntity.notifyItemChangedInJukebox, JukeboxPlayable.tryInsertIntoJukebox
func TestJukeboxDiscChangeVibration(t *testing.T) {
	t.Parallel()
	srv, c := survivalScene(t)
	srv.SetBlock(2, 64, 4, servertest.MustState(t, "minecraft:jukebox", map[string]string{"has_record": "false"}))
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:music_disc_cat"), Count: 1})
	c.Settle()
	calibratedAt(t, srv, 4)
	srv.SetBlock(4, 64, 5, servertest.MustState(t, "minecraft:target", map[string]string{"power": "11"}))

	var sourceFree, attributed atomic.Int32
	srv.ObserveGameEventContexts(func(event string, x, y, z float64, ctx rules.GameEventContext) {
		if event != "minecraft:block_change" || x != 2.5 || y != 64.5 || z != 4.5 {
			return
		}
		if ctx.Player == uuid.Nil {
			sourceFree.Add(1)
		} else {
			attributed.Add(1)
		}
	})

	heard := func(step string) {
		t.Helper()
		servertest.UntilTicks(t, srv, 10, "the sensor to hear the "+step, func() bool {
			return registry.StateValue(srv.BlockAt(4, 64, 4), registry.PropSculkSensorPhase) == "active"
		})
		tag, _ := srv.BlockEntityTagAt(4, 64, 4)
		if got, _ := nbt.IntField(tag, "last_vibration_frequency"); got != 11 {
			t.Errorf("%s gave frequency %d, want 11", step, got)
		}
	}
	quiet := func() {
		t.Helper()
		// Ten active ticks, then ten of cooldown.
		servertest.UntilTicks(t, srv, 25, "the sensor to settle", func() bool {
			return registry.StateValue(srv.BlockAt(4, 64, 4), registry.PropSculkSensorPhase) == "inactive"
		})
	}

	c.PlaceBlock(2, 64, 4, protocol.FaceTop)
	c.Settle()
	heard("insertion")
	if sourceFree.Load() != 1 || attributed.Load() != 1 {
		t.Errorf("insertion posted %d source-free and %d player events, want 1 and 1", sourceFree.Load(), attributed.Load())
	}
	quiet()

	c.PlaceBlock(2, 64, 4, protocol.FaceTop)
	c.Settle()
	heard("ejection")
	if sourceFree.Load() != 2 || attributed.Load() != 1 {
		t.Errorf("after ejection %d source-free and %d player events, want 2 and 1", sourceFree.Load(), attributed.Load())
	}
}

// A click on a jukebox that claims a record but holds none ejects nothing and
// posts no vibration.
// vanilla: JukeboxBlock.useWithoutItem, JukeboxBlockEntity.popOutTheItem
func TestEmptyJukeboxClickIsSilent(t *testing.T) {
	t.Parallel()
	srv, c := survivalScene(t)
	srv.SetBlock(2, 64, 4, servertest.MustState(t, "minecraft:jukebox", map[string]string{"has_record": "true"}))
	c.Settle()
	var events atomic.Int32
	srv.ObserveGameEvents(func(event string, x, y, z float64) {
		if event == "minecraft:block_change" && x == 2.5 && z == 4.5 {
			events.Add(1)
		}
	})
	c.PlaceBlock(2, 64, 4, protocol.FaceTop)
	c.Settle()
	servertest.RunTicks(t, srv, 5)
	if got := events.Load(); got != 0 {
		t.Errorf("the click posted %d block_change events, want 0", got)
	}
	if got := registry.StateBool(srv.BlockAt(2, 64, 4), registry.PropHasRecord); !got {
		t.Error("the click cleared has_record")
	}
}
