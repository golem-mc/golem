package protocol

import "testing"

func TestTropicalFishBucketComponentsRoundTrip(t *testing.T) {
	const variant = 117506305
	stack := ItemStack{ItemID: 1050, Count: 1}
	stack.SetTropicalFishVariant(variant)
	var b Buffer
	b.PutSlot(stack)
	out, ok := NewReader(b.Bytes()).ReadSlot()
	if !ok || out.TropicalFishVariant(0) != variant {
		t.Fatalf("bucket round trip = %+v (%v)", out, ok)
	}
	clone := stack
	clone.SetTropicalFishVariant(0x04000500)
	if stack.TropicalFishVariant(0) != variant {
		t.Fatal("writing a copied bucket changed the original")
	}
	if len(clone.Raw) != 3 {
		t.Fatalf("replacing components left %d components", len(clone.Raw))
	}
	out.Raw = []RawComponent{{Type: DataComponentTropicalFishBaseColor, Data: []byte{4}}}
	if got := out.TropicalFishVariant(variant); got != 117702913 {
		t.Fatalf("partial components = %d", got)
	}
	out.Raw = []RawComponent{{Type: DataComponentTropicalFishBaseColor, Data: []byte{0x81, 0x02}}}
	if got := out.TropicalFishVariant(variant); got != 117440769 {
		t.Fatalf("invalid color did not fall back to white: %d", got)
	}
}
