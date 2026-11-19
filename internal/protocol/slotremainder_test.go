package protocol

import (
	"errors"
	"testing"
)

func TestUseRemainderTemplateRoundTrips(t *testing.T) {
	t.Parallel()
	remainder := ItemStack{ItemID: 2, Count: 2, CustomName: &Text{Text: "Leftovers"}}
	remainder.Remove(DataComponentFood)
	stack := ItemStack{ItemID: 1, Count: 1}
	stack.Remove(DataComponentUseRemainder)
	stack.SetUseRemainder(remainder)
	if stack.Removes(DataComponentUseRemainder) {
		t.Fatal("setting a remainder kept its removal")
	}
	clone := stack
	clone.SetUseRemainder(ItemStack{ItemID: 3, Count: 1})
	for _, untrusted := range []bool{false, true} {
		var b Buffer
		if untrusted {
			b.PutUntrustedSlot(stack)
		} else {
			b.PutSlot(stack)
		}
		r := NewReader(b.Bytes())
		var got ItemStack
		var ok bool
		if untrusted {
			got, ok = r.ReadUntrustedSlot()
		} else {
			got, ok = r.ReadSlot()
		}
		if !ok || r.Remaining() != 0 {
			t.Fatalf("untrusted=%v: read failed: %v", untrusted, r.Err())
		}
		gotRemainder, ok := got.UseRemainderOf()
		if !ok || gotRemainder.Count != 2 || !gotRemainder.Same(remainder) {
			t.Fatalf("untrusted=%v: remainder = %+v, %v", untrusted, gotRemainder, ok)
		}
	}
	stack.Remove(DataComponentUseRemainder)
	var b Buffer
	b.PutSlot(stack)
	got, ok := NewReader(b.Bytes()).ReadSlot()
	if !ok || !got.Removes(DataComponentUseRemainder) {
		t.Fatal("remainder removal was lost")
	}
}

func TestUseRemainderRefusesInvalidTemplates(t *testing.T) {
	t.Parallel()
	for _, data := range [][]byte{
		{}, {0, 1, 0, 0}, {1, 0, 0, 0}, {1, 1, 1, 0},
	} {
		var b Buffer
		b.PutVarInt(1)
		b.PutVarInt(1)
		b.PutVarInt(1)
		b.PutVarInt(0)
		b.PutVarInt(DataComponentUseRemainder)
		b.PutRaw(data)
		if _, ok := NewReader(b.Bytes()).ReadSlot(); ok {
			t.Errorf("invalid template %v read successfully", data)
		}
	}
	var b Buffer
	b.PutVarInt(1)
	b.PutVarInt(1)
	for range maxComponentDepth + 1 {
		b.PutVarInt(1)
		b.PutVarInt(0)
		b.PutVarInt(DataComponentUseRemainder)
		b.PutVarInt(1)
		b.PutVarInt(1)
	}
	b.PutVarInt(0)
	b.PutVarInt(0)
	r := NewReader(b.Bytes())
	if _, ok := r.ReadSlot(); ok || !errors.Is(r.Err(), ErrSlotTooDeep) {
		t.Fatalf("deeply nested remainder: ok=%v, err=%v", ok, r.Err())
	}
}
