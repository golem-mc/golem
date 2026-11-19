package rules

import (
	"github.com/golem-mc/golem/registry"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// TestXpNeededForNextLevelIsTheVanillaCurve checks the three segments of
// Player.getXpNeededForNextLevel against the levels the vanilla table names:
// seven a level of twice the level at the bottom, thirty-seven of five a
// level in the middle, and a hundred and twelve of nine a level at the top.
func TestXpNeededForNextLevelIsTheVanillaCurve(t *testing.T) {
	cases := []struct{ level, want int }{
		{0, 7}, {1, 9}, {7, 21}, {14, 35}, {15, 37}, {16, 42}, {29, 107},
		{30, 112}, {31, 121}, {40, 202},
	}
	for _, c := range cases {
		if got := XpNeededForNextLevel(c.level); got != c.want {
			t.Errorf("XpNeededForNextLevel(%d) = %d, want %d", c.level, got, c.want)
		}
	}
}

// TestOrbValueSplitFollowsTheLadder checks the award's cut: the ladder
// ExperienceOrb.getExperienceValue splits by, whose rungs leave an award of
// vanilla's own sizes.
func TestOrbValueSplitFollowsTheLadder(t *testing.T) {
	cases := []struct {
		total int
		want  []int
	}{
		{0, nil},
		{1, []int{1}},
		{2, []int{1, 1}},
		{3, []int{3}},
		{4, []int{3, 1}},
		{7, []int{7}},
		{8, []int{7, 1}},
		{14, []int{7, 7}},
		{17, []int{17}},
		{21, []int{17, 3, 1}},
		{30, []int{17, 7, 3, 3}},
		{100, []int{73, 17, 7, 3}},
	}
	for _, c := range cases {
		got := OrbValueSplit(c.total)
		if len(got) != len(c.want) {
			t.Errorf("OrbValueSplit(%d) = %v, want %v", c.total, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("OrbValueSplit(%d)[%d] = %d, want %d", c.total, i, got[i], c.want[i])
			}
		}
	}
}

// TestXpDeathDropsCapsAtOneHundred checks Player.getBaseExperienceReward's
// answer: seven a level, a hundred at most.
func TestXpDeathDropsCapsAtOneHundred(t *testing.T) {
	cases := []struct{ level, want int }{
		{0, 0}, {1, 7}, {2, 14}, {14, 98}, {15, 100}, {30, 100}, {100, 100},
	}
	for _, c := range cases {
		if got := XpDeathDrops(c.level); got != c.want {
			t.Errorf("XpDeathDrops(%d) = %d, want %d", c.level, got, c.want)
		}
	}
}

// TestOreExperienceIsTheBlocksOwnRange checks the ranges the ore blocks'
// DropExperienceBlock and RedStoneOreBlock constructors carry, against the
// ones their bytecode reads.
func TestOreExperienceIsTheBlocksOwnRange(t *testing.T) {
	cases := []struct {
		name     registry.Block
		min, max int
		ok       bool
	}{
		{registry.BlockCoalOre, 0, 2, true},
		{registry.BlockDeepslateCoalOre, 0, 2, true},
		{registry.BlockLapisOre, 2, 5, true},
		{registry.BlockDiamondOre, 3, 7, true},
		{registry.BlockEmeraldOre, 3, 7, true},
		{registry.BlockNetherQuartzOre, 2, 5, true},
		{registry.BlockRedstoneOre, 1, 5, true},
		{registry.BlockCopperOre, 0, 0, true},
		{registry.BlockStone, 0, 0, false},
	}
	for _, c := range cases {
		min, max, ok := OreExperience(c.name)
		if ok != c.ok || min != c.min || max != c.max {
			t.Errorf("OreExperience(%s) = (%d, %d, %v), want (%d, %d, %v)",
				c.name.Name(), min, max, ok, c.min, c.max, c.ok)
		}
	}
}

// TestBlockBreakExperiencePreservesSpawnerDraws checks the two draws and
// enchantment bypass against the enchantment-adjusted ore and sculk path.
func TestBlockBreakExperiencePreservesSpawnerDraws(t *testing.T) {
	silk := LootTool{Enchantments: map[string]int32{"minecraft:silk_touch": 1}}
	for seed := range int64(100) {
		wantRNG := rand.New(rand.NewSource(seed))
		want := 15 + wantRNG.Intn(15) + wantRNG.Intn(15)
		wantNext := wantRNG.Int63()
		for _, tool := range []LootTool{{}, silk} {
			rng := rand.New(rand.NewSource(seed))
			if got := BlockBreakExperience(registry.BlockSpawner, tool, rng); got != want {
				t.Fatalf("seed %d: spawner award = %d, want %d", seed, got, want)
			}
			if got := rng.Int63(); got != wantNext {
				t.Fatalf("seed %d: spawner consumed the wrong number of draws", seed)
			}
		}
	}
	cases := []struct {
		name registry.Block
		tool LootTool
		want int
	}{
		{registry.BlockSculk, LootTool{}, 1},
		{registry.BlockSculkCatalyst, LootTool{}, 5},
		{registry.BlockSculkSensor, LootTool{}, 5},
		{registry.BlockCalibratedSculkSensor, LootTool{}, 5},
		{registry.BlockSculkShrieker, LootTool{}, 5},
		{registry.BlockSculkCatalyst, silk, 0},
		{registry.BlockDiamondOre, silk, 0},
		{registry.BlockStone, LootTool{}, 0},
	}
	for _, tc := range cases {
		if got := BlockBreakExperience(tc.name, tc.tool, rand.New(rand.NewSource(0))); got != tc.want {
			t.Errorf("%s with enchantments %v: award = %d, want %d", tc.name.Name(), tc.tool.Enchantments, got, tc.want)
		}
	}
}

// RepairFromXp is vanilla's repairPlayerItems, checked against the numbers
// ExperienceOrb's own walk runs: a mending item two a point the xp was
// worth, the share the whole-number division spends of an odd repair's
// half left over, and a sweep that leaves undamaged magic and worn-out
// plain tools alone.
func TestRepairFromXpMendsTwoAPoint(t *testing.T) {
	pick := enchantedStack(t, "minecraft:diamond_pickaxe", "minecraft:mending", 1)
	helmet := enchantedStack(t, "minecraft:diamond_helmet", "minecraft:mending", 1)
	worn := func(stack protocol.ItemStack, damage int32, slot string) EnchPiece {
		stack.Damage = damage
		return EnchPiece{Stack: stack, Slot: slot}
	}
	cases := []struct {
		name   string
		pieces []EnchPiece
		xp     int
		want   []int32
		left   int
	}{
		{
			name:   "a hundred wear under ten points",
			pieces: []EnchPiece{worn(pick, 100, "mainhand")},
			xp:     10,
			want:   []int32{20},
			left:   0,
		},
		{
			name:   "a wear too small to spend the points",
			pieces: []EnchPiece{worn(pick, 5, "mainhand")},
			xp:     10,
			want:   []int32{5},
			left:   8,
		},
		{
			// An odd repair spends only its half rounded down: a single
			// wear against a single point leaves the point whole.
			name:   "an odd repair's half rounds down",
			pieces: []EnchPiece{worn(pick, 1, "mainhand")},
			xp:     1,
			want:   []int32{1},
			left:   1,
		},
		{
			name:   "the wear beyond the points stays",
			pieces: []EnchPiece{worn(pick, 25, "mainhand")},
			xp:     10,
			want:   []int32{20},
			left:   0,
		},
		{
			name:   "undamaged magic takes no share",
			pieces: []EnchPiece{worn(pick, 0, "mainhand")},
			xp:     10,
			want:   []int32{0},
			left:   10,
		},
		{
			name:   "a worn plain tool takes no share",
			pieces: []EnchPiece{EnchPiece{Stack: protocol.ItemStack{ItemID: pick.ItemID, Count: 1, Damage: 100}, Slot: "mainhand"}},
			xp:     10,
			want:   []int32{0},
			left:   10,
		},
		{
			// The first candidate's mend spends the seven its fifteen
			// are worth, and the three left mend six of the helmet.
			name:   "the hands and the armour both",
			pieces: []EnchPiece{worn(pick, 15, "mainhand"), worn(helmet, 7, "head")},
			xp:     10,
			want:   []int32{15, 6},
			left:   0,
		},
		{
			// The carry the recursion is: the first candidate's full mend
			// spends two of ten and the rest mends on into the second.
			name:   "the unspent reaches the next item",
			pieces: []EnchPiece{worn(pick, 5, "feet"), worn(helmet, 5, "head")},
			xp:     10,
			want:   []int32{5, 5},
			left:   6,
		},
	}
	for _, c := range cases {
		got, left := RepairFromXp(c.pieces, c.xp, nil)
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("%s: repair[%d] = %d, want %d", c.name, i, got[i], c.want[i])
			}
		}
		if left != c.left {
			t.Errorf("%s: unspent = %d, want %d", c.name, left, c.left)
		}
	}
}

