package mobs

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

type listedOffer struct {
	cost, result int32
	maxUses      int32
}

// armorerOffers opens a summoned level-1 armorer with no offers, on a server
// whose level holds the pack files given, and reads the Merchant Offers it
// sends. Not parallel: the trade sets are process-wide.
func armorerOffers(t *testing.T, files map[string]string) []listedOffer {
	t.Helper()
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "survival"
	cfg.LevelName = t.TempDir()
	for name, content := range files {
		path := filepath.Join(cfg.LevelName, "datapacks", "p", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv, c := servertest.Join(t, cfg, "Villager", servertest.OpsListing(t, "Villager"))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon minecraft:villager 1.5 64 0.5 {NoAI:1b,VillagerData:{profession:"minecraft:armorer",level:1,type:"minecraft:plains"}}`)
	id := c.AwaitSpawn(villagerType(t)).EntityID
	c.UseOn(id)
	var payload []byte
	c.SettleAndCollect(func(pid int32, data []byte) {
		if pid == protocol.IDPlayMerchantOffers {
			payload = data
		}
	})
	if payload == nil {
		t.Fatal("no merchant offers")
	}
	r := protocol.NewReader(payload)
	r.ReadVarInt() // window
	count := r.ReadVarInt()
	var out []listedOffer
	for range count {
		o := listedOffer{cost: r.ReadVarInt()}
		r.ReadVarInt() // cost count
		r.ReadVarInt() // cost predicate
		result, _ := r.ReadSlot()
		o.result = result.ItemID
		if r.ReadBool() {
			r.ReadVarInt()
			r.ReadVarInt()
			r.ReadVarInt()
		}
		r.ReadBool() // out of stock
		r.ReadI32()  // uses
		o.maxUses = r.ReadI32()
		r.ReadI32() // xp
		r.ReadI32() // special price
		r.ReadF32() // multiplier
		r.ReadI32() // demand
		out = append(out, o)
	}
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	return out
}

// TestPackTradesChangeAVillagersOffers checks that a pack's villager_trade,
// its replacing villager_trade tag and its trade_set decide what a level-1
// armorer offers, and that without the pack the vanilla trades stay.
// vanilla: VillagerTrade, TradeSet, VillagerProfession
func TestPackTradesChangeAVillagersOffers(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTradeSets(nil) })
	emerald, stick := servertest.MustItemID(t, "minecraft:emerald"), servertest.MustItemID(t, "minecraft:stick")
	t.Run("pack", func(t *testing.T) {
		offers := armorerOffers(t, map[string]string{
			"pack.mcmeta":                       `{"pack":{"description":"p"}}`,
			"data/pk/villager_trade/stick.json": `{"wants":{"id":"minecraft:emerald","count":1},"gives":{"id":"minecraft:stick","count":1},"max_uses":3,"xp":1}`,
			"data/minecraft/tags/villager_trade/armorer/level_1.json": `{"replace":true,"values":["pk:stick"]}`,
			"data/minecraft/trade_set/armorer/level_1.json":           `{"amount":1,"trades":"#minecraft:armorer/level_1"}`,
		})
		if len(offers) != 1 || offers[0] != (listedOffer{cost: emerald, result: stick, maxUses: 3}) {
			t.Fatalf("offers = %v, want emerald -> stick with max uses 3", offers)
		}
	})
	t.Run("vanilla", func(t *testing.T) {
		offers := armorerOffers(t, nil)
		set, _ := registry.FindTradeSet("armorer/level_1")
		if len(offers) != set.Amount {
			t.Fatalf("%d offers, want %d", len(offers), set.Amount)
		}
		for _, o := range offers {
			if o.result == stick {
				t.Fatalf("vanilla armorer offers a stick: %v", offers)
			}
		}
	})
}

// TestPackTradeResultComponents follows a template through drawing, buying,
// reopening and rejoining. Trade registries are process-wide: do not parallelize.
func TestPackTradeResultComponents(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTradeSets(nil) })
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "survival"
	cfg.LevelName = t.TempDir()
	files := map[string]string{
		"pack.mcmeta":                                   `{"pack":{"description":"p"}}`,
		"data/pk/villager_trade/plain.json":             `{"wants":{"id":"minecraft:emerald"},"gives":{"id":"minecraft:diamond"}}`,
		"data/pk/villager_trade/named.json":             `{"wants":{"id":"minecraft:emerald"},"gives":{"id":"minecraft:diamond","components":{"minecraft:custom_name":{"text":"Audit Gem"},"minecraft:lore":[{"text":"Template"}]}},"given_item_modifier":[{"function":"minecraft:set_count","count":1,"add":true},{"function":"minecraft:set_lore","lore":[{"text":"Modified"}],"mode":"append"}]}`,
		"data/minecraft/trade_set/armorer/level_1.json": `{"amount":2,"trades":["pk:plain","pk:named"]}`,
	}
	for name, content := range files {
		path := filepath.Join(cfg.LevelName, "datapacks", "p", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv, c := servertest.Join(t, cfg, "Villager", servertest.OpsListing(t, "Villager"))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	w := &servertest.WindowedClient{Client: c}
	summon := func() int32 {
		c.ChatCommand(`summon minecraft:villager 1.5 64 0.5 {NoAI:1b,VillagerData:{profession:"minecraft:armorer",level:1,type:"minecraft:plains"}}`)
		return c.AwaitSpawn(villagerType(t)).EntityID
	}
	open := func(id int32) (int32, protocol.ItemStack) {
		t.Helper()
		w.UseOn(id)
		var payload []byte
		w.SettleAndCollect(func(pid int32, data []byte) {
			r := protocol.NewReader(data)
			switch pid {
			case protocol.IDPlayContainerContent:
				window, state := r.ReadVarInt(), r.ReadVarInt()
				w.TrackState(window, state)
			case protocol.IDPlayMerchantOffers:
				payload = data
			}
		})
		if payload == nil {
			t.Fatal("no merchant offers")
		}
		r := protocol.NewReader(payload)
		w.Window = r.ReadVarInt()
		if n := r.ReadVarInt(); n != 2 {
			t.Fatalf("offers = %d, want plain and named diamond", n)
		}
		var index int32 = -1
		var named protocol.ItemStack
		for i := range int32(2) {
			r.ReadVarInt() // cost item
			r.ReadVarInt() // cost count
			if r.ReadVarInt() != 0 {
				t.Fatal("unexpected cost predicate")
			}
			result, ok := r.ReadSlot()
			if !ok || result.ItemID != int32(registry.ItemDiamond) {
				t.Fatalf("result = %+v", result)
			}
			if result.CustomName != nil {
				index, named = i, result
			}
			if r.ReadBool() {
				t.Fatal("unexpected extra cost")
			}
			r.ReadBool()
			for range 4 { // uses, max uses, xp, special price
				r.ReadI32()
			}
			r.ReadF32()
			r.ReadI32()
		}
		if r.Err() != nil || index < 0 {
			t.Fatalf("named offer missing, read error %v", r.Err())
		}
		lore := named.LoreOf()
		if named.CustomName.Text != "Audit Gem" || named.Count != 2 || len(lore) != 2 || lore[0].Text != "Template" || lore[1].Text != "Modified" {
			t.Fatalf("template and modifier result = %+v, lore %+v", named, lore)
		}
		return index, named
	}
	id := summon()
	index, named := open(id)
	srv.GiveSlot("Villager", player.HotbarSlot, protocol.ItemStack{ItemID: int32(registry.ItemEmerald), Count: 1})
	w.Settle()
	w.Send(protocol.IDPlaySelectTradeIn, func(b *protocol.Buffer) { b.PutVarInt(index) })
	w.CollectContainerSlots()
	w.Click(2, 0, protocol.ClickModeQuickMove)
	w.CollectContainerSlots()
	assertBought := func() {
		t.Helper()
		for slot := player.MainSlot; slot < player.HotbarSlot+9; slot++ {
			if srv.InventorySlot("Villager", slot).Equal(named) {
				return
			}
		}
		t.Fatal("purchased named diamond missing from inventory")
	}
	assertBought()
	w.CloseWindow()
	w.Settle()
	open(id)
	w.CloseWindow()
	w.Settle()
	w.Disconnect()
	c = servertest.DialServer(t, srv).Join("Villager")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	w = &servertest.WindowedClient{Client: c}
	assertBought()
	open(id)
	w.CloseWindow()
	w.Settle()
	// A second villager draws the same shared template, applying each modifier once.
	open(summon())
}

// TestPackTradeRejectsInvalidResultTemplates checks creation before a modifier
// can reduce the count, including the sizes of contained templates.
// vanilla: ItemStackTemplate.create, ItemStack.validateStrict
func TestPackTradeRejectsInvalidResultTemplates(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTradeSets(nil) })
	for _, tc := range []struct {
		name, gives string
	}{
		{"oversized result", `{"id":"minecraft:diamond","count":65,"components":{"minecraft:custom_name":{"text":"Invalid Gem"}}}`},
		{"container", `{"id":"minecraft:diamond","components":{"minecraft:container":[{"slot":0,"item":{"id":"minecraft:diamond","count":65}}]}}`},
		{"bundle", `{"id":"minecraft:diamond","components":{"minecraft:bundle_contents":[{"id":"minecraft:diamond","count":65}]}}`},
		{"projectiles", `{"id":"minecraft:diamond","components":{"minecraft:charged_projectiles":[{"id":"minecraft:arrow","count":65}]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			offers := armorerOffers(t, map[string]string{
				"pack.mcmeta":                                   `{"pack":{"description":"p"}}`,
				"data/pk/villager_trade/plain.json":             `{"wants":{"id":"minecraft:emerald"},"gives":"minecraft:diamond"}`,
				"data/pk/villager_trade/invalid.json":           fmt.Sprintf(`{"wants":{"id":"minecraft:emerald"},"gives":%s,"given_item_modifier":{"type":"minecraft:set_count","count":1}}`, tc.gives),
				"data/minecraft/trade_set/armorer/level_1.json": `{"amount":2,"trades":["pk:plain","pk:invalid"]}`,
			})
			if len(offers) != 1 || offers[0].result != int32(registry.ItemDiamond) {
				t.Fatalf("offers = %v, want only plain diamond", offers)
			}
		})
	}
}
