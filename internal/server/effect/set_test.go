package effect

import (
	"reflect"
	"slices"
	"testing"
)

func TestSetRegistryOrderAndReuse(t *testing.T) {
	var s Set
	// Include bit 63 to catch signed shifts and boundary iteration mistakes.
	ids := []int32{0, hunger, poison, 63}
	for i, id := range slices.Backward(ids) {
		s.Add(New(id, 1, int32(i), false, true, true))
	}
	check := func(all []Instance) {
		t.Helper()
		var got []int32
		for _, in := range all {
			got = append(got, in.ID)
		}
		if !reflect.DeepEqual(got, ids) {
			t.Fatalf("IDs = %v, want %v", got, ids)
		}
	}
	check(s.All())
	clone := s.Clone()
	changes := s.Tick(1, func(*Instance) bool { return true })
	var removed []Instance
	for _, ch := range changes {
		if ch.Kind != Removed {
			t.Fatalf("change = %+v, want removal", ch)
		}
		removed = append(removed, ch.Instance)
	}
	check(removed)
	if len(s.All()) != 0 || len(s.Clear()) != 0 {
		t.Fatal("expired effects remain")
	}
	for _, id := range ids {
		if _, ok := s.Get(id); ok {
			t.Fatalf("effect %d survived expiry", id)
		}
		if _, ok := s.Amplifier(id); ok {
			t.Fatalf("amplifier %d survived expiry", id)
		}
		if _, ok := s.Remove(id); ok {
			t.Fatalf("removed expired effect %d twice", id)
		}
	}
	for i, ch := range clone.Clear() {
		if ch.Kind != Removed || ch.Instance.ID != ids[i] {
			t.Fatalf("clear[%d] = %+v", i, ch)
		}
	}
	if len(clone.All()) != 0 {
		t.Fatal("clear left effects behind")
	}
	s.Add(New(hunger, Infinite, 2, false, true, true))
	if amp, ok := s.Amplifier(hunger); !ok || amp != 2 {
		t.Fatalf("reused slot = %d, %v", amp, ok)
	}
	if ch, ok := s.Remove(hunger); !ok || ch.Instance.Duration != Infinite {
		t.Fatalf("remove reused slot = %+v, %v", ch, ok)
	}
	if len(s.All()) != 0 {
		t.Fatal("removed slot remains active")
	}
}

func TestSetTicksInRegistryOrder(t *testing.T) {
	var s Set
	ids := []int32{regeneration, hunger, poison, wither}
	for _, id := range slices.Backward(ids) {
		s.Add(New(id, 200, 0, false, true, true))
	}
	var applied []int32
	changes := s.Tick(1, func(in *Instance) bool {
		applied = append(applied, in.ID)
		return in.ID != poison
	})
	if !reflect.DeepEqual(applied, ids) {
		t.Fatalf("applied = %v, want %v", applied, ids)
	}
	if len(changes) != 1 || changes[0].Kind != Removed || changes[0].Instance.ID != poison {
		t.Fatalf("refused tick = %+v, want poison removed", changes)
	}
	if _, ok := s.Get(poison); ok {
		t.Fatal("refused effect remains")
	}
	for _, id := range []int32{regeneration, hunger, wither} {
		if in, ok := s.Get(id); !ok || in.Duration != 199 {
			t.Fatalf("effect %d after tick = %+v, %v", id, in, ok)
		}
	}
}
