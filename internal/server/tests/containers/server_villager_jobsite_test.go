package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
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

// A fresh villager that loses its job site loses its profession and its
// trades, so the window it has open closes instead of trading on.
// vanilla: Villager.customServerAiStep, ResetProfession, AbstractVillager.stillValid
func TestAVillagerLosingItsJobSiteClosesItsTradeWindow(t *testing.T) {
	srv, w, villager := jobSiteVillager(t)
	closed := false
	w.ReadTicks(srv, 3, func(id int32, _ []byte) bool {
		closed = closed || id == protocol.IDPlayCloseContainer
		return false
	})
	if !closed {
		t.Fatal("the trade window stayed open after the job site was removed")
	}
	offers, _ := srv.VillagerSaved(villager)
	if recipes, _ := nbt.Get[nbt.List](offers, "Recipes"); len(recipes.Items) != 0 {
		t.Errorf("the villager kept %d offers after losing its profession", len(recipes.Items))
	}
}

// A take from a window whose villager has just lost its offers still pays
// its experience and neither panics nor stalls the server.
// vanilla: AbstractVillager.notifyTrade, MerchantResultSlot.onTake
func TestATakeAfterTheVillagerLostItsOffersStillPaysTheTrade(t *testing.T) {
	srv, w, villager := jobSiteVillager(t)
	selectTrade(w, 0)
	srv.TickMobs(2) // the mob pass only: the window is not validated yet
	if offers, _ := srv.VillagerSaved(villager); offers != nil {
		t.Fatal("the villager kept its offers")
	}
	w.Click(merchantResult, 0, protocol.ClickModePickup)
	w.Settle()
	offers, xp := srv.VillagerSaved(villager)
	if offers != nil {
		t.Errorf("the take wrote offers %v back to a villager without a profession", offers)
	}
	if xp != 5 {
		t.Errorf("villager xp %d, want the trade's 5", xp)
	}
	srv.TickOpenWindow("Buyer")
	w.Settle()
}
