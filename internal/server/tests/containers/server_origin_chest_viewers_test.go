package containers

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// A trade or mount window keeps its block position at the origin, so a chest
// there neither changes nor closes it.
// vanilla: ContainerOpenersCounter.recheckOpeners, ServerPlayer.containerMenu
func TestOriginChestChangesLeaveTradeAndMountWindowsAlone(t *testing.T) {
	t.Parallel()
	stone := protocol.ItemStack{ItemID: registry.MustItem("minecraft:stone"), Count: 1}
	cases := []struct {
		name   string
		open   func(t *testing.T, srv *server.Server, w *servertest.WindowedClient)
		hotbar int16
	}{
		{"trade", func(t *testing.T, srv *server.Server, w *servertest.WindowedClient) {
			villager := srv.SpawnVillagerWithOffers("minecraft:farmer", 2, breadOffer(0), 1.5, 64, 0.5)
			w.UseOn(villager)
			w.SettleAndCollect(func(id int32, data []byte) {
				r := protocol.NewReader(data)
				switch id {
				case protocol.IDPlayOpenScreen:
					w.Window = r.ReadVarInt()
				case protocol.IDPlayContainerContent:
					if r.ReadVarInt() == w.Window {
						w.TrackState(w.Window, r.ReadVarInt())
					}
				}
			})
		}, merchantHotbar0},
		{"horse", func(t *testing.T, srv *server.Server, w *servertest.WindowedClient) {
			horse := srv.SpawnMobTagged("minecraft:horse", 1.5, 64, 0.5, nbt.Compound{"Tame": nbt.Byte(1)})
			w.UseOnSneaking(horse)
			var content bool
			w.ReadTicks(srv, 0, func(id int32, data []byte) bool {
				r := protocol.NewReader(data)
				switch id {
				case protocol.IDPlayMountScreenOpen:
					w.Window = r.ReadVarInt()
				case protocol.IDPlayContainerContent:
					if r.ReadVarInt() == w.Window {
						w.TrackState(w.Window, r.ReadVarInt())
						content = true
					}
				}
				return false
			})
			if !content {
				t.Fatal("no mount content after opening the horse screen")
			}
		}, 2 + 27},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.MobTestConfig()
			cfg.ViewDistance = config.MinDistance
			cfg.SimulationDistance = config.MinDistance
			srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Buyer", "Breaker"))
			w := servertest.WindowedOf(t, "Buyer", connect())
			breaker := connect()
			breaker.Join("Breaker")
			srv.GiveSlot("Buyer", player.HotbarSlot, stone)
			srv.PlaceContainer(0, 0, 0, "minecraft:chest")
			servertest.HoldTicks(t, srv)
			tc.open(t, srv, w)

			changed := servertest.PacketIs(protocol.IDPlayContainerSlot, protocol.IDPlayContainerContent, protocol.IDPlayCloseContainer)
			run := func(command, success string) {
				breaker.ChatCommand(command)
				var ok bool
				breaker.ReadTicks(srv, 0, func(id int32, data []byte) bool {
					ok = ok || id == protocol.IDPlaySystemChat && bytes.Contains(data, []byte(success))
					return false
				})
				if !ok {
					t.Fatalf("%q did not report %s", command, success)
				}
				w.ExpectNoneWithinTicks(srv, 0, changed)
			}
			run("item replace block 0 0 0 container.0 with minecraft:stone", "commands.item.block.replace.success")
			run("setblock 0 0 0 minecraft:air destroy", "commands.setblock.success")

			w.Click(tc.hotbar, 0, protocol.ClickModePickup)
			cursor, _ := protocol.NewReader(w.ExpectWithinTicks(srv, 0, protocol.IDPlaySetCursorItem)).ReadSlot()
			if !cursor.Equal(stone) {
				t.Errorf("cursor after the click = %+v, want the stone from the hotbar", cursor)
			}
		})
	}
}
