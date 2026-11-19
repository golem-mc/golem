package advancements

import (
	"math"
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

// TestTamingACatObtainsItsVariantCriterion: a tamed calico cat is
// tame_an_animal and the calico entry of complete_catalogue, and no other.
// vanilla: TamableAnimal.tame, TameAnimalTrigger
func TestTamingACatObtainsItsVariantCriterion(t *testing.T) {
	t.Parallel()
	got := tameVariant(t, "minecraft:cat", "minecraft:cod", "minecraft:calico", "minecraft:husbandry/complete_catalogue")
	if !got("minecraft:calico") {
		t.Error("taming a calico cat did not obtain its complete_catalogue criterion")
	}
	if got("minecraft:tabby") || got("minecraft:black") {
		t.Error("taming a calico cat obtained another variant's criterion")
	}
}

// TestTamingACatObtainsTameAnAnimal: cats count for tame_an_animal.
// vanilla: TamableAnimal.tame, TameAnimalTrigger
func TestTamingACatObtainsTameAnAnimal(t *testing.T) {
	t.Parallel()
	got := tameVariant(t, "minecraft:cat", "minecraft:cod", "minecraft:tabby", "minecraft:husbandry/tame_an_animal")
	if !got("tamed_animal") {
		t.Error("taming a cat did not obtain tame_an_animal")
	}
}

// TestTamingAWolfObtainsItsVariantCriterion: a tamed rusty wolf is the rusty
// entry of whole_pack, and a cat's variant of the same name is not.
// vanilla: TamableAnimal.tame, TameAnimalTrigger
func TestTamingAWolfObtainsItsVariantCriterion(t *testing.T) {
	t.Parallel()
	got := tameVariant(t, "minecraft:wolf", "minecraft:bone", "minecraft:black", "minecraft:husbandry/whole_pack")
	if !got("minecraft:black") {
		t.Error("taming a black wolf did not obtain its whole_pack criterion")
	}
	if got("minecraft:ashen") {
		t.Error("taming a black wolf obtained another variant's criterion")
	}
	cat := tameVariant(t, "minecraft:cat", "minecraft:cod", "minecraft:black", "minecraft:husbandry/whole_pack")
	if cat("minecraft:black") {
		t.Error("taming a black cat obtained the black wolf's criterion")
	}
}

// TestScoopingAFishObtainsItsBucketCriterion: a water bucket used on a cod
// is a filled_bucket for the cod bucket, and for no other fish's.
// vanilla: Bucketable.bucketMobPickup, FilledBucketTrigger
func TestScoopingAFishObtainsItsBucketCriterion(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Fisher", func(s *server.Server) {
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 64, 0.5, true)
	srv.GiveSlot("Fisher", player.HotbarSlot, protocol.ItemStack{ItemID: registry.MustItem("minecraft:water_bucket"), Count: 1})
	c.Settle()
	id := srv.SpawnMob("minecraft:cod", 1.5, 64, 0.5)
	srv.SetMobNoAI(id)

	c.UseOn(id)
	c.Settle()
	const fishing = "minecraft:husbandry/tactical_fishing"
	if !srv.AdvancementObtained("Fisher", fishing, "cod_bucket") {
		t.Error("scooping a cod did not obtain tactical_fishing's cod_bucket")
	}
	if srv.AdvancementObtained("Fisher", fishing, "salmon_bucket") {
		t.Error("scooping a cod obtained tactical_fishing's salmon_bucket")
	}
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

// TestReelingInAFishObtainsItsFishyBusinessCriterion: the catch a reel pays
// is a fishing_rod_hooked for the item it holds; junk obtains nothing, and a
// cod obtains the cod and not the salmon.
// vanilla: FishingHook.retrieve, FishingRodHookedTrigger
func TestReelingInAFishObtainsItsFishyBusinessCriterion(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Fisher")
	srv.GiveSlot("Fisher", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:fishing_rod"), Count: 1})
	servertest.HoldTicks(t, srv)
	for x := int32(1); x <= 3; x++ {
		for z := int32(2); z <= 4; z++ {
			srv.SetBlock(x, 63, z, servertest.MustState(t, "minecraft:water", nil))
		}
	}
	c.MoveTo(2.5, 64, 1.5, 0, 40)
	const business = "minecraft:husbandry/fishy_business"

	reelSeeded(t, srv, c, "Fisher", seedCatching(t, ""))
	for _, name := range []string{"cod", "salmon", "pufferfish", "tropical_fish"} {
		if srv.AdvancementObtained("Fisher", business, name) {
			t.Fatalf("a junk catch obtained fishy_business's %s", name)
		}
	}
	reelSeeded(t, srv, c, "Fisher", seedCatching(t, "minecraft:cod"))
	if !srv.AdvancementObtained("Fisher", business, "cod") {
		t.Error("catching a cod did not obtain fishy_business's cod")
	}
	if srv.AdvancementObtained("Fisher", business, "salmon") {
		t.Error("catching a cod obtained fishy_business's salmon")
	}
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

// TestShearingWolfArmorObtainsRemoveWolfArmor: shears on the owner's armored
// wolf take the armor off, which is the advancement; a wild wolf's armor is
// left, and so is the advancement.
// vanilla: Mob.shearItem, PlayerInteractTrigger
func TestShearingWolfArmorObtainsRemoveWolfArmor(t *testing.T) {
	t.Parallel()
	const removed = "minecraft:husbandry/remove_wolf_armor"
	srv, c, id := armoredWolfScene(t, false, 5, "minecraft:shears")
	c.UseOn(id)
	c.Settle()
	if srv.AdvancementObtained("Owner", removed, "remove_wolf_armor") {
		t.Fatal("shears on a wild wolf obtained remove_wolf_armor")
	}
	srv.TameMobFor(id, "Owner")
	c.UseOn(id)
	c.Settle()
	if item, _ := srv.WolfArmor(id); item != "" {
		t.Fatalf("the shears left %q on the owner's wolf", item)
	}
	if !srv.AdvancementObtained("Owner", removed, "remove_wolf_armor") {
		t.Error("shearing the armor off did not obtain remove_wolf_armor")
	}
}

// TestMendingWolfArmorWithScutesObtainsRepairWolfArmor: a scute that leaves
// the armor with damage left is no advancement; the one that mends it fully
// is.
// vanilla: Wolf.mobInteract, PlayerInteractTrigger
func TestMendingWolfArmorWithScutesObtainsRepairWolfArmor(t *testing.T) {
	t.Parallel()
	const repaired = "minecraft:husbandry/repair_wolf_armor"
	srv, c, id := armoredWolfScene(t, true, 20, "minecraft:armadillo_scute")
	srv.SetWolfStance(id, true)
	c.UseOn(id)
	c.Settle()
	if _, damage := srv.WolfArmor(id); damage != 12 {
		t.Fatalf("armor damage after a scute = %d, want 12", damage)
	}
	if srv.AdvancementObtained("Owner", repaired, "repair_wolf_armor") {
		t.Fatal("a partial mend obtained repair_wolf_armor")
	}
	srv.SetWolfArmor(id, "minecraft:wolf_armor", 8)
	c.UseOn(id)
	c.Settle()
	if _, damage := srv.WolfArmor(id); damage != 0 {
		t.Fatalf("armor damage after the last scute = %d, want 0", damage)
	}
	if !srv.AdvancementObtained("Owner", repaired, "repair_wolf_armor") {
		t.Error("mending the armor fully did not obtain repair_wolf_armor")
	}
}

// TestBrushingAnArmadilloObtainsIsntItScute: a brush on a baby passes and
// fires nothing; the scute brushed off an adult is the advancement.
// vanilla: Armadillo.mobInteract, ServerGamePacketListenerImpl.handleInteract
func TestBrushingAnArmadilloObtainsIsntItScute(t *testing.T) {
	t.Parallel()
	const scute = "minecraft:adventure/brush_armadillo"
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:brush", 1)
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob("minecraft:armadillo", 1.5, 64, 2.5)
	srv.SetMobBaby(id, true)
	c.UseOn(id)
	c.Settle()
	if srv.AdvancementObtained("Rancher", scute, "brush_armadillo") {
		t.Fatal("a brush on a baby armadillo obtained brush_armadillo")
	}
	srv.SetMobBaby(id, false)
	c.UseOn(id)
	c.Settle()
	if !srv.AdvancementObtained("Rancher", scute, "brush_armadillo") {
		t.Error("brushing a scute off an adult did not obtain brush_armadillo")
	}
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

// TestBreakingALootShulkerBoxObtainsItsCriterion: a survival player breaking
// a shulker box with a pending bastion table rolls it for themselves, which
// is a player_generates_container_loot for that table.
// vanilla: ShulkerBoxBlock.playerWillDestroy, RandomizableContainer.unpackLootTable
func TestBreakingALootShulkerBoxObtainsItsCriterion(t *testing.T) {
	t.Parallel()
	const name, adv = "Breaker", "minecraft:nether/loot_bastion"
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	srv.GiveSlot(name, player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:netherite_pickaxe"), Count: 1})
	pos := srv.PlayerPosition(name)
	x, y, z := int32(math.Floor(pos.X))+1, int32(math.Floor(pos.Y)), int32(math.Floor(pos.Z))
	srv.SetBlock(x, y, z, registry.BlockStates["minecraft:shulker_box"])
	srv.SetBlockEntityTagAt(x, y, z, "minecraft:shulker_box",
		nbt.Compound{"LootTable": nbt.String("minecraft:chests/bastion_bridge")})
	c.Settle()
	if srv.AdvancementObtained(name, adv, "loot_bastion_bridge") {
		t.Fatal("loot_bastion_bridge was obtained before the box was broken")
	}

	c.BreakBlock(x, y, z)
	c.Settle()
	servertest.UntilTicks(t, srv, 40, "the shulker box's break", func() bool {
		return srv.BlockAt(x, y, z) == registry.BlockStates["minecraft:air"]
	})
	if !srv.AdvancementObtained(name, adv, "loot_bastion_bridge") {
		t.Error("breaking the bridge shulker box did not obtain loot_bastion_bridge")
	}
}

// vanilla: ShulkerBoxBlock.playerWillDestroy, RandomizableContainerBlockEntity.isEmpty
func TestCreativeBreakingALootShulkerBoxGrantsNoCriterion(t *testing.T) {
	t.Parallel()
	const name, adv = "Breaker", "minecraft:nether/loot_bastion"
	cfg := servertest.BlockTestConfig()
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	c.MoveToGround(3.5, 64, 3.5, true)
	c.Settle()
	servertest.Setblock(t, c, 3, 64, 0, `shulker_box{LootTable:"minecraft:chests/bastion_bridge"}`)
	c.BreakBlock(3, 64, 0)
	var drop protocol.ItemStack
	servertest.CollectDropsTicks(c, srv, 100, func(stack protocol.ItemStack) bool {
		if registry.Item(stack.ItemID) != registry.ItemShulkerBox {
			return false
		}
		drop = stack
		return true
	})
	if drop.Empty() || len(drop.Container) == 0 {
		t.Fatalf("creative drop = %+v, want generated contents", drop)
	}
	if srv.AdvancementObtained(name, adv, "loot_bastion_bridge") {
		t.Error("a creative break rolled the table for the player and granted loot_bastion_bridge")
	}
}

// TestBrushingALootBlockObtainsItsCriterion: the first stroke on suspicious
// sand with a pending desert pyramid table rolls it, which is a
// player_generates_container_loot for that table alone; a later stroke on
// the unpacked block fires nothing.
// vanilla: BrushableBlockEntity.unpackLootTable, GenerateContainerLootTrigger
func TestBrushingALootBlockObtainsItsCriterion(t *testing.T) {
	t.Parallel()
	const name, adv = "Brusher", "minecraft:adventure/salvage_sherd"
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	srv.GiveSlot(name, player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:brush"), Count: 1})
	pos := srv.PlayerPosition(name)
	c.MoveTo(pos.X, pos.Y, pos.Z, 0, 90)
	c.Settle()
	x, y, z := int32(math.Floor(pos.X)), int32(math.Floor(pos.Y))-1, int32(math.Floor(pos.Z))
	srv.SetBlock(x, y, z, servertest.BlockStateWith(t, "minecraft:suspicious_sand", "dusted=0"))
	srv.SetBlockEntityTagAt(x, y, z, "minecraft:brushable_block",
		nbt.Compound{"LootTable": nbt.String("minecraft:archaeology/desert_pyramid")})

	c.Send(protocol.IDPlayUseItemOn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.HandMain)
		b.PutPosition(x, y, z)
		b.PutVarInt(protocol.FaceTop)
		b.PutF32(0.5)
		b.PutF32(1)
		b.PutF32(0.5)
		b.PutBool(false)
		b.PutBool(false)
		b.PutVarInt(1)
	})
	c.Settle()
	servertest.RunTicks(t, srv, 4)
	if srv.AdvancementObtained(name, adv, "desert_pyramid") {
		t.Fatal("desert_pyramid was obtained before the first stroke")
	}
	servertest.RunTicks(t, srv, 1) // the first stroke
	if !srv.AdvancementObtained(name, adv, "desert_pyramid") {
		t.Fatal("the first stroke did not obtain desert_pyramid")
	}
	if srv.AdvancementObtained(name, adv, "desert_well") {
		t.Error("the desert pyramid roll obtained desert_well")
	}
	if tag, _ := srv.BlockEntityTagAt(x, y, z); tag["LootTable"] != nil {
		t.Fatalf("the loot table is still pending after the first stroke: %v", tag)
	}

	c.ChatCommand("advancement revoke @s only " + adv + " desert_pyramid")
	c.Settle()
	if srv.AdvancementObtained(name, adv, "desert_pyramid") {
		t.Fatal("desert_pyramid is still obtained after the revoke")
	}
	servertest.RunTicks(t, srv, 10) // the second stroke
	if got := srv.BlockAt(x, y, z); got == registry.BlockStates["minecraft:sand"] {
		t.Fatal("the block finished before the second stroke was checked")
	}
	if srv.AdvancementObtained(name, adv, "desert_pyramid") {
		t.Error("a stroke on the unpacked block obtained desert_pyramid again")
	}
}

// TestFeedingABabySnifferObtainsLittleSniffs: feeding a sniffer its seeds is
// feed_snifflet only while the sniffer is a baby.
// vanilla: Animal.mobInteract, PlayerInteractTrigger
func TestFeedingABabySnifferObtainsLittleSniffs(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:torchflower_seeds", 2)
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob("minecraft:sniffer", 1.5, 64, 0.5)
	srv.SetMobNoAI(id)

	const snifflet = "minecraft:husbandry/feed_snifflet"
	c.UseOn(id)
	c.Settle()
	if srv.AdvancementObtained("Rancher", snifflet, "feed_snifflet") {
		t.Fatal("feeding an adult sniffer obtained feed_snifflet")
	}
	srv.SetMobBaby(id, true)
	c.UseOn(id)
	c.Settle()
	if !srv.AdvancementObtained("Rancher", snifflet, "feed_snifflet") {
		t.Error("feeding a baby sniffer did not obtain feed_snifflet")
	}
}

// TestLeashingEachFrogVariantObtainsItsCriterion: a lead tied to a frog
// obtains that frog's variant entry of leash_all_frog_variants, and only it.
// vanilla: Entity.interact, PlayerInteractTrigger
func TestLeashingEachFrogVariantObtainsItsCriterion(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:lead", 3)
	servertest.HoldTicks(t, srv)

	const leashAll = "minecraft:husbandry/leash_all_frog_variants"
	variants := []string{"minecraft:cold", "minecraft:temperate", "minecraft:warm"}
	at := [][2]float64{{1.5, 0.5}, {0.5, 1.5}, {-0.5, 0.5}}
	frogs := make([]int32, len(variants))
	for i, variant := range variants {
		frogs[i] = srv.SpawnMob("minecraft:frog", at[i][0], 64, at[i][1])
		srv.SetMobNoAI(frogs[i])
		srv.SetMobVariant(frogs[i], variant)
	}
	for i, variant := range variants {
		if srv.AdvancementObtained("Rancher", leashAll, variant) {
			t.Fatalf("%s was obtained before its frog was leashed", variant)
		}
		c.UseOn(frogs[i])
		c.Settle()
		for j, other := range variants {
			if got := srv.AdvancementObtained("Rancher", leashAll, other); got != (j <= i) {
				t.Errorf("after leashing the %s frog, %s obtained = %v", variant, other, got)
			}
		}
	}
}

// TestHandingAPiglinGoldObtainsOhShiny: gold handed to an adult piglin is
// distract_piglin_directly, unless the player wears gold armour.
// vanilla: PiglinAi.mobInteract, PlayerInteractTrigger
func TestHandingAPiglinGoldObtainsOhShiny(t *testing.T) {
	t.Parallel()
	const distract = "minecraft:nether/distract_piglin"
	for _, tc := range []struct {
		name    string
		helmet  bool
		obtains bool
	}{
		{"bare", false, true},
		{"golden helmet", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.BreedingPlayer(t, "survival", "Trader", "minecraft:gold_ingot", 4)
			servertest.HoldTicks(t, srv)
			if tc.helmet {
				srv.GiveSlot("Trader", player.ArmorSlot, servertest.ItemStack(t, "minecraft:golden_helmet", 1))
			}
			id := srv.SpawnMob("minecraft:piglin", 1.5, 64, 0.5)
			srv.SetMobNoAI(id)
			c.UseOn(id)
			c.Settle()
			if got := srv.AdvancementObtained("Trader", distract, "distract_piglin_directly"); got != tc.obtains {
				t.Errorf("handing a piglin gold obtained distract_piglin_directly = %v, want %v", got, tc.obtains)
			}
		})
	}
}
