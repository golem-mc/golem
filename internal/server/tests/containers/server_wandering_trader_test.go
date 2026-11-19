package containers

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Right-clicking a wandering trader draws its two buying, two uncommon and
// five common trades and opens them at level 1, with no progress bar and no
// restock; a trade gives the trader no experience.
// vanilla: WanderingTrader.mobInteract, WanderingTrader.updateTrades
func TestRightClickingAWanderingTraderOpensItsDrawnTrades(t *testing.T) {
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	w := servertest.WindowedOf(t, "Buyer", connect())
	trader := srv.SpawnMob("minecraft:wandering_trader", 1.5, 64, 0.5)
	w.UseOn(trader)

	var menu int32 = -1
	var offers []byte
	w.SettleAndCollect(func(id int32, data []byte) {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlayOpenScreen:
			w.Window = r.ReadVarInt()
			menu = r.ReadVarInt()
		case protocol.IDPlayMerchantOffers:
			offers = data
		}
	})
	if menu != protocol.MenuMerchant {
		t.Fatalf("open screen menu = %d, want the merchant's %d", menu, protocol.MenuMerchant)
	}
	r := protocol.NewReader(offers)
	r.ReadVarInt() // window
	if n := r.ReadVarInt(); n != 9 {
		t.Errorf("%d offers sent, want 2 + 2 + 5", n)
	}
	// The packet ends with level 1, experience 0, no progress bar, no restock.
	if tail := offers[len(offers)-4:]; !bytes.Equal(tail, []byte{1, 0, 0, 0}) {
		t.Errorf("offers packet ends % x, want level 1, xp 0, false, false", tail)
	}

	saved, _ := srv.VillagerSaved(trader)
	recipes, _ := nbt.Get[nbt.List](saved, "Recipes")
	if len(recipes.Items) != 9 {
		t.Fatalf("%d saved recipes, want 9", len(recipes.Items))
	}
	// The buying set comes first; its trades all pay out emeralds.
	emerald := registry.MustItem("minecraft:emerald")
	first, _ := recipes.Items[0].(nbt.Compound)
	sell, _ := nbt.Get[nbt.Compound](first, "sell")
	if id, _ := nbt.Get[nbt.String](sell, "id"); id != "minecraft:emerald" {
		t.Errorf("first trade sells %q, want an emerald from the buying set", id)
	}

	// Pay for a common trade: they cost emeralds.
	srv.GiveSlot("Buyer", player.HotbarSlot, protocol.ItemStack{ItemID: emerald, Count: 64})
	w.Settle()
	selectTrade(w, 4)
	w.Click(merchantResult, 0, protocol.ClickModePickup)
	w.Settle()
	saved, xp := srv.VillagerSaved(trader)
	recipes, _ = nbt.Get[nbt.List](saved, "Recipes")
	recipe, _ := recipes.Items[4].(nbt.Compound)
	if uses, _ := nbt.Get[nbt.Int](recipe, "uses"); uses != 1 {
		t.Errorf("saved uses %d after one take, want 1", uses)
	}
	if xp != 0 {
		t.Errorf("trader xp %d, want 0", xp)
	}
}
