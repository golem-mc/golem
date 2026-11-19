package rules

import (
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
