package nbt

import "testing"

func TestEqualKeepsTypes(t *testing.T) {
	a := Compound{"x": Int(1), "l": List{Elem: TypeString, Items: []Tag{String("a")}}}
	b := Compound{"x": Int(1), "l": List{Elem: TypeString, Items: []Tag{String("a")}}}
	if !Equal(a, b) {
		t.Fatal("equal compounds differ")
	}
	if Equal(Compound{"x": Int(1)}, Compound{"x": Long(1)}) {
		t.Fatal("an int equals a long")
	}
}
