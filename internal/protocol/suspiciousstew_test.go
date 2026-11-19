package protocol

import "testing"

func TestSuspiciousStewComponentRoundTrips(t *testing.T) {
	t.Parallel()
	var value Buffer
	value.PutVarInt(2)
	value.PutVarInt(1)
	value.PutVarInt(160)
	value.PutVarInt(2)
	value.PutVarInt(300)
	stack := ItemStack{ItemID: 1, Count: 1}
	stack.SetRaw(RawComponent{Type: DataComponentSuspiciousStew, Data: value.Bytes()})
	var b Buffer
	b.PutSlot(stack)
	got, ok := NewReader(b.Bytes()).ReadSlot()
	if !ok || len(got.Raw) != 1 || string(got.Raw[0].Data) != string(value.Bytes()) {
		t.Fatalf("round trip = %+v, %v", got, ok)
	}
}

func TestSuspiciousStewRefusesUnknownEffect(t *testing.T) {
	t.Parallel()
	var value Buffer
	value.PutVarInt(1)
	value.PutVarInt(RegistrySizeMobEffect)
	value.PutVarInt(160)
	if readSuspiciousStew(NewReader(value.Bytes())) {
		t.Error("an effect id past the registry was accepted")
	}
	if readSuspiciousStew(NewReader([]byte{1, 1})) {
		t.Error("a missing duration was accepted")
	}
}