// TestRepairFromXpPicksARandomCandidate checks the dice: with two damaged
// mending pieces the pick among them is drawn at random, so across seeds
// either can be mended first. A nil rng would always take the first, leaving
// the second's share untested.
func TestRepairFromXpPicksARandomCandidate(t *testing.T) {
	pick := enchantedStack(t, "minecraft:diamond_pickaxe", "minecraft:mending", 1)
	helmet := enchantedStack(t, "minecraft:diamond_helmet", "minecraft:mending", 1)
	worn := func(stack protocol.ItemStack, damage int32, slot string) EnchPiece {
		stack.Damage = damage
		return EnchPiece{Stack: stack, Slot: slot}
	}
	// Ten points mend the hundred-wear helmet whole on their own, so the
	// one-wear pickaxe takes a share only when the pick lands on it first.
	sawFirst, sawSecond := false, false
	for seed := range int64(64) {
		got, _ := RepairFromXp([]EnchPiece{
			worn(pick, 1, "mainhand"),
			worn(helmet, 100, "head"),
		}, 10, rand.New(rand.NewSource(seed)))
		if got[0] > 0 {
			sawFirst = true
		} else {
			sawSecond = true
		}
	}
	if !sawFirst || !sawSecond {
		t.Errorf("across seeds the pick mended the pickaxe=%v and the helmet=%v, want both", sawFirst, sawSecond)
	}
}

