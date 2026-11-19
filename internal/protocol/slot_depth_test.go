package protocol

import (
	"errors"
	"testing"
)

// The depth limit on slot and component decoding: a container component
// holds slots, and each of those can carry a container again, so a level
// of nesting costs a few bytes on the wire but a frame of the decode. A
// stack nested past the limit fails the reader rather than the stack
// alone, whichever shape it travels in.

// nestedContainerValue writes the contents of a stack whose every level
// carries one entry, and that entry one container component again, levels
// deep. It is the value the container component holds, bare the way the
// entries inside a slot carry it: the trusted shape sends it as it is, the
// untrusted one inside its byte-length prefix.
func nestedContainerValue(b *Buffer, levels int) {
	for range levels {
		b.PutVarInt(1) // one entry
		b.PutBool(true)
		b.PutVarInt(1) // the entry's item
		b.PutVarInt(1) // the entry's count
		b.PutVarInt(1) // one component added
		b.PutVarInt(0) // none removed
		b.PutVarInt(DataComponentContainer)
	}
	// The innermost entry adds nothing back: an empty patch.
	b.PutVarInt(0)
	b.PutVarInt(0)
}

// nestingDepthOf walks a stack's container components to the level its
// nesting ends at.
func nestingDepthOf(stack ItemStack) int {
	depth := 0
	for len(stack.Container) > 0 {
		depth++
		stack = stack.Container[0]
	}
	return depth
}

// TestSlotBoundsComponentNesting pins the trusted shape: a stack nested
// within the limit reads back with its nesting intact, one past it fails
// the reader with ErrSlotTooDeep, so a packet carrying one is a packet
// that did not read.
func TestSlotBoundsComponentNesting(t *testing.T) {
	var held Buffer
	held.PutVarInt(maxComponentDepth)
	held.PutVarInt(1) // the item
	held.PutVarInt(1) // one component added
	held.PutVarInt(0) // none removed
	held.PutVarInt(DataComponentContainer)
	nestedContainerValue(&held, maxComponentDepth)
	stack, ok := NewReader(held.Bytes()).ReadSlot()
	if !ok {
		t.Fatal("ReadSlot refused a stack nested within the limit")
	}
	if got := nestingDepthOf(stack); got != maxComponentDepth {
		t.Errorf("the stack holds %d levels of nesting, want %d", got, maxComponentDepth)
	}

	var deep Buffer
	deep.PutVarInt(maxComponentDepth + 1)
	deep.PutVarInt(1) // the item
	deep.PutVarInt(1) // one component added
	deep.PutVarInt(0) // none removed
	deep.PutVarInt(DataComponentContainer)
	nestedContainerValue(&deep, maxComponentDepth+1)
	r := NewReader(deep.Bytes())
	if stack, ok := r.ReadSlot(); ok {
		t.Errorf("ReadSlot = %+v, want the stack nested past the limit refused", stack)
	}
	if !errors.Is(r.Err(), ErrSlotTooDeep) {
		t.Errorf("reader error = %v, want ErrSlotTooDeep", r.Err())
	}
}

// TestUntrustedSlotBoundsComponentNesting pins the untrusted shape of the
// same nesting: the refusal fires inside the length-prefixed slice a
// component's value arrives in, and it is lifted to the packet's own
// reader — the packet fails, not only the component it travelled in.
func TestUntrustedSlotBoundsComponentNesting(t *testing.T) {
	slot := func(levels int) []byte {
		var b Buffer
		b.PutVarInt(maxComponentDepth) // the count
		b.PutVarInt(1)                 // the item
		b.PutVarInt(1)                 // one component added
		b.PutVarInt(0)                 // none removed
		b.PutVarInt(DataComponentContainer)
		var value Buffer
		nestedContainerValue(&value, levels)
		b.PutVarInt(int32(value.Len()))
		b.PutRaw(value.Bytes())
		return b.Bytes()
	}

	stack, ok := NewReader(slot(maxComponentDepth)).ReadUntrustedSlot()
	if !ok {
		t.Fatal("ReadUntrustedSlot refused a stack nested within the limit")
	}
	if got := nestingDepthOf(stack); got != maxComponentDepth {
		t.Errorf("the stack holds %d levels of nesting, want %d", got, maxComponentDepth)
	}

	r := NewReader(slot(maxComponentDepth + 1))
	if stack, ok := r.ReadUntrustedSlot(); ok {
		t.Errorf("ReadUntrustedSlot = %+v, want the stack nested past the limit refused", stack)
	}
	if !errors.Is(r.Err(), ErrSlotTooDeep) {
		t.Errorf("reader error = %v, want ErrSlotTooDeep", r.Err())
	}
}

// bundleChain returns a bundle_contents value holding nested bundles,
// bundles deep, the innermost empty. A bundle weighs a sixteenth of itself,
// so seventeen is the deepest a survival shulker box of bundles reaches.
// vanilla: BundleContents.BUNDLE_IN_BUNDLE_WEIGHT
func bundleChain(bundles int) []byte {
	var e Buffer
	e.PutVarInt(0) // the innermost holds nothing
	value := e.Bytes()
	for range bundles - 1 {
		inner := value
		value = bundleHolding(DataComponentBundleContents, func(b *Buffer) { b.PutRaw(inner) })
	}
	return value
}
