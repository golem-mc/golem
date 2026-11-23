package entity

import (
	"math"
	"testing"
)

func TestPufferfishScaryMobsAndPuffScale(t *testing.T) {
	fish := NewMob(TypePufferfish, 1, 0, 64, 0, 1)
	zombie := NewMob(TypeZombie, 2, 0, 64, 0, 2)
	cod := NewMob(TypeCod, 3, 0, 64, 0, 3)
	if !scaryTo(zombie) || scaryTo(cod) || scaryTo(fish) {
		t.Errorf("scary: zombie %v cod %v pufferfish %v, want true false false", scaryTo(zombie), scaryTo(cod), scaryTo(fish))
	}
	var widths []float64
	for _, state := range []int{PuffSmall, PuffMid, PuffFull, 9} {
		fish.SetPuffState(state)
		widths = append(widths, fish.BbWidth())
	}
	if math.Abs(widths[0]-0.35) > 1e-9 || math.Abs(widths[1]-0.49) > 1e-9 || widths[2] != 0.7 || widths[3] != 0.7 {
		t.Errorf("widths by state = %v, want 0.7 scaled by 0.5, 0.7, 1 and a clamp to 1", widths)
	}
}

// TestAPufferfishStingsAnOpenShulkerShellAmongAWholeHerd: the grid query
// still hands over a shulker whose open shell, not its cell, is in reach.
// vanilla: Pufferfish.aiStep, Shulker.getProgressAabb
func TestAPufferfishStingsAnOpenShulkerShellAmongAWholeHerd(t *testing.T) {
	w := shulkerFloor(t)
	var mobs Mobs
	shulker := NewMob(TypeShulker, 1, 0.5, 64, 0.5, 1)
	shulker.NoAI = true
	shulker.shulker.face, shulker.shulker.peekNow = faceDown, 1
	// The shell reaches y 66; the fish's box starts above the cell (y 65).
	fish := NewMob(TypePufferfish, 2, 0.5, 66.1, 0.5, 2)
	fish.NoAI = true
	fish.SetPuffState(PuffFull)
	mobs = append(mobs, shulker, fish)
	for i := range villagerGridMinHerd {
		mobs = append(mobs, NewMob(TypePig, int32(i+10), float64(i%8)*2+5, 64, float64(i/8)*2-8, int64(i+10)))
	}
	stung := 0
	for _, ev := range mobs.Tick(w, nil, nil) {
		if ev.Kind == MobAttackedPlayer && ev.Mob == fish && ev.Player.EntityID == shulker.ID {
			stung++
		}
	}
	if stung != 1 {
		t.Errorf("the shulker was stung %d times, want 1", stung)
	}
}