// TestDurabilityFromXpRunsTheRepairChain checks the rate the data pack's own
// effects come to: mending's multiply doubling the points, one by one the
// level-based values chain underneath a data pack that grows one.
func TestDurabilityFromXpRunsTheRepairChain(t *testing.T) {
	pick := enchantedStack(t, "minecraft:diamond_pickaxe", "minecraft:mending", 1)
	if got := DurabilityFromXp(pick, 10); got != 20 {
		t.Errorf("DurabilityFromXp of mending on 10 = %d, want the chain's twenty", got)
	}
	plain := protocol.ItemStack{ItemID: pick.ItemID, Count: 1}
	if got := DurabilityFromXp(plain, 10); got != 10 {
		t.Errorf("DurabilityFromXp of nothing on 10 = %d, want the points as they are", got)
	}
}

// TestGiveExperiencePointsWalksTheCrossings checks the bar's walk: the
// crossing arithmetic in the floats vanilla's own field is, which is what
// makes a level's fill land where a vanilla client would draw it.
func TestGiveExperiencePointsWalksTheCrossings(t *testing.T) {
	v := &player.Vitals{}
	GiveExperiencePoints(v, 30)
	if v.ExperienceLevel != 3 || v.ExperienceTotal != 30 {
		t.Errorf("after 30 points: level=%d total=%d, want 3 and 30", v.ExperienceLevel, v.ExperienceTotal)
	}
	// The points the query reads off the fill are the two the float chain
	// the crossings ran in lands under: the same chain vanilla's own float
	// field runs, so the same hair's difference from the thirds.
	if got := ExperiencePointsWithinLevel(v); got != 2 {
		t.Errorf("points within the level = %d, want the two the float chain lands under", got)
	}
}

