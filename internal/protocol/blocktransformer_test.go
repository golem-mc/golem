package protocol

import "testing"

func TestBlockTransformerWire(t *testing.T) {
	t.Parallel()
	for _, id := range []int32{-1, 0, 1, 2, 3} {
		stack := ItemStack{ItemID: 1, Count: 4}
		stack.SetBlockTransformer(id)
		for _, untrusted := range []bool{false, true} {
			var wire Buffer
			if untrusted {
				wire.PutUntrustedSlot(stack)
			} else {
				wire.PutSlot(stack)
			}
			r := NewReader(wire.Bytes())
			read := r.ReadSlot
			if untrusted {
				read = r.ReadUntrustedSlot
			}
			got, ok := read()
			wantOK := id >= 0 && id < 3
			if ok != wantOK || ok && (!got.Equal(stack) || r.Remaining() != 0) {
				t.Errorf("id=%d untrusted=%v: stack=%+v ok=%v", id, untrusted, got, ok)
			}
			if ok {
				if holder, present := got.BlockTransformerOf(); !present || holder != id {
					t.Errorf("holder=%d present=%v, want %d", holder, present, id)
				}
			}
		}
	}
	stack := ItemStack{ItemID: 1, Count: 1}
	stack.SetBlockTransformer(0)
	if raw := stack.Raw[0].Data; len(raw) != 1 || raw[0] != 0 {
		t.Fatalf("axe reference = %x, want 00 (no inline marker)", raw)
	}
	copy := stack
	copy.SetBlockTransformer(2)
	if id, _ := stack.BlockTransformerOf(); id != 0 {
		t.Fatal("setting a copied stack changed the original")
	}
	copy.Remove(DataComponentBlockTransformer)
	var wire Buffer
	wire.PutSlot(copy)
	if got, ok := NewReader(wire.Bytes()).ReadSlot(); !ok || !got.Equal(copy) {
		t.Fatal("removal did not survive the wire")
	}
	copy.SetBlockTransformer(1)
	if copy.Removes(DataComponentBlockTransformer) {
		t.Fatal("setting the transformer kept its removal")
	}
	for _, raw := range [][]byte{nil, {0x80}, {0, 0}} {
		bad := ItemStack{Raw: []RawComponent{{Type: DataComponentBlockTransformer, Data: raw}}}
		if _, ok := bad.BlockTransformerOf(); ok {
			t.Errorf("accepted malformed holder %x", raw)
		}
	}
}
