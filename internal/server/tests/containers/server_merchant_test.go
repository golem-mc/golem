package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The merchant window: the Open Screen, contents and Merchant Offers a
// trade opens with, the Select Trade that lays a trade's costs in the
// payment slots, and the take that spends them and counts a use.

const (
	merchantPay0    = int16(0)
	merchantPay1    = int16(1)
	merchantResult  = int16(2)
	merchantHotbar0 = int16(3 + (player.HotbarSlot - player.MainSlot))
)

// merchantOpening is what a merchant window's open sent.
type merchantOpening struct {
	menu    int32
	slots   []protocol.ItemStack
	offers  []byte
	offerID int32
}

// openMerchantFor opens a merchant window with the offers for the player
// and reads what the open sent.
func openMerchantFor(t *testing.T, srv *server.Server, w *servertest.WindowedClient, offers []inventory.Offer) merchantOpening {
	t.Helper()
	srv.OpenMerchant("Testificate", offers)
	var got merchantOpening
	w.SettleAndCollect(func(id int32, data []byte) {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlayOpenScreen:
			w.Window = r.ReadVarInt()
			got.menu = r.ReadVarInt()
		case protocol.IDPlayContainerContent:
			if r.ReadVarInt() == w.Window {
				w.TrackState(w.Window, r.ReadVarInt())
				n := r.ReadVarInt()
				for range n {
					s, _ := r.ReadSlot()
					got.slots = append(got.slots, s)
				}
			}
		case protocol.IDPlayMerchantOffers:
			got.offers = data
			got.offerID = r.ReadVarInt()
		}
	})
	if got.menu != protocol.MenuMerchant {
		t.Fatalf("open screen menu = %d, want the merchant's %d", got.menu, protocol.MenuMerchant)
	}
	return got
}

// selectTrade sends Select Trade and reads the slot corrections it answers with.
func selectTrade(w *servertest.WindowedClient, index int32) []servertest.ContainerSlotPacket {
	w.T.Helper()
	w.Send(protocol.IDPlaySelectTradeIn, func(b *protocol.Buffer) { b.PutVarInt(index) })
	return w.CollectContainerSlots()
}

// slotOf is the last stack the packets told for a slot.
func slotOf(packets []servertest.ContainerSlotPacket, slot int16) (protocol.ItemStack, bool) {
	for _, packet := range slices.Backward(packets) {
		if packet.Slot == slot {
			return packet.Stack, true
		}
	}
	return protocol.ItemStack{}, false
}

func TestMerchantWindowOpensWithTheOffers(t *testing.T) {
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	w := servertest.WindowedOf(t, "Testificate", connect())
	emerald, bread := registry.MustItem("minecraft:emerald"), registry.MustItem("minecraft:bread")
	open := openMerchantFor(t, srv, w, []inventory.Offer{{
		CostA: inventory.Cost{ItemID: emerald, Count: 1}, Result: protocol.ItemStack{ItemID: bread, Count: 6},
		MaxUses: 16, XP: 2, PriceMultiplier: 0.05,
	}})
	if len(open.slots) != 3+36 {
		t.Fatalf("the merchant window has %d slots, want 3 and the player's 36", len(open.slots))
	}
	if open.offerID != w.Window {
		t.Errorf("offers addressed window %d, want %d", open.offerID, w.Window)
	}
	var want protocol.Buffer
	want.PutVarInt(w.Window)
	want.PutVarInt(1)
	want.PutVarInt(emerald)
	want.PutVarInt(1)
	want.PutVarInt(0)
	want.PutSlot(protocol.ItemStack{ItemID: bread, Count: 6})
	want.PutBool(false)
	want.PutBool(false)
	want.PutI32(0)
	want.PutI32(16)
	want.PutI32(2)
	want.PutI32(0)
	want.PutF32(0.05)
	want.PutI32(0)
	want.PutVarInt(1)
	want.PutVarInt(0)
	want.PutBool(true)
	want.PutBool(true)
	if string(open.offers) != string(want.Bytes()) {
		t.Errorf("merchant offers = % x, want % x", open.offers, want.Bytes())
	}
}

