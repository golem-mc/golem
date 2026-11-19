package rules

import (
	"maps"
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

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

// TestOreDropsWidensLapis rolls lapis ore under a fortune III pickaxe:
// the ore's ore_drops formula multiplies the table's own count by one more
// than a bonus of nought to the level, so the four to nine lapis of the
// plain pickaxe widen to four to thirty-six and nothing lands outside it,
// every size of the multiplication having come.
func TestOreDropsWidensLapis(t *testing.T) {
	plain := rollMany(t, "minecraft:blocks/lapis_ore",
		LootContext{BlockState: registry.MustState("minecraft:lapis_ore", nil), HasBlock: true, Tool: toolWith("minecraft:iron_pickaxe", nil)}, 200, 201)
	for _, count := range countsOf(plain, "minecraft:lapis_lazuli") {
		if count < 4 || count > 9 {
			t.Fatalf("a plain pickaxe rolled %d lapis, want the vanilla four to nine", count)
		}
	}
	fortuned := rollMany(t, "minecraft:blocks/lapis_ore",
		LootContext{BlockState: registry.MustState("minecraft:lapis_ore", nil), HasBlock: true, Tool: toolWith("minecraft:iron_pickaxe",
			map[string]int32{"minecraft:fortune": 3})}, 600, 202)
	seen := map[int32]bool{}
	for _, count := range countsOf(fortuned, "minecraft:lapis_lazuli") {
		if count < 4 || count > 36 {
			t.Fatalf("fortune III rolled %d lapis, want the vanilla bound of four to thirty-six", count)
		}
		seen[count] = true
	}
	for _, count := range []int32{4, 9, 18, 27, 36} {
		if !seen[count] {
			t.Errorf("fortune III never rolled %d lapis in %d rolls", count, len(countsOf(fortuned, "minecraft:lapis_lazuli")))
		}
	}
}

// TestUniformBonusCountWidensRedstone rolls redstone ore under a fortune
// III pickaxe: the ore's uniform_bonus_count formula adds a roll between
// nothing and the level to the table's own count, so the four to five dust
// of the plain pickaxe widen to four to eight and nothing lands outside
// it.
func TestUniformBonusCountWidensRedstone(t *testing.T) {
	plain := rollMany(t, "minecraft:blocks/redstone_ore",
		LootContext{BlockState: registry.MustState("minecraft:redstone_ore", nil), HasBlock: true, Tool: toolWith("minecraft:iron_pickaxe", nil)}, 200, 221)
	for _, count := range countsOf(plain, "minecraft:redstone") {
		if count < 4 || count > 5 {
			t.Fatalf("a plain pickaxe rolled %d redstone, want the vanilla four to five", count)
		}
	}
	fortuned := rollMany(t, "minecraft:blocks/redstone_ore",
		LootContext{BlockState: registry.MustState("minecraft:redstone_ore", nil), HasBlock: true, Tool: toolWith("minecraft:iron_pickaxe",
			map[string]int32{"minecraft:fortune": 3})}, 400, 222)
	seenHigh := false
	for _, count := range countsOf(fortuned, "minecraft:redstone") {
		if count < 4 || count > 8 {
			t.Fatalf("fortune III rolled %d redstone, want the vanilla bound of four to eight", count)
		}
		if count > 5 {
			seenHigh = true
		}
	}
	if !seenHigh {
		t.Error("fortune III never rolled past the plain ore's five redstone")
	}
}

// TestABinomialCountStaysInItsRounds rolls a ripe melon stem, whose seeds
// are the binomial provider: three coins a stem, each landing with the
// chance the ripe age's table carries, so the seeds come out nowhere to
// three and every size of that came up, and never a fourth.
func TestABinomialCountStaysInItsRounds(t *testing.T) {
	// The ripe age's own set_count is the last of the eight the entry
	// carries, its binomial n=3 at p=0.533: the seeds come out nought to
	// three, a nought rolling as nothing rather than as an absence.
	fortuned := rollMany(t, "minecraft:blocks/melon_stem",
		LootContext{BlockState: registry.MustState("minecraft:melon_stem", map[string]string{"age": "7"}), HasBlock: true}, 400, 301)
	counts := countsOf(fortuned, "minecraft:melon_seeds")
	if len(counts) == 0 {
		t.Fatal("a ripe melon stem never rolled its seeds")
	}
	seen := map[int32]bool{}
	for _, count := range counts {
		if count < 1 || count > 3 {
			t.Fatalf("a ripe melon stem rolled %d seeds, want the vanilla nought to three", count)
		}
		seen[count] = true
	}
	for _, count := range []int32{1, 2, 3} {
		if !seen[count] {
			t.Errorf("a ripe melon stem never rolled %d seeds in %d rolls", count, len(counts))
		}
	}
	// A coin's run of three misses is about a roll in ten at the ripe
	// age's chance, and a stem that rolled nothing gave nothing.
	if empties := 400 - len(counts); empties < 15 || empties > 70 {
		t.Errorf("a ripe melon stem rolled nothing %d times in 400, want about the forty the binomial gives", empties)
	}
}

// TestALimitCountCapsTheRoll rolls a melon under a fortune III hoe: the
// table's uniform three to seven widens by the uniform bonus and is then
// capped by the limit_count at nine, so the slices land nowhere past nine
// even though the bonus alone could climb to ten. A silk touch tool keeps
// the melon itself, the alternatives cascade's first leg.
func TestALimitCountCapsTheRoll(t *testing.T) {
	fortuned := rollMany(t, "minecraft:blocks/melon",
		LootContext{BlockState: registry.MustState("minecraft:melon", nil), HasBlock: true, Tool: toolWith("minecraft:iron_hoe",
			map[string]int32{"minecraft:fortune": 3})}, 600, 311)
	counts := countsOf(fortuned, "minecraft:melon_slice")
	if len(counts) == 0 {
		t.Fatal("a fortuned melon never rolled its slices")
	}
	seenHigh := false
	for _, count := range counts {
		if count < 3 || count > 9 {
			t.Fatalf("a fortuned melon rolled %d slices, want the vanilla bound of three to nine", count)
		}
		if count == 9 {
			seenHigh = true
		}
	}
	if !seenHigh {
		t.Error("a fortuned melon never rolled the nine the limit_count caps it at")
	}
	silk := rollMany(t, "minecraft:blocks/melon",
		LootContext{BlockState: registry.MustState("minecraft:melon", nil), HasBlock: true, Tool: toolWith("minecraft:iron_hoe",
			map[string]int32{"minecraft:silk_touch": 1})}, 50, 312)
	for _, drops := range silk {
		if len(drops) != 1 || drops[0].item != "minecraft:melon" || drops[0].count != 1 {
			t.Fatalf("a silk touch tool rolled %+v, want the melon itself", drops)
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

// TestExplosionDecayRollsTheRadius rolls stone in a blast of radius four:
// the survives_explosion condition and the decay each keep one stack in
// four, so about a quarter of the rolls give anything and what they give
// is one cobblestone, while the same table outside a blast gives it every
// time.
func TestExplosionDecayRollsTheRadius(t *testing.T) {
	var dropped int
	for i := range 800 {
		rng := rand.New(rand.NewSource(int64(701 + i)))
		stacks := RollLoot("minecraft:blocks/stone",
			&LootContext{BlockState: registry.MustState("minecraft:stone", nil), HasBlock: true, Radius: 4}, rng)
		dropped += len(stacks)
	}
	// One in four survives, four sigma either side of two hundred in
	// eight hundred.
	if dropped < 160 || dropped > 240 {
		t.Errorf("a radius-four blast left %d of 800 stones dropped, want about 200 at the decay's quarter", dropped)
	}
	peaceful := rollMany(t, "minecraft:blocks/stone",
		LootContext{BlockState: registry.MustState("minecraft:stone", nil), HasBlock: true}, 50, 702)
	for _, drops := range peaceful {
		if len(drops) != 1 || drops[0].item != "minecraft:cobblestone" {
			t.Fatalf("a stone outside a blast rolled %+v, want one cobblestone", drops)
		}
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

// TestAFurnaceSmeltKeepsTheStackCount rolls a burning cow's death many
// times: the smelt runs after the table's set_count of 1-3, so the cooked
// beef keeps that count rather than the recipe's single result.
func TestAFurnaceSmeltKeepsTheStackCount(t *testing.T) {
	rng := rand.New(rand.NewSource(1301))
	seen := map[int32]bool{}
	for range 300 {
		drops := rollNames(t, RollLoot("minecraft:entities/cow",
			&LootContext{This: LootEntity{Type: "minecraft:cow", OnFire: true}}, rng))
		total := int32(0)
		for _, d := range drops {
			switch d.item {
			case "minecraft:beef":
				t.Fatalf("a burning cow rolled raw beef: %+v", drops)
			case "minecraft:cooked_beef":
				total += d.count
			}
		}
		if total < 1 || total > 3 {
			t.Fatalf("a burning cow rolled %d cooked beef (%+v), want 1-3", total, drops)
		}
		seen[total] = true
	}
	if !seen[2] || !seen[3] {
		t.Fatalf("cooked beef totals seen: %v, want both 2 and 3 among them", seen)
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

// TestTableBonusChanceRisesWithTheLevel rolls gravel under each fortune
// level: the flint's chances are the table's own ladder, a tenth by hand
// and every last pebble at fortune III, where the flint always comes.
func TestTableBonusChanceRisesWithTheLevel(t *testing.T) {
	for level, chance := range map[int32]float64{0: 0.1, 3: 1.0} {
		tool := toolWith("minecraft:iron_shovel", nil)
		if level > 0 {
			tool.Enchantments["minecraft:fortune"] = level
		}
		var flints, rolls int
		for i := range 600 {
			rolls++
			drops := rollNames(t, RollLoot("minecraft:blocks/gravel",
				&LootContext{BlockState: registry.MustState("minecraft:gravel", nil), HasBlock: true, Tool: tool}, rand.New(rand.NewSource(1101+int64(level)*1000+int64(i)))))
			for _, drop := range drops {
				if drop.item == "minecraft:flint" {
					flints++
				}
			}
		}
		got := float64(flints) / float64(rolls)
		if got < chance-0.06 || got > chance+0.06 {
			t.Errorf("fortune level %d rolled flint %d of %d times (%.3f), want about %.2f",
				level, flints, rolls, got, chance)
		}
	}
}

// TestABlockStateGatesItsOwnDrops rolls a wheat crop at each end of its
// age: the ripe crop gives its seeds and its wheat, the seedling gives
// seeds alone, the table's block_state_property being the gate the
// properties answer.
func TestABlockStateGatesItsOwnDrops(t *testing.T) {
	ripe := rollNames(t, RollLoot("minecraft:blocks/wheat",
		&LootContext{BlockState: registry.MustState("minecraft:wheat", map[string]string{"age": "7"}), HasBlock: true}, rand.New(rand.NewSource(1201))))
	items := map[string]int32{}
	for _, drop := range ripe {
		items[drop.item]++
	}
	if items["minecraft:wheat"] == 0 || items["minecraft:wheat_seeds"] == 0 {
		t.Fatalf("a ripe wheat rolled %+v, want its wheat and its seeds", ripe)
	}
	young := rollNames(t, RollLoot("minecraft:blocks/wheat",
		&LootContext{BlockState: registry.MustState("minecraft:wheat", map[string]string{"age": "0"}), HasBlock: true}, rand.New(rand.NewSource(1202))))
	for _, drop := range young {
		if drop.item == "minecraft:wheat" {
			t.Fatalf("a seedling wheat rolled %+v, want seeds alone", young)
		}
	}
}

// TestTallGrassDropsOnlyBesideItsPartner rolls the tall grass halves, whose
// pools each check the other half one block away: with shears a half
// beside its partner drops two short grass, and a half alone, or a roll
// that cannot see the world, drops nothing.
func TestTallGrassDropsOnlyBesideItsPartner(t *testing.T) {
	lower, _ := registry.FindState("minecraft:tall_grass", map[string]string{"half": "lower"})
	upper, _ := registry.FindState("minecraft:tall_grass", map[string]string{"half": "upper"})
	column := func(below, above uint16) func(dx, dy, dz int32) uint16 {
		return func(dx, dy, dz int32) uint16 {
			switch {
			case dx != 0 || dz != 0:
				return airState
			case dy == -1:
				return below
			case dy == 1:
				return above
			}
			return airState
		}
	}
	for _, tc := range []struct {
		name    string
		half    string
		blockAt func(dx, dy, dz int32) uint16
		want    int32
	}{
		{"lower under its upper", "lower", column(airState, upper), 2},
		{"upper over its lower", "upper", column(lower, airState), 2},
		{"lower alone", "lower", column(airState, airState), 0},
		{"upper alone", "upper", column(airState, airState), 0},
		{"no world", "lower", nil, 0},
	} {
		ctx := LootContext{
			BlockState: registry.MustState("minecraft:tall_grass", map[string]string{"half": tc.half}),
			HasBlock:   true,
			Tool:       toolWith("minecraft:shears", nil),
			BlockAt:    tc.blockAt,
		}
		drops := rollNames(t, RollLoot(BlockLootTable(registry.BlockTallGrass), &ctx, rand.New(rand.NewSource(1))))
		var got int32
		for _, d := range drops {
			if d.item != "minecraft:short_grass" {
				t.Errorf("%s: dropped %v, want only short grass", tc.name, d)
			}
			got += d.count
		}
		if got != tc.want {
			t.Errorf("%s: dropped %d short grass, want %d", tc.name, got, tc.want)
		}
	}
}

// TestAParchedsTippedArrowNeedsAPlayer rolls the parched's death with and
// without a player behind it: the killed_by_player pool's tipped arrow of
// weakness never drops without one, and a looting III kill lays no more
// than the one arrow the pool's limit allows.
func TestAParchedsTippedArrowNeedsAPlayer(t *testing.T) {
	const table = "minecraft:entities/parched"
	nobody := rollMany(t, table, LootContext{}, 300, 1301)
	for _, drops := range nobody {
		for _, drop := range drops {
			if drop.item == "minecraft:tipped_arrow" {
				t.Fatal("a death nobody is behind rolled the parched's tipped arrow")
			}
		}
	}
	killed := rollMany(t, table, LootContext{
		KilledByPlayer: true,
		Attacker: LootEntity{Type: "minecraft:player",
			Mainhand: toolWith("minecraft:diamond_sword", map[string]int32{"minecraft:looting": 3})},
	}, 300, 1302)
	for _, count := range countsOf(killed, "minecraft:tipped_arrow") {
		if count > 1 {
			t.Fatalf("a looting III kill rolled %d tipped arrows, want the pool's limit of one", count)
		}
	}
}

// TestTheTrialChamberRewardRollsOminousBottlesWithAnAmplifier: the ominous
// reward table sets the amplifier of the bottles it gives, 2 to 4.
// vanilla: SetOminousBottleAmplifierFunction.run
func TestTheTrialChamberRewardRollsOminousBottlesWithAnAmplifier(t *testing.T) {
	bottle := registry.MustItem("minecraft:ominous_bottle")
	rng := rand.New(rand.NewSource(7))
	seen := 0
	for range 400 {
		stacks := RollLoot("minecraft:chests/trial_chambers/reward_ominous", &LootContext{}, rng)
		if len(stacks) == 0 {
			t.Fatal("the ominous reward table rolled nothing")
		}
		for _, s := range stacks {
			if s.ItemID != bottle {
				continue
			}
			seen++
			if a := s.OminousBottleAmplifierOf(); a < 2 || a > 4 {
				t.Fatalf("ominous bottle amplifier %d, want 2 to 4", a)
			}
		}
	}
	if seen == 0 {
		t.Error("no ominous bottle in 400 rolls")
	}
	if len(RollLoot("minecraft:chests/trial_chambers/reward", &LootContext{}, rng)) == 0 {
		t.Error("the reward table rolled nothing")
	}
}

// TestAZombifiedPiglinDropsFleshNuggetsAndRareGold rolls the piglin's table:
// rotten flesh and gold nuggets nought to one each, and the gold ingot only
// to a player's kill, at about 2.5 per cent.
func TestAZombifiedPiglinDropsFleshNuggetsAndRareGold(t *testing.T) {
	plain := rollMany(t, "minecraft:entities/zombified_piglin", LootContext{}, 400, 701)
	flesh, nuggets := 0, 0
	for _, drops := range plain {
		for _, drop := range drops {
			switch drop.item {
			case "minecraft:rotten_flesh":
				flesh++
			case "minecraft:gold_nugget":
				nuggets++
			case "minecraft:gold_ingot":
				t.Fatal("a death nobody is behind rolled the rare gold ingot")
			}
		}
	}
	if flesh < 100 || flesh > 300 || nuggets < 100 || nuggets > 300 {
		t.Errorf("400 deaths gave %d rotten flesh and %d gold nuggets, want about 200 of each", flesh, nuggets)
	}
	ingots := 0
	for _, drops := range rollMany(t, "minecraft:entities/zombified_piglin", LootContext{KilledByPlayer: true}, 2000, 702) {
		for _, drop := range drops {
			if drop.item == "minecraft:gold_ingot" {
				ingots++
			}
		}
	}
	if ingots < 20 || ingots > 90 {
		t.Errorf("a player's kill rolled %d gold ingots in 2000 deaths, want about 50", ingots)
	}
}

// TestAPolarBearDropsRawFishWithoutAPlayer rolls the polar bear's table: one
// pool of raw cod (weight 3) or raw salmon (weight 1), nought to two of it,
// whoever the killer is.
// vanilla: entities/polar_bear
func TestAPolarBearDropsRawFishWithoutAPlayer(t *testing.T) {
	cod, salmon := 0, 0
	for _, drops := range rollMany(t, "minecraft:entities/polar_bear", LootContext{}, 600, 801) {
		for _, drop := range drops {
			if drop.count < 0 || drop.count > 2 {
				t.Fatalf("%s dropped %d, want 0 to 2", drop.item, drop.count)
			}
			switch drop.item {
			case "minecraft:cod":
				cod += int(drop.count)
			case "minecraft:salmon":
				salmon += int(drop.count)
			default:
				t.Fatalf("a polar bear dropped %s", drop.item)
			}
		}
	}
	if cod == 0 || salmon == 0 || cod < salmon {
		t.Errorf("600 deaths gave %d cod and %d salmon, want cod ahead of salmon and both present", cod, salmon)
	}
}

// TestTheMineshaftChestRollsItsPictureDiscOnlyInTheSulfurCaves: the entry
// the table gates on its biome rolls there and nowhere else - not in the
// plains, not without a world behind the roll.
func TestTheMineshaftChestRollsItsPictureDiscOnlyInTheSulfurCaves(t *testing.T) {
	const table = "minecraft:chests/abandoned_mineshaft"
	const disc = "minecraft:music_disc_bounce"
	at := func(biome string) func(int32, int32, int32) string {
		return func(dx, dy, dz int32) string { return biome }
	}
	if len(countsOf(rollMany(t, table, LootContext{BiomeAt: at("minecraft:sulfur_caves")}, 300, 1401), disc)) == 0 {
		t.Error("no picture disc in 300 sulfur-caves chests")
	}
	for _, tc := range []struct {
		name string
		ctx  LootContext
	}{
		{"the plains", LootContext{BiomeAt: at("minecraft:plains")}},
		{"no world", LootContext{}},
	} {
		for _, drops := range rollMany(t, table, tc.ctx, 300, 1402) {
			for _, drop := range drops {
				if drop.item == disc {
					t.Errorf("%s: the sulfur-caves disc rolled outside its biome", tc.name)
				}
			}
		}
	}
}

// entryTable is the one-entry, one-roll table the function tests roll.
func entryTable(item string, functions []registry.LootFunction) registry.LootTable {
	id, _ := registry.ItemByName(item)
	return registry.LootTable{Pools: []registry.LootPool{{
		Rolls:   registry.LootConstant{Value: 1},
		Entries: []registry.LootEntry{registry.LootItemEntry{Item: id, Weight: 1, Functions: functions}},
	}}}
}

// TestAStewsEffectsJoinTheStewAlone reads a set_stew_effect roll by roll:
// one of the listed effects joins a suspicious stew - its seconds twentyfold
// in ticks when the effect lasts, unconverted when it is instantaneous -
// and no other item carries one.
// vanilla: SetStewEffectFunction.run
func TestAStewsEffectsJoinTheStewAlone(t *testing.T) {
	seven := registry.LootConstant{Value: 7}
	stewEffect := []registry.LootFunction{registry.LootSetStewEffect{
		Effects: []registry.LootStewEffect{
			{Effect: "minecraft:night_vision", Duration: seven},
			{Effect: "minecraft:saturation", Duration: seven},
		},
	}}
	stewed := entryTable("minecraft:suspicious_stew", stewEffect)
	want := map[int32]int32{
		registry.MustMobEffect("minecraft:night_vision"): 140, // 7 seconds, in ticks
		registry.MustMobEffect("minecraft:saturation"):   7,   // instantaneous: not twentyfold
	}
	rolled := map[int32]bool{}
	for i := range 40 {
		stacks := RollLootTable("test", stewed, &LootContext{}, rand.New(rand.NewSource(1501+int64(i))))
		if len(stacks) != 1 || stacks[0].Count != 1 {
			t.Fatal("the stew table rolled more than its one stack")
		}
		effects := stacks[0].SuspiciousStewEffectsOf()
		if len(effects) != 1 {
			t.Fatalf("the stew carries %d effects, want the one its table rolled", len(effects))
		}
		duration, known := want[effects[0].Effect]
		if !known {
			t.Fatalf("the stew carries effect ID %d, want night vision or saturation", effects[0].Effect)
		}
		if effects[0].Duration != duration {
			t.Errorf("the effect lasts %d ticks, want %d", effects[0].Duration, duration)
		}
		rolled[effects[0].Effect] = true
	}
	if len(rolled) != 2 {
		t.Error("both of the listed effects should have come up in 40 rolls")
	}
	// Only a stew: the same functions leave another item alone.
	loaves := RollLootTable("test", entryTable("minecraft:bread", stewEffect), &LootContext{}, rand.New(rand.NewSource(1599)))
	if len(loaves) != 1 || loaves[0].SuspiciousStewEffectsOf() != nil {
		t.Error("a bread rolled the stew's effects")
	}
}

// TestAHornsInstrumentIsOneOfTheOptions reads a set_instrument roll by
// roll: one of the options becomes a goat horn's instrument, which the
// table's tag narrowed to the regular four, and no other item carries one.
// vanilla: SetInstrumentFunction.run
func TestAHornsInstrumentIsOneOfTheOptions(t *testing.T) {
	regular := map[int32]bool{}
	for _, name := range []string{
		"minecraft:feel_goat_horn", "minecraft:ponder_goat_horn",
		"minecraft:seek_goat_horn", "minecraft:sing_goat_horn",
	} {
		regular[registry.MustID("minecraft:instrument", name)] = true
	}
	setInstrument := []registry.LootFunction{registry.LootSetInstrument{
		Options: []string{"minecraft:feel_goat_horn", "minecraft:ponder_goat_horn", "minecraft:seek_goat_horn", "minecraft:sing_goat_horn"},
	}}
	rolled := map[int32]bool{}
	for i := range 40 {
		stacks := RollLootTable("test", entryTable("minecraft:goat_horn", setInstrument), &LootContext{}, rand.New(rand.NewSource(1601+int64(i))))
		if len(stacks) != 1 {
			t.Fatal("the horn table rolled more than its one stack")
		}
		found, has := stacks[0].InstrumentOf()
		if !has {
			t.Fatal("the horn carries no instrument")
		}
		if !regular[found] {
			t.Fatalf("the horn's instrument is ID %d, which is no regular goat horn", found)
		}
		rolled[found] = true
	}
	if len(rolled) < 2 {
		t.Error("one instrument alone came up in 40 rolls")
	}
	// Only a horn: the same function leaves another item alone.
	pebbles := RollLootTable("test", entryTable("minecraft:stone", setInstrument), &LootContext{}, rand.New(rand.NewSource(1699)))
	if len(pebbles) != 1 {
		t.Fatal("the stone table rolled more than its one stack")
	}
	if _, has := pebbles[0].InstrumentOf(); has {
		t.Error("a stone rolled the horn's instrument")
	}
}

// TestOnlyACaptainPillagerDropsAnOminousBottle: the pillager's table asks the
// raider predicate for a captain.
// vanilla: loot_table entities/pillager
func TestOnlyACaptainPillagerDropsAnOminousBottle(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	bottle := registry.MustItem("minecraft:ominous_bottle")
	plain := RollLoot("minecraft:entities/pillager", &LootContext{This: LootEntity{Type: "minecraft:pillager"}}, rng)
	if len(plain) != 0 {
		t.Errorf("a plain pillager dropped %v", plain)
	}
	lead := RollLoot("minecraft:entities/pillager", &LootContext{This: LootEntity{Type: "minecraft:pillager", Raider: true, Captain: true}}, rng)
	if len(lead) != 1 || lead[0].ItemID != bottle {
		t.Fatalf("a captain pillager dropped %v, want one ominous bottle", lead)
	}
	if a := lead[0].OminousBottleAmplifierOf(); a < 0 || a > 4 {
		t.Errorf("amplifier %d, want 0 to 4", a)
	}
}

// TestRipeCropsRollTheBinomialBonusWithoutATool rolls a ripe carrot crop
// with no tool: the binomial bonus still runs its three extra rounds at
// p=0.5714, so the carrots come out two to five with a mean of 3.71, while a
// seedling gives its single carrot.
func TestRipeCropsRollTheBinomialBonusWithoutATool(t *testing.T) {
	const rolls = 500
	total := func(age string, seed int64) []int32 {
		ctx := &LootContext{BlockState: registry.MustState("minecraft:carrots", map[string]string{"age": age}), HasBlock: true}
		rng := rand.New(rand.NewSource(seed))
		var out []int32
		for range rolls {
			n := int32(0)
			for _, drop := range rollNames(t, RollLoot("minecraft:blocks/carrots", ctx, rng)) {
				if drop.item == "minecraft:carrot" {
					n += drop.count
				}
			}
			out = append(out, n)
		}
		return out
	}
	seen := map[int32]bool{}
	sum := int32(0)
	for _, n := range total("7", 1301) {
		if n < 2 || n > 5 {
			t.Fatalf("a ripe carrot crop rolled %d carrots, want two to five", n)
		}
		seen[n] = true
		sum += n
	}
	for _, n := range []int32{3, 4, 5} {
		if !seen[n] {
			t.Errorf("a ripe carrot crop never rolled %d carrots in %d rolls", n, rolls)
		}
	}
	if mean := float64(sum) / rolls; math.Abs(mean-3.71) > 0.3 {
		t.Errorf("a ripe carrot crop rolled a mean of %.2f carrots, want about 3.71", mean)
	}
	for _, n := range total("0", 1302) {
		if n != 1 {
			t.Fatalf("a seedling carrot crop rolled %d carrots, want one", n)
		}
	}
}

// vanilla: RaiderPredicate.matches
func TestRaiderLootPredicateChecksBothFlagsAndBody(t *testing.T) {
	for _, wantRaid := range []bool{false, true} {
		for _, wantCaptain := range []bool{false, true} {
			p := registry.LootEntityPredicate{HasRaid: new(wantRaid), Captain: new(wantCaptain)}
			for _, hasRaid := range []bool{false, true} {
				for _, captain := range []bool{false, true} {
					for _, raider := range []bool{false, true} {
						e := LootEntity{Raider: raider, HasRaid: hasRaid, Captain: captain}
						want := raider && hasRaid == wantRaid && captain == wantCaptain
						if got := entityPredicate(p, e, nil); got != want {
							t.Errorf("predicate (%v, %v), body (%v, %v, %v) = %v, want %v", wantRaid, wantCaptain, raider, hasRaid, captain, got, want)
						}
					}
				}
			}
		}
	}
}

func TestLootEntityFreezeStopsAtCycle(t *testing.T) {
	var rider, vehicle func() LootEntity
	rider = func() LootEntity {
		return LootEntity{ScoreHolder: "rider", VehicleOf: func() (LootEntity, bool) { return vehicle(), true }}
	}
	vehicle = func() LootEntity {
		return LootEntity{ScoreHolder: "boat", Passengers: func() []LootEntity { return []LootEntity{rider()} }}
	}
	e := rider()
	e.Freeze()
	v, ok := e.VehicleOf()
	if !ok || v.ScoreHolder != "boat" {
		t.Fatalf("vehicle = %v, %v", v.ScoreHolder, ok)
	}
	if got := v.Passengers(); len(got) != 1 || got[0].ScoreHolder != "rider" || got[0].VehicleOf != nil {
		t.Fatalf("passengers = %+v", got)
	}
}
