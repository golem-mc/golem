package advancements

import (
	"math/rand"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The triggers the player's dealings with mobs and containers fire: the
// villager's trade, the brewing stand's potion, the wolf's taming and the
// fish scooped in a bucket. Each test holds the tick loop so nothing but
// the clicks moves the world.

// breadTrade is a villager's one offer, two emeralds for six bread.
func breadTrade() nbt.Compound {
	recipe := nbt.Compound{
		"buy":             nbt.Compound{"id": nbt.String("minecraft:emerald"), "count": nbt.Int(2)},
		"sell":            nbt.Compound{"id": nbt.String("minecraft:bread"), "count": nbt.Int(6)},
		"maxUses":         nbt.Int(16),
		"xp":              nbt.Int(1),
		"priceMultiplier": nbt.Float(0.05),
	}
	return nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{recipe}}}
}

// tradeWithVillager takes the one trade of a villager standing beside the
// player, who is at the height given.
func tradeWithVillager(t *testing.T, srv *server.Server, w *servertest.WindowedClient, y float64) {
	t.Helper()
	const merchantResult = int16(2)
	w.MoveTo(0.5, y, 0.5, 0, 0)
	w.Settle()
	villager := srv.SpawnVillagerWithOffers("minecraft:farmer", 2, breadTrade(), 1.5, y, 0.5)
	w.UseOn(villager)
	w.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayOpenScreen {
			w.Window = protocol.NewReader(data).ReadVarInt()
		}
	})
	w.Send(protocol.IDPlaySelectTradeIn, func(b *protocol.Buffer) { b.PutVarInt(0) })
	w.CollectContainerSlots()
	w.Click(merchantResult, 0, protocol.ClickModePickup)
	w.Settle()
}

// TestTakingATradeObtainsItsAdvancement: the result slot's take is a
// villager_trade, and the height-gated criterion wants the player at 319 or
// above while the plain one does not.
// vanilla: AbstractVillager.notifyTrade, TradeTrigger
func TestTakingATradeObtainsItsAdvancement(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.MobTestConfig())
	servertest.HoldTicks(t, srv)
	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:emerald", 8))

	tradeWithVillager(t, srv, w, 64)
	if !srv.AdvancementObtained("Testificate", "minecraft:adventure/trade", "traded") {
		t.Error("taking a trade did not obtain adventure/trade")
	}
	if srv.AdvancementObtained("Testificate", "minecraft:adventure/trade_at_world_height", "trade_at_world_height") {
		t.Fatal("a trade at height 64 obtained trade_at_world_height")
	}
	tradeWithVillager(t, srv, w, 320)
	if !srv.AdvancementObtained("Testificate", "minecraft:adventure/trade_at_world_height", "trade_at_world_height") {
		t.Error("a trade at height 320 did not obtain trade_at_world_height")
	}
}

// TestTakingAPotionFromABrewingStandObtainsLocalBrewery: a bottle slot's
// potion taken out is a brewed_potion, an empty glass bottle taken out is
// not, and a potion swapped into the slot is not a take.
// vanilla: BrewingStandMenu.PotionSlot.onTake, BrewedPotionTrigger
func TestTakingAPotionFromABrewingStandObtainsLocalBrewery(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.StopTheLoop(t, srv)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:brewing_stand", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.SettleKeeping()
	srv.FlushBlockAcks()
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpenType(protocol.MenuBrewingStand, protocol.BlockEntityBrewingStand, 4, 64, 0)
	const brewed = "minecraft:nether/brew_potion"

	// A glass bottle shift-clicked into slot 0 and picked up again.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:glass_bottle", 1))
	w.Click(32, 0, protocol.ClickModeQuickMove)
	w.Settle()
	w.Click(0, 0, protocol.ClickModePickup)
	w.Settle()
	if srv.AdvancementObtained("Testificate", brewed, "potion") {
		t.Fatal("taking an empty bottle obtained brew_potion")
	}
	w.Click(0, 0, protocol.ClickModePickup)
	w.Settle()

	// A potion swapped for the bottle on the cursor is a placement.
	w.SetCreativeSlot(36, servertest.PotionBottle(t, "minecraft:potion", "minecraft:water"))
	w.Click(32, 0, protocol.ClickModePickup)
	w.Click(0, 0, protocol.ClickModePickup)
	w.Settle()
	if srv.AdvancementObtained("Testificate", brewed, "potion") {
		t.Fatal("swapping a potion into the stand obtained brew_potion")
	}

	// The bottle on the cursor put away, the potion in the slot picked up.
	w.Click(32, 0, protocol.ClickModePickup)
	w.Click(0, 0, protocol.ClickModePickup)
	w.Settle()
	if !srv.AdvancementObtained("Testificate", brewed, "potion") {
		t.Error("taking a potion out of the stand did not obtain brew_potion")
	}
}

