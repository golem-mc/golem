package entities

import (
	"slices"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// awaitBoatCargoTitle waits for the chest boat's 9x3 window and returns its title.
func awaitBoatCargoTitle(t *testing.T, srv *server.Server, c *servertest.Client) (protocol.Text, bool) {
	t.Helper()
	var title protocol.Text
	opened := false
	c.ReadTicksSkipping(srv, 20, servertest.BoatSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlayOpenScreen {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadVarInt() // the window ID
		if r.ReadVarInt() != protocol.MenuGeneric9x3 {
			return false
		}
		var ok bool
		if title, ok = protocol.ReadText(r); !ok {
			c.T.Fatal("open screen title did not read back")
		}
		opened = true
		return true
	})
	return title, opened
}

// vanilla: AbstractChestBoat.openCustomInventoryScreen, interact
func TestOpeningChestBoatCargoEmitsContainerOpen(t *testing.T) {
	t.Parallel()
	for _, riding := range []bool{false, true} {
		name := "sneak click"
		if riding {
			name = "inventory key"
		}
		t.Run(name, func(t *testing.T) {
			srv, c, _ := servertest.BoatPlayer(t, "creative", "Captain", 0)
			servertest.HoldTicks(t, srv)
			boatID := srv.SpawnBoat(2.5, 64, 0.5, "minecraft:oak_chest_boat")
			var mu sync.Mutex
			var events [][3]float64
			srv.ObserveGameEventContexts(func(event string, x, y, z float64, ctx rules.GameEventContext) {
				if event == "minecraft:container_open" && ctx.Player == protocol.OfflineUUID("Captain") {
					mu.Lock()
					events = append(events, [3]float64{x, y, z})
					mu.Unlock()
				}
			})
			if riding {
				c.UseOn(boatID)
				c.Settle()
				if !slices.Contains(srv.BoatPassengerIDs(boatID), c.PlayerEntityID) {
					t.Fatal("player did not board the chest boat")
				}
				c.Send(protocol.IDPlayPlayerCommand, func(b *protocol.Buffer) {
					b.PutVarInt(c.PlayerEntityID)
					b.PutVarInt(protocol.PlayerCommandOpenInventory)
					b.PutVarInt(0)
				})
			} else {
				c.UseOnSneaking(boatID)
			}
			title, opened := awaitBoatCargoTitle(t, srv, c)
			if !opened || title.Translate != "entity.minecraft.oak_chest_boat" {
				t.Fatalf("cargo opened=%v, title=%+v, want the oak chest boat's 9x3 menu within 20 ticks", opened, title)
			}
			c.Settle()
			mu.Lock()
			defer mu.Unlock()
			if len(events) != 1 || events[0] != [3]float64{2.5, 64, 0.5} {
				t.Errorf("container open events = %v, want one at the boat with the player as source", events)
			}
		})
	}
}

// TestASpectatorOpensAChestBoatsCargo: a spectator's click on a chest boat
// opens its menu, and boards nothing.
// vanilla: Player.interactOn
func TestASpectatorOpensAChestBoatsCargo(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "spectator", "Ghost", 0)
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_chest_boat")
	c.UseOn(boatID)
	if _, opened := awaitBoatCargoTitle(t, srv, c); !opened {
		t.Fatal("a spectator's click opened nothing")
	}
}
