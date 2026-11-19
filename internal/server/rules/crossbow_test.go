package rules

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func crossbowWith(t *testing.T, name string, level int32) protocol.ItemStack {
	t.Helper()
	item, ok := registry.ItemID("minecraft:crossbow")
	if !ok {
		t.Fatal("no crossbow item")
	}
	id, ok := registry.EnchantmentID(name)
	if !ok {
		t.Fatalf("no enchantment %s", name)
	}
	return protocol.ItemStack{ItemID: item, Count: 1, Enchantments: map[int32]int32{id: level}}
}

func TestQuickChargeShortensTheLoad(t *testing.T) {
	t.Parallel()
	for level, want := range map[int32]int32{1: 20, 2: 15, 3: 10} {
		if got := CrossbowChargeTicks(crossbowWith(t, "minecraft:quick_charge", level)); got != want {
			t.Errorf("quick charge %d loads in %d ticks, want %d", level, got, want)
		}
	}
	if got := CrossbowChargeTicks(protocol.ItemStack{}); got != 25 {
		t.Errorf("a plain crossbow loads in %d ticks, want 25", got)
	}
}

func TestMultishotDrawsThreeFannedArrows(t *testing.T) {
	t.Parallel()
	cb := crossbowWith(t, "minecraft:multishot", 1)
	if got := CrossbowProjectileCount(cb); got != 3 {
		t.Errorf("multishot draws %d arrows, want 3", got)
	}
	got := CrossbowAngles(3, CrossbowMaxSpread(cb))
	if want := []float32{0, -10, 10}; !slices.Equal(got, want) {
		t.Errorf("angles = %v, want %v", got, want)
	}
	if got := CrossbowAngles(1, 0); !slices.Equal(got, []float32{0}) {
		t.Errorf("a single arrow turns %v, want straight on", got)
	}
}

func TestPiercingLevelReadsTheEnchantment(t *testing.T) {
	t.Parallel()
	if got := PiercingLevel(crossbowWith(t, "minecraft:piercing", 3), protocol.ItemStack{}); got != 3 {
		t.Errorf("piercing level = %d, want 3", got)
	}
}

func TestCrossbowChargeStepPinsTheTicks(t *testing.T) {
	t.Parallel()
	type step struct{ start, mid, load bool }
	for _, tc := range []struct {
		duration int32
		want     map[int32]step
	}{
		{25, map[int32]step{0: {}, 4: {}, 5: {start: true}, 12: {}, 13: {mid: true}, 24: {}, 25: {load: true}, 26: {load: true}}},
		{10, map[int32]step{1: {}, 2: {start: true}, 4: {}, 5: {mid: true}, 9: {}, 10: {load: true}}},
		{0, map[int32]step{0: {}, 1: {true, true, true}, 2: {load: true}}},
	} {
		for elapsed, want := range tc.want {
			start, mid, load := CrossbowChargeStep(elapsed, tc.duration)
			if got := (step{start, mid, load}); got != want {
				t.Errorf("duration %d elapsed %d = %+v, want %+v", tc.duration, elapsed, got, want)
			}
		}
	}
}
