package protocol

import (
	"bytes"
	"testing"
)

func TestCompostableWire(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		c    CompostableComponent
		wire []byte
	}{
		{CompostableComponent{Layers: 7}, []byte{1, 0, 0, 0, 7}},
		{CompostableComponent{Layers: -1}, []byte{1, 255, 255, 255, 255}},
		{CompostableComponent{Provider: "pk:layers"}, append([]byte{0, 9}, []byte("pk:layers")...)},
	} {
		stack := ItemStack{ItemID: 1, Count: 2}
		stack.SetCompostable(tc.c)
		if !bytes.Equal(stack.Raw[0].Data, tc.wire) {
			t.Errorf("wire = %x, want %x", stack.Raw[0].Data, tc.wire)
		}
		for _, untrusted := range []bool{false, true} {
			var b Buffer
			if untrusted {
				b.PutUntrustedSlot(stack)
			} else {
				b.PutSlot(stack)
			}
			r := NewReader(b.Bytes())
			read := r.ReadSlot
			if untrusted {
				read = r.ReadUntrustedSlot
			}
			got, ok := read()
			if !ok || r.Remaining() != 0 || !got.Equal(stack) {
				t.Fatalf("round trip: %+v ok=%v err=%v", got, ok, r.Err())
			}
			c, ok := got.CompostableOf()
			if !ok || c != tc.c {
				t.Errorf("component = %+v, %v, want %+v", c, ok, tc.c)
			}
		}
	}
	for _, wire := range [][]byte{{1, 0, 0, 0}, {0, 3, 'B', 'A', 'D'}} {
		if _, ok := readCompostable(NewReader(wire)); ok {
			t.Errorf("accepted malformed %x", wire)
		}
	}
}
