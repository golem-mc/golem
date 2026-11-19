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
