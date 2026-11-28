package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/effect"
)

// The share scales the follow range before the visibility percent, so an
// invisible wolf is seen from a fraction of the llama's share, not of its full range.
// vanilla: TargetingConditions.test
func TestTargetVisibleUsesFollowShare(t *testing.T) {
	llama := NewMob(TypeLlama, 1, 0, 64, 0, 1)
	g := llamaWolfGoal(llama)
	reach := g.follow()
	wolf := NewMob(TypeWolf, 2, 3, 64, 0, 2)
	if !g.visible(wolf) {
		t.Fatal("a plain wolf inside the share is not visible")
	}
	wolf.effects.Add(effect.Instance{ID: mobEffectInvisibility, Duration: 100})
	if g.visible(wolf) {
		t.Errorf("an invisible wolf at 3 blocks is visible, reach %v", reach)
	}
	wolf.X = 1.5 // inside the 2-block floor
	if !g.visible(wolf) {
		t.Error("an invisible wolf inside the 2-block floor is not visible")
	}
}
