package nbt

import (
	"reflect"
	"slices"
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

// TestMixedListWrapsAWrapperLikeCompound checks a compound that looks like a
// wrapper is wrapped again in a mixed list, so it does not read back as the
// value it holds.
// vanilla: ListTag.wrapIfNeeded
func TestMixedListWrapsAWrapperLikeCompound(t *testing.T) {
	list := List{Elem: TypeCompound, Items: []Tag{Byte(1), Compound{"": Byte(2)}}}
	data, err := Encode("", list)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	_, tag, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !reflect.DeepEqual(tag, list) {
		t.Fatalf("Decode = %#v, want %#v", tag, list)
	}
	again, err := Encode("", tag)
	if err != nil {
		t.Fatalf("re-Encode: %v", err)
	}
	if !slices.Equal(again, data) {
		t.Errorf("re-Encode wrote %x, want the bytes it read %x", again, data)
	}
}

// TestListNamesItsTypeByItsEntries checks a list whose Elem disagrees with
// the one type its entries share is written as that type, not refused.
// vanilla: ListTag.identifyRawElementType
func TestListNamesItsTypeByItsEntries(t *testing.T) {
	data, err := Encode("", List{Elem: TypeCompound, Items: []Tag{Byte(1), Byte(2)}})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	_, tag, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	want := List{Elem: TypeByte, Items: []Tag{Byte(1), Byte(2)}}
	if !reflect.DeepEqual(tag, want) {
		t.Errorf("Decode = %#v, want %#v", tag, want)
	}
}
