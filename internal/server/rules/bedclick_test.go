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
