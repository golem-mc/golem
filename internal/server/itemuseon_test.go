package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// TestItemUsesOnMatchOneEntryPerItem holds itemUsesOn's claim that under the
// built-in tags no item is tried by two entries, so the table's order
// decides nothing until a data pack's tags overlap the families.
func TestItemUsesOnMatchOneEntryPerItem(t *testing.T) {
	t.Parallel()
	for id := range int32(len(registry.Items)) {
		stack := protocol.ItemStack{ItemID: id, Count: 1}
		var matched []int
		for i, entry := range itemUsesOn {
			if entry.matches(stack) {
				matched = append(matched, i)
			}
		}
		if len(matched) > 1 {
			t.Errorf("%s matches itemUsesOn entries %v, want at most one", registry.Items[id], matched)
		}
	}
}
