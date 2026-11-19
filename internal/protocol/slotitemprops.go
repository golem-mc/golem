package protocol

// The wire shapes of the components that override an item's prototype and are
// held as raw bytes; registry holds their NBT forms.

// readPositiveVarInt reads a VarInt of at least 1.
// vanilla: ExtraCodecs.POSITIVE_INT, Enchantable.STREAM_CODEC
func readPositiveVarInt(r *Reader) bool {
	n := r.ReadVarInt()
	return r.Err() == nil && n >= 1
}

// readRegistryRef reads a registry ID with no inline form.
// vanilla: ByteBufCodecs.holderRegistry
func readRegistryRef(r *Reader, registry string) bool {
	id := r.ReadVarInt()
	return r.Err() == nil && id >= 0 && id < registrySize(registry, 1<<20)
}

// readHolderSet reads a tag key (0 and an identifier) or a count plus one of
// registry IDs; the key is not checked against the tags here.
// vanilla: ByteBufCodecs.holderSet
func readHolderSet(r *Reader, registry string) bool {
	n := r.ReadVarInt()
	if r.Err() != nil || n < 0 {
		return false
	}
	if n == 0 {
		return readIdentifier(r)
	}
	for range n - 1 {
		if !readRegistryRef(r, registry) {
			return false
		}
	}
	return r.Err() == nil
}
