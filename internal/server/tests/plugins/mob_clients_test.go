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

func TestPluginVillagerTraderIsTheTradingPlayer(t *testing.T) {
	srv, host, c, run := survivalPlugin(t)
	p, _ := host.PlayerNamed("Walker")
	var v plugin.Villager
	run(func() {
		w := host.Overworld()
		e, err := w.Spawn(registry.EntityTypeVillager, plugin.Location{World: w, Pos: plugin.Vec3{X: 1.5, Y: 64, Z: 0.5}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		v = e.(plugin.Villager)
		// A villager with no job site and no experience drops its profession.
		v.SetProfession(plugin.MinecraftKey("librarian"))
		v.SetLevel(2)
		v.SetTrades([]plugin.Trade{{
			Cost:   plugin.ItemStack{Item: registry.ItemEmerald, Count: 5},
			Result: plugin.ItemStack{Item: registry.ItemDiamond, Count: 1}, MaxUses: 9, XP: 1,
		}})
		if _, trading := v.Trader(); trading {
			t.Error("idle villager has a trader")
		}
	})
	c.Settle()
	c.UseOn(v.ID())
	servertest.RunTicks(t, srv, 2)
	offers := false
	c.SettleAndCollect(func(id int32, _ []byte) { offers = offers || id == protocol.IDPlayMerchantOffers })
	if !offers {
		t.Fatal("the client got no merchant offers")
	}
	run(func() {
		if tr, ok := v.Trader(); !ok || tr.UUID() != p.UUID() {
			t.Errorf("trader %v %v", tr, ok)
		}
	})
}

func TestPluginMoveToHoldsTheGoalsUntilTheWalkEnds(t *testing.T) {
	srv, host, _, run := survivalPlugin(t)
	var husk plugin.Mob
	run(func() {
		w := host.Overworld()
		e, err := w.Spawn(registry.EntityTypeHusk, plugin.Location{World: w, Pos: plugin.Vec3{X: 6.5, Y: 64, Z: 0.5}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		husk = e.(plugin.Mob)
		husk.SetPersistent(true)
		if !husk.MoveTo(plugin.Vec3{X: 6.5, Y: 64, Z: 12.5}, 1) {
			t.Fatal("no path on open ground")
		}
	})
	// The hunt goals would pick the player a few ticks in; they wait.
	servertest.RunTicks(t, srv, 60)
	run(func() {
		if _, ok := husk.Target(); ok {
			t.Error("the husk hunted during the ordered walk")
		}
		if z := husk.Pos().Z; z < 5 {
			t.Errorf("the husk is at z=%v, not walking", z)
		}
	})
	servertest.RunTicks(t, srv, 160)
	run(func() {
		if _, ok := husk.Target(); !ok {
			t.Error("the goals did not resume after the walk")
		}
		if z := husk.Pos().Z; z > 11 {
			t.Errorf("the husk is still at z=%v, not hunting the player", z)
		}
	})
}
