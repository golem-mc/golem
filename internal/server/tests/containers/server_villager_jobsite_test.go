package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// jobSiteVillager opens a trade window with a farmer standing by a composter
// on a held loop, then removes the composter. It returns once the window is
// open; the villager still has its offers.
func jobSiteVillager(t *testing.T) (*server.Server, *servertest.WindowedClient, int32) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	w := servertest.WindowedOf(t, "Buyer", connect())
	servertest.HoldTicks(t, srv)
	emerald := registry.MustItem("minecraft:emerald")
	srv.GiveSlot("Buyer", player.HotbarSlot, protocol.ItemStack{ItemID: emerald, Count: 4})
	composter, ok := registry.FindState("minecraft:composter", map[string]string{"level": "0"})
	if !ok {
		t.Fatal("no composter state")
	}
	srv.SetBlock(3, 64, 0, composter)
	villager := srv.SpawnVillagerAtJobSite("minecraft:farmer", breadOffer(0), 1.5, 64, 0.5, 3, 64, 0)
	if openTrade(w, villager) != 1 {
		t.Fatal("the trade window did not open")
	}
	air, _ := registry.FindState("minecraft:air", nil)
	srv.SetBlock(3, 64, 0, air)
	return srv, w, villager
}
