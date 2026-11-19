package protocol

import "slices"

// CustomModelData is minecraft:custom_model_data, the values a resource
// pack's item model selects on.
// vanilla: CustomModelData
type CustomModelData struct {
	Floats  []float32
	Flags   []bool
	Strings []string
	Colors  []int32 // RGB
}

// SetCustomModelData lays minecraft:custom_model_data on the stack.
// vanilla: CustomModelData.STREAM_CODEC
func (s *ItemStack) SetCustomModelData(d CustomModelData) {
	var b Buffer
	b.PutVarInt(int32(len(d.Floats)))
	for _, f := range d.Floats {
		b.PutF32(f)
	}
	b.PutVarInt(int32(len(d.Flags)))
	for _, f := range d.Flags {
		b.PutBool(f)
	}
	b.PutVarInt(int32(len(d.Strings)))
	for _, str := range d.Strings {
		b.PutString(str)
	}
	b.PutVarInt(int32(len(d.Colors)))
	for _, c := range d.Colors {
		b.PutI32(c)
	}
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentCustomModelData, Data: b.Bytes()})
}

// CustomModelDataOf returns the stack's minecraft:custom_model_data, false
// when it carries none or its value does not read.
func (s ItemStack) CustomModelDataOf() (CustomModelData, bool) {
	for _, raw := range s.Raw {
		if raw.Type == DataComponentCustomModelData {
			return readCustomModelData(NewReader(raw.Data))
		}
	}
	return CustomModelData{}, false
}

// readCustomModelData reads the four counted lists: floats, flags, strings
// of at most 32767 code units and int colours.
// vanilla: CustomModelData.STREAM_CODEC
func readCustomModelData(r *Reader) (CustomModelData, bool) {
	var d CustomModelData
	count := func() int {
		n := r.ReadVarInt()
		if r.Err() != nil || n < 0 || int(n) > r.Remaining() {
			return -1
		}
		return int(n)
	}
	n := count()
	for range n {
		d.Floats = append(d.Floats, r.ReadF32())
	}
	if n < 0 {
		return CustomModelData{}, false
	}
	n = count()
	for range n {
		d.Flags = append(d.Flags, r.ReadBool())
	}
	if n < 0 {
		return CustomModelData{}, false
	}
	n = count()
	for range n {
		d.Strings = append(d.Strings, r.ReadString(32767))
	}
	if n < 0 {
		return CustomModelData{}, false
	}
	n = count()
	for range n {
		d.Colors = append(d.Colors, r.ReadI32())
	}
	if n < 0 || r.Err() != nil {
		return CustomModelData{}, false
	}
	return d, true
}

// ContainerLoot is minecraft:container_loot, the table a placed container
// rolls into its slots on first open; the zero value is absence. It has no
// network codec, so it lives only in the server's slots and saved files.
// vanilla: DataComponents.CONTAINER_LOOT, SeededContainerLoot
type ContainerLoot struct {
	Table string
	Seed  int64
}
