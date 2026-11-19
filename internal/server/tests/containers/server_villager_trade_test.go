package containers

import (
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/console"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Right-clicking a villager opens its saved trades as a merchant window; a
// take counts the use and the experience on the villager.

func breadOffer(uses int32) nbt.Compound {
	recipe := nbt.Compound{
		"buy":             nbt.Compound{"id": nbt.String("minecraft:emerald"), "count": nbt.Int(2)},
		"sell":            nbt.Compound{"id": nbt.String("minecraft:bread"), "count": nbt.Int(6)},
		"uses":            nbt.Int(uses),
		"maxUses":         nbt.Int(16),
		"xp":              nbt.Int(5),
		"priceMultiplier": nbt.Float(0.05),
	}
	return nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{recipe}}}
}

func TestRightClickingAVillagerOpensItsTrades(t *testing.T) {
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	w := servertest.WindowedOf(t, "Buyer", connect())
	emerald, bread := registry.MustItem("minecraft:emerald"), registry.MustItem("minecraft:bread")
	srv.GiveSlot("Buyer", player.HotbarSlot, protocol.ItemStack{ItemID: emerald, Count: 4})

	villager := srv.SpawnVillagerWithOffers("minecraft:farmer", 2, breadOffer(1), 1.5, 64, 0.5)
	w.UseOn(villager)

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
	if n := r.ReadVarInt(); n != 1 {
		t.Fatalf("%d offers sent, want 1", n)
	}
	if id := r.ReadVarInt(); id != emerald {
		t.Errorf("cost item %d, want emerald %d", id, emerald)
	}
	if count := r.ReadVarInt(); count != 2 {
		t.Errorf("cost count %d, want 2", count)
	}
	r.ReadVarInt() // predicate
	if res, _ := r.ReadSlot(); res.ItemID != bread || res.Count != 6 {
		t.Errorf("result %+v, want 6 bread", res)
	}
	r.ReadBool() // costB
	r.ReadBool() // out of stock
	if uses := r.ReadI32(); uses != 1 {
		t.Errorf("uses %d, want the saved 1", uses)
	}

	selectTrade(w, 0)
	w.Click(merchantResult, 0, protocol.ClickModePickup)
	w.Settle()
	saved, xp := srv.VillagerSaved(villager)
	recipes, _ := nbt.Get[nbt.List](saved, "Recipes")
	recipe, _ := recipes.Items[0].(nbt.Compound)
	if uses, _ := nbt.Get[nbt.Int](recipe, "uses"); uses != 2 {
		t.Errorf("saved uses %d, want 2", uses)
	}
	if xp != 5 {
		t.Errorf("villager xp %d, want 5", xp)
	}
}

func TestAVillagerWithNoTradesOpensNoWindow(t *testing.T) {
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	w := servertest.WindowedOf(t, "Buyer", connect())
	villager := srv.SpawnVillagerWithOffers("minecraft:nitwit", 1, nil, 1.5, 64, 0.5)
	w.UseOn(villager)
	w.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlayOpenScreen {
			t.Error("a villager with no trades opened a window")
		}
	})
}

func TestAVillagerTradesWithOnePlayerUntilTheWindowCloses(t *testing.T) {
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	w := servertest.WindowedOf(t, "Buyer", connect())
	villager := srv.SpawnVillagerWithOffers("minecraft:farmer", 1, breadOffer(0), 1.5, 64, 0.5)
	opens := func() int {
		n := 0
		w.UseOn(villager)
		w.SettleAndCollect(func(id int32, data []byte) {
			if id == protocol.IDPlayOpenScreen {
				w.Window = protocol.NewReader(data).ReadVarInt()
				n++
			}
		})
		return n
	}
	if n := opens(); n != 1 {
		t.Fatalf("first click opened %d windows, want 1", n)
	}
	if n := opens(); n != 0 {
		t.Errorf("click on a villager already trading opened %d windows, want 0", n)
	}
	w.CloseWindow()
	w.Settle()
	if n := opens(); n != 1 {
		t.Errorf("click after the window closed opened %d windows, want 1", n)
	}
}

// openTrade right-clicks the villager and returns how many windows opened.
func openTrade(w *servertest.WindowedClient, villager int32) int {
	n := 0
	w.UseOn(villager)
	w.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayOpenScreen {
			w.Window = protocol.NewReader(data).ReadVarInt()
			n++
		}
	})
	return n
}

