package protocol

import (
	"math"
	"slices"
)

// BlocksAttacksComponent is a stack's own minecraft:blocks_attacks, with the
// damage type sets and sounds as the wire carries them (registry IDs); the
// registry package resolves the names.
// vanilla: BlocksAttacks.STREAM_CODEC
type BlocksAttacksComponent struct {
	BlockDelaySeconds    float32
	DisableCooldownScale float32
	Reductions           []DamageReductionComponent
	ItemDamage           [3]float32 // threshold, base, factor
	BypassedBy           DamageSetComponent
	BlockSound           *SoundHolder // nil for none
	DisableSound         *SoundHolder // nil for none
}

// DamageReductionComponent is one damage_reductions entry.
// vanilla: BlocksAttacks.DamageReduction.STREAM_CODEC
type DamageReductionComponent struct {
	Angle  float32
	Type   DamageSetComponent
	Base   float32
	Factor float32
}

// DamageSetComponent is an optional damage type holder set: a tag, or else
// damage type registry IDs.
type DamageSetComponent struct {
	Present bool
	Tag     string
	// TagID is bound by DamageTypeTagIndex when the component is decoded.
	TagID uint16
	IDs   []int32
}

// clone detaches editable holder contents from the shared stack snapshot.
func (set DamageSetComponent) clone() DamageSetComponent {
	set.IDs = slices.Clone(set.IDs)
	return set
}

// clone detaches editable reductions and sounds from the shared stack snapshot.
func (b BlocksAttacksComponent) clone() BlocksAttacksComponent {
	b.Reductions = slices.Clone(b.Reductions)
	for i := range b.Reductions {
		b.Reductions[i].Type = b.Reductions[i].Type.clone()
	}
	b.BypassedBy = b.BypassedBy.clone()
	if b.BlockSound != nil {
		sound := *b.BlockSound
		b.BlockSound = &sound
	}
	if b.DisableSound != nil {
		sound := *b.DisableSound
		b.DisableSound = &sound
	}
	return b
}

// WeaponComponent is a stack's own minecraft:weapon.
// vanilla: Weapon.STREAM_CODEC
type WeaponComponent struct {
	DamagePerAttack        int32
	DisableBlockingSeconds float32
}

// readBlocksAttacks reads a minecraft:blocks_attacks value.
// vanilla: BlocksAttacks.STREAM_CODEC
func readBlocksAttacks(r *Reader) (BlocksAttacksComponent, bool) {
	b := BlocksAttacksComponent{BlockDelaySeconds: r.ReadF32(), DisableCooldownScale: r.ReadF32()}
	count := r.ReadVarInt()
	if r.Err() != nil || count < 0 {
		return BlocksAttacksComponent{}, false
	}
	// A reduction is at least an angle, an absent flag, a base and a factor.
	b.Reductions = make([]DamageReductionComponent, 0, min(count, int32(r.Remaining()/13)))
	for range count {
		d := DamageReductionComponent{Angle: r.ReadF32()}
		var ok bool
		if d.Type, ok = readDamageSet(r); !ok {
			return BlocksAttacksComponent{}, false
		}
		d.Base, d.Factor = r.ReadF32(), r.ReadF32()
		if r.Err() != nil {
			return BlocksAttacksComponent{}, false
		}
		b.Reductions = append(b.Reductions, d)
	}
	for i := range b.ItemDamage {
		b.ItemDamage[i] = r.ReadF32()
	}
	var ok bool
	if b.BypassedBy, ok = readDamageSet(r); !ok {
		return BlocksAttacksComponent{}, false
	}
	if b.BlockSound, ok = readOptionalSound(r); !ok {
		return BlocksAttacksComponent{}, false
	}
	if b.DisableSound, ok = readOptionalSound(r); !ok {
		return BlocksAttacksComponent{}, false
	}
	if r.Err() != nil || !finite(b.BlockDelaySeconds, b.DisableCooldownScale) {
		return BlocksAttacksComponent{}, false
	}
	return b, true
}

func finite(values ...float32) bool {
	for _, v := range values {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return false
		}
	}
	return true
}

// readDamageSet reads an optional holder set of damage types.
// vanilla: ByteBufCodecs.holderSet
func readDamageSet(r *Reader) (DamageSetComponent, bool) {
	if !r.ReadBool() {
		return DamageSetComponent{}, r.Err() == nil
	}
	return readDamageTypes(r)
}

