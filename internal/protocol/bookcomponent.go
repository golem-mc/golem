package protocol

// BookPages returns how many pages the stack's written or writable book
// content holds, and false for a stack carrying neither component. The
// components are raw-held, so the count is read off their wire bytes.
// vanilla: WrittenBookContent.STREAM_CODEC, WritableBookContent.STREAM_CODEC
func (s ItemStack) BookPages() (int, bool) {
	for _, raw := range s.Raw {
		r := NewReader(raw.Data)
		switch raw.Type {
		case DataComponentWrittenBookContent:
			r.ReadString(32)
			if r.ReadBool() {
				r.ReadString(32)
			}
			r.ReadString(32767)
			r.ReadVarInt() // generation
		case DataComponentWritableBookContent:
		default:
			continue
		}
		pages := r.ReadVarInt()
		if r.Err() != nil || pages < 0 {
			return 0, false
		}
		return int(pages), true
	}
	return 0, false
}
