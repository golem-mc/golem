package nbt

import (
	"testing"

	"github.com/google/uuid"
)

// TestUUIDIntArrayRoundTrip checks the four big-endian words a UUID is kept
// as, read straight from its own bytes, and that a shape that is not four
// words decodes as no UUID.
func TestUUIDIntArrayRoundTrip(t *testing.T) {
	id := uuid.MustParse("f81d4fae-7dec-11d0-a765-00a0c91e6bf6")
	var want IntArray
	for i := 0; i < len(id); i += 4 {
		want = append(want, int32(uint32(id[i])<<24|uint32(id[i+1])<<16|uint32(id[i+2])<<8|uint32(id[i+3])))
	}
	if got := UUIDToIntArray(id); len(got) != 4 || got[0] != want[0] || got[1] != want[1] ||
		got[2] != want[2] || got[3] != want[3] {
		t.Fatalf("UUIDToIntArray(%s) = %v, want %v", id, got, want)
	}
	if got, ok := UUIDFromIntArray(want); !ok || got != id {
		t.Errorf("UUIDFromIntArray(%v) = %s/%v, want %s", want, got, ok, id)
	}
	if got, ok := UUIDFromIntArray(IntArray{1, 2, 3}); ok || got != uuid.Nil {
		t.Errorf("a three-word array decoded to %s/%v, want the nil UUID and false", got, ok)
	}
	if got, ok := UUIDFromIntArray(nil); ok || got != uuid.Nil {
		t.Errorf("a nil array decoded to %s/%v, want the nil UUID and false", got, ok)
	}
}

// TestUUIDFieldReadsBothShapes checks that a UUID field reads from the int
// array and from a list of four ints, that the zero UUID is a real UUID, and
// that a missing field, a wrong length, a list holding anything but ints and
// any other tag read as absent.
func TestUUIDFieldReadsBothShapes(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("f81d4fae-7dec-11d0-a765-00a0c91e6bf6")
	words := UUIDToIntArray(id)
	list := List{Elem: TypeInt}
	for _, w := range words {
		list.Items = append(list.Items, Int(w))
	}
	c := Compound{
		"array":     words,
		"list":      list,
		"zero":      IntArray{0, 0, 0, 0},
		"short":     IntArray{1, 2, 3},
		"long":      IntArray{1, 2, 3, 4, 5},
		"emptyList": List{Elem: TypeInt},
		"mixedList": List{Items: []Tag{Int(1), Int(2), Long(3), Int(4)}},
		"longArray": LongArray{1, 2},
		"string":    String(id.String()),
	}
	for _, key := range []string{"array", "list"} {
		if got, ok := UUIDField(c, key); !ok || got != id {
			t.Errorf("UUIDField(%q) = %s/%v, want %s/true", key, got, ok, id)
		}
	}
	if got, ok := UUIDField(c, "zero"); !ok || got != uuid.Nil {
		t.Errorf("UUIDField(zero) = %s/%v, want the nil UUID and true", got, ok)
	}
	for _, key := range []string{"missing", "short", "long", "emptyList", "mixedList", "longArray", "string"} {
		if got, ok := UUIDField(c, key); ok || got != uuid.Nil {
			t.Errorf("UUIDField(%q) = %s/%v, want the nil UUID and false", key, got, ok)
		}
	}
}
