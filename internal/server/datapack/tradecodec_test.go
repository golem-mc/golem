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

func TestDecodeTradeRejectsBadResultTemplate(t *testing.T) {
	for _, gives := range []string{
		`{"id":"diamond","count":0}`,
		`{"id":"diamond","count":100}`,
		`{"id":"diamond","count":-1}`,
		`{"id":"diamond","count":"one"}`,
		`{"id":"diamond","count":{"type":"minecraft:constant","value":1}}`,
		`{"id":"diamond","components":{"minecraft:custom_name":{"text":12}}}`,
		`{"id":"diamond","components":{"minecraft:not_a_component":{}}}`,
		`{"id":"diamond","components":{"!minecraft:tool":1}}`,
		`{"id":"diamond","components":[]}`,
		`{"id":"missing"}`,
		`"air"`,
		`null`,
	} {
		t.Run(gives, func(t *testing.T) {
			if trade, err := DecodeVillagerTrade(fmt.Appendf(nil, `{"wants":{"id":"emerald"},"gives":%s}`, gives)); err == nil {
				t.Fatalf("decoded bad template as %+v", trade)
			}
		})
	}
}