// TestTamingAWolfObtainsTameAnAnimal: bones until a roll takes is a
// tame_animal for the wolf.
// vanilla: TamableAnimal.tame, TameAnimalTrigger
func TestTamingAWolfObtainsTameAnAnimal(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Owner", func(s *server.Server) {
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 64, 0.5, true)
	srv.GiveSlot("Owner", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:bone"), Count: 64})
	c.Settle()
	id := srv.SpawnMob("minecraft:wolf", 1.5, 64, 0.5)
	srv.SetMobNoAI(id)

	const tamed = "minecraft:husbandry/tame_an_animal"
	for range 64 {
		if srv.AdvancementObtained("Owner", tamed, "tamed_animal") {
			break
		}
		if st, _ := srv.MobTamable(id); st.Tame {
			t.Fatal("the wolf is tame and tame_an_animal is not obtained")
		}
		c.UseOn(id)
		c.Settle()
	}
	if st, _ := srv.MobTamable(id); !st.Tame {
		t.Fatal("64 bones did not tame the wolf")
	}
	if !srv.AdvancementObtained("Owner", tamed, "tamed_animal") {
		t.Error("taming a wolf did not obtain tame_an_animal")
	}
}

// tameVariant tames a mob of kind pinned to variant by feeding it food and
// returns once the criterion of advancement for variant is obtained or the
// food runs out.
func tameVariant(t *testing.T, kind, food, variant, advancement string) (obtained func(criterion string) bool) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Owner", func(s *server.Server) {
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 64, 0.5, true)
	srv.GiveSlot("Owner", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, food), Count: 64})
	c.Settle()
	id := srv.SpawnMob(kind, 1.5, 64, 0.5)
	srv.SetMobNoAI(id)
	srv.SetMobVariant(id, variant)
	for range 64 {
		if st, _ := srv.MobTamable(id); st.Tame {
			break
		}
		c.UseOn(id)
		c.Settle()
	}
	if st, _ := srv.MobTamable(id); !st.Tame {
		t.Fatalf("64 meals did not tame the %s", kind)
	}
	return func(criterion string) bool { return srv.AdvancementObtained("Owner", advancement, criterion) }
}

// reelSeeded casts a rod into a pool, puts the hook in its bite window with
// the loot roll seeded, and reels the catch in.
func reelSeeded(t *testing.T, srv *server.Server, c *servertest.Client, name string, seed int64) {
	t.Helper()
	servertest.UseHeld(c, 0, 40)
	c.Settle()
	if !srv.BiteBobber(name, seed) {
		t.Fatal("no hook was out to bite")
	}
	servertest.UseHeld(c, 0, 40)
	c.Settle()
}

// seedCatching finds the first seed whose catch in the pool is exactly the
// item, or, with none named, a catch that is no fish at all.
func seedCatching(t *testing.T, item string) int64 {
	t.Helper()
	for seed := int64(1); seed < 10000; seed++ {
		loot := rules.FishingLoot(rand.New(rand.NewSource(seed)), 0, false, "minecraft:plains", nil)
		if len(loot) != 1 {
			continue
		}
		name, _ := registry.ItemName(loot[0].ItemID)
		if item == "" && !registry.Item(loot[0].ItemID).InTag(registry.ItemTagIDFishes) || item != "" && name == item {
			return seed
		}
	}
	t.Fatalf("no seed catches %q", item)
	return 0
}

// armoredWolfScene joins "Owner" beside a still wolf of theirs in wolf armor
// worn to damage, holding item.
func armoredWolfScene(t *testing.T, tame bool, damage int32, item string) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Owner", func(s *server.Server) {
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 64, 0.5, true)
	srv.GiveSlot("Owner", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, item), Count: 8})
	c.Settle()
	id := srv.SpawnMob("minecraft:wolf", 1.5, 64, 0.5)
	srv.SetMobNoAI(id)
	if tame {
		srv.TameMobFor(id, "Owner")
	}
	srv.SetWolfArmor(id, "minecraft:wolf_armor", damage)
	return srv, c, id
}

// TestOpeningALootChestObtainsItsCriterion: a chest with a pending bastion
// table rolls on the open, which is a player_generates_container_loot for
// that table alone; an ordinary chest fires nothing.
// vanilla: RandomizableContainer.unpackLootTable, GenerateContainerLootTrigger
func TestOpeningALootChestObtainsItsCriterion(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	servertest.HoldTicks(t, srv)

	w.ChatCommand(`setblock 4 64 0 minecraft:chest{LootTable:"minecraft:chests/bastion_bridge"}`)
	w.Drain()
	if srv.AdvancementObtained("Testificate", "minecraft:nether/loot_bastion", "loot_bastion_bridge") {
		t.Fatal("loot_bastion_bridge was obtained before the chest was opened")
	}
	w.UseBlock(4, 64, 0)
	w.Settle()
	if !srv.AdvancementObtained("Testificate", "minecraft:nether/loot_bastion", "loot_bastion_bridge") {
		t.Error("opening the bridge chest did not obtain loot_bastion_bridge")
	}
	if srv.AdvancementObtained("Testificate", "minecraft:nether/loot_bastion", "loot_bastion_treasure") {
		t.Error("opening the bridge chest obtained loot_bastion_treasure")
	}
}
