package protocol

import (
	"encoding/hex"
	"testing"
)

// The map components: how the cartography table's marking of a map and the
// id of a filled map's saved data ride the slot the raw bytes carry them
// through, and what their setters write.

// TestMapPostProcessingRidesTheSlot checks a map with the component through
// the untrusted shape it arrives in and the trusted one the server sends,
// the value read as the raw bytes of the form its codec takes.
// vanilla: MapPostProcessing.STREAM_CODEC
func TestMapPostProcessingRidesTheSlot(t *testing.T) {
	// A filled map marked for the zoom out: count 1, item 1238
	// (minecraft:filled_map), the component patch of one added — type 50,
	// the byte length of its value, and the value, scale's id 1 — and no
	// component removed.
	data, err := hex.DecodeString("01d6090100320101")
	if err != nil {
		t.Fatal(err)
	}
	want := ItemStack{ItemID: 1238, Count: 1, Raw: []RawComponent{{
		Type: DataComponentMapPostProcessing,
		Data: []byte{MapPostProcessingScale},
	}}}
	got, ok := NewReader(data).ReadUntrustedSlot()
	if !ok || !got.Equal(want) {
		t.Errorf("ReadUntrustedSlot = %+v (%v), want %+v", got, ok, want)
	}

	// The stack rides back out bare in the trusted shape the server
	// sends: the type and the value without the length.
	var b Buffer
	b.PutSlot(got)
	if hex.EncodeToString(b.Bytes()) != "01d60901003201" {
		t.Errorf("PutSlot = %s, want 01ed0801003001", hex.EncodeToString(b.Bytes()))
	}
}

// TestSetMapPostProcessingReplacesTheComponent checks the setter: the value
// it writes is one VarInt, a second set replaces the first rather than
// riding beside it, and the copy the result keeps does not reach the map it
// was sent with.
func TestSetMapPostProcessingReplacesTheComponent(t *testing.T) {
	stack := ItemStack{ItemID: 1238, Count: 1}
	stack.SetMapPostProcessing(MapPostProcessingScale)
	if len(stack.Raw) != 1 || stack.Raw[0].Type != DataComponentMapPostProcessing ||
		hex.EncodeToString(stack.Raw[0].Data) != "01" {
		t.Fatalf("the marking = %+v, want one component of scale's id", stack.Raw)
	}
	stack.SetMapPostProcessing(MapPostProcessingLock)
	if len(stack.Raw) != 1 || hex.EncodeToString(stack.Raw[0].Data) != "00" {
		t.Errorf("the marking = %+v, want the lock to have replaced the scale", stack.Raw)
	}

	// A stack the marking is laid on is the copy the cartography result is:
	// a component beside its own that rides with the copy, and the stack
	// they were copied from keeps the value it was marked with.
	marked := ItemStack{ItemID: 1238, Count: 3, Raw: []RawComponent{{
		Type: DataComponentMapPostProcessing,
		Data: []byte{MapPostProcessingLock},
	}}}
	result := marked
	result.Count = 1
	result.SetMapPostProcessing(MapPostProcessingScale)
	if len(result.Raw) != 1 || hex.EncodeToString(result.Raw[0].Data) != "01" {
		t.Errorf("the result = %+v, want the marking it was sent with", result.Raw)
	}
	if len(marked.Raw) != 1 || hex.EncodeToString(marked.Raw[0].Data) != "00" {
		t.Errorf("the marked map = %+v, want its marking left as it was", marked.Raw)
	}
}

// TestUntrustedSlotRefusesAMarkingWithNoValue checks the codec's refusal: a
// value whose length names bytes the packet does not hold refuses the stack
// rather than relaying it.
func TestUntrustedSlotRefusesAMarkingWithNoValue(t *testing.T) {
	data, err := hex.DecodeString("01d60901003201")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := NewReader(data).ReadUntrustedSlot(); ok {
		t.Error("ReadUntrustedSlot took a marking with no value behind its length")
	}
}

// The minecraft:map_id component: the number naming the saved map data a
// filled map shows, riding the slot the raw bytes carry it through.

// TestMapIDRidesTheSlot checks a filled map carrying a map_id through the
// untrusted shape it arrives in and the trusted one the server sends, the
// value read as the raw bytes of the VarInt its codec takes, and MapIDOf
// answering it back.
// vanilla: MapId.STREAM_CODEC
func TestMapIDRidesTheSlot(t *testing.T) {
	// A filled map of map 0: count 1, item 1238 (minecraft:filled_map),
	// the component patch of one added — type 48 — the byte length of its
	// value, and the value, the id's VarInt 0.
	data, err := hex.DecodeString("01d6090100300100")
	if err != nil {
		t.Fatal(err)
	}
	want := ItemStack{ItemID: 1238, Count: 1, Raw: []RawComponent{{
		Type: DataComponentMapID,
		Data: []byte{0},
	}}}
	got, ok := NewReader(data).ReadUntrustedSlot()
	if !ok || !got.Equal(want) {
		t.Errorf("ReadUntrustedSlot = %+v (%v), want %+v", got, ok, want)
	}
	if id, ok := got.MapIDOf(); !ok || id != 0 {
		t.Errorf("MapIDOf = %d, %v; want the map_id 0", id, ok)
	}

	// The stack rides back out bare in the trusted shape the server
	// sends: the type and the value without the length.
	var b Buffer
	b.PutSlot(got)
	if hex.EncodeToString(b.Bytes()) != "01d60901003000" {
		t.Errorf("PutSlot = %s, want 01ed0801002e00", hex.EncodeToString(b.Bytes()))
	}
}

// TestSetMapIDReplacesTheComponent checks the setter: a second set replaces
// the id rather than riding beside it, and the copy the result keeps does not
// reach the stack it was laid on.
func TestSetMapIDReplacesTheComponent(t *testing.T) {
	stack := ItemStack{ItemID: 1238, Count: 3}
	stack.SetMapID(3)
	if len(stack.Raw) != 1 || stack.Raw[0].Type != DataComponentMapID ||
		hex.EncodeToString(stack.Raw[0].Data) != "03" {
		t.Fatalf("the map id = %+v, want one component of id 3", stack.Raw)
	}
	stack.SetMapID(0)
	if len(stack.Raw) != 1 || hex.EncodeToString(stack.Raw[0].Data) != "00" {
		t.Errorf("the map id = %+v, want the 0 to have replaced the 3", stack.Raw)
	}

	// A stack the id is laid on is the copy a take hands over: the
	// component beside its own that rides with the copy, and the stack
	// they were copied from keeps the value it was holding.
	taken := stack
	taken.Count = 1
	taken.SetMapID(7)
	if id, ok := taken.MapIDOf(); !ok || id != 7 {
		t.Errorf("the copy's map id = %d, %v; want 7", id, ok)
	}
	if id, ok := stack.MapIDOf(); !ok || id != 0 {
		t.Errorf("the original's map id = %d, %v; want its own 0 left as it was", id, ok)
	}
}
