package protocol

import (
	"bytes"
	"testing"
)

func TestFuelComponentWire(t *testing.T) {
	t.Parallel()
	one := []byte{0x3f, 0x80, 0, 0}
	for _, tc := range []struct {
		c    FuelComponent
		wire []byte
	}{
		{FuelComponent{Count: 1600, Speed: 1}, append([]byte{1, 0, 0, 6, 0x40, 1}, one...)},
		{FuelComponent{CountProvider: "pk:burn", SpeedProvider: "pk:speed"},
			append(append([]byte{0, 7}, "pk:burn"...), append([]byte{0, 8}, "pk:speed"...)...)},
	} {
		stack := ItemStack{ItemID: 1, Count: 1}
		stack.SetCookingFuel(tc.c)
		if !bytes.Equal(stack.Raw[0].Data, tc.wire) {
			t.Errorf("wire = %x, want %x", stack.Raw[0].Data, tc.wire)
		}
		var b Buffer
		b.PutSlot(stack)
		got, ok := NewReader(b.Bytes()).ReadSlot()
		if !ok || !got.Equal(stack) {
			t.Fatalf("round trip: %+v", got)
		}
		if c, ok := got.CookingFuelOf(); !ok || c != tc.c {
			t.Errorf("component = %+v, %v, want %+v", c, ok, tc.c)
		}
		if _, ok := got.BrewingFuelOf(); ok {
			t.Error("cooking_fuel read as brewing_fuel")
		}
	}
	if _, ok := readFuel(NewReader([]byte{1, 0, 0, 0, 1})); ok {
		t.Error("accepted a short value")
	}
}
