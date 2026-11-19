package world

import (
	"slices"
	"testing"
)

func TestTakeContainerChangesReusesDedupe(t *testing.T) {
	var w World
	a, b := posKey{1, 2, 3}, posKey{4, 5, 6}
	for i := range 3 {
		w.containerChanges = []containerChange{{a, 8}, {b, 7}, {a, 3}, {b, 9}}
		want := []containerChange{{a, 3}, {b, 7}}
		if i == 1 {
			w.containerChanges = []containerChange{{b, 12}}
			want = []containerChange{{b, 12}}
		}
		got := w.takeContainerChanges()
		if !slices.Equal(got, want) {
			t.Fatalf("drain %d = %v, want %v", i, got, want)
		}
		if len(w.takeContainerChanges()) != 0 {
			t.Fatal("drain retained changes")
		}
		if !slices.Equal(got, want) {
			t.Fatal("later drain changed earlier result")
		}
	}
}
