package rules

import (
	"math"
	"math/rand"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// Fishing loot, kept as plain data because, unlike the block and mob tables
// the loot engine rolls, it reads the hook (open water) and uses functions
// (set damage, potions, enchantment levels) the generated shapes do not carry
// whole. Weights and items are the data pack's.
// vanilla: BuiltInLootTables.FISHING, data/minecraft/loot_table/gameplay/fishing*.json

// FishingLootTable is the name of the table FishingLoot rolls.
const FishingLootTable = "minecraft:gameplay/fishing"

// FishingTableNames is the fishing table and its three sub-tables, the
// names a loot_table reference or /loot may give to roll them.
var FishingTableNames = []string{
	FishingLootTable,
	FishingLootTable + "/fish",
	FishingLootTable + "/junk",
	FishingLootTable + "/treasure",
}

// FishingTableLoot rolls the fishing table or one of its sub-tables by
// name at luck 0 with no hook, so the main table never gives treasure, in
// level (nil for none); ok is false for any other name.
// vanilla: gameplay/fishing, gameplay/fishing/fish, /junk, /treasure
func FishingTableLoot(name string, rng *rand.Rand, biome string, level LootLevel) (stacks []protocol.ItemStack, ok bool) {
	if table, packed := registry.PackLootTable(name); packed && slices.Contains(FishingTableNames, name) {
		return RollLootTable(name, table, &LootContext{Level: level}, rng), true
	}
	switch name {
	case FishingLootTable:
		return FishingLoot(rng, 0, false, biome, level), true
	case FishingLootTable + "/fish":
		return fishingFish(rng), true
	case FishingLootTable + "/junk":
		return fishingJunk(rng, biome), true
	case FishingLootTable + "/treasure":
		return fishingTreasure(rng), true
	}
	return nil, false
}

// FishingLuck is the rod's bonus loot luck.
// vanilla: EnchantmentHelper.getFishingLuckBonus
func FishingLuck(rod protocol.ItemStack, ctx ...*EnchContext) int {
	value := itemEnchValue(rod, ctx, func(e *registry.EnchantmentEffects) []registry.EnchConditional { return e.FishingLuckBonus }, 0)
	return max(0, int(value))
}

// FishingLureTicks is the rod's fishing-time reduction in ticks.
// vanilla: FishingRodItem.use, EnchantmentHelper.getFishingTimeReduction
func FishingLureTicks(rod protocol.ItemStack, ctx ...*EnchContext) int {
	value := itemEnchValue(rod, ctx, func(e *registry.EnchantmentEffects) []registry.EnchConditional { return e.FishingTimeReduction }, 0)
	return int(float32(max(0, value)) * 20)
}

// FishingLoot rolls one catch from the rod's luck, open water and the biome,
// in level (nil for none), which only a data pack's table reads.
// Junk 10, treasure 5 (open water only) and fish 85, in pool order, each
// folding luck in through its quality, fishing being the last vanilla table
// that still has one. luck stays a float, as vanilla's loot luck, so a
// fractional attribute is not truncated.
// vanilla: gameplay/fishing, LootPoolSingletonContainer.getWeight
func FishingLoot(rng *rand.Rand, luck float64, openWater bool, biome string, level LootLevel) []protocol.ItemStack {
	if table, packed := registry.PackLootTable(FishingLootTable); packed {
		return RollLootTable(FishingLootTable, table, &LootContext{Level: level, Luck: float32(luck)}, rng)
	}
	junk := fishingWeight(10, -2, luck)
	fish := fishingWeight(85, -1, luck)
	treasure := 0
	if openWater {
		treasure = fishingWeight(5, 2, luck)
	}
	total := junk + fish + treasure
	if total <= 0 {
		return nil
	}
	pick := rng.Int31n(int32(total))
	switch {
	case pick < int32(junk):
		return fishingJunk(rng, biome)
	case pick < int32(junk+treasure):
		return fishingTreasure(rng)
	}
	return fishingFish(rng)
}

// fishingWeight is floor(weight + quality*luck), at least 0.
// vanilla: LootPoolSingletonContainer.getWeight
func fishingWeight(weight, quality int, luck float64) int {
	w := math.Floor(float64(weight) + float64(quality)*luck)
	if w <= 0 {
		return 0
	}
	return int(w)
}

// fishingShare is one entry of a catch's share list, in data-pack order:
// the pick walks the entries in this order, so a seeded roll reproduces
// rather than following a Go map's own iteration order.
type fishingShare struct {
	item   registry.Item
	weight int32
}

// fishingFish picks the fish the catch is: the data pack's own shares.
// vanilla: gameplay/fishing/fish
func fishingFish(rng *rand.Rand) []protocol.ItemStack {
	share := []fishingShare{
		{registry.ItemCod, 60},
		{registry.ItemSalmon, 25},
		{registry.ItemTropicalFish, 2},
		{registry.ItemPufferfish, 13},
	}
	_, stack := fishingPickOf(rng, share)
	if stack.ItemID == 0 {
		return nil
	}
	return []protocol.ItemStack{stack}
}

// fishingJunk picks a junk catch: the water bottle gets water potion
// contents, bamboo only in a jungle, and boots and rod come worn by the
// rolled fraction.
// vanilla: gameplay/fishing/junk, SetItemDamageFunction.run
func fishingJunk(rng *rand.Rand, biome string) []protocol.ItemStack {
	share := []fishingShare{
		{registry.ItemLilyPad, 17},
		{registry.ItemLeatherBoots, 10},
		{registry.ItemLeather, 10},
		{registry.ItemBone, 10},
		{registry.ItemPotion, 10},
		{registry.ItemString, 5},
		{registry.ItemFishingRod, 2},
		{registry.ItemBowl, 10},
		{registry.ItemStick, 5},
		{registry.ItemInkSac, 1},
		{registry.ItemTripwireHook, 10},
		{registry.ItemRottenFlesh, 10},
	}
	switch biome {
	case "minecraft:jungle", "minecraft:sparse_jungle", "minecraft:bamboo_jungle":
		share = append(share, fishingShare{registry.ItemBamboo, 10})
	}
	item, stack := fishingPickOf(rng, share)
	switch item {
	case registry.ItemPotion:
		stack.SetPotionContents(protocol.PotionContents{Potion: registry.MustPotion("minecraft:water")})
	case registry.ItemLeatherBoots, registry.ItemFishingRod:
		stack.Damage = fishingWear(rng, stack.ItemID, 0.9)
	case registry.ItemInkSac:
		stack.Count = 10
	}
	return []protocol.ItemStack{stack}
}

// fishingTreasure picks a treasure catch: bow, rod and book enchanted by
// enchant_with_levels 30 #on_random_loot (the book becoming an enchanted
// book), bow and rod worn by up to a quarter.
// vanilla: gameplay/fishing/treasure, EnchantWithLevelsFunction.run
func fishingTreasure(rng *rand.Rand) []protocol.ItemStack {
	share := []fishingShare{
		{registry.ItemNameTag, 1},
		{registry.ItemSaddle, 1},
		{registry.ItemBow, 1},
		{registry.ItemFishingRod, 1},
		{registry.ItemBook, 1},
		{registry.ItemNautilusShell, 1},
	}
	item, stack := fishingPickOf(rng, share)
	switch item {
	case registry.ItemBow, registry.ItemFishingRod:
		stack.Damage = fishingWear(rng, stack.ItemID, 0.25)
		stack.SetEnchantments(false, fishingEnchant(rng, stack))
	case registry.ItemBook:
		// The plain book is what the enchantment draw asks; the stack
		// becomes the enchanted book only after, as vanilla's enchantItem.
		enchanted := fishingEnchant(rng, stack)
		stack.ItemID = int32(registry.ItemEnchantedBook)
		stack.SetEnchantments(true, enchanted)
	}
	return []protocol.ItemStack{stack}
}

// fishingEnchant rolls the enchant_with_levels 30 the treasure's bow, rod
// and book carry, the levels a flat 30 and the draw the loot function's
// own on_random_loot pool.
func fishingEnchant(rng LootRand, stack protocol.ItemStack) map[int32]int32 {
	offers := SelectEnchantment(lootEnchantRand{rng}, stack, 30,
		registry.EnchantmentTagIDs("minecraft:on_random_loot"))
	if len(offers) == 0 {
		return nil
	}
	enchanted := make(map[int32]int32, len(offers))
	for _, o := range offers {
		enchanted[o.Enchantment] = o.Level
	}
	return enchanted
}

// fishingPickOf picks one item of a catch's shares and returns it with its
// stack; the item is what the callers' functions work by.
func fishingPickOf(rng *rand.Rand, share []fishingShare) (registry.Item, protocol.ItemStack) {
	var total int32
	for _, entry := range share {
		total += entry.weight
	}
	pick := rng.Int31n(total)
	for _, entry := range share {
		pick -= entry.weight
		if pick < 0 {
			// counts are the junk's own affair; one to a catch.
			return entry.item, protocol.ItemStack{ItemID: int32(entry.item), Count: 1}
		}
	}
	// unreachable in a share that carries weight
	return 0, protocol.ItemStack{}
}

// fishingWear rolls uniform remaining durability within worst's fraction,
// floored as the function does, and returns it as wear.
// vanilla: SetItemDamageFunction.run
func fishingWear(rng *rand.Rand, item int32, worst float64) int32 {
	max := registry.MaxDamage(item)
	if max <= 0 {
		return 0
	}
	f := rng.Float64() * worst
	return int32(math.Floor((1 - f) * float64(max)))
}
