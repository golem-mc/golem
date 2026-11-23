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
	if stack.Remove(DataComponentCustomName) || stack.Removes(DataComponentCustomName) {
		t.Error("a removal of minecraft:custom_name was kept")
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

func BenchmarkReadSlotContainerAndGlint(b *testing.B) {
	var nameVal Buffer
	nameVal.PutText(Text{Text: "probe"})
	var emptyContainerVal Buffer
	emptyContainerVal.PutVarInt(0)
	var glintVal Buffer
	glintVal.PutBool(true)

	encode := func(untrusted bool, comps ...struct {
		id  int32
		val []byte
	}) []byte {
		var wire Buffer
		wire.PutVarInt(1) // count
		wire.PutVarInt(1) // item
		wire.PutVarInt(int32(len(comps)))
		wire.PutVarInt(0) // removals
		for _, c := range comps {
			wire.PutVarInt(c.id)
			if untrusted {
				wire.PutVarInt(int32(len(c.val)))
			}
			wire.PutRaw(c.val)
		}
		return wire.Bytes()
	}

	cases := []struct {
		name  string
		comps []struct {
			id  int32
			val []byte
		}
	}{
		{
			name: "empty_container_after_name",
			comps: []struct {
				id  int32
				val []byte
			}{
				{DataComponentCustomName, nameVal.Bytes()},
				{DataComponentContainer, emptyContainerVal.Bytes()},
			},
		},
		{
			name: "glint_after_name",
			comps: []struct {
				id  int32
				val []byte
			}{
				{DataComponentCustomName, nameVal.Bytes()},
				{DataComponentEnchantmentGlintOverride, glintVal.Bytes()},
			},
		},
	}

	for _, tc := range cases {
		for _, untrusted := range []bool{false, true} {
			raw := encode(untrusted, tc.comps...)
			r := NewReader(raw)
			var ok bool
			if untrusted {
				_, ok = r.ReadUntrustedSlot()
			} else {
				_, ok = r.ReadSlot()
			}
			if !ok || r.Err() != nil || r.Remaining() != 0 {
				b.Fatalf("%s untrusted=%v setup decode failed: ok=%v err=%v rem=%d", tc.name, untrusted, ok, r.Err(), r.Remaining())
			}
			b.Run(fmt.Sprintf("%s/untrusted=%v", tc.name, untrusted), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					rd := NewReader(raw)
					if untrusted {
						_, _ = rd.ReadUntrustedSlot()
					} else {
						_, _ = rd.ReadSlot()
					}
				}
			})
		}
	}
}

// TestItemTemplateCountsRemovalsAboveBit31: a template removing
// provides_banner_patterns (bit 32) and writable_book_content (bit 68, word 1)
// writes a removal count matching its IDs and round-trips through slots.
func TestItemTemplateCountsRemovalsAboveBit31(t *testing.T) {
	t.Parallel()
	stack := ItemStack{ItemID: 1, Count: 1}
	if !stack.Remove(DataComponentProvidesBannerPatterns) {
		t.Fatal("provides_banner_patterns removal was not kept")
	}
	if !stack.Remove(DataComponentWritableBookContent) {
		t.Fatal("writable_book_content removal was not kept")
	}
	var b Buffer
	b.PutItemTemplate(stack)
	want := []byte{1, 1, 0, 2, byte(DataComponentProvidesBannerPatterns), byte(DataComponentWritableBookContent)}
	if !bytes.Equal(b.Bytes(), want) {
		t.Errorf("template bytes = %v, want %v", b.Bytes(), want)
	}
	var slotBuf Buffer
	slotBuf.PutSlot(stack)
	got, ok := NewReader(slotBuf.Bytes()).ReadUntrustedSlot()
	if !ok || !got.Same(stack) {
		t.Errorf("round trip ok=%v removed=%v, want %v", ok, got.RemovedNames(), stack.RemovedNames())
	}

	// Setting a non-raw component clears a prior removal.
	stack.Remove(DataComponentContainer)
	stack.SetContainer([]ItemStack{{ItemID: 1, Count: 1}})
	if stack.Removes(DataComponentContainer) {
		t.Error("SetContainer left container removal bit set")
	}
	stack.Remove(DataComponentEnchantmentGlintOverride)
	stack.SetGlintOverride(GlintHidden)
	if stack.Removes(DataComponentEnchantmentGlintOverride) {
		t.Error("SetGlintOverride left glint removal bit set")
	}
	stack.Remove(DataComponentRecipes)
	stack.SetRecipes([]string{"minecraft:oak_boat"})
	if stack.Removes(DataComponentRecipes) {
		t.Error("SetRecipes left recipes removal bit set")
	}
	stack.Remove(DataComponentDebugStickState)
	stack.SetDebugStickProperty("minecraft:oak_stairs", "half")
	if stack.Removes(DataComponentDebugStickState) {
		t.Error("SetDebugStickProperty left debug_stick_state removal bit set")
	}
	stack.Remove(DataComponentMapDecorations)
	stack.SetMapDecoration("+", MapDecoration{Type: "minecraft:mansion"})
	if stack.Removes(DataComponentMapDecorations) {
		t.Error("SetMapDecoration left map_decorations removal bit set")
	}
}
