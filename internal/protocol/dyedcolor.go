package protocol

import "slices"

// readDyedColor reads minecraft:dyed_color: the colour as one four-byte int.
// vanilla: DyedItemColor.STREAM_CODEC
func readDyedColor(r *Reader) bool {
	r.ReadI32()
	return r.Err() == nil
}

// SetDyedColor lays minecraft:dyed_color on the stack with the given RGB.
func (s *ItemStack) SetDyedColor(rgb int32) {
	var value Buffer
	value.PutI32(rgb)
	// A copied stack shares the Raw backing array; the write must not reach
	// the stack it came from.
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentDyedColor, Data: value.Bytes()})
}

// HasDyedColor reports whether the stack carries minecraft:dyed_color.
func (s ItemStack) HasDyedColor() bool {
	for _, raw := range s.Raw {
		if raw.Type == DataComponentDyedColor {
			return true
		}
	}
	return false
}

// RemoveDyedColor takes minecraft:dyed_color off the stack.
func (s *ItemStack) RemoveDyedColor() { s.dropRaw(DataComponentDyedColor) }

// RemoveLastBannerPattern drops the top layer of the stack's
// minecraft:banner_patterns, keeping the other layers byte for byte (inline
// patterns included, which SetBannerPatterns cannot write). It reports false
// for a stack with no layer to drop or a value that does not read.
// vanilla: BannerPatternLayers.removeLast
func (s *ItemStack) RemoveLastBannerPattern() bool {
	for i, raw := range s.Raw {
		if raw.Type != DataComponentBannerPatterns {
			continue
		}
		r := NewReader(raw.Data)
		count := r.ReadVarInt()
		if r.Err() != nil || count <= 0 {
			return false
		}
		layersStart, lastStart := len(raw.Data)-r.Remaining(), 0
		for range count {
			lastStart = len(raw.Data) - r.Remaining()
			if pattern := r.ReadVarInt(); pattern == 0 {
				readIdentifier(r)
				r.ReadString(32767)
			}
			r.ReadVarInt()
			if r.Err() != nil {
				return false
			}
		}
		s.Raw = slices.Clone(s.Raw)
		if count == 1 {
			// An empty list is the banner's default, which the patch leaves out.
			s.dropRaw(DataComponentBannerPatterns)
			return true
		}
		var out Buffer
		out.PutVarInt(count - 1)
		out.PutRaw(raw.Data[layersStart:lastStart])
		s.Raw[i] = RawComponent{Type: DataComponentBannerPatterns, Data: out.Bytes()}
		return true
	}
	return false
}

// DyedColorOf returns the RGB the stack's minecraft:dyed_color carries.
func (s ItemStack) DyedColorOf() (int32, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentDyedColor {
			continue
		}
		r := NewReader(raw.Data)
		rgb := r.ReadI32()
		if r.Err() != nil || r.Remaining() != 0 {
			return 0, false
		}
		return rgb, true
	}
	return 0, false
}
