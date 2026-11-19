package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// survivalPlugin is a plugin server with a joined survival player named
// Walker standing in the loaded chunk.
func survivalPlugin(t *testing.T) (*server.Server, plugin.Server, *servertest.Client, func(func())) {
	t.Helper()
	var host plugin.Server
	srv := servertest.NewTestServerWithPlugins(t, servertest.SurvivalTestConfig(), []plugin.Registration{{Name: "mobs", Plugin: &testPlugin{enable: func(s plugin.Server) error { host = s; return nil }}}})
	c := servertest.DialServer(t, srv)
	c.JoinAs("Walker")
	srv.OverworldWorld().SetChunkForced(world.ChunkPos{X: 0, Z: 0}, true)
	servertest.HoldTicks(t, srv)
	run := func(f func()) {
		t.Helper()
		ran := false
		host.Scheduler().RunTask(func() { f(); ran = true })
		servertest.RunTicks(t, srv, 1)
		if !ran {
			t.Fatal("plugin task did not complete")
		}
	}
	return srv, host, c, run
}

func TestPluginPlayerActiveItemWhileEating(t *testing.T) {
	t.Parallel()
	srv, host, c, run := survivalPlugin(t)
	p, ok := host.PlayerNamed("Walker")
	if !ok {
		t.Fatal("player missing")
	}
	run(func() {
		if _, _, using := p.ActiveItem(); using {
			t.Error("an idle player uses an item")
		}
	})
	srv.GiveSlot("Walker", player.HotbarSlot, protocol.ItemStack{ItemID: int32(registry.ItemGoldenApple), Count: 1})
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	run(func() {
		item, hand, using := p.ActiveItem()
		if !using || hand != plugin.MainHand || item.Item != registry.ItemGoldenApple {
			t.Errorf("eating: %v %v %v", item, hand, using)
		}
	})
}

func TestPluginMobLeashShowsOnTheClientAndTargetsAPlayer(t *testing.T) {
	t.Parallel()
	_, host, c, run := survivalPlugin(t)
	p, _ := host.PlayerNamed("Walker")
	var cow plugin.Mob
	run(func() {
		cow = spawnAt(t, host.Overworld(), registry.EntityTypeCow, nil).(plugin.Mob)
	})
	c.Settle()
	run(func() {
		cow.SetLeash(p)
		cow.SetTarget(p)
		if h, ok := cow.Leash(); !ok || h.UUID() != p.UUID() {
			t.Errorf("leash %v %v", h, ok)
		}
		if tg, ok := cow.Target(); !ok || tg.UUID() != p.UUID() {
			t.Errorf("target %v %v", tg, ok)
		}
	})
	if attached, holding := c.ExpectLinkPacket(); attached != cow.ID() || holding != p.ID() {
		t.Errorf("link (%d, %d), want the cow %d on the player %d", attached, holding, cow.ID(), p.ID())
	}
	run(func() {
		cow.SetTarget(nil)
		if _, ok := cow.Target(); ok {
			t.Error("target survived being cleared")
		}
	})
}
