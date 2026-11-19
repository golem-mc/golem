package world

import "testing"

// TestCrafterTickClearsTheFlagOnTheSixthTick checks the countdown: a started
// animation runs out on exactly the sixth tick, and an idle crafter never does.
func TestCrafterTickClearsTheFlagOnTheSixthTick(t *testing.T) {
	t.Parallel()
	b := &BlockEntity{Kind: KindCrafter}
	if b.CrafterTick() {
		t.Fatal("an idle crafter finished an animation")
	}
	b.CrafterStartAnimation()
	for i := 1; i <= CrafterAnimationTicks; i++ {
		if done := b.CrafterTick(); done != (i == CrafterAnimationTicks) {
			t.Fatalf("tick %d: done = %v", i, done)
		}
	}
	if b.CrafterTick() {
		t.Error("the animation finished twice")
	}
}
