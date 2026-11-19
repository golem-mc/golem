package main

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

func TestReadStewSlotDisplayConsumesComponentPatch(t *testing.T) {
	var b protocol.Buffer
	// vanilla: SlotDisplay.ItemStackSlotDisplay, ItemStackTemplate.STREAM_CODEC
	for _, n := range []int32{5, 1492, 1, 1, 0, protocol.DataComponentSuspiciousStew, 1, 15, 100} {
		b.PutVarInt(n)
	}
	b.PutBool(true) // the field after the slot display
	r := protocol.NewReader(b.Bytes())
	display, ok := readSlotDisplay(r)
	if !ok || !strings.Contains(display, "stew=[{15 100}]") {
		t.Fatalf("display=%s ok=%v", display, ok)
	}
	if !r.ReadBool() || r.Err() != nil || r.Remaining() != 0 {
		t.Fatalf("after display: leftover=%d err=%v", r.Remaining(), r.Err())
	}
	for n := 0; n < len(b.Bytes())-1; n++ {
		r := protocol.NewReader(b.Bytes()[:n])
		if _, ok := readSlotDisplay(r); ok && r.Err() == nil {
			t.Errorf("accepted slot display truncated to %d bytes", n)
		}
	}
}

func TestReadRecipeBookDisplayVariantsStayAligned(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fields []int32
	}{
		{"potion", []int32{2, 5, 1, 1, 0, 0}},
		{"component", []int32{3, 4, 1, 46}},
		{"item list", []int32{6, 4, 1, 2, 3}},
		{"dyed", []int32{7, 3, 4, 1, 46, 4, 2}},
		{"trim", []int32{8, 4, 1, 4, 2, 1}},
		{"remainder", []int32{9, 4, 1, 4, 2}},
	} {
		var b protocol.Buffer
		for _, n := range tc.fields {
			b.PutVarInt(n)
		}
		r := protocol.NewReader(b.Bytes())
		if _, ok := readSlotDisplay(r); !ok || r.Err() != nil || r.Remaining() != 0 {
			t.Errorf("%s: ok=%v leftover=%d err=%v", tc.name, ok, r.Remaining(), r.Err())
		}
	}
	var b protocol.Buffer
	// vanilla: FurnaceRecipeDisplay.STREAM_CODEC: ingredient, fuel, result,
	// station, duration and experience.
	for _, n := range []int32{2, 4, 1, 1, 5, 2, 1, 0, 0, 4, 3, 200} {
		b.PutVarInt(n)
	}
	b.PutF32(0.35)
	r := protocol.NewReader(b.Bytes())
	if _, ok := readRecipeDisplay(r); !ok || r.Err() != nil || r.Remaining() != 0 {
		t.Errorf("furnace: ok=%v leftover=%d err=%v", ok, r.Remaining(), r.Err())
	}
}