// windowClosedAfter runs act and reports whether the server then closed the
// window.
func windowClosedAfter(srv *server.Server, w *servertest.WindowedClient, act func()) bool {
	act()
	closed := false
	w.ReadTicks(srv, 3, func(id int32, _ []byte) bool {
		closed = closed || id == protocol.IDPlayCloseContainer
		return false
	})
	return closed
}

// vanilla: AbstractVillager.stillValid, AbstractVillager.die
func TestAVillagerDyingClosesItsTradeWindow(t *testing.T) {
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	w := servertest.WindowedOf(t, "Buyer", connect())
	villager := srv.SpawnVillagerWithOffers("minecraft:farmer", 1, breadOffer(0), 1.5, 64, 0.5)
	if openTrade(w, villager) != 1 {
		t.Fatal("the trade window did not open")
	}
	if !windowClosedAfter(srv, w, func() { srv.KillMob(villager) }) {
		t.Error("the trade window stayed open after the villager died")
	}
}

// vanilla: AbstractVillager.stillValid
func TestAPlayerWalkedOutOfReachLosesTheTradeWindow(t *testing.T) {
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	w := servertest.WindowedOf(t, "Buyer", connect())
	villager := srv.SpawnVillagerWithOffers("minecraft:farmer", 1, breadOffer(0), 1.5, 64, 0.5)
	if openTrade(w, villager) != 1 {
		t.Fatal("the trade window did not open")
	}
	if !windowClosedAfter(srv, w, func() { tp(srv, "tp Buyer 40 64 0.5\n") }) {
		t.Error("the trade window stayed open 40 blocks from the villager")
	}
}

// vanilla: AbstractVillager.stillValid, ServerPlayer creative_mode_entity_range
func TestACreativePlayerKeepsTheTradeWindowTwoBlocksFarther(t *testing.T) {
	for _, tc := range []struct {
		mode       string
		wantClosed bool
	}{{"survival", true}, {"creative", false}} {
		t.Run(tc.mode, func(t *testing.T) {
			srv, connect := servertest.Start(t, servertest.MobTestConfig())
			w := servertest.WindowedOf(t, "Buyer", connect())
			villager := srv.SpawnVillagerWithOffers("minecraft:farmer", 1, breadOffer(0), 1.5, 64, 0.5)
			if openTrade(w, villager) != 1 {
				t.Fatal("the trade window did not open")
			}
			tp(srv, "gamemode "+tc.mode+" Buyer\n")
			// 7.4 blocks from the villager's box: past 3+4, inside 5+4.
			if got := windowClosedAfter(srv, w, func() { tp(srv, "tp Buyer 9.2 64 0.5\n") }); got != tc.wantClosed {
				t.Errorf("window closed = %v, want %v", got, tc.wantClosed)
			}
		})
	}
}

func TestAPlayerStayingInReachKeepsTheTradeWindow(t *testing.T) {
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	w := servertest.WindowedOf(t, "Buyer", connect())
	villager := srv.SpawnVillagerWithOffers("minecraft:farmer", 1, breadOffer(0), 1.5, 64, 0.5)
	if openTrade(w, villager) != 1 {
		t.Fatal("the trade window did not open")
	}
	if windowClosedAfter(srv, w, func() { tp(srv, "tp Buyer 5 64 0.5\n") }) {
		t.Error("the trade window closed 3.5 blocks from the villager")
	}
}

func tp(srv *server.Server, command string) {
	console.New(strings.NewReader(command), io.Discard, io.Discard, srv).Serve()
}

// vanilla: AbstractVillager.canBeLeashed, Villager.mobInteract
func TestALeadInHandStillOpensAVillagersTrades(t *testing.T) {
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	w := servertest.WindowedOf(t, "Buyer", connect())
	srv.GiveSlot("Buyer", player.HotbarSlot, protocol.ItemStack{ItemID: registry.MustItem("minecraft:lead"), Count: 1})
	villager := srv.SpawnVillagerWithOffers("minecraft:farmer", 1, breadOffer(0), 1.5, 64, 0.5)
	if n := openTrade(w, villager); n != 1 {
		t.Errorf("a lead in hand opened %d windows, want 1", n)
	}
}

