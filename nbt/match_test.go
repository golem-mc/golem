package nbt

import "testing"

func TestMatchesPartialNBT(t *testing.T) {
	t.Parallel()
	list := func(items ...Tag) List { return List{Elem: TypeInt, Items: items} }
	for _, tc := range []struct {
		name             string
		expected, actual Tag
		want             bool
	}{
		{"nil pattern", nil, Int(1), true},
		{"missing actual", Compound{}, nil, false},
		{"partial compound", Compound{"nested": Compound{"a": Int(1)}}, Compound{"nested": Compound{"a": Int(1), "b": Int(2)}, "extra": Byte(1)}, true},
		{"missing entry", Compound{"a": Int(1)}, Compound{}, false},
		{"unordered list", list(Int(2), Int(1)), list(Int(1), Int(3), Int(2)), true},
		{"missing list element", list(Int(2)), list(Int(1)), false},
		{"repeated list entries", list(Int(1), Int(1)), list(Int(1), Int(2)), true},
		{"short actual list", list(Int(1), Int(1)), list(Int(1)), false},
		{"empty list", List{}, list(), true},
		{"empty list refuses nonempty", List{}, list(Int(1)), false},
		{"numeric width", Byte(1), Int(1), false},
		{"numeric float", Float(1), Double(1), false},
		{"scalar mismatch", Int(1), Int(2), false},
		{"array exact", IntArray{1, 2}, IntArray{1, 2}, true},
		{"array is ordered", IntArray{2, 1}, IntArray{1, 2}, false},
		{"array is not partial", IntArray{1}, IntArray{1, 2}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Matches(tc.expected, tc.actual); got != tc.want {
				t.Errorf("Matches(%v, %v) = %t, want %t", tc.expected, tc.actual, got, tc.want)
			}
		})
	}
}
