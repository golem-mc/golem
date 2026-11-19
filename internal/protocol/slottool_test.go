package protocol

import (
	"bytes"
	"testing"
)

// TestAToolComponentTravelsTheWire: a tool laid on a stack reads back from
// the untrusted slot shape and its bytes are checked on the way in.
// vanilla: Tool.STREAM_CODEC
func TestAToolComponentTravelsTheWire(t *testing.T) {
	t.Parallel()
	stack := ItemStack{ItemID: 1, Count: 1}
	want := ToolComponent{
		Rules: []ToolRule{{Blocks: BlockPredicateSet{Present: true, Inline: []int32{9, 10}},
			Speed: 3, HasSpeed: true, Correct: false, HasCorrect: true}},
		DefaultMiningSpeed: 1.5, DamagePerBlock: 4, CanDestroyBlocksInCreative: true,
	}
	stack.SetTool(want)
	var b Buffer
	b.PutUntrustedSlot(stack)
	got, ok := NewReader(b.Bytes()).ReadUntrustedSlot()
	if !ok {
		t.Fatal("the stack was refused")
	}
	tool, has := got.ToolOf()
	if !has || len(tool.Rules) != 1 || tool.Rules[0].Speed != 3 || !tool.Rules[0].HasCorrect || tool.Rules[0].Correct ||
		len(tool.Rules[0].Blocks.Inline) != 2 || tool.DefaultMiningSpeed != 1.5 || tool.DamagePerBlock != 4 {
		t.Errorf("tool = %+v (%v), want %+v", tool, has, want)
	}

	// An inline block past the registry is refused with the stack.
	bad := ItemStack{ItemID: 1, Count: 1}
	bad.SetTool(ToolComponent{Rules: []ToolRule{{Blocks: BlockPredicateSet{Present: true, Inline: []int32{RegistrySizeBlock}}}}})
	b = Buffer{}
	b.PutUntrustedSlot(bad)
	if _, ok := NewReader(b.Bytes()).ReadUntrustedSlot(); ok {
		t.Error("a tool naming a block past the registry was accepted")
	}
}

// TestARemovedToolIsKept: a patch removing minecraft:tool leaves the
// stack with no tool at all, and a tool added afterwards clears the removal.
// vanilla: DataComponentPatch removed entries
func TestARemovedToolIsKept(t *testing.T) {
	t.Parallel()
	var b Buffer
	b.PutVarInt(1) // count
	b.PutVarInt(1) // item
	b.PutVarInt(0) // added
	b.PutVarInt(1) // removed
	b.PutVarInt(DataComponentTool)
	got, ok := NewReader(b.Bytes()).ReadUntrustedSlot()
	if !ok {
		t.Fatal("the stack was refused")
	}
	if !got.Removes(DataComponentTool) {
		t.Fatal("Removes(tool) = false after a removed minecraft:tool")
	}
	plain := ItemStack{ItemID: 1, Count: 1}
	if got.Same(plain) {
		t.Error("a stack without its tool is the same as a plain one")
	}
	got.SetTool(ToolComponent{DefaultMiningSpeed: 1})
	if got.Removes(DataComponentTool) {
		t.Error("the tool removal stayed set after a tool was laid on the stack")
	}
}

// TestARemovedToolIsWrittenAsARemovedComponent: a stack with no tool lists
// minecraft:tool in its removed components in both slot shapes and reads back
// as the same stack.
func TestARemovedToolIsWrittenAsARemovedComponent(t *testing.T) {
	t.Parallel()
	stack := ItemStack{ItemID: 1, Count: 1}
	stack.Remove(DataComponentTool)
	var plain, untrusted Buffer
	plain.PutSlot(stack)
	untrusted.PutUntrustedSlot(stack)
	want := []byte{1, 1, 0, 1, byte(DataComponentTool)}
	if !bytes.Equal(plain.Bytes(), want) || !bytes.Equal(untrusted.Bytes(), want) {
		t.Errorf("slot bytes = %v / %v, want %v", plain.Bytes(), untrusted.Bytes(), want)
	}
	got, ok := NewReader(untrusted.Bytes()).ReadUntrustedSlot()
	if !ok || !got.Removes(DataComponentTool) {
		t.Errorf("read back ok=%v removed=%v", ok, got.Removes(DataComponentTool))
	}
}