// vanilla: Villager.mobInteract; a name tag with no name is not used
func TestABlankNameTagStillOpensAVillagersTrades(t *testing.T) {
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	w := servertest.WindowedOf(t, "Buyer", connect())
	srv.GiveSlot("Buyer", player.HotbarSlot, protocol.ItemStack{ItemID: registry.MustItem("minecraft:name_tag"), Count: 1})
	villager := srv.SpawnVillagerWithOffers("minecraft:farmer", 1, breadOffer(0), 1.5, 64, 0.5)
	if n := openTrade(w, villager); n != 1 {
		t.Errorf("a blank name tag opened %d windows, want 1", n)
	}
}

func TestABabyVillagerOpensNoWindow(t *testing.T) {
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	w := servertest.WindowedOf(t, "Buyer", connect())
	villager := srv.SpawnVillagerWithOffers("minecraft:farmer", 1, breadOffer(0), 1.5, 64, 0.5)
	srv.SetMobBaby(villager, true)
	if n := openTrade(w, villager); n != 0 {
		t.Errorf("a baby villager opened %d windows, want 0", n)
	}
}

// soundsAfterClick counts the sound effects a click on the villager makes.
func soundsAfterClick(w *servertest.WindowedClient, villager int32) int {
	n := 0
	w.UseOn(villager)
	w.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlaySoundEffect {
			n++
		}
	})
	return n
}

// vanilla: Villager.setUnhappy
func TestAVillagerWithNoTradesShakesItsHeadUnlessSilent(t *testing.T) {
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	w := servertest.WindowedOf(t, "Buyer", connect())
	loud := srv.SpawnVillagerWithOffers("minecraft:nitwit", 1, nil, 1.5, 64, 0.5)
	if soundsAfterClick(w, loud) == 0 {
		t.Error("a villager with no trades made no sound")
	}
	quiet := srv.SpawnVillagerWithOffers("minecraft:nitwit", 1, nil, 1.5, 64, 0.5)
	srv.SetMobSilent(quiet, true)
	if n := soundsAfterClick(w, quiet); n != 0 {
		t.Errorf("a silent villager made %d sounds", n)
	}
}

func TestAVillagerWithAProfessionDrawsItsTradesOnFirstUse(t *testing.T) {
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	w := servertest.WindowedOf(t, "Buyer", connect())
	villager := srv.SpawnVillagerWithOffers("minecraft:farmer", 1, nil, 1.5, 64, 0.5)
	w.UseOn(villager)
	w.Settle()

	set, _ := registry.FindTradeSet("farmer/level_1")
	saved, _ := srv.VillagerSaved(villager)
	recipes, _ := nbt.Get[nbt.List](saved, "Recipes")
	if len(recipes.Items) != set.Amount {
		t.Fatalf("%d trades drawn, want the set's %d", len(recipes.Items), set.Amount)
	}
	seen := map[string]bool{}
	for _, item := range recipes.Items {
		recipe := item.(nbt.Compound)
		buy, _ := nbt.Get[nbt.Compound](recipe, "buy")
		sell, _ := nbt.Get[nbt.Compound](recipe, "sell")
		key := string(buy["id"].(nbt.String)) + ">" + string(sell["id"].(nbt.String))
		if seen[key] {
			t.Errorf("trade %s drawn twice", key)
		}
		seen[key] = true
		found := false
		for _, tr := range set.Trades {
			found = found || tr.Wants.Item == string(buy["id"].(nbt.String)) && tr.Gives.Item == string(sell["id"].(nbt.String))
		}
		if !found {
			t.Errorf("trade %s is not in farmer/level_1", key)
		}
	}
}

func TestAVillagerKeepsItsDrawnTrades(t *testing.T) {
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	w := servertest.WindowedOf(t, "Buyer", connect())
	villager := srv.SpawnVillagerWithOffers("minecraft:farmer", 1, nil, 1.5, 64, 0.5)
	w.UseOn(villager)
	w.Settle()
	first, _ := srv.VillagerSaved(villager)
	w.CloseWindow()
	w.Settle()
	w.UseOn(villager)
	w.Settle()
	second, _ := srv.VillagerSaved(villager)
	if !reflect.DeepEqual(first, second) {
		t.Errorf("trades changed on the second use:\n%v\n%v", first, second)
	}
}

