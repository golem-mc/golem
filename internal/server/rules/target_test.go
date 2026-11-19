package rules

import "testing"

// TestTargetBlockHold checks the target block test and the hold a hit books.
func TestTargetBlockHold(t *testing.T) {
	if !IsTargetBlock(stateOf(t, "minecraft:target")) || IsTargetBlock(stateOf(t, "minecraft:stone")) {
		t.Error("only the target block is one")
	}
	if TargetHoldTicks(true) != 20 || TargetHoldTicks(false) != 8 {
		t.Error("an arrow holds twenty ticks, anything else eight")
	}
}
