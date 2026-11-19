package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
)

// vanilla: ServerLevel.getPlayers, PlayerList.remove.
func TestPluginWorldPlayerSnapshots(t *testing.T) {
	srv, h, run, c := worldEffectsPlugin(t)
	var snapshot []plugin.Player
	run(func() {
		snapshot = h.Overworld().Players()
		p, ok := h.PlayerNamed("Effects")
		if len(snapshot) != 1 || !ok || snapshot[0] != p {
			t.Error("world player handle lost its connection or plugin ownership")
		}
		end, _ := h.World(plugin.MinecraftKey("the_end"))
		if len(end.Players()) != 0 {
			t.Error("overworld player leaked into another dimension")
		}
	})
	servertest.ReleaseTicks(t, srv)
	other := servertest.DialServer(t, srv)
	other.ViewAsk = config.MinDistance
	other.Join("Visitor")
	servertest.HoldTicks(t, srv)
	c.ReadTicks(srv, 0, func(int32, []byte) bool { return false })
	run(func() {
		players := h.Overworld().Players()
		if len(snapshot) != 1 || len(players) != 2 || players[0] != snapshot[0] || players[1].Name() != "Visitor" {
			t.Error("world player snapshot or join order changed")
		}
		players[0] = nil
		if h.Overworld().Players()[0] == nil {
			t.Error("mutating a snapshot changed the live player list")
		}
		snapshot[0].SendMessage(plugin.Plain("world handle"))
	})
	r := protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat))
	text, ok := protocol.ReadText(r)
	if !ok || text.Plain() != "world handle" {
		t.Fatal("world player handle did not send its message")
	}
	other.Disconnect()
	run(func() {
		if players := h.Overworld().Players(); len(players) != 1 || players[0] != snapshot[0] {
			t.Error("disconnected player remained in world snapshot")
		}
	})
	c.Disconnect()
	run(func() {
		if len(h.Overworld().Players()) != 0 || snapshot[0].IsValid() {
			t.Error("world handles did not expire after disconnect")
		}
	})
}

func TestPluginWorldPlayersFollowDimension(t *testing.T) {
	srv, h, run, c := worldEffectsPlugin(t)
	srv.MovePlayerToDimension("Effects", world.EndName, 100.5, 50, .5)
	c.ExpectWithinTicks(srv, 0, protocol.IDPlayRespawn)
	var moved bool
	task := h.Scheduler().RunTimer(1, 1, func() {
		end, _ := h.World(plugin.MinecraftKey("the_end"))
		players := end.Players()
		moved = len(players) == 1 && players[0].Name() == "Effects"
	})
	defer task.Cancel()
	servertest.UntilTicks(t, srv, 1000, "player joining the End", func() bool {
		c.ReadTicks(srv, 0, func(int32, []byte) bool { return false })
		return moved
	})
	run(func() {
		if len(h.Overworld().Players()) != 0 {
			t.Error("departed player remained in old world")
		}
		end, _ := h.World(plugin.MinecraftKey("the_end"))
		p, _ := h.PlayerNamed("Effects")
		if got := end.Players(); len(got) != 1 || got[0] != p {
			t.Error("dimension travel replaced the connection-bound handle")
		}
	})
}
