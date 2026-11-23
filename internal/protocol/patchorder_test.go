package protocol

import (
	"fmt"
	"slices"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func wireComponents(s ItemStack) []int32 { return s.PatchOrdered(componentsOf(s)) }

func TestPatchOrderWireRoundTrip(t *testing.T) {
	stack := ItemStack{ItemID: 1, Count: 1, Damage: 3, RepairCost: 2}
	stack.NotePatch(DataComponentRepairCost, DataComponentDamage)
	var out Buffer
	out.PutUntrustedSlot(stack)
	if got, want := wireComponents(stack), []int32{DataComponentRepairCost, DataComponentDamage}; !slices.Equal(got, want) {
		t.Fatalf("wire order = %v, want %v", got, want)
	}
	read, ok := NewReader(out.Bytes()).ReadUntrustedSlot()
	if !ok {
		t.Fatal("slot refused")
	}
	var again Buffer
	again.PutUntrustedSlot(read)
	if string(again.Bytes()) != string(out.Bytes()) {
		t.Errorf("round trip changed the bytes: %x vs %x", again.Bytes(), out.Bytes())
	}
	if !stack.Same(read) {
		t.Error("history made equal stacks differ")
	}
	plain := ItemStack{ItemID: 1, Count: 1, Damage: 3, RepairCost: 2}
	if !stack.Same(plain) {
		t.Error("Same must ignore the patch order")
	}
}

func TestPatchOrderRemoveThenSetAppends(t *testing.T) {
	var stack ItemStack
	stack.SetDyedColor(1)
	stack.SetCustomDataTag(nbt.Compound{"a": nbt.Int(1)})
	stack.RemoveDyedColor()
	stack.SetDyedColor(2)
	got := wireComponents(stack)
	if want := []int32{DataComponentCustomData, DataComponentDyedColor}; !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

// TestPatchOrderWithoutHistoryKeepsWireBytes compares captures from 0c799c182,
// before patch-root history.
func TestPatchOrderWithoutHistoryKeepsWireBytes(t *testing.T) {
	stack := ItemStack{
		ItemID: 1050, Count: 1, Damage: 3, Unbreakable: true,
		RepairCost: 2, GlintOverride: GlintShown,
		Raw: []RawComponent{{Type: DataComponentDyedColor, Data: []byte{0, 0x12, 0x34, 0x56}}},
	}
	stack.Remove(DataComponentFood)
	stack.Remove(DataComponentTool)
	stack.patchOrder = nil
	container := ItemStack{ItemID: 1, Count: 1, Container: []ItemStack{{}, stack}}
	for _, tc := range []struct {
		name  string
		stack ItemStack
		write func(*Buffer, ItemStack)
		want  string
	}{
		{"slot", stack, (*Buffer).PutSlot, "019a080502030304130215012f001234561c17"},
		{"untrusted", stack, (*Buffer).PutUntrustedSlot, "019a08050203010304001301021501012f04001234561c17"},
		{"template", stack, (*Buffer).PutItemTemplate, "9a08010502030304130215012f001234561c17"},
		{"container", container, (*Buffer).PutSlot, "010101004d0200019a08010502030304130215012f001234561c17"},
		{"untrusted_container", container, (*Buffer).PutUntrustedSlot, "010101004d160200019a08010502030304130215012f001234561c17"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out Buffer
			tc.write(&out, tc.stack)
			if got := fmt.Sprintf("%x", out.Bytes()); got != tc.want {
				t.Errorf("wire bytes = %s, want pre-history bytes %s", got, tc.want)
			}
		})
	}
}
