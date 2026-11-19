package scoreboard

import "testing"

// TestEntityRemovedDropsScoresAndTeam checks that a destroyed entity loses
// its scores and team with one event each, and one with neither is silent.
func TestEntityRemovedDropsScoresAndTeam(t *testing.T) {
	var events []Event
	b := New(func(ev Event) { events = append(events, ev) })
	dummy, _ := LookupCriteria("dummy")
	b.AddObjective("a", dummy, nil)
	b.AddObjective("b", dummy, nil)
	b.SetDisplay(DisplaySlot(1), "a")
	b.AddTeam("red", nil)
	zombie := Holder{Name: "zombie"}
	if err := b.Store([]Holder{zombie}, "a", 1); err != nil {
		t.Fatal(err)
	}
	if err := b.Store([]Holder{zombie}, "b", 2); err != nil {
		t.Fatal(err)
	}
	b.JoinTeam("red", []string{"zombie"})

	events = nil
	b.EntityRemoved("zombie")
	var resets, leaves int
	for _, ev := range events {
		switch e := ev.(type) {
		case *ResetEvent:
			if e.Holder == "zombie" {
				resets++
			}
		case *TeamMemberEvent:
			if !e.Joined {
				leaves++
			}
		}
	}
	if resets != 1 || leaves != 1 || len(events) != 2 {
		t.Errorf("events %d (resets %d, leaves %d), want one of each", len(events), resets, leaves)
	}
	if len(b.Holders()) != 0 {
		t.Errorf("holders %v, want none", b.Holders())
	}
	if _, ok := b.MemberTeam("zombie"); ok {
		t.Error("still on a team")
	}

	events = nil
	b.EntityRemoved("zombie")
	if len(events) != 0 {
		t.Errorf("events %v for a holder with nothing", events)
	}
}
