package protocol

// equipmentSlotNames is EquipmentSlot by its wire ID, which is the enum's
// own id field and not its ordinal.
// vanilla: EquipmentSlot.STREAM_CODEC
var equipmentSlotNames = [...]string{"mainhand", "feet", "legs", "chest", "head", "offhand", "body", "saddle"}

// EquipmentSlotID is the wire ID of an equipment slot name.
func EquipmentSlotID(name string) (int32, bool) {
	for id, n := range equipmentSlotNames {
		if n == name {
			return int32(id), true
		}
	}
	return 0, false
}

// Equippable is a stack's own minecraft:equippable as the wire carries it.
// vanilla: Equippable
type Equippable struct {
	Slot       string
	EquipSound SoundHolder
	// AssetID and CameraOverlay are identifiers, empty when absent.
	AssetID         string
	CameraOverlay   string
	AllowedEntities EntityTypeSet
	Dispensable     bool
	Swappable       bool
	DamageOnHurt    bool
	EquipOnInteract bool
	CanBeSheared    bool
	ShearingSound   SoundHolder
}

// SoundHolder is a sound event: a registry ID, or, when Name is set, an
// inline identifier with an optional fixed range. A negative ID forces inline
// encoding even when Name is empty.
// vanilla: SoundEvent.STREAM_CODEC
type SoundHolder struct {
	ID       int32
	Name     string
	Range    float32
	HasRange bool
}

// EntityTypeSet is a holder set of entity types: a tag or inline registry
// IDs; Present is false for an absent optional set.
type EntityTypeSet struct {
	Present bool
	Tag     string
	IDs     []int32
}

// readEquippable reads a minecraft:equippable value.
// vanilla: Equippable.STREAM_CODEC
func readEquippable(r *Reader) (Equippable, bool) {
	var e Equippable
	slotID := r.ReadVarInt()
	if r.Err() != nil || slotID < 0 || int(slotID) >= len(equipmentSlotNames) {
		return e, false
	}
	e.Slot = equipmentSlotNames[slotID]
	var ok bool
	if e.EquipSound, ok = readSoundHolder(r); !ok {
		return e, false
	}
	for _, id := range []*string{&e.AssetID, &e.CameraOverlay} {
		if !r.ReadBool() {
			continue
		}
		*id = r.ReadString(32767)
		if r.Err() != nil || !ValidIdentifier(*id) {
			return e, false
		}
	}
	if r.ReadBool() {
		if e.AllowedEntities, ok = readEntitySet(r); !ok {
			return e, false
		}
	}
	e.Dispensable = r.ReadBool()
	e.Swappable = r.ReadBool()
	e.DamageOnHurt = r.ReadBool()
	e.EquipOnInteract = r.ReadBool()
	e.CanBeSheared = r.ReadBool()
	if e.ShearingSound, ok = readSoundHolder(r); !ok || r.Err() != nil {
		return e, false
	}
	return e, true
}

// readSoundHolder reads a registry ID plus one, or 0 and an inline sound:
// its identifier and an optional fixed range.
// vanilla: SoundEvent.STREAM_CODEC
func readSoundHolder(r *Reader) (SoundHolder, bool) {
	n := r.ReadVarInt()
	if r.Err() != nil || n < 0 || n > RegistrySizeSoundEvent {
		return SoundHolder{}, false
	}
	if n > 0 {
		return SoundHolder{ID: n - 1}, true
	}
	s := SoundHolder{Name: r.ReadString(32767)}
	if r.Err() != nil || !ValidIdentifier(s.Name) {
		return SoundHolder{}, false
	}
	if s.HasRange = r.ReadBool(); s.HasRange {
		s.Range = r.ReadF32()
	}
	return s, r.Err() == nil
}

// readEntitySet reads 0 and a tag identifier, or the inline entity type
// count plus one and the IDs.
// vanilla: ByteBufCodecs.holderSet
func readEntitySet(r *Reader) (EntityTypeSet, bool) {
	set := EntityTypeSet{Present: true}
	n := r.ReadVarInt()
	if r.Err() != nil || n < 0 {
		return set, false
	}
	if n == 0 {
		set.Tag = r.ReadString(32767)
		return set, r.Err() == nil && ValidIdentifier(set.Tag)
	}
	set.IDs = make([]int32, 0, min(n-1, int32(r.Remaining())))
	for range n - 1 {
		id := r.ReadVarInt()
		if r.Err() != nil || id < 0 || id >= RegistrySizeEntityType {
			return set, false
		}
		set.IDs = append(set.IDs, id)
	}
	return set, true
}

// EquippableOf returns the stack's own minecraft:equippable, false for a
// stack without one, which goes by its item's default.
func (s ItemStack) EquippableOf() (Equippable, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentEquippable {
			continue
		}
		r := NewReader(raw.Data)
		e, ok := readEquippable(r)
		return e, ok && r.Remaining() == 0
	}
	return Equippable{}, false
}