// TestGiveExperienceLevelsResetsOnNegative checks the whole reset a level
// below nought is: level, progress and total all to nought, the way
// giveExperienceLevels' own guard is - and that a walk to exactly nought
// keeps what came before, the guard's own boundary.
func TestGiveExperienceLevelsResetsOnNegative(t *testing.T) {
	v := &player.Vitals{}
	GiveExperiencePoints(v, 30)
	GiveExperienceLevels(v, -3)
	if v.ExperienceLevel != 0 || v.ExperienceTotal != 30 {
		t.Errorf("after three levels back from three: level=%d total=%d, want 0 and the thirty kept",
			v.ExperienceLevel, v.ExperienceTotal)
	}
	GiveExperienceLevels(v, -1)
	if v.ExperienceLevel != 0 || v.ExperienceTotal != 0 || v.ExperienceProgress != 0 {
		t.Errorf("after a level below nought: level=%d total=%d progress=%g, want all nought",
			v.ExperienceLevel, v.ExperienceTotal, v.ExperienceProgress)
	}
}

// TestFurnaceExperienceFloorsAndRollsTheFraction checks the payout a
// furnace's smelt history earns: the count times the recipe's experience,
// floored, and one more on top with the chance the fraction gives. The ten
// raw iron of a full furnace pays seven points flat - ten times nought
// point seven landing exactly on seven in the float arithmetic the vanilla
// method runs - and the fraction cases are pinned from both sides of the
// roll: nought point two after the floor needs a roll under it to give the
// extra one, nought point nine almost always, and a fraction of exactly
// nought rolls for nothing at all.
func TestFurnaceExperienceFloorsAndRollsTheFraction(t *testing.T) {
	cases := []struct {
		count      int
		experience float32
		roll       float32
		want       int
	}{
		{10, 0.7, 0.0, 7},
		{10, 0.7, 0.999, 7},
		{3, 0.7, 0.09, 3},
		{3, 0.7, 0.10, 2},
		{1, 0.35, 0.34, 1},
		{1, 0.35, 0.35, 0},
		{2, 1.0, 0.0, 2},
		{0, 0.7, 0.0, 0},
	}
	for _, c := range cases {
		if got := FurnaceExperience(c.count, c.experience, c.roll); got != c.want {
			t.Errorf("FurnaceExperience(%d, %g, %g) = %d, want %d",
				c.count, c.experience, c.roll, got, c.want)
		}
	}
}

func TestGiveExperiencePointsRaisesTheScoreAndARespawnKeepsItOnlyWithTheInventory(t *testing.T) {
	t.Parallel()
	var v player.Vitals
	GiveExperiencePoints(&v, 7)
	GiveExperiencePoints(&v, 5)
	if v.Score != 12 {
		t.Errorf("score = %d, want 12", v.Score)
	}
	for _, keep := range []bool{false, true} {
		want := int32(0)
		if keep {
			want = 12
		}
		if got, _ := RespawnVitalsKeeping(v, keep, keep); got.Score != want {
			t.Errorf("keep=%v: respawned score = %d, want %d", keep, got.Score, want)
		}
	}
}

// TestASilverfishIsWorthFiveExperience: Monster's constructor sets xpReward 5.
// vanilla: Monster.<init>
func TestASilverfishIsWorthFiveExperience(t *testing.T) {
	t.Parallel()
	min, max, ok := MobExperienceReward("minecraft:silverfish")
	if !ok || min != 5 || max != 5 {
		t.Errorf("silverfish reward = %d..%d (known %v), want 5..5", min, max, ok)
	}
}

// TestABeeIsWorthOneToThreeExperience: Animal's 1 + nextInt(3).
// vanilla: Animal.getBaseExperienceReward
func TestABeeIsWorthOneToThreeExperience(t *testing.T) {
	t.Parallel()
	min, max, ok := MobExperienceReward("minecraft:bee")
	if !ok || min != 1 || max != 3 {
		t.Errorf("bee reward = %d..%d (known %v), want 1..3", min, max, ok)
	}
}