// vanilla: Villager.mobInteract returns before getOffers for a baby
func TestABabyVillagerDrawsNoTrades(t *testing.T) {
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	w := servertest.WindowedOf(t, "Buyer", connect())
	villager := srv.SpawnVillagerWithOffers("minecraft:farmer", 1, nil, 1.5, 64, 0.5)
	srv.SetMobBaby(villager, true)
	w.UseOn(villager)
	w.Settle()
	if saved, _ := srv.VillagerSaved(villager); saved != nil {
		t.Errorf("a baby drew trades: %v", saved)
	}
}

// vanilla: AbstractVillager.notifyTradeUpdated
func TestAVillagerSoundsOnceWhenTheTradeResultChanges(t *testing.T) {
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	w := servertest.WindowedOf(t, "Buyer", connect())
	srv.GiveSlot("Buyer", player.HotbarSlot, protocol.ItemStack{ItemID: registry.MustItem("minecraft:emerald"), Count: 4})
	villager := srv.SpawnVillagerWithOffers("minecraft:farmer", 1, breadOffer(0), 1.5, 64, 0.5)
	if n := openTrade(w, villager); n != 1 {
		t.Fatalf("opened %d windows, want 1", n)
	}
	servertest.HoldTicks(t, srv)
	// heard is the villager sounds that arrive while the collect settles.
	heard := func(act func()) []string {
		var names []string
		act()
		w.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlaySoundEffect {
				return
			}
			if sound := protocol.NewReader(data).ReadVarInt(); sound > 0 {
				name, _ := servertest.SoundEventName(sound - 1)
				names = append(names, name)
			}
		})
		return names
	}
	pick := func() {
		w.Send(protocol.IDPlaySelectTradeIn, func(b *protocol.Buffer) { b.PutVarInt(0) })
	}
	take := func() { w.Click(0, 0, protocol.ClickModePickup) }
	if got := heard(pick); !slices.Equal(got, []string{"minecraft:entity.villager.yes"}) {
		t.Errorf("the first result change played %v, want one yes", got)
	}
	servertest.RunTicks(t, srv, 10)
	if got := heard(pick); len(got) != 0 {
		t.Errorf("a change inside the 20-tick gap played %v, want none", got)
	}
	servertest.RunTicks(t, srv, 25)
	if got := heard(take); !slices.Equal(got, []string{"minecraft:entity.villager.no"}) {
		t.Errorf("emptying the payment after the gap played %v, want one no", got)
	}
}

// vanilla: Slot.tryRemove, MerchantContainer.setChanged, AbstractVillager.notifyTrade
func TestMerchantsSoundOnceWhenTakingATrade(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		mode       int32
		ticks      int
		trades     int32
		voices     int
	}{
		{"pickup", "villager", protocol.ClickModePickup, 25, 1, 1},
		{"quick_move", "villager", protocol.ClickModeQuickMove, 25, 2, 1},
		{"cooldown", "villager", protocol.ClickModePickup, 10, 1, 0},
		{"wandering_trader", "wandering_trader", protocol.ClickModePickup, 25, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := servertest.MobTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, c := servertest.Join(t, cfg, "Buyer", servertest.OpsListing(t, "Buyer"))
			servertest.HoldTicks(t, srv)
			w := &servertest.WindowedClient{Client: c, WantBlockEntityType: -1, States: map[int32]int32{0: 1}}
			c.ChatCommand(fmt.Sprintf(`summon %s 1.5 64 0.5 {NoAI:1b,VillagerData:{profession:"minecraft:farmer",level:1,type:"minecraft:plains"},Offers:{Recipes:[{buy:{id:"minecraft:emerald",count:2},sell:{id:"minecraft:bread",count:6},maxUses:2,xp:0}]}}`, tc.kind))
			entityType := int32(protocol.EntityTypeVillager)
			if tc.kind == "wandering_trader" {
				entityType = protocol.EntityTypeWanderingTrader
			}
			merchant := c.AwaitSpawn(entityType).EntityID
			c.ExpectFeedbackLine("commands.summon.success")
			srv.GiveSlot("Buyer", player.HotbarSlot, protocol.ItemStack{ItemID: int32(registry.ItemEmerald), Count: 2 * tc.trades})
			if n := openTrade(w, merchant); n != 1 {
				t.Fatalf("opened %d windows, want 1", n)
			}
			heard := func() []string {
				var names []string
				w.SettleAndCollect(func(id int32, data []byte) {
					if id != protocol.IDPlaySoundEffect {
						return
					}
					name, _ := servertest.SoundEventName(protocol.NewReader(data).ReadVarInt() - 1)
					if strings.HasPrefix(name, "minecraft:entity."+tc.kind+".") {
						names = append(names, name)
					}
				})
				return names
			}
			w.Send(protocol.IDPlaySelectTradeIn, func(b *protocol.Buffer) { b.PutVarInt(0) })
			yes := "minecraft:entity." + tc.kind + ".yes"
			if got := heard(); !slices.Equal(got, []string{yes}) {
				t.Fatalf("putting in payment played %v, want one %s", got, yes)
			}
			servertest.RunTicks(t, srv, tc.ticks)
			w.Settle()
			w.Click(merchantResult, 0, tc.mode)
			want := make([]string, tc.voices)
			for i := range want {
				want[i] = yes
			}
			if got := heard(); !slices.Equal(got, want) {
				t.Errorf("taking the result played %v, want %v", got, want)
			}
			saved, _ := srv.VillagerSaved(merchant)
			recipes, _ := nbt.Get[nbt.List](saved, "Recipes")
			if len(recipes.Items) != 1 {
				t.Fatalf("saved recipes = %v, want one", recipes)
			}
			recipe, _ := recipes.Items[0].(nbt.Compound)
			if uses, _ := nbt.Get[nbt.Int](recipe, "uses"); int32(uses) != tc.trades {
				t.Errorf("completed %d trades, want %d", uses, tc.trades)
			}
		})
	}
}

