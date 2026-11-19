package datapack

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func TestDecodeTradeResultTemplate(t *testing.T) {
	for _, tc := range []struct {
		name, gives string
		count       int32
		removed     bool
	}{
		{"string", `"diamond"`, 1, false},
		{"default count", `{"id":"minecraft:diamond"}`, 1, false},
		{"maximum count", `{"id":"diamond","count":99}`, 99, false},
		{"fraction", `{"id":"diamond","count":2.9}`, 2, false},
		{"removal", `{"id":"diamond","components":{"!minecraft:tool":{}}}`, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trade, err := DecodeVillagerTrade(fmt.Appendf(nil, `{"wants":{"id":"emerald"},"gives":%s}`, tc.gives))
			if err != nil {
				t.Fatal(err)
			}
			stack := trade.GivesStack
			if trade.Unsupported || stack == nil || stack.ItemID != int32(registry.ItemDiamond) || stack.Count != tc.count || stack.Removes(protocol.DataComponentTool) != tc.removed {
				t.Fatalf("trade = %+v, stack = %+v", trade, stack)
			}
		})
	}
}
