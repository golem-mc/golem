package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"

	"github.com/golem-mc/golem/internal/server/world"
)

// TestBedRuleOf checks each dimension type's bed and straw bed rules.
func TestBedRuleOf(t *testing.T) {
	for _, tc := range []struct {
		dim   string
		block registry.Block
		want  BedRule
	}{
		{world.DimensionType, registry.BlockRedBed, bedCanSleepWhenDark},
		{world.DimensionType, registry.BlockStrawBed, bedDestroyOnLeave},
		{world.NetherType, registry.BlockRedBed, bedDestroyOnUse},
		{world.NetherType, registry.BlockStrawBed, bedDestroyOnUse},
		{world.EndType, registry.BlockRedBed, bedDestroyOnUse},
		{world.EndType, registry.BlockStrawBed, bedDestroyOnUse},
	} {
		if got := BedRuleOf(tc.dim, tc.block); got != tc.want {
			t.Errorf("BedRuleOf(%s, %v) = %+v, want %+v", tc.dim, tc.block, got, tc.want)
		}
	}
	if !BedWhenDark.Holds(true) || BedWhenDark.Holds(false) || BedNever.Holds(true) || !BedAlways.Holds(false) {
		t.Error("BedCond.Holds")
	}
}

// TestBedHeadOf checks the foot's retarget to its head.
func TestBedHeadOf(t *testing.T) {
	head := mustState(t, "minecraft:red_bed", map[string]string{"part": "head", "facing": "east"})
	foot := mustState(t, "minecraft:red_bed", map[string]string{"part": "foot", "facing": "east"})
	w := stubBlocks{{1, 64, 0}: head, {0, 64, 0}: foot}
	hx, hz, got, ok := BedHeadOf(w, 0, 64, 0, foot)
	if !ok || hx != 1 || hz != 0 || got != head {
		t.Errorf("foot retarget = %d %d %d %v", hx, hz, got, ok)
	}
	if _, _, _, ok := BedHeadOf(stubBlocks{}, 0, 64, 0, foot); ok {
		t.Error("a foot without its head answers nothing")
	}
	if hx, hz, got, ok := BedHeadOf(w, 1, 64, 0, head); !ok || hx != 1 || hz != 0 || got != head {
		t.Error("a head click stays on the head")
	}
	if _, _, _, ok := BedHeadOf(stubBlocks{}, 5, 64, 6, head); ok {
		t.Error("a head without its foot answers nothing")
	}
	straw := mustState(t, "minecraft:straw_bed", map[string]string{"part": "foot", "facing": "east"})
	if _, _, _, ok := BedHeadOf(stubBlocks{{1, 64, 0}: head}, 0, 64, 0, straw); ok {
		t.Error("a straw foot does not pair with a red head")
	}
}

// TestBedInRange checks the 3-across, 2-up reach of either half.
func TestBedInRange(t *testing.T) {
	for _, tc := range []struct {
		px, py, pz float64
		want       bool
	}{
		{3.5, 64, 0.5, true},
		{-2.5, 64, 0.5, true},
		{-2.6, 64, 0.5, false},
		{0.5, 66, 0.5, true},
		{0.5, 66.1, 0.5, false},
	} {
		if got := BedInRange(tc.px, tc.py, tc.pz, 1, 64, 0, 0, 0); got != tc.want {
			t.Errorf("BedInRange(%v, %v, %v) = %v, want %v", tc.px, tc.py, tc.pz, got, tc.want)
		}
	}
}

// TestBedFreeAt checks that glass leaves room and stone does not.
func TestBedFreeAt(t *testing.T) {
	w := stubBlocks{{0, 65, 0}: stateOf(t, "minecraft:glass"), {1, 65, 0}: stateOf(t, "minecraft:stone")}
	if !BedFreeAt(w, 0, 65, 0) || BedFreeAt(w, 1, 65, 0) || !BedFreeAt(w, 2, 65, 0) {
		t.Error("glass and air are free, stone is not")
	}
}