// readDamageTypes reads a holder set of damage types: 0 and a tag, or the
// ID count plus one and the IDs. vanilla: ByteBufCodecs.holderSet
func readDamageTypes(r *Reader) (DamageSetComponent, bool) {
	set := DamageSetComponent{Present: true}
	n := r.ReadVarInt()
	if r.Err() != nil || n < 0 {
		return DamageSetComponent{}, false
	}
	if n == 0 {
		set.Tag = r.ReadString(32767)
		if r.Err() != nil || !ValidIdentifier(set.Tag) {
			return DamageSetComponent{}, false
		}
		if DamageTypeTagIndex != nil {
			set.TagID = DamageTypeTagIndex(set.Tag)
		}
		return set, true
	}
	set.IDs = make([]int32, 0, min(n-1, int32(r.Remaining())))
	for range n - 1 {
		id := r.ReadVarInt()
		if r.Err() != nil || id < 0 {
			return DamageSetComponent{}, false
		}
		set.IDs = append(set.IDs, id)
	}
	return set, true
}

// readOptionalSound reads an optional sound holder, nil when absent.
// vanilla: ByteBufCodecs.optional, SoundEvent.STREAM_CODEC
func readOptionalSound(r *Reader) (*SoundHolder, bool) {
	if !r.ReadBool() {
		return nil, r.Err() == nil
	}
	s, ok := readSoundHolder(r)
	if !ok {
		return nil, false
	}
	return &s, true
}

// BlocksAttacksOf returns an editable copy of the stack's own
// minecraft:blocks_attacks, false for a stack without one.
func (s ItemStack) BlocksAttacksOf() (BlocksAttacksComponent, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentBlocksAttacks {
			continue
		}
		if raw.damage != nil {
			return raw.damage.blocks.clone(), raw.damage.valid
		}
		r := NewReader(raw.Data)
		b, ok := readBlocksAttacks(r)
		return b, ok && r.Remaining() == 0
	}
	return BlocksAttacksComponent{}, false
}

// SetBlocksAttacks lays a minecraft:blocks_attacks on the stack in its wire
// shape, replacing any the stack had.
func (s *ItemStack) SetBlocksAttacks(b BlocksAttacksComponent) {
	var value Buffer
	value.PutF32(b.BlockDelaySeconds)
	value.PutF32(b.DisableCooldownScale)
	value.PutVarInt(int32(len(b.Reductions)))
	for _, d := range b.Reductions {
		value.PutF32(d.Angle)
		putDamageSet(&value, d.Type)
		value.PutF32(d.Base)
		value.PutF32(d.Factor)
	}
	for _, f := range b.ItemDamage {
		value.PutF32(f)
	}
	putDamageSet(&value, b.BypassedBy)
	putOptionalSound(&value, b.BlockSound)
	putOptionalSound(&value, b.DisableSound)
	s.Raw = append([]RawComponent(nil), s.Raw...)
	s.putRaw(RawComponent{Type: DataComponentBlocksAttacks, Data: value.Bytes()})
}

func putDamageSet(b *Buffer, set DamageSetComponent) {
	b.PutBool(set.Present)
	if set.Present {
		putDamageTypes(b, set)
	}
}

// putDamageTypes writes a holder set as readDamageTypes reads it.
func putDamageTypes(b *Buffer, set DamageSetComponent) {
	if set.Tag != "" {
		b.PutVarInt(0)
		b.PutString(set.Tag)
		return
	}
	b.PutVarInt(int32(len(set.IDs)) + 1)
	for _, id := range set.IDs {
		b.PutVarInt(id)
	}
}

func putOptionalSound(b *Buffer, s *SoundHolder) {
	b.PutBool(s != nil)
	if s != nil {
		PutSoundHolder(b, *s)
	}
}

// readWeapon reads a minecraft:weapon value.
// vanilla: Weapon.STREAM_CODEC
func readWeapon(r *Reader) (WeaponComponent, bool) {
	w := WeaponComponent{DamagePerAttack: r.ReadVarInt(), DisableBlockingSeconds: r.ReadF32()}
	return w, r.Err() == nil && w.DamagePerAttack >= 0 && w.DisableBlockingSeconds >= 0
}

// WeaponOf returns the stack's own minecraft:weapon, false for a stack
// without one.
func (s ItemStack) WeaponOf() (WeaponComponent, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentWeapon {
			continue
		}
		r := NewReader(raw.Data)
		w, ok := readWeapon(r)
		return w, ok && r.Remaining() == 0
	}
	return WeaponComponent{}, false
}

// SetWeapon lays a minecraft:weapon on the stack, replacing any the stack had.
func (s *ItemStack) SetWeapon(w WeaponComponent) {
	var value Buffer
	value.PutVarInt(w.DamagePerAttack)
	value.PutF32(w.DisableBlockingSeconds)
	s.Raw = append([]RawComponent(nil), s.Raw...)
	s.putRaw(RawComponent{Type: DataComponentWeapon, Data: value.Bytes()})
}