func TestSelectTradeFillsThePaymentAndTheTakeSpendsIt(t *testing.T) {
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	w := servertest.WindowedOf(t, "Testificate", connect())
	emerald, bread := registry.MustItem("minecraft:emerald"), registry.MustItem("minecraft:bread")
	paper := registry.MustItem("minecraft:paper")
	srv.GiveSlot("Testificate", player.HotbarSlot, protocol.ItemStack{ItemID: emerald, Count: 5})
	srv.GiveSlot("Testificate", player.HotbarSlot+1, protocol.ItemStack{ItemID: paper, Count: 3})
	openMerchantFor(t, srv, w, []inventory.Offer{
		{CostA: inventory.Cost{ItemID: paper, Count: 1}, Result: protocol.ItemStack{ItemID: emerald, Count: 1}, MaxUses: 12, XP: 3},
		{CostA: inventory.Cost{ItemID: emerald, Count: 2}, Result: protocol.ItemStack{ItemID: bread, Count: 6}, MaxUses: 16, XP: 2},
	})

	got := selectTrade(w, 1)
	if pay, _ := slotOf(got, merchantPay0); pay.ItemID != emerald || pay.Count != 5 {
		t.Errorf("payment slot after Select Trade = %+v, want the hotbar's 5 emeralds", pay)
	}
	if res, _ := slotOf(got, merchantResult); res.ItemID != bread || res.Count != 6 {
		t.Errorf("result after Select Trade = %+v, want 6 bread", res)
	}
	if left, ok := slotOf(got, merchantHotbar0); !ok || !left.Empty() {
		t.Errorf("hotbar slot after Select Trade = %+v (told: %v), want empty", left, ok)
	}

	w.Click(merchantResult, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		merchantPay0:   {ItemID: emerald, Count: 3},
		merchantResult: {ItemID: bread, Count: 6},
	}, protocol.ItemStack{ItemID: bread, Count: 6})
	if uses, xp := srv.MerchantState("Testificate"); uses[1] != 1 || uses[0] != 0 || xp != 2 {
		t.Errorf("uses = %v, xp = %d after one trade, want [0 1] and 2", uses, xp)
	}
}

// A merchant with a window open turns to its trading player; a villager
// walks to within two blocks of them, a wandering trader holds still (it only
// stops, so it starts within reach).
// vanilla: LookAndFollowTradingPlayerSink, TradeWithPlayerGoal
func TestMerchantsStayWithTheirTradingPlayer(t *testing.T) {
	for _, kind := range []string{"villager", "wandering_trader"} {
		t.Run(kind, func(t *testing.T) {
			srv, connect := servertest.Start(t, servertest.MobTestConfig())
			w := servertest.WindowedOf(t, "Buyer", connect())
			var merchant int32
			if kind == "villager" {
				merchant = srv.SpawnVillagerWithOffers("minecraft:farmer", 1, breadOffer(0), 3.5, 64, 0.5)
			} else {
				merchant = srv.SpawnMob("minecraft:wandering_trader", 2.5, 64, 0.5)
			}
			servertest.HoldTicks(t, srv)
			if n := openTrade(w, merchant); n != 1 {
				t.Fatalf("opened %d windows, want 1", n)
			}
			servertest.RunTicks(t, srv, 200)

			pos := srv.PlayerPosition("Buyer")
			x, y, z, _ := srv.MobPos(merchant)
			dx, dz := pos.X-x, pos.Z-z
			if d := math.Hypot(dx, dz); d > 2.5 {
				t.Errorf("%s is %.2f blocks from the trading player, want within 2.5 (at %.2f,%.2f,%.2f)", kind, d, x, y, z)
			}
			want := float32(math.Atan2(-dx, dz) * 180 / math.Pi)
			for _, m := range srv.Mobs() {
				if m.ID == merchant {
					diff := math.Mod(float64(m.HeadYaw-want)+540, 360) - 180
					if math.Abs(diff) > 5 {
						t.Errorf("head yaw %.1f, want %.1f toward the player", m.HeadYaw, want)
					}
				}
			}
		})
	}
}
