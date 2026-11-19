package protocol

import (
	"bytes"
	"fmt"
	"testing"
)

// TestRemovedComponentsAreWrittenInOrderAndReadBack checks that removed item
// defaults go out in the order the patch gained them, and read back.
func TestRemovedComponentsAreWrittenInOrderAndReadBack(t *testing.T) {
	t.Parallel()
	stack := ItemStack{ItemID: 1, Count: 1}
	stack.Remove(DataComponentBlockTransformer)
	stack.Remove(DataComponentAttackRange)
	stack.Remove(DataComponentTool)
	stack.Remove(DataComponentOminousBottleAmplifier)
	var b Buffer
	b.PutSlot(stack)
	want := []byte{1, 1, 0, 4, byte(DataComponentBlockTransformer), byte(DataComponentAttackRange), byte(DataComponentTool), byte(DataComponentOminousBottleAmplifier)}
	if !bytes.Equal(b.Bytes(), want) {
		t.Errorf("slot bytes = %v, want %v", b.Bytes(), want)
	}
	got, ok := NewReader(b.Bytes()).ReadUntrustedSlot()
	if !ok || !got.Same(stack) {
		t.Errorf("read back ok=%v removed=%b, want %b", ok, got.Removed, stack.Removed)
	}
	if stack.Remove(DataComponentDamage) || stack.Removes(DataComponentDamage) {
		t.Error("a removal of minecraft:damage was kept")
	}
}

func TestMixedComponentPatchRoundTrip(t *testing.T) {
	t.Parallel()
	stack := ItemStack{ItemID: 1, Count: 1, CustomName: &Text{Text: "probe"}}
	stack.Remove(DataComponentFood)
	for _, container := range []bool{false, true} {
		for _, untrusted := range []bool{false, true} {
			t.Run(fmt.Sprintf("container=%v/untrusted=%v", container, untrusted), func(t *testing.T) {
				want := stack
				if container {
					want = ItemStack{ItemID: 1, Count: 1, Container: []ItemStack{stack}}
				}
				var wire Buffer
				if untrusted {
					wire.PutUntrustedSlot(want)
				} else {
					wire.PutSlot(want)
				}
				r := NewReader(wire.Bytes())
				read := r.ReadSlot
				if untrusted {
					read = r.ReadUntrustedSlot
				}
				got, ok := read()
				if !ok || r.Err() != nil || r.Remaining() != 0 || !got.Equal(want) {
					t.Fatalf("round trip = %+v, ok=%v err=%v unread=%d; want %+v (wire %x)",
						got, ok, r.Err(), r.Remaining(), want, wire.Bytes())
				}
			})
		}
	}
}

func BenchmarkPutMixedComponentSlot(b *testing.B) {
	stack := ItemStack{ItemID: 1, Count: 1, CustomName: &Text{Text: "probe"}}
	stack.Remove(DataComponentFood)
	for _, untrusted := range []bool{false, true} {
		b.Run(fmt.Sprintf("untrusted=%v", untrusted), func(b *testing.B) {
			var wire Buffer
			b.ReportAllocs()
			for b.Loop() {
				wire.Reset()
				if untrusted {
					wire.PutUntrustedSlot(stack)
				} else {
					wire.PutSlot(stack)
				}
			}
		})
	}
}

// TestItemTemplateCountsRemovalsAboveBit31: a template removing
// provides_banner_patterns (bit 32) writes a removal count matching its IDs.
func TestItemTemplateCountsRemovalsAboveBit31(t *testing.T) {
	t.Parallel()
	stack := ItemStack{ItemID: 1, Count: 1}
	if !stack.Remove(DataComponentProvidesBannerPatterns) {
		t.Fatal("provides_banner_patterns removal was not kept")
	}
	var b Buffer
	b.PutItemTemplate(stack)
	want := []byte{1, 1, 0, 1, byte(DataComponentProvidesBannerPatterns)}
	if !bytes.Equal(b.Bytes(), want) {
		t.Errorf("template bytes = %v, want %v", b.Bytes(), want)
	}
}
