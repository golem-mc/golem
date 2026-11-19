package nbt

import "slices"

// Equal answers whether two tags are the same value, a comparison
// that keeps each tag's type the way the NBT equals does.
func Equal(a, b Tag) bool {
	switch v := a.(type) {
	case Byte:
		w, ok := b.(Byte)
		return ok && v == w
	case Short:
		w, ok := b.(Short)
		return ok && v == w
	case Int:
		w, ok := b.(Int)
		return ok && v == w
	case Long:
		w, ok := b.(Long)
		return ok && v == w
	case Float:
		w, ok := b.(Float)
		return ok && v == w
	case Double:
		w, ok := b.(Double)
		return ok && v == w
	case String:
		w, ok := b.(String)
		return ok && v == w
	}
	if a == nil || b == nil || a.Type() != b.Type() {
		return a == b
	}
	switch v := a.(type) {
	case CompoundTag:
		w, ok := b.(CompoundTag)
		if !ok || len(v.Entries()) != len(w.Entries()) {
			return false
		}
		for name, child := range v.Entries() {
			other, ok := w.Entries()[name]
			if !ok || !Equal(child, other) {
				return false
			}
		}
		return true
	case List:
		w, ok := b.(List)
		if !ok || v.Elem != w.Elem || len(v.Items) != len(w.Items) {
			return false
		}
		for i, item := range v.Items {
			if !Equal(item, w.Items[i]) {
				return false
			}
		}
		return true
	case ByteArray:
		w, ok := b.(ByteArray)
		return ok && slices.Equal(v, w)
	case IntArray:
		w, ok := b.(IntArray)
		return ok && slices.Equal(v, w)
	case LongArray:
		w, ok := b.(LongArray)
		return ok && slices.Equal(v, w)
	}
	return false
}
