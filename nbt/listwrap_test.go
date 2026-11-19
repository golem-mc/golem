package nbt

import (
	"reflect"
	"testing"
)

// TestWrappedListRoundTrip checks a list of {"":x} wrappers the game saves
// reads back into a tree that encodes again, and that the second read gives
// the same tree: the file a chunk, a player or a storage was read from can
// always be saved again.
// vanilla: ListTag.tryUnwrap, ListTag.wrapIfNeeded
func TestWrappedListRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		// raw is the list as the file holds it, every entry a compound.
		raw  []Tag
		want List
	}{
		{
			name: "every entry a wrapper of one type",
			raw:  []Tag{Compound{"": Byte(1)}, Compound{"": Byte(2)}},
			want: List{Elem: TypeByte, Items: []Tag{Byte(1), Byte(2)}},
		},
		{
			name: "a single wrapper",
			raw:  []Tag{Compound{"": String("a")}},
			want: List{Elem: TypeString, Items: []Tag{String("a")}},
		},
		{
			name: "wrappers of mixed types",
			raw:  []Tag{Compound{"": Byte(1)}, Compound{"": String("two")}},
			want: List{Elem: TypeCompound, Items: []Tag{Byte(1), String("two")}},
		},
		{
			name: "a wrapper beside a plain compound",
			raw:  []Tag{Compound{"": Int(1)}, Compound{"a": Int(2)}},
			want: List{Elem: TypeCompound, Items: []Tag{Int(1), Compound{"a": Int(2)}}},
		},
		{
			name: "a wrapped compound that is itself one empty-named field",
			raw:  []Tag{Compound{"": Byte(1)}, Compound{"": Compound{"": Byte(2)}}},
			want: List{Elem: TypeCompound, Items: []Tag{Byte(1), Compound{"": Byte(2)}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			file, err := Encode("", Compound{"list": List{Elem: TypeCompound, Items: tc.raw}})
			if err != nil {
				t.Fatalf("Encode the file: %v", err)
			}
			_, first, err := Decode(file)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got := first.(Compound)["list"]; !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Decode = %#v, want %#v", got, tc.want)
			}
			again, err := Encode("", first)
			if err != nil {
				t.Fatalf("re-Encode the decoded tree: %v", err)
			}
			_, second, err := Decode(again)
			if err != nil {
				t.Fatalf("Decode the re-encoded file: %v", err)
			}
			if !reflect.DeepEqual(second, first) {
				t.Errorf("second Decode = %#v, want %#v", second, first)
			}
		})
	}
}
