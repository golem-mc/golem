package entity

import (
	"testing"
)

// TestAPlayersStandingDimensions checks the numbers every package above this
// one measures a player's body with, now that they are defined here alone:
// the vanilla standing body is 0.6 blocks wide and 1.8 tall, the half the
// box's centre sits from either side being half of the width.
func TestAPlayersStandingDimensions(t *testing.T) {
	if PlayerWidth != 0.6 {
		t.Errorf("PlayerWidth = %v, want the vanilla standing width of 0.6", PlayerWidth)
	}
	if PlayerHeight != 1.8 {
		t.Errorf("PlayerHeight = %v, want the vanilla standing height of 1.8", PlayerHeight)
	}
	if PlayerHalf != PlayerWidth/2 {
		t.Errorf("PlayerHalf = %v, want half of the width (%v)", PlayerHalf, PlayerWidth/2)
	}
}
