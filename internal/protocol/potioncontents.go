package protocol

import "slices"

// The minecraft:potion_contents component is one of the unmodelled ones
// this server keeps as the bytes it arrived in, but a finished drink reads
// what the potion was brewed as, so its value is decoded here.

// PotionContents is minecraft:potion_contents: the brewed potion, the colour
// and name overriding it in the tooltip, and custom effects on top.
// vanilla: the PotionContents record
type PotionContents struct {
	// Potion is the potion the component names, by the ID the built-in
	// minecraft:potion registry numbers it by; NoPotion for a component
	// that names none.
	Potion int32
	// CustomColor is the ARGB colour the client draws the potion by in
	// place of the potion's own; nil is the component's absence.
	CustomColor *int32
	// CustomEffects are the effects added over the potion's own, each by
	// its mob-effect ID.
	CustomEffects []PotionEffectSpec
	// CustomName is the name the client shows in place of the potion's
	// own; nil is the component's absence.
	CustomName *string
}

// NoPotion is PotionContents.Potion for a component that names no potion.
const NoPotion = -1

// PotionEffectSpec is one custom effect by wire IDs, with the hidden
// instance that takes over when it runs out.
// vanilla: MobEffectInstance
type PotionEffectSpec struct {
	ID                               int32
	Amplifier, Duration              int32
	Ambient, ShowParticles, ShowIcon bool
	Hidden                           *PotionEffectSpec
}

// PotionContentsOf returns what the potion_contents component a stack
// carries, reporting false for a stack that carries none — absent, the
// component is the item's own default: a brewing of no potion at all.
func (s ItemStack) PotionContentsOf() (PotionContents, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentPotionContents {
			continue
		}
		contents, ok := readPotionContents(NewReader(raw.Data))
		if !ok {
			return PotionContents{}, false
		}
		return contents, true
	}
	return PotionContents{}, false
}

// SetPotionContents lays the minecraft:potion_contents component on the
// stack, its value encoded in the wire shape the component travels in: how a
// stack read out of a file's NBT carries the contents the file names.
func (s *ItemStack) SetPotionContents(contents PotionContents) {
	var value Buffer
	value.PutPotionContents(contents)
	s.putRaw(RawComponent{Type: DataComponentPotionContents, Data: value.Bytes()})
}

// SetPotion sets potion_contents to the potion alone, as a brew's result
// carries it, replacing any the stack had.
// vanilla: PotionContents.createItemStack
func (s *ItemStack) SetPotion(potion int32) {
	var value Buffer
	value.PutBool(true)
	value.PutVarInt(potion)
	value.PutBool(false) // the custom colour
	value.PutVarInt(0)   // the custom effects
	value.PutBool(false) // the custom name
	s.putRaw(RawComponent{Type: DataComponentPotionContents, Data: value.Bytes()})
}

// PotionDurationScaleOf is the stack's minecraft:potion_duration_scale, 1 for
// a stack that carries none or an unreadable one.
func (s ItemStack) PotionDurationScaleOf() float32 {
	if scale, ok := s.PotionDurationScale(); ok {
		return scale
	}
	return 1
}

// PotionDurationScale is the stack's minecraft:potion_duration_scale and
// whether it carries a readable one.
func (s ItemStack) PotionDurationScale() (float32, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentPotionDurationScale {
			continue
		}
		r := NewReader(raw.Data)
		scale := r.ReadF32()
		return scale, r.Err() == nil
	}
	return 0, false
}

// SetPotionDurationScale lays minecraft:potion_duration_scale on the stack.
func (s *ItemStack) SetPotionDurationScale(scale float32) {
	var value Buffer
	value.PutF32(scale)
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentPotionDurationScale, Data: value.Bytes()})
}

// StewEffect is one suspicious_stew_effects entry: a mob effect by registry
// ID and its duration in ticks.
type StewEffect struct {
	Effect, Duration int32
}

// SuspiciousStewEffectsOf returns the effects the stack's
// suspicious_stew_effects component stores, nil when it carries none.
func (s ItemStack) SuspiciousStewEffectsOf() []StewEffect {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentSuspiciousStew {
			continue
		}
		r := NewReader(raw.Data)
		n := r.ReadVarInt()
		var out []StewEffect
		for range n {
			e := StewEffect{Effect: r.ReadVarInt(), Duration: r.ReadVarInt()}
			if r.Err() != nil {
				return nil
			}
			out = append(out, e)
		}
		return out
	}
	return nil
}

// SetSuspiciousStewEffects lays minecraft:suspicious_stew_effects on the
// stack.
func (s *ItemStack) SetSuspiciousStewEffects(effects []StewEffect) {
	var value Buffer
	value.PutVarInt(int32(len(effects)))
	for _, e := range effects {
		value.PutVarInt(e.Effect)
		value.PutVarInt(e.Duration)
	}
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentSuspiciousStew, Data: value.Bytes()})
}

// ItemOminousBottleAmplifier resolves an item's default component through registry.
var ItemOminousBottleAmplifier func(item int32) (int32, bool)

// OminousBottleAmplifier returns the effective ominous_bottle_amplifier,
// false when the stack removes it or neither the patch nor the item has one.
func (s ItemStack) OminousBottleAmplifier() (int32, bool) {
	if s.Removes(DataComponentOminousBottleAmplifier) {
		return 0, false
	}
	for _, raw := range s.Raw {
		if raw.Type != DataComponentOminousBottleAmplifier {
			continue
		}
		r := NewReader(raw.Data)
		v := r.ReadVarInt()
		return v, r.Err() == nil
	}
	if ItemOminousBottleAmplifier != nil {
		return ItemOminousBottleAmplifier(s.ItemID)
	}
	return 0, false
}

// OminousBottleAmplifierOf returns the amplifier, zero without a component.
func (s ItemStack) OminousBottleAmplifierOf() int32 {
	amplifier, _ := s.OminousBottleAmplifier()
	return amplifier
}

// SetOminousBottleAmplifier lays minecraft:ominous_bottle_amplifier on the
// stack.
func (s *ItemStack) SetOminousBottleAmplifier(amplifier int32) {
	var value Buffer
	value.PutVarInt(amplifier)
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentOminousBottleAmplifier, Data: value.Bytes()})
}
