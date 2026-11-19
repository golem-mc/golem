package rules

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestApplyListOperation checks each ListOperation mode, and that a refused
// edit leaves the list as it was.
// vanilla: ListOperation.apply
func TestApplyListOperation(t *testing.T) {
	orig := []int{1, 2, 3}
	repl := []int{9}
	for _, tc := range []struct {
		op   registry.LootListOperation
		max  int
		want []int
	}{
		{registry.LootListOperation{Mode: "append", Size: -1}, 10, []int{1, 2, 3, 9}},
		{registry.LootListOperation{Mode: "append", Size: -1}, 3, orig},
		{registry.LootListOperation{Mode: "replace_all", Size: -1}, 10, repl},
		{registry.LootListOperation{Mode: "insert", Offset: 1, Size: -1}, 10, []int{1, 9, 2, 3}},
		{registry.LootListOperation{Mode: "insert", Offset: 4, Size: -1}, 10, orig},
		{registry.LootListOperation{Mode: "replace_section", Offset: 1, Size: -1}, 10, []int{1, 9, 3}},
		{registry.LootListOperation{Mode: "replace_section", Offset: 1, Size: 2}, 10, []int{1, 9}},
		{registry.LootListOperation{Mode: "replace_section", Offset: 4, Size: -1}, 10, orig},
	} {
		if got := applyListOperation(tc.op, orig, repl, tc.max); !slices.Equal(got, tc.want) {
			t.Errorf("%+v max %d = %v, want %v", tc.op, tc.max, got, tc.want)
		}
	}
}
