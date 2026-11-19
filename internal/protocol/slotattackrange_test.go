package protocol

import (
	"bytes"
	"encoding/hex"
	"math"
	"testing"
)

func TestAttackRangeWireRoundTrip(t *testing.T) {
	t.Parallel()
	want := AttackRangeComponent{1.25, 9.5, 2.25, 10.75, 0.625, 1.5}
	// vanilla: /give's ContainerSetSlot, with the six fields set to want.
	vanilla, err := hex.DecodeString("019a0801001e3fa000004118000040100000412c00003f2000003fc00000")
	if err != nil {
		t.Fatal(err)
	}
	r := NewReader(vanilla)
	captured, ok := r.ReadSlot()
	if got, has := captured.AttackRangeOf(); !ok || !has || got != want || r.Remaining() != 0 {
		t.Fatalf("vanilla slot attack_range = %+v (%v, %v), want %+v", got, ok, has, want)
	}
	stack := ItemStack{ItemID: 1, Count: 1}
	stack.Remove(DataComponentAttackRange)
	stack.SetAttackRange(want)
	if stack.Removes(DataComponentAttackRange) {
		t.Fatal("setting attack_range kept its removal")
	}
	var payload Buffer
	for _, f := range []float32{1.25, 9.5, 2.25, 10.75, 0.625, 1.5} {
		payload.PutF32(f)
	}
	if !bytes.Equal(stack.Raw[0].Data, payload.Bytes()) {
		t.Fatalf("attack_range payload = %x, want %x", stack.Raw[0].Data, payload.Bytes())
	}
	for _, trusted := range []bool{true, false} {
		var b Buffer
		var back ItemStack
		var ok bool
		if trusted {
			b.PutSlot(stack)
			back, ok = NewReader(b.Bytes()).ReadSlot()
		} else {
			b.PutUntrustedSlot(stack)
			back, ok = NewReader(b.Bytes()).ReadUntrustedSlot()
		}
		if !ok {
			t.Fatalf("trusted=%v: slot was refused", trusted)
		}
		if got, has := back.AttackRangeOf(); !has || got != want {
			t.Errorf("trusted=%v: attack_range = %+v (%v), want %+v", trusted, got, has, want)
		}
	}
	copy := stack
	copy.SetAttackRange(AttackRangeComponent{MaxReach: 1})
	if got, _ := stack.AttackRangeOf(); got != want {
		t.Errorf("editing a copy changed the original component to %+v", got)
	}
	copy.Remove(DataComponentAttackRange)
	if _, ok := copy.AttackRangeOf(); ok {
		t.Error("removing attack_range kept the override")
	}
}

func TestAttackRangeWireRejectsMalformedValues(t *testing.T) {
	t.Parallel()
	for field := range 6 {
		for _, bad := range []float32{float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1))} {
			var value Buffer
			for i := range 6 {
				f := float32(1)
				if i == field {
					f = bad
				}
				value.PutF32(f)
			}
			stack := ItemStack{ItemID: 1, Count: 1}
			stack.SetRaw(RawComponent{Type: DataComponentAttackRange, Data: value.Bytes()})
			var b Buffer
			b.PutUntrustedSlot(stack)
			if _, ok := NewReader(b.Bytes()).ReadUntrustedSlot(); ok {
				t.Errorf("field %d: accepted %v", field, bad)
			}
		}
	}
	for size := range 25 {
		if _, ok := readAttackRange(NewReader(make([]byte, size))); ok != (size == 24) {
			t.Errorf("%d-byte attack_range: accepted=%v", size, ok)
		}
	}
	stack := ItemStack{Raw: []RawComponent{{Type: DataComponentAttackRange, Data: make([]byte, 25)}}}
	if _, ok := stack.AttackRangeOf(); ok {
		t.Error("accepted an override with trailing bytes")
	}
}
