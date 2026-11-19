package entity

import (
	"math"
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

// TestTheRangedBodyTurnsTowardTheTargetNotTheHead checks the direction of the
// strafe's body turn: with the head thrown to the far side of the body from
// the target, Mob.lookAt turns the body the short way onto the target rather
// than following the head the wrong way around.
func TestTheRangedBodyTurnsTowardTheTargetNotTheHead(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	s := rangedSkeleton()
	s.TargetID = 7
	s.Yaw, s.HeadYaw = 0, -40
	// The target at 170 degrees around the body, the head forty on the other
	// side: the two headings want the body turned opposite ways.
	rad := 170 * math.Pi / 180
	players := []Player{
		{X: 0.5 - 2*math.Sin(rad), Y: 64, Z: 0.5 + 2*math.Cos(rad), EntityID: 7, Huntable: true},
	}
	g := &rangedAttackGoal{m: s, priority: priorityRanged, seeTime: rangedSeeTicks, strafingTime: 0}

	g.Tick(&tickView{w: w, players: players})

	if got := float64(s.Yaw); math.Abs(got-float64(headTurnSpeed)) > 0.001 {
		t.Errorf("the body turned to %v, want the %v of the target's way, not the head's", s.Yaw, float64(headTurnSpeed))
	}
}

// TestTheRangedBowRelaxesWhenSightIsLongLost covers RangedBowAttackGoal's
// unseen give-up: with the target behind a wall the bow stays drawn while
// seeTime runs down to minus sixty, comes down on the sixty-first hidden
// tick, and is not raised again while the target stays hidden.
func TestTheRangedBowRelaxesWhenSightIsLongLost(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	stone := defaultState(t, "minecraft:stone")
	// The wall crossing the skeleton's sight line to the target, from the
	// floor to above both pairs of eyes.
	for y := int32(64); y <= 67; y++ {
		w.set(1, y, 0, stone)
	}
	s := rangedSkeleton()
	s.TargetID = 7
	s.UsingItem = true
	players := []Player{survivor(7, 3.5)}
	g := &rangedAttackGoal{m: s, priority: priorityRanged}

	// The first sixty hidden ticks run seeTime down to minus sixty, the
	// boundary the give-up is strict about.
	for tick := 1; tick <= 60; tick++ {
		g.Tick(&tickView{w: w, players: players})
		if !s.UsingItem {
			t.Fatalf("the bow relaxed after %d hidden ticks, want it held through the sixtieth", tick)
		}
	}
	// The sixty-first drops below the memory and the bow comes down.
	g.Tick(&tickView{w: w, players: players})
	if s.UsingItem {
		t.Fatal("the bow stayed drawn past the sixty-tick give-up")
	}
	// Past the memory it must not be drawn again while the target is hidden.
	for range 40 {
		g.Tick(&tickView{w: w, players: players})
		if s.UsingItem {
			t.Fatal("the bow was drawn again while the target stayed hidden past the memory")
		}
	}
}

// TestTheRangedOverheadYawGuardsTheHeadNotTheBody checks the empty getYRotD:
// a target in the skeleton's own column has no horizontal direction, so the
// head is left alone, but the strafe's body turn is Mob.lookAt, which has no
// such guard and turns the body toward the overhead target's -90 anyway.
func TestTheRangedOverheadYawGuardsTheHeadNotTheBody(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	s := rangedSkeleton()
	s.TargetID = 7
	s.Yaw, s.HeadYaw = 0, 90
	players := []Player{{X: 0.5, Y: 64, Z: 0.5, EntityID: 7, Huntable: true}}
	g := &rangedAttackGoal{m: s, priority: priorityRanged, seeTime: rangedSeeTicks, strafingTime: 0}

	g.Tick(&tickView{w: w, players: players})

	if s.HeadYaw != 90 {
		t.Errorf("a target straight overhead turned the head to %v, want it left at %v", s.HeadYaw, 90)
	}
	if want := float32(-headTurnSpeed); s.Yaw != want {
		t.Errorf("a target straight overhead left the body at %v, want it turned toward -90 by %v", s.Yaw, headTurnSpeed)
	}
}

// vanilla: MoveControl.strafe, MoveControl.tick (STRAFE)
func TestStrafeProbeUsesModifiedSpeedAtABlockBoundary(t *testing.T) {
	for _, tc := range []struct {
		name     string
		kind     MobType
		x        float64
		fallback bool
	}{
		{"skeleton inside", TypeSkeleton, 0.9, false},
		{"skeleton edge", TypeSkeleton, 0.99, true},
		{"piglin inside", TypePiglin, 0.1, false},
		{"piglin edge", TypePiglin, 0.01, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorld()
			w.set(0, 63, 0, defaultState(t, "minecraft:stone"))
			m := NewMob(tc.kind, 1, tc.x, 64, 0.5, 1)
			var dirX, dirZ, speed float64
			if tc.kind == TypeSkeleton {
				(&rangedAttackGoal{m: m}).strafe(w, false, true)
				dirX, dirZ, speed = math.Sqrt(0.5), math.Sqrt(0.5), m.pace()*math.Sqrt(0.5)
			} else {
				newPiglinCrossbowGoal(m, priorityRanged).backUp(w)
				dirX, dirZ, speed = 0, -1, m.pace()*0.75
			}
			if tc.fallback {
				dirX, dirZ, speed = 0, 1, m.pace()*0.25
			}
			if got := m.riderWalk; !got.set || !closeEnough(got.dirX, dirX) || !closeEnough(got.dirZ, dirZ) || !closeEnough(got.speed, speed) {
				t.Fatalf("strafe = %+v, want direction (%g, %g), speed %g", got, dirX, dirZ, speed)
			}
		})
	}
}
