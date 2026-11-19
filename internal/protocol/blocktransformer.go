package protocol

import "slices"

// BlockTransformerOf returns the built-in block_transformer holder's registry ID.
func (s ItemStack) BlockTransformerOf() (int32, bool) {
	for _, raw := range s.Raw {
		if raw.Type == DataComponentBlockTransformer {
			r := NewReader(raw.Data)
			id := r.ReadVarInt()
			return id, r.Err() == nil && r.Remaining() == 0 && id >= 0 && id < RegistrySizeBlockTransformer
		}
	}
	return 0, false
}

// SetBlockTransformer sets a built-in block_transformer registry reference.
// vanilla: BlockTransformer.STREAM_CODEC
func (s *ItemStack) SetBlockTransformer(id int32) {
	var value Buffer
	value.PutVarInt(id)
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentBlockTransformer, Data: value.Bytes()})
}
