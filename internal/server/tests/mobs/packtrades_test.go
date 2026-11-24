package mobs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
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
	offers, _ := armorerOfferStacks(t, files)
	return offers
}

// armorerOfferStacks is armorerOffers with the result stack of each offer.
func armorerOfferStacks(t *testing.T, files map[string]string) ([]listedOffer, []protocol.ItemStack) {
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
	var stacks []protocol.ItemStack
	for range count {
		o := listedOffer{cost: r.ReadVarInt()}
		r.ReadVarInt() // cost count
		r.ReadVarInt() // cost predicate
		result, _ := r.ReadSlot()
		o.result = result.ItemID
		stacks = append(stacks, result)
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
	return out, stacks
}

// TestPackTradesChangeAVillagersOffers checks that a pack's villager_trade,
// its replacing villager_trade tag and its trade_set decide what a level-1
// armorer offers, and that without the pack the vanilla trades stay.
// vanilla: VillagerTrade, TradeSet, VillagerProfession
func TestPackTradesChangeAVillagersOffers(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTradeSets(nil) })
	emerald, stick := servertest.MustItemID(t, "minecraft:emerald"), servertest.MustItemID(t, "minecraft:stick")
	t.Run("pack", func(t *testing.T) {
		offers, stacks := armorerOfferStacks(t, map[string]string{
			"pack.mcmeta": `{"pack":{"description":"p"}}`,
			"data/pk/villager_trade/stick.json": `{"wants":{"id":"minecraft:emerald","count":1},"gives":{"id":"minecraft:stick","count":1},"max_uses":3,"xp":1,` +
				`"given_item_modifier":{"function":"minecraft:set_count","count":{"type":"minecraft:uniform","min":1,"max":32}}}`,
			"data/minecraft/tags/villager_trade/armorer/level_1.json": `{"replace":true,"values":["pk:stick"]}`,
			"data/minecraft/trade_set/armorer/level_1.json":           `{"amount":1,"trades":"#minecraft:armorer/level_1","random_sequence":"pk:armorer"}`,
		})
		if len(offers) != 1 || offers[0] != (listedOffer{cost: emerald, result: stick, maxUses: 3}) {
			t.Fatalf("offers = %v, want emerald -> stick with max uses 3", offers)
		}
		// At world seed 0, vanilla's pk:armorer rolls nextInt(1) for the pick,
		// then 1 + nextInt(32) for the result count.
		if got := stacks[0].Count; got != 27 {
			t.Errorf("result count = %d, want 27 from the trade set's sequence", got)
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
