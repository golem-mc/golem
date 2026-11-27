package scoreboard

import (
	"strconv"
	"testing"
)

// TestMemberSnapshotsFollowWrites checks that the shared MemberTeams and
// TeamCollisions snapshots are rebuilt after every write that changes them.
func TestMemberSnapshotsFollowWrites(t *testing.T) {
	b := New(func(Event) {})
	if b.MemberTeams() != nil || teamCollisionsOf(b) != nil {
		t.Fatal("snapshots not nil on an empty board")
	}
	b.AddTeam("red", nil)
	if got := teamCollisionsOf(b); got["red"] != CollideAlways {
		t.Fatalf("collisions = %v", got)
	}
	b.UpdateTeam("red", func(tm *Team) { tm.Collision = CollideNever })
	if got := teamCollisionsOf(b); got["red"] != CollideNever {
		t.Fatalf("collisions after update = %v", got)
	}
	b.JoinTeam("red", []string{"a", "b"})
	if got := b.MemberTeams(); len(got) != 2 || got["a"] != "red" {
		t.Fatalf("members = %v", got)
	}
	b.AddTeam("blue", nil)
	b.JoinTeam("blue", []string{"a"})
	if got := b.MemberTeams(); got["a"] != "blue" || got["b"] != "red" {
		t.Fatalf("members after move = %v", got)
	}
	b.LeaveTeam([]string{"b"})
	if got := b.MemberTeams(); len(got) != 1 {
		t.Fatalf("members after leave = %v", got)
	}
	b.RemoveTeam("blue")
	if b.MemberTeams() != nil {
		t.Fatalf("members after remove = %v", b.MemberTeams())
	}
	if got := teamCollisionsOf(b); len(got) != 1 {
		t.Fatalf("collisions after remove = %v", got)
	}
}

func BenchmarkMemberTeams(b *testing.B) {
	board := New(func(Event) {})
	board.AddTeam("herd", nil)
	members := make([]string, 2000)
	for i := range members {
		members[i] = "member-" + strconv.Itoa(i)
	}
	board.JoinTeam("herd", members)
	board.MemberTeams()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		board.MemberTeams()
		teamCollisionsOf(board)
	}
}

func teamCollisionsOf(b *Board) map[string]CollisionRule {
	rules, _ := b.TeamCollisions()
	return rules
}
