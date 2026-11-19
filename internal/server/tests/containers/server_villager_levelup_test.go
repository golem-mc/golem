package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// A trade that brings a villager to its next level's experience drops the
// bonus orb and levels the villager up at once: it draws that level's trades,
// gets regeneration, and the open window is sent the new offers.
// vanilla: Villager.rewardTradeXp, Villager.increaseMerchantCareer,
// Villager.updateSpecialPrices
func TestATradeLevelsUpAVillagerAndRefreshesTheOpenWindow(t *testing.T) {
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	w := servertest.WindowedOf(t, "Buyer", connect())
	emerald := registry.MustItem("minecraft:emerald")
	srv.GiveSlot("Buyer", player.HotbarSlot, protocol.ItemStack{ItemID: emerald, Count: 4})
	offers := breadOffer(0)
	recipes, _ := nbt.Get[nbt.List](offers, "Recipes")
	recipes.Items[0].(nbt.Compound)["xp"] = nbt.Int(10) // farmer level 1 needs 10
	villager := srv.SpawnVillagerWithOffers("minecraft:farmer", 1, offers, 1.5, 64, 0.5)
	servertest.HoldTicks(t, srv)

	if n := openTrade(w, villager); n != 1 {
		t.Fatalf("opened %d windows, want 1", n)
	}
	selectTrade(w, 0)
	w.Click(merchantResult, 0, protocol.ClickModePickup)
	happy := false
	var sent []merchantOffers
	w.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			if r.ReadI32() == villager && r.ReadU8() == protocol.EntityStatusVillagerHappy {
				happy = true
			}
		case protocol.IDPlayMerchantOffers:
			sent = append(sent, readMerchantOffers(t, data))
		}
	})
	if !happy {
		t.Error("the trade sent no happy-villager entity event")
	}
	if len(sent) != 1 {
		t.Fatalf("the trade sent %d merchant offers packets, want 1", len(sent))
	}
	if got := sent[0]; got.window != w.Window || got.level != 2 || got.xp != 10 || got.offers != 3 {
		t.Errorf("merchant offers %+v, want window %d at level 2 with 10 xp and 3 offers (the bread and farmer level 2's two)", got, w.Window)
	}
	if level := srv.VillagerLevel(villager); level != 2 {
		t.Errorf("level %d with the window open, want 2", level)
	}
	if d, amp, ok := srv.MobEffectOf(villager, "minecraft:regeneration"); !ok || amp != 0 || d < 190 {
		t.Errorf("regeneration = %d ticks, amplifier %d, present %v; want 200 at amplifier 0", d, amp, ok)
	}
	orbs := srv.OrbsInWorld()
	if len(orbs) != 1 || orbs[0].XP < 8 || orbs[0].XP > 11 || orbs[0].Y != 64.5 {
		t.Errorf("orbs after the trade %+v, want one of 8-11 xp (3-6 plus the level-up 5) at y 64.5", orbs)
	}
	saved, xp := srv.VillagerSaved(villager)
	if xp != 10 {
		t.Errorf("villager xp %d, want 10", xp)
	}
	if got, _ := nbt.Get[nbt.List](saved, "Recipes"); len(got.Items) != 3 {
		t.Errorf("%d saved recipes, want the bread and farmer level 2's two", len(got.Items))
	}
}

// The offers a level-up adds are discounted for the trading player like the
// ones the window opened with.
// vanilla: Villager.updateTrades, Villager.updateSpecialPrices
func TestALevelUpDiscountsTheNewOffersForAHero(t *testing.T) {
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	w := servertest.WindowedOf(t, "Buyer", connect())
	srv.GiveEffect("Buyer", "minecraft:hero_of_the_village", 6000, 0, false)
	srv.GiveSlot("Buyer", player.HotbarSlot, protocol.ItemStack{ItemID: registry.MustItem("minecraft:emerald"), Count: 4})
	offers := breadOffer(0)
	recipes, _ := nbt.Get[nbt.List](offers, "Recipes")
	recipes.Items[0].(nbt.Compound)["xp"] = nbt.Int(10)
	villager := srv.SpawnVillagerWithOffers("minecraft:farmer", 1, offers, 1.5, 64, 0.5)
	servertest.HoldTicks(t, srv)

	if n := openTrade(w, villager); n != 1 {
		t.Fatalf("opened %d windows, want 1", n)
	}
	selectTrade(w, 0)
	w.Click(merchantResult, 0, protocol.ClickModePickup)
	var sent []merchantOffers
	w.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayMerchantOffers {
			sent = append(sent, readMerchantOffers(t, data))
		}
	})
	if len(sent) != 1 || len(sent[0].specialPrices) != 3 {
		t.Fatalf("merchant offers after the level-up %+v, want one packet of 3 offers", sent)
	}
	// Hero at amplifier 0 takes max(floor(0.3*cost), 1): 1 off the bread's
	// 2 emeralds and off each farmer level 2 cost (6 pumpkins, 1 emerald).
	// Reputation is still 0: the TRADE gossip lands on the next tick.
	for i, p := range sent[0].specialPrices {
		if p != -1 {
			t.Errorf("offer %d special price %d, want -1", i, p)
		}
	}
}

// merchantOffers is what a Merchant Offers packet says beyond each offer.
type merchantOffers struct {
	window, offers, level, xp int32
	specialPrices             []int32
}

// readMerchantOffers decodes a Merchant Offers packet whose costs carry no
// component predicate.
// vanilla: ClientboundMerchantOffersPacket, MerchantOffer.writeToStream
func readMerchantOffers(t *testing.T, data []byte) merchantOffers {
	t.Helper()
	r := protocol.NewReader(data)
	got := merchantOffers{window: r.ReadVarInt(), offers: r.ReadVarInt()}
	for range got.offers {
		readCost := func() {
			r.ReadVarInt() // item
			r.ReadVarInt() // count
			if n := r.ReadVarInt(); n != 0 {
				t.Fatalf("cost with %d predicate components", n)
			}
		}
		readCost()
		r.ReadSlot()
		if r.ReadBool() {
			readCost()
		}
		r.ReadBool() // out of stock
		r.ReadI32()  // uses
		r.ReadI32()  // max uses
		r.ReadI32()  // xp
		got.specialPrices = append(got.specialPrices, r.ReadI32())
		r.ReadI32() // price multiplier
		r.ReadI32() // demand
	}
	got.level, got.xp = r.ReadVarInt(), r.ReadVarInt()
	if err := r.Err(); err != nil {
		t.Fatalf("merchant offers: %v", err)
	}
	return got
}
