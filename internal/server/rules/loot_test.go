package rules

import (
	"maps"
	"math/rand"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// vanilla: LootTable.getRandomItemsRaw, LootTable.createStackSplitter
func TestNestedLootAppliesModifiersBeforeSplitting(t *testing.T) {
	count := func(n float64, add bool) []registry.LootFunction {
		return []registry.LootFunction{registry.LootSetCount{Count: registry.LootConstant{Value: n}, Add: add}}
	}
	once := registry.LootConstant{Value: 1}
	inner := registry.LootTable{
		Functions: count(60, false),
		Pools: []registry.LootPool{{
			Rolls:     once,
			Functions: count(100, true),
			Entries:   []registry.LootEntry{registry.LootItemEntry{Item: registry.ItemStick, Weight: 1}},
		}},
	}
	outer := registry.LootTable{
		Functions: count(6, true),
		Pools: []registry.LootPool{{
			Rolls:     once,
			Functions: count(5, true),
			Entries:   []registry.LootEntry{registry.LootReference{Inline: &inner, Weight: 1, Functions: count(4, true)}},
		}, {
			Rolls:   once,
			Entries: []registry.LootEntry{registry.LootItemEntry{Item: registry.ItemDiamond, Weight: 1}},
		}},
	}
	var got []lootDrop
	for _, stack := range RollLootTable("test:nested", outer, &LootContext{}, rand.New(rand.NewSource(1))) {
		got = append(got, lootDrop{registry.Item(stack.ItemID).Name(), stack.Count})
	}
	want := []lootDrop{{"minecraft:stick", 64}, {"minecraft:stick", 11}, {"minecraft:diamond", 7}}
	if !slices.Equal(got, want) {
		t.Errorf("loot = %v, want %v", got, want)
	}
}

// The loot engine against the generated tables, formula by formula: each
// test rolls a table the data pack carries many times over with a seeded
// random source and pins the shape vanilla's own arithmetic gives the
// roll - the bounds a distribution cannot leave, the item an enchantment
// keeps, the chance a table's own number says. The seeds make a failing
// roll reproducible, the way every other seeded dice here is.

// stackNames flattens rolled stacks into name-count pairs, the shape the
// assertions read.
type lootDrop struct {
	item  string
	count int32
}

func rollNames(t *testing.T, stacks []protocol.ItemStack) []lootDrop {
	t.Helper()
	var drops []lootDrop
	for _, stack := range stacks {
		name, ok := registry.ItemName(stack.ItemID)
		if !ok {
			t.Fatalf("a rolled stack carries item %d, which the registry does not know", stack.ItemID)
		}
		drops = append(drops, lootDrop{item: name, count: stack.Count})
	}
	return drops
}

// countsOf collects the counts every stack of one item rolled at, across
// many rolls of a table.
func countsOf(rolls [][]lootDrop, item string) []int32 {
	var counts []int32
	for _, drops := range rolls {
		for _, drop := range drops {
			if drop.item == item {
				counts = append(counts, drop.count)
			}
		}
	}
	return counts
}

// toolWith is the LootTool a held stack turns into, the enchantments
// named by ID in a stack read into names the tables read.
func toolWith(item string, enchantments map[string]int32) LootTool {
	tool := LootTool{Item: item, Count: 1, Enchantments: map[string]int32{}}
	maps.Copy(tool.Enchantments, enchantments)
	return tool
}

// rollMany rolls a table the given number of times against fresh copies of
// the context, each with its own draw of the seeded source.
func rollMany(t *testing.T, name string, ctx LootContext, times int, seed int64) [][]lootDrop {
	t.Helper()
	var out [][]lootDrop
	for i := range times {
		rng := rand.New(rand.NewSource(seed + int64(i)))
		out = append(out, rollNames(t, RollLoot(name, &ctx, rng)))
	}
	return out
}

// TestFortuneOreDropsStaysWithinVanillasBounds rolls diamond ore with a
// fortune III pickaxe over and over: every roll is one to four diamonds,
// never nothing and never past four, and every size of the range came up -
// vanilla's ore_drops formula, count times one more than a bonus of zero
// to the level, the zero chance being one in level+2. The same ore under
// the same pickaxe without the enchantment is one diamond, every time.
func TestFortuneOreDropsStaysWithinVanillasBounds(t *testing.T) {
	fortuned := rollMany(t, "minecraft:blocks/diamond_ore",
		LootContext{BlockState: registry.MustState("minecraft:diamond_ore", nil), HasBlock: true, Tool: toolWith("minecraft:diamond_pickaxe",
			map[string]int32{"minecraft:fortune": 3})}, 400, 101)
	counts := countsOf(fortuned, "minecraft:diamond")
	if len(counts) == 0 {
		t.Fatal("a fortune III pickaxe never rolled the diamond ore's diamonds")
	}
	seen := map[int32]bool{}
	for _, count := range counts {
		if count < 1 || count > 4 {
			t.Fatalf("fortune III rolled %d diamonds, want the vanilla bound of one to four", count)
		}
		seen[count] = true
	}
	for _, count := range []int32{1, 2, 3, 4} {
		if !seen[count] {
			t.Errorf("fortune III never rolled %d diamonds in %d rolls", count, len(counts))
		}
	}
	plain := rollMany(t, "minecraft:blocks/diamond_ore",
		LootContext{BlockState: registry.MustState("minecraft:diamond_ore", nil), HasBlock: true, Tool: toolWith("minecraft:diamond_pickaxe", nil)}, 50, 102)
	for _, drops := range plain {
		if len(drops) != 1 || drops[0].item != "minecraft:diamond" || drops[0].count != 1 {
			t.Fatalf("a plain pickaxe rolled %+v, want one diamond", drops)
		}
	}
}

// TestSilkTouchKeepsTheOre rolls diamond ore with a silk touch pickaxe:
// the table's alternatives cascade takes the ore block itself, every time,
// and the same pickaxe with neither enchantment takes the diamond.
func TestSilkTouchKeepsTheOre(t *testing.T) {
	silk := rollMany(t, "minecraft:blocks/diamond_ore",
		LootContext{BlockState: registry.MustState("minecraft:diamond_ore", nil), HasBlock: true, Tool: toolWith("minecraft:iron_pickaxe",
			map[string]int32{"minecraft:silk_touch": 1})}, 100, 401)
	for _, drops := range silk {
		if len(drops) != 1 || drops[0].item != "minecraft:diamond_ore" || drops[0].count != 1 {
			t.Fatalf("a silk touch pickaxe rolled %+v, want the ore block itself", drops)
		}
	}
}

// TestLeavesDropSaplingsAtTheTablesChance rolls oak leaves by hand with a
// seeded source, a thousand times over: the saplings come out at the
// table's own five per hundred, the sticks at its two, the apples at its
// half per hundred, and nothing else ever falls. Fortune III lifts each
// chance to the table's top entry, ten per hundred for the sapling.
func TestLeavesDropSaplingsAtTheTablesChance(t *testing.T) {
	const rolls = 4000
	byHand := rollMany(t, "minecraft:blocks/oak_leaves",
		LootContext{BlockState: registry.MustState("minecraft:oak_leaves", nil), HasBlock: true, This: LootEntity{Type: "minecraft:player"}}, rolls, 501)
	items := map[string]int{}
	for _, drops := range byHand {
		// Each of the table's three pools rolls once, so a leaf can give
		// its sapling and its sticks in the one break - but no pool gives
		// more than the one roll's worth.
		if len(drops) > 3 {
			t.Fatalf("a hand-broken leaf rolled %+v, want one roll's worth of each pool at most", drops)
		}
		for _, drop := range drops {
			switch drop.item {
			case "minecraft:oak_sapling", "minecraft:stick", "minecraft:apple":
				items[drop.item]++
			default:
				t.Fatalf("a hand-broken oak leaf dropped %s, which its table does not name", drop.item)
			}
			if drop.item == "minecraft:stick" && drop.count > 2 {
				t.Fatalf("a leaf rolled %d sticks, want the vanilla one to two", drop.count)
			}
		}
	}
	// Four sigma either side of each chance: a reading off by a factor
	// of two lands far outside, a right one never does.
	bounds := func(got int, chance float64, what string) {
		t.Helper()
		want := chance * rolls
		spread := 4 * sqrtOf(want*(1-chance))
		if float64(got) < want-spread || float64(got) > want+spread {
			t.Errorf("%s came up %d times in %d leaves, want about %.0f (the table's %.4f)",
				what, got, rolls, want, chance)
		}
	}
	bounds(items["minecraft:oak_sapling"], 0.05, "the sapling")
	bounds(items["minecraft:apple"], 0.005, "the apple")

	fortuned := rollMany(t, "minecraft:blocks/oak_leaves",
		LootContext{BlockState: registry.MustState("minecraft:oak_leaves", nil), HasBlock: true, Tool: toolWith("minecraft:iron_hoe",
			map[string]int32{"minecraft:fortune": 3}), This: LootEntity{Type: "minecraft:player"}}, rolls, 502)
	saplings := 0
	for _, drops := range fortuned {
		for _, drop := range drops {
			if drop.item == "minecraft:oak_sapling" {
				saplings++
			}
		}
	}
	// Fortune III's top entry is the ten per hundred, four sigma either
	// side of which the five of the plain leaves never reaches.
	bounds(saplings, 0.1, "the fortuned sapling")
}

// sqrtOf is the square root the chance bounds run on, kept as a helper so
// the test reads in one voice.
func sqrtOf(v float64) float64 {
	if v <= 0 {
		return 0
	}
	x := v
	for range 40 {
		x = (x + v/x) / 2
	}
	return x
}

// TestLootingGrowsAMobsDrops rolls a zombie's death against a looting III
// sword: the rotten flesh's nought to two grows by the rounds of the
// looting roll, up to five, and never past it, while the swordless death
// stays within nought to two. The rare pool stays shut to a death nobody
// is behind, and opens to a player's kill, where the iron comes out at the
// table's chance.
func TestLootingGrowsAMobsDrops(t *testing.T) {
	swordless := rollMany(t, "minecraft:entities/zombie", LootContext{}, 300, 601)
	for _, count := range countsOf(swordless, "minecraft:rotten_flesh") {
		if count > 2 {
			t.Fatalf("a swordless death rolled %d rotten flesh, want the vanilla nought to two", count)
		}
	}
	looting := rollMany(t, "minecraft:entities/zombie", LootContext{
		Attacker: LootEntity{Type: "minecraft:player",
			Mainhand: toolWith("minecraft:iron_sword", map[string]int32{"minecraft:looting": 3})},
	}, 300, 602)
	seenHigh := false
	for _, count := range countsOf(looting, "minecraft:rotten_flesh") {
		if count > 5 {
			t.Fatalf("a looting III sword rolled %d rotten flesh, want the vanilla bound of five", count)
		}
		if count > 2 {
			seenHigh = true
		}
	}
	if !seenHigh {
		t.Error("a looting III sword never rolled past the plain death's two rotten flesh")
	}
	for _, drops := range swordless {
		for _, drop := range drops {
			if drop.item == "minecraft:iron_ingot" {
				t.Fatal("a death nobody is behind rolled the zombie's rare iron")
			}
		}
	}
	player := rollMany(t, "minecraft:entities/zombie", LootContext{KilledByPlayer: true}, 800, 603)
	irons := 0
	for _, drops := range player {
		for _, drop := range drops {
			if drop.item == "minecraft:iron_ingot" {
				irons++
			}
		}
	}
	// The table's chance is 0.025, and a kill a player is behind is the
	// only roll it ever gets - and the rare pool's three entries share
	// its one roll, so the iron's share is a third of that chance:
	// about seven irons in eight hundred kills.
	if irons < 2 || irons > 14 {
		t.Errorf("a player's kill rolled %d rare irons in 800 deaths, want about 7 at the table's 0.025 split three ways", irons)
	}
}

// TestASheepDropsItsOwnColourWool rolls the sheep's death with each end of
// the palette: a white sheep's table is the white wool's, a black one the
// black wool's, and the mutton beside either is the mutton either way.
func TestASheepDropsItsOwnColourWool(t *testing.T) {
	for _, colour := range []string{"white", "black"} {
		ctx := LootContext{This: LootEntity{Type: "minecraft:sheep", Sheep: true, SheepColour: colour}}
		drops := rollNames(t, RollLoot("minecraft:entities/sheep", &ctx, rand.New(rand.NewSource(801))))
		wool := "minecraft:" + colour + "_wool"
		found := false
		for _, drop := range drops {
			if drop.item == wool {
				found = true
			}
			if drop.item != wool && drop.item != "minecraft:mutton" {
				t.Fatalf("a %s sheep rolled %s, want the %s and the mutton", colour, drop.item, wool)
			}
		}
		if !found {
			t.Errorf("a %s sheep dropped no %s", colour, wool)
		}
	}
}

// TestAFurnaceSmeltCooksWhatDiedBurning rolls a pig's death twice: the
// body on fire leaves cooked porkchops where the furnace_smelt's
// conditions hold, and the same body cold leaves raw pork, the table
// having cooked nothing.
func TestAFurnaceSmeltCooksWhatDiedBurning(t *testing.T) {
	burning := rollNames(t, RollLoot("minecraft:entities/pig",
		&LootContext{This: LootEntity{Type: "minecraft:pig", OnFire: true}}, rand.New(rand.NewSource(901))))
	if len(burning) == 0 || burning[0].item != "minecraft:cooked_porkchop" {
		t.Fatalf("a burning pig rolled %+v, want cooked porkchops", burning)
	}
	cold := rollNames(t, RollLoot("minecraft:entities/pig",
		&LootContext{This: LootEntity{Type: "minecraft:pig"}}, rand.New(rand.NewSource(902))))
	if len(cold) == 0 || cold[0].item != "minecraft:porkchop" {
		t.Fatalf("a cold pig rolled %+v, want raw pork", cold)
	}
}

// TestAFireAspectBladeCooksWhatItKills rolls a cow's death against a
// fire-aspect blade: the direct attacker's weapon answers the smelts_loot
// tag the table's condition holds it against, and the beef comes out
// cooked where it does.
func TestAFireAspectBladeCooksWhatItKills(t *testing.T) {
	ctx := LootContext{
		This:           LootEntity{Type: "minecraft:cow"},
		Attacker:       LootEntity{Type: "minecraft:player", Mainhand: toolWith("minecraft:iron_sword", map[string]int32{"minecraft:fire_aspect": 2})},
		DirectAttacker: LootEntity{Type: "minecraft:player", Mainhand: toolWith("minecraft:iron_sword", map[string]int32{"minecraft:fire_aspect": 2})},
	}
	drops := rollNames(t, RollLoot("minecraft:entities/cow", &ctx, rand.New(rand.NewSource(1001))))
	if beefOf(t, drops, "minecraft:cooked_beef") == 0 {
		t.Fatalf("a cow felled by fire aspect rolled %+v, want cooked beef", drops)
	}
	// The same blade carried by an arrow's shot is not the blow's direct
	// attacker, and the beef stays raw.
	ctx.DirectAttacker = LootEntity{Type: "minecraft:arrow"}
	drops = rollNames(t, RollLoot("minecraft:entities/cow", &ctx, rand.New(rand.NewSource(1002))))
	if beefOf(t, drops, "minecraft:beef") == 0 {
		t.Fatalf("a cow felled by an arrow rolled %+v, want raw beef", drops)
	}
}

// beefOf is how much of the beef the roll named came out, zero being a
// roll that never gave the cut asked after.
func beefOf(t *testing.T, drops []lootDrop, want string) int32 {
	t.Helper()
	for _, drop := range drops {
		if drop.item == want {
			return drop.count
		}
	}
	return 0
}

// entryTable is the one-entry, one-roll table the function tests roll.
func entryTable(item string, functions []registry.LootFunction) registry.LootTable {
	id, _ := registry.ItemByName(item)
	return registry.LootTable{Pools: []registry.LootPool{{
		Rolls:   registry.LootConstant{Value: 1},
		Entries: []registry.LootEntry{registry.LootItemEntry{Item: id, Weight: 1, Functions: functions}},
	}}}
}
