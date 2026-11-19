package nbt

import "testing"

// Exercise the shared codec's map compounds and mixed-list wrappers, as used
// by saved block entities and item components.
func BenchmarkCompoundCodec(b *testing.B) {
	tag := Compound{
		"id": String("minecraft:chest"), "x": Int(12), "y": Int(64), "z": Int(-3),
		"Items": List{Elem: TypeCompound, Items: []Tag{
			Compound{"Slot": Byte(0), "id": String("minecraft:stone"), "count": Int(64)},
			Compound{"Slot": Byte(1), "id": String("minecraft:diamond"), "count": Int(1)},
		}},
		"custom": List{Elem: TypeCompound, Items: []Tag{Int(1), String("two"), Compound{"v": Int(3)}}},
	}
	data, err := EncodeNetwork(tag)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("Encode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := EncodeNetwork(tag); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Decode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, _, err := DecodeNetwork(data); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkCompoundCopy(b *testing.B) {
	ordered, _, err := ParseOrderedValueTag(`{nested:{big:1,s:1,t:1,v:1,f:1,w:1,em:1,k:1,l:1,m:1,bo:1,n:1,a:1},list:[{v:1,f:2,m:3,bo:4}]}`)
	if err != nil {
		b.Fatal(err)
	}
	ordered.(CompoundTag).Entries()["nested"].(CompoundTag).Delete("a")
	for _, tc := range []struct {
		name string
		tag  Tag
	}{
		{"Map", PlainCopy(ordered)},
		{"Ordered", ordered},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				Copy(tc.tag)
			}
		})
	}
}
