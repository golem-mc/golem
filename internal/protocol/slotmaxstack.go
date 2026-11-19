package protocol

// MaxStackSizeRange is the bounds of a max_stack_size value.
// vanilla: DataComponents.MAX_STACK_SIZE, ExtraCodecs.intRange(1, 99)
const (
	MinMaxStackSize = 1
	MaxMaxStackSize = 99
)

func readMaxStackSize(r *Reader) (int32, bool) {
	n := r.ReadVarInt()
	return n, r.Err() == nil && n >= MinMaxStackSize && n <= MaxMaxStackSize
}

// MaxStackSizeOf returns the stack's own max_stack_size component.
func (s ItemStack) MaxStackSizeOf() (int32, bool) {
	for _, raw := range s.Raw {
		if raw.Type == DataComponentMaxStackSize {
			r := NewReader(raw.Data)
			n, ok := readMaxStackSize(r)
			return n, ok && r.Remaining() == 0
		}
	}
	return 0, false
}

// SetMaxStackSize replaces the stack's max_stack_size component.
func (s *ItemStack) SetMaxStackSize(n int32) {
	var b Buffer
	b.PutVarInt(n)
	s.putRaw(RawComponent{Type: DataComponentMaxStackSize, Data: b.Bytes()})
}
