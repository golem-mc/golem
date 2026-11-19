package nbt

import (
	"reflect"
	"slices"
	"testing"
)

func TestNewListPreservesInput(t *testing.T) {
	for _, tc := range []struct {
		name  string
		items []Tag
		want  List
	}{
		{"empty", nil, List{Elem: TypeEnd}},
		{"homogeneous", []Tag{Int(1), Int(2)}, List{Elem: TypeInt, Items: []Tag{Int(1), Int(2)}}},
		{"compounds", []Tag{Compound{"a": Int(1)}, Compound{"": String("x")}}, List{Elem: TypeCompound, Items: []Tag{Compound{"a": Int(1)}, Compound{"": String("x")}}}},
		{"mixed", []Tag{Int(1), String("x")}, List{Elem: TypeCompound, Items: []Tag{Compound{"": Int(1)}, Compound{"": String("x")}}}},
		// vanilla: ListTag.wrapIfNeeded keeps a plain compound and wraps one shaped like a wrapper.
		{"mixed compound", []Tag{Int(1), Compound{"id": String("b")}}, List{Elem: TypeCompound, Items: []Tag{Compound{"": Int(1)}, Compound{"id": String("b")}}}},
		{"mixed wrapper compound", []Tag{Int(1), Compound{"": String("x")}}, List{Elem: TypeCompound, Items: []Tag{Compound{"": Int(1)}, Compound{"": Compound{"": String("x")}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := slices.Clone(tc.items)
			if got := NewList(tc.items); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("NewList = %#v, want %#v", got, tc.want)
			}
			if !reflect.DeepEqual(tc.items, before) {
				t.Error("NewList changed its input slice")
			}
		})
	}
}