// vanilla: Villager.updateSpecialPrices
func TestAHeroOfTheVillagerSeesADiscountOnTheOffers(t *testing.T) {
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	w := servertest.WindowedOf(t, "Buyer", connect())
	srv.GiveEffect("Buyer", "minecraft:hero_of_the_village", 6000, 0, false)
	villager := srv.SpawnVillagerWithOffers("minecraft:farmer", 1, breadOffer(0), 1.5, 64, 0.5)
	var offers []byte
	w.UseOn(villager)
	w.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayMerchantOffers {
			offers = data
		}
	})
	r := protocol.NewReader(offers)
	r.ReadVarInt() // window
	r.ReadVarInt() // offers
	r.ReadVarInt() // item
	r.ReadVarInt() // count
	r.ReadVarInt() // predicate
	r.ReadSlot()
	r.ReadBool()
	r.ReadBool()
	r.ReadI32() // uses
	r.ReadI32() // max uses
	r.ReadI32() // xp
	if got := r.ReadI32(); got != -1 {
		t.Errorf("special price on the wire = %d, want -1 (0.3 of 2 floors to 0, at least 1)", got)
	}
}

func TestAnOfferCostWrittenWithoutANamespaceStillTrades(t *testing.T) {
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	w := servertest.WindowedOf(t, "Buyer", connect())
	emerald, helmet := registry.MustItem("minecraft:emerald"), registry.MustItem("minecraft:iron_helmet")
	srv.GiveSlot("Buyer", player.HotbarSlot, protocol.ItemStack{ItemID: emerald, Count: 5})

	offer := nbt.Compound{
		"buy":  nbt.Compound{"id": nbt.String("emerald"), "count": nbt.Int(5)},
		"sell": nbt.Compound{"id": nbt.String("iron_helmet"), "count": nbt.Int(1)},
	}
	offers := nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{offer}}}
	villager := srv.SpawnVillagerWithOffers("minecraft:armorer", 2, offers, 1.5, 64, 0.5)
	w.UseOn(villager)

	var sent []byte
	w.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayMerchantOffers {
			sent = data
		}
	})
	if sent == nil {
		t.Fatal("no merchant offers sent")
	}
	r := protocol.NewReader(sent)
	r.ReadVarInt() // window
	if n := r.ReadVarInt(); n != 1 {
		t.Fatalf("%d offers sent, want 1", n)
	}
	if id, count := r.ReadVarInt(), r.ReadVarInt(); id != emerald || count != 5 {
		t.Errorf("cost item %d x%d, want emerald %d x5", id, count, emerald)
	}
	r.ReadVarInt() // predicate
	if res, _ := r.ReadSlot(); res.ItemID != helmet || res.Count != 1 {
		t.Errorf("result %+v, want an iron helmet", res)
	}
}
