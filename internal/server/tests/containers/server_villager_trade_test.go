package containers

import (
	"fmt"
	"io"
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

func tp(srv *server.Server, command string) {
	console.New(strings.NewReader(command), io.Discard, io.Discard, srv).Serve()
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

// vanilla: AbstractVillager.addOffersFromTradeSet
func TestArmorerOffersRepeatAfterRandomSequenceReset(t *testing.T) {
	cfg := servertest.MobTestConfig()
	cfg.LevelSeed = "1"
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Buyer", servertest.OpsListing(t, "Buyer"))
	servertest.HoldTicks(t, srv)
	w := &servertest.WindowedClient{Client: c}

	draw := func() (merchantOffers, []byte) {
		t.Helper()
		c.ChatCommand("random reset minecraft:trade_set/armorer/level_1 5 true true")
		c.ExpectFeedbackLine("commands.random.reset.success")
		c.ChatCommand(`summon villager 1.5 64 0.5 {NoAI:1b,VillagerData:{profession:"minecraft:armorer",level:1,type:"minecraft:plains"}}`)
		spawn := c.AwaitSpawn(protocol.EntityTypeVillager)
		c.ExpectFeedbackLine("commands.summon.success")
		w.UseOn(spawn.EntityID)
		var data []byte
		w.SettleAndCollect(func(id int32, payload []byte) {
			if id == protocol.IDPlayMerchantOffers {
				data = payload
			}
		})
		if data == nil {
			t.Fatal("the trade window sent no offers")
		}
		offers := readMerchantOffers(t, data)
		w.Window = offers.window
		w.CloseWindow()
		// Each window has its own ID. The full offer list should match byte for byte.
		r := protocol.NewReader(data)
		r.ReadVarInt()
		return offers, data[len(data)-r.Remaining():]
	}
	first, firstPayload := draw()
	second, secondPayload := draw()
	if !slices.Equal(firstPayload, secondPayload) {
		t.Errorf("armorer results before and after reset: %v and %v, want identical offer packets", first.results, second.results)
	}
	want := []int32{int32(registry.ItemIronLeggings), int32(registry.ItemEmerald)}
	if !slices.Equal(first.results, want) {
		t.Errorf("first armorer results = %v, want %v (iron leggings, emerald)", first.results, want)
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
			recipe, _ := nbt.CompoundView(recipes.Items[0])
			if uses, _ := nbt.Get[nbt.Int](recipe, "uses"); int32(uses) != tc.trades {
				t.Errorf("completed %d trades, want %d", uses, tc.trades)
			}
		})
	}
}
