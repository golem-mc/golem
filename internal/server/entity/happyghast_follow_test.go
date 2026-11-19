package entity

import "testing"

// TestAGhastlingTestsOnlyItsNearestCandidate: each kind has one candidate,
// its nearest, so a cow inside the three-block near limit leaves the goal
// idle rather than passing to a farther cow, and a player that close falls
// through to the cows.
// vanilla: AdultSensorAnyType, PlayerSensor.doTick, BabyFollowAdult.create
func TestAGhastlingTestsOnlyItsNearestCandidate(t *testing.T) {
	t.Parallel()
	ghastling := NewMob(TypeHappyGhast, 1, 0.5, 66, 0.5, 1)
	ghastling.SetBaby(true)
	near := NewMob(TypeCow, 2, 2.5, 66, 0.5, 2)
	far := NewMob(TypeCow, 3, 10.5, 66, 0.5, 3)
	g := &babyFollowGoal{m: ghastling}

	if g.CanStart(&tickView{w: testWorld{}, others: []*Mob{ghastling, near, far}}) {
		t.Error("the goal starts with a cow 2 blocks off and one 10 off, want it idle: only the nearest cow is a candidate")
	}

	tv := &tickView{w: testWorld{}, others: []*Mob{ghastling, far}, players: []Player{{EntityID: 9, X: 2.5, Y: 66, Z: 0.5}}}
	if x, _, _, ok := g.nearestTarget(tv); !ok || x != far.X {
		t.Errorf("with a player 2 blocks off and a cow 10 off, target x = %v (ok %v), want the cow at %v", x, ok, far.X)
	}

	tv.players[0].X = 17.5
	if x, _, _, ok := g.nearestTarget(tv); !ok || x != far.X {
		t.Errorf("with a player 17 blocks off and a cow 10 off, target x = %v (ok %v), want the cow at %v", x, ok, far.X)
	}
}
