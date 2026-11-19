package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
)

// rangedSkeleton is a skeleton with the bow a spawn hands it and its goals
// left wired the way a spawned one comes.
func rangedSkeleton() *Mob {
	s := NewMob(TypeSkeleton, 1, 0.5, 64, 0.5, 3)
	s.FinalizeSpawn(GoDice{s.Rand}, testBiome("minecraft:plains"), nil, rules.LocalDifficulty{})
	return s
}

// TestTheRangedBodyTurnsOnlyWhileStrafing holds the body turn to the strafe
// branch: a walking goal whose search finds no way to a target buried in stone
// keeps the body's yaw and brings only the head round, where vanilla's walk
// branch asks the look control alone.
func TestTheRangedBodyTurnsOnlyWhileStrafing(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	stone := defaultState(t, "minecraft:stone")
	// The target's column and the ring the search reads around it, solid:
	// nothing walkable stands within reach, so seekTo finds no way at all.
	for x := int32(0); x <= 6; x++ {
		for z := int32(-3); z <= 3; z++ {
			for y := int32(60); y <= 68; y++ {
				w.set(x, y, z, stone)
			}
		}
	}
	s := rangedSkeleton()
	s.TargetID = 7
	s.Yaw, s.HeadYaw = 90, 90
	players := []Player{{X: 3.5, Y: 64, Z: 0.5, EntityID: 7, Huntable: true}}
	g := &rangedAttackGoal{m: s, priority: priorityRanged}

	g.Tick(&tickView{w: w, players: players})

	if s.Yaw != 90 {
		t.Errorf("a walking ranged goal turned the body to %v, want it left at %v", s.Yaw, 90)
	}
	if s.HeadYaw == 90 {
		t.Error("a walking ranged goal left the head on the body, want the head turned toward the target")
	}
}
