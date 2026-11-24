package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

func TestItemPickupTriesAnotherPlayerInTheSameTick(t *testing.T) {
	t.Parallel()
	for _, frozen := range []bool{false, true} {
		name := "ticking"
		if frozen {
			name = "frozen"
		}
		t.Run(name, func(t *testing.T) {
			names := [2]string{"Alice", "Bob"}
			var full, collector string
			listener := &testPlugin{enable: func(s plugin.Server) error {
				plugin.Listen(s, func(e *plugin.EntityPickupItemEvent) {
					if full != "" {
						return
					}
					// Both players start full. Free a slot for the other player
					// on the first touch so the test does not depend on map order.
					full = e.Entity.(plugin.Player).Name()
					collector = names[0]
					if full == collector {
						collector = names[1]
					}
					p, _ := s.PlayerNamed(collector)
					p.Inventory().SetItem(0, plugin.ItemStack{})
				})
				return nil
			}}
			srv := servertest.NewTestServerWithPlugins(t, servertest.SurvivalTestConfig(),
				[]plugin.Registration{{Name: "pickup", Plugin: listener}})
			clients := make(map[string]*servertest.Client, len(names))
			ids := make(map[string]int32, len(names))
			filler := servertest.ItemStack(t, "minecraft:dirt", 64)
			for i, name := range names {
				c := servertest.DialServer(t, srv)
				ids[name] = c.JoinAs(name)
				clients[name] = c
				c.MoveToGround(0.5+float64(i), 64, 0.5, true)
				c.Settle()
				for slot := 9; slot <= player.OffHandSlot; slot++ {
					srv.GiveSlot(name, slot, filler)
				}
			}

			servertest.HoldTicks(t, srv)
			srv.SetTickFrozen(frozen)
			drop := srv.SpawnItemEntityDelay(1, 64+entity.ItemHalfSize, 0.5, "minecraft:diamond", 1, 0)
			servertest.RunTicks(t, srv, 1)
			if full == "" {
				t.Fatal("neither player touched the drop")
			}
			if got := srv.InventorySlot(full, player.HotbarSlot); !got.Equal(filler) {
				t.Errorf("the full player's hotbar slot held %+v, want the original stack", got)
			}
			want := servertest.ItemStack(t, "minecraft:diamond", 1)
			if got := srv.InventorySlot(collector, player.HotbarSlot); !got.Equal(want) {
				t.Fatalf("after one tick, %s held %+v after %s could not take the drop, want one diamond", collector, got, full)
			}
			c := clients[collector]
			item, who, count := servertest.ReadPickupItem(c, c.ExpectWithinTicks(srv, 0, protocol.IDPlayPickupItem))
			if item != drop || who != ids[collector] || count != 1 {
				t.Errorf("pickup = (%d, %d, %d), want (%d, %d, 1)", item, who, count, drop, ids[collector])
			}
		})
	}
}
