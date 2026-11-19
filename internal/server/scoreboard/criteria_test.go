package scoreboard

import "testing"

// TestUpdateCriteriaWritesReadOnlyObjectives checks that the game's write
// reaches a read-only objective, skips others, and a Sampler writes only a
// change.
func TestUpdateCriteriaWritesReadOnlyObjectives(t *testing.T) {
	b := New(nil)
	health, _ := LookupCriteria("health")
	dummy, _ := LookupCriteria("dummy")
	b.AddObjective("hp", health, nil)
	b.AddObjective("other", dummy, nil)

	built := 0
	holder := func() Holder { built++; return Holder{Name: "Alex"} }
	var s Sampler
	s.Apply(b, holder, Samples{Health: 20})
	if got, ok := b.Score("Alex", "hp"); !ok || got.Value != 20 {
		t.Fatalf("hp = %+v (%t), want 20", got, ok)
	}
	if _, ok := b.Score("Alex", "other"); ok {
		t.Error("a dummy objective was written by the health criteria")
	}

	b.Update([]Holder{{Name: "Alex"}}, "hp", func(int32) int32 { return 3 })
	s.Apply(b, holder, Samples{Health: 20})
	if got, _ := b.Score("Alex", "hp"); got.Value != 3 {
		t.Errorf("an unchanged sample rewrote hp to %d", got.Value)
	}
	if built != 6 {
		t.Errorf("holder built %d times, want once per first write and none for the unchanged sample", built)
	}
	s.Apply(b, holder, Samples{Health: 19})
	if got, _ := b.Score("Alex", "hp"); got.Value != 19 {
		t.Errorf("hp = %d after a change, want 19", got.Value)
	}
}
