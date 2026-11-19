package rules

import "testing"

// TestAnchorRefusesRespawn checks that only an empty anchor refuses, and a
// forced point never.
func TestAnchorRefusesRespawn(t *testing.T) {
	if !AnchorRefusesRespawn(0, false) || AnchorRefusesRespawn(0, true) || AnchorRefusesRespawn(1, false) {
		t.Error("only an empty anchor at an unforced point refuses")
	}
}

// TestAnchorFlooded checks the water that braces the blast: any water above,
// a horizontal source beside, and never flowing water that would not run.
func TestAnchorFlooded(t *testing.T) {
	source := mustState(t, "minecraft:water", map[string]string{"level": "0"})
	w := stubBlocks{}
	if AnchorFlooded(w, 0, 64, 0) {
		t.Error("dry anchor flooded")
	}
	w[[3]int32{0, 65, 0}] = source
	if !AnchorFlooded(w, 0, 64, 0) {
		t.Error("water above braces the blast")
	}
	w = stubBlocks{{1, 64, 0}: source}
	if !AnchorFlooded(w, 0, 64, 0) {
		t.Error("a water source beside braces the blast")
	}
	w = stubBlocks{{0, 63, 0}: source}
	if AnchorFlooded(w, 0, 64, 0) {
		t.Error("water below does not brace the blast")
	}
}

// TestAnchorUse pins each branch of the click decision.
func TestAnchorUse(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		charges                    int
		main, off, works, sameSpot bool
		want                       AnchorUseOutcome
	}{
		{"empty", 0, true, false, true, false, AnchorUsePass},
		{"off hand glowstone", 2, true, true, true, false, AnchorUsePass},
		{"off hand glowstone, full", 4, true, true, true, false, AnchorUseSetPoint},
		{"off hand click", 2, false, true, true, false, AnchorUseSetPoint},
		{"blast", 2, true, false, false, false, AnchorUseBlast},
		{"same point", 2, true, false, true, true, AnchorUseKeepPoint},
		{"set point", 2, true, false, true, false, AnchorUseSetPoint},
	} {
		if got := AnchorUse(tc.charges, tc.main, tc.off, tc.works, tc.sameSpot); got != tc.want {
			t.Errorf("%s: AnchorUse = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// anchorTestWorld is a stone floor at y 63 with the anchor at (0, 64, 0).
func anchorTestWorld(t *testing.T) bedWorld {
	t.Helper()
	stone := stateOf(t, "minecraft:stone")
	blocks := map[[3]int32]uint16{}
	for x := int32(-3); x <= 3; x++ {
		for z := int32(-3); z <= 3; z++ {
			blocks[[3]int32{x, 63, z}] = stone
		}
	}
	blocks[[3]int32{0, 64, 0}] = mustState(t, "minecraft:respawn_anchor", map[string]string{"charges": "4"})
	return bedWorld{blocks: blocks}
}

// TestAnchorStandUpSkipsADangerousCellWhileASafeOneExists checks that lava
// in the first cell searched hands the body to the second, west, cell.
func TestAnchorStandUpSkipsADangerousCellWhileASafeOneExists(t *testing.T) {
	w := anchorTestWorld(t)
	w.blocks[[3]int32{0, 64, -1}] = mustState(t, "minecraft:lava", map[string]string{"level": "0"})
	px, py, pz, ok := AnchorStandUpSpot(w, 0, 64, 0)
	if !ok || px != -0.5 || py != 64 || pz != 0.5 {
		t.Errorf("stand-up = (%v, %v, %v), %v; want (-0.5, 64, 0.5), true", px, py, pz, ok)
	}
}
