package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
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

// TestAPufferfishStingsAScaledMobAmongAWholeHerd: the grid query reaches a mob
// whose scale attribute puts its box in reach from beyond its cell, unless the
// mob is filed in a section past the ones vanilla's walk visits.
// vanilla: Pufferfish.aiStep, LivingEntity.getScale,
// EntitySectionStorage.forEachAccessibleNonEmptySection
func TestAPufferfishStingsAScaledMobAmongAWholeHerd(t *testing.T) {
	for _, tc := range []struct {
		name        string
		fishX, pigX float64
		wantStung   int
	}{
		{"same section", 3.9, 8.1, 1},
		{"pig a section past the walk", 11.9, 16.1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := shulkerFloor(t)
			stone := defaultState(t, "minecraft:stone")
			for x := int32(13); x <= 24; x++ {
				for z := int32(-12); z <= 12; z++ {
					w.set(x, 63, z, stone)
				}
			}
			var mobs Mobs
			fish := NewMob(TypePufferfish, 1, tc.fishX, 64, 0.5, 1)
			fish.NoAI = true
			fish.SetPuffState(PuffFull)
			pig := NewMob(TypePig, 2, tc.pigX, 64, 0.5, 2)
			pig.NoAI = true
			pig.SetAttributes(pig.BuildAttributes(nbt.List{}))
			pig.attrs.SetBase(registry.AttributeScale, 16)
			mobs = append(mobs, fish, pig)
			for i := range villagerGridMinHerd {
				mobs = append(mobs, NewMob(TypePig, int32(i+10), float64(i%8)*2-11, 64, float64(i/8)*2+4, int64(i+10)))
			}
			stung := 0
			for _, ev := range mobs.Tick(w, nil, nil) {
				if ev.Kind == MobAttackedPlayer && ev.Mob == fish && ev.Player.EntityID == pig.ID {
					stung++
				}
			}
			if stung != tc.wantStung {
				t.Errorf("the scaled pig was stung %d times, want %d", stung, tc.wantStung)
			}
		})
	}
}
