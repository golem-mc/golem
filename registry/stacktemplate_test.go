package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

func TestValidStackTemplate(t *testing.T) {
	diamond := protocol.ItemStack{ItemID: int32(ItemDiamond), Count: 64}
	oversized := diamond
	oversized.Count = 65
	bundle := protocol.ItemStack{ItemID: int32(ItemBundle), Count: 1}
	bundle.SetBundleContents([]protocol.ItemStack{diamond, diamond})
	loaded := protocol.ItemStack{ItemID: int32(ItemCrossbow), Count: 1}
	loaded.SetChargedProjectiles([]protocol.ItemStack{oversized})
	for _, tc := range []struct {
		name  string
		stack protocol.ItemStack
		valid bool
	}{
		{"full stack", diamond, true},
		{"oversized stack", oversized, false},
		{"stacked tool", protocol.ItemStack{ItemID: int32(ItemDiamondSword), Count: 2}, false},
		{"container", protocol.ItemStack{ItemID: int32(ItemShulkerBox), Count: 1, Container: []protocol.ItemStack{{}, diamond}}, true},
		{"oversized contained stack", protocol.ItemStack{ItemID: int32(ItemShulkerBox), Count: 1, Container: []protocol.ItemStack{oversized}}, false},
		{"bundle over capacity", bundle, true},
		{"oversized charged projectile", loaded, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidStackTemplate(tc.stack); got != tc.valid {
				t.Fatalf("ValidStackTemplate = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestStackTemplateBundleWeightOverflow(t *testing.T) {
	// Multiplying a nested bundle weight by 64 eventually exceeds Fraction's
	// numerator bound, even though every contained stack fits its own maximum.
	stack := protocol.ItemStack{ItemID: int32(ItemDiamond), Count: 64}
	for range 6 {
		outer := protocol.ItemStack{ItemID: int32(ItemDiamond), Count: 64}
		outer.SetBundleContents([]protocol.ItemStack{stack})
		stack = outer
	}
	if !ValidStackTemplate(stack) {
		t.Fatal("representable nested weight rejected")
	}
	outer := protocol.ItemStack{ItemID: int32(ItemDiamond), Count: 1}
	outer.SetBundleContents([]protocol.ItemStack{stack})
	if ValidStackTemplate(outer) {
		t.Fatal("overflowing nested weight accepted")
	}
}
