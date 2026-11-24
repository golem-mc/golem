package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
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
			w.ChatCommand("summon minecraft:horse 1.5 64 0.5 {Tame:1b}")
			var horse int32 = -1
			for id, data := range w.Packets(servertest.ExpectTimeout) {
				if id != protocol.IDPlaySpawnEntity {
					continue
				}
				r := protocol.NewReader(data)
				entity := r.ReadVarInt()
				r.ReadUUID()
				if r.ReadVarInt() == protocol.EntityTypeHorse {
					horse = entity
					break
				}
			}
			if horse < 0 {
				t.Fatal("no horse spawned")
			}
			w.UseOnSneaking(horse)
			for id, data := range w.Packets(servertest.ExpectTimeout) {
				if id == protocol.IDPlayMountScreenOpen {
					w.Window = protocol.NewReader(data).ReadVarInt()
					break
				}
			}
			w.ExpectWindowContent()
		}, 2 + 27},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Buyer", "Breaker"))
			w := servertest.WindowedOf(t, "Buyer", connect())
			breaker := connect()
			breaker.Join("Breaker")
			srv.GiveSlot("Buyer", player.HotbarSlot, stone)
			srv.PlaceContainer(0, 0, 0, "minecraft:chest")
			tc.open(t, srv, w)
			w.Settle()
			breaker.Settle()

			servertest.HoldTicks(t, srv)
			changed := servertest.PacketIs(protocol.IDPlayContainerSlot, protocol.IDPlayContainerContent, protocol.IDPlayCloseContainer)
			breaker.ChatCommand("item replace block 0 0 0 container.0 with minecraft:stone")
			breaker.Settle()
			w.ExpectNoneWithinTicks(srv, 2, changed)
			breaker.ChatCommand("setblock 0 0 0 minecraft:air destroy")
			breaker.Settle()
			w.ExpectNoneWithinTicks(srv, 2, changed)

			w.Click(tc.hotbar, 0, protocol.ClickModePickup)
			cursor, _ := protocol.NewReader(w.ExpectWithinTicks(srv, 2, protocol.IDPlaySetCursorItem)).ReadSlot()
			if !cursor.Equal(stone) {
				t.Errorf("cursor after the click = %+v, want the stone from the hotbar", cursor)
			}
		})
	}
}
