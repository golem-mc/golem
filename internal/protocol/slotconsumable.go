package protocol

// ConsumableComponent is a stack's own minecraft:consumable, which replaces
// the item's default one for every use.
// vanilla: Consumable.STREAM_CODEC
type ConsumableComponent struct {
	Seconds      float32
	Animation    int32
	Sound        SoundHolder
	HasParticles bool
	Effects      []ConsumeEffectComponent
}

// ConsumeEffectComponent is one on_consume_effects entry; the fields its
// Type does not use are zero.
// vanilla: ConsumeEffect.STREAM_CODEC
type ConsumeEffectComponent struct {
	Type        int32
	Effects     []PotionEffectSpec // apply_effects
	Probability float32            // apply_effects
	Remove      []int32            // remove_effects
	Diameter    float32            // teleport_randomly
	Directional bool               // teleport_randomly
	Sound       SoundHolder        // play_sound
}

// The consume effect type IDs (vanilla: ConsumeEffect.Type registry order).
const (
	ConsumeEffectApplyEffects int32 = iota
	ConsumeEffectRemoveEffects
	ConsumeEffectClearAllEffects
	ConsumeEffectTeleportRandomly
	ConsumeEffectPlaySound
)

// itemUseAnimationCount is the number of ItemUseAnimation constants.
const itemUseAnimationCount = 12

// ItemUseAnimationDrink is ItemUseAnimation.DRINK's ID.
const ItemUseAnimationDrink = 2

// FoodComponent is a stack's own minecraft:food.
// vanilla: FoodProperties.DIRECT_STREAM_CODEC
type FoodComponent struct {
	Nutrition    int32
	Saturation   float32
	CanAlwaysEat bool
}

// PutSoundHolder writes a registered or inline sound event holder.
// A negative ID with no name preserves the empty inline playback fallback.
func PutSoundHolder(b *Buffer, h SoundHolder) {
	if h.Name == "" && h.ID >= 0 {
		b.PutVarInt(h.ID + 1)
		return
	}
	b.PutVarInt(0)
	b.PutString(h.Name)
	b.PutBool(h.HasRange)
	if h.HasRange {
		b.PutF32(h.Range)
	}
}

// readConsumeEffect reads one ConsumeEffect: a type ID and its value.
// vanilla: ConsumeEffect.STREAM_CODEC
func readConsumeEffect(r *Reader) (ConsumeEffectComponent, bool) {
	e := ConsumeEffectComponent{Type: r.ReadVarInt()}
	if r.Err() != nil {
		return e, false
	}
	switch e.Type {
	case ConsumeEffectApplyEffects:
		count := r.ReadVarInt()
		// An effect is at least an ID and seven bytes of details.
		if r.Err() != nil || count < 0 || int(count) > r.Remaining()/8 {
			return e, false
		}
		for range count {
			id := r.ReadVarInt()
			if r.Err() != nil || id < 0 || id >= RegistrySizeMobEffect {
				return e, false
			}
			spec, ok := readPotionEffect(r, id, 0)
			if !ok {
				return e, false
			}
			e.Effects = append(e.Effects, spec)
		}
		e.Probability = r.ReadF32()
	case ConsumeEffectRemoveEffects:
		n := r.ReadVarInt()
		// 0 would be a mob effect tag; vanilla defines none, so none was ever sent.
		if r.Err() != nil || n <= 0 {
			return e, false
		}
		if int(n-1) > r.Remaining() {
			return e, false
		}
		e.Remove = make([]int32, 0, n-1)
		for range n - 1 {
			id := r.ReadVarInt()
			if r.Err() != nil || id < 0 || id >= RegistrySizeMobEffect {
				return e, false
			}
			e.Remove = append(e.Remove, id)
		}
	case ConsumeEffectClearAllEffects:
	case ConsumeEffectTeleportRandomly:
		e.Diameter = r.ReadF32()
		e.Directional = r.ReadBool()
	case ConsumeEffectPlaySound:
		sound, ok := readSoundHolder(r)
		if !ok {
			return e, false
		}
		e.Sound = sound
	default:
		return e, false
	}
	return e, r.Err() == nil
}

func putConsumeEffect(b *Buffer, e ConsumeEffectComponent) {
	b.PutVarInt(e.Type)
	switch e.Type {
	case ConsumeEffectApplyEffects:
		b.PutVarInt(int32(len(e.Effects)))
		for i := range e.Effects {
			b.PutVarInt(e.Effects[i].ID)
			putPotionEffectChain(b, &e.Effects[i])
		}
		b.PutF32(e.Probability)
	case ConsumeEffectRemoveEffects:
		b.PutVarInt(int32(len(e.Remove)) + 1)
		for _, id := range e.Remove {
			b.PutVarInt(id)
		}
	case ConsumeEffectTeleportRandomly:
		b.PutF32(e.Diameter)
		b.PutBool(e.Directional)
	case ConsumeEffectPlaySound:
		PutSoundHolder(b, e.Sound)
	}
}

// readConsumable reads a minecraft:consumable value.
// vanilla: Consumable.STREAM_CODEC
func readConsumable(r *Reader) (ConsumableComponent, bool) {
	c := ConsumableComponent{Seconds: r.ReadF32(), Animation: r.ReadVarInt()}
	if r.Err() != nil || c.Animation < 0 || c.Animation >= itemUseAnimationCount {
		return ConsumableComponent{}, false
	}
	var ok bool
	if c.Sound, ok = readSoundHolder(r); !ok {
		return ConsumableComponent{}, false
	}
	c.HasParticles = r.ReadBool()
	count := r.ReadVarInt()
	// An effect is at least a type ID and nothing more.
	if r.Err() != nil || count < 0 || int(count) > r.Remaining() {
		return ConsumableComponent{}, false
	}
	for range count {
		e, ok := readConsumeEffect(r)
		if !ok {
			return ConsumableComponent{}, false
		}
		c.Effects = append(c.Effects, e)
	}
	return c, r.Err() == nil
}

// readFood reads a minecraft:food value.
// vanilla: FoodProperties.DIRECT_STREAM_CODEC
func readFood(r *Reader) (FoodComponent, bool) {
	f := FoodComponent{Nutrition: r.ReadVarInt(), Saturation: r.ReadF32(), CanAlwaysEat: r.ReadBool()}
	return f, r.Err() == nil
}

// ConsumableOf returns the stack's own minecraft:consumable, false for a
// stack without one, which is consumed by its item's default.
func (s ItemStack) ConsumableOf() (ConsumableComponent, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentConsumable {
			continue
		}
		r := NewReader(raw.Data)
		c, ok := readConsumable(r)
		return c, ok && r.Remaining() == 0
	}
	return ConsumableComponent{}, false
}

// FoodOf returns the stack's own minecraft:food, false for a stack without
// one.
func (s ItemStack) FoodOf() (FoodComponent, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentFood {
			continue
		}
		r := NewReader(raw.Data)
		f, ok := readFood(r)
		return f, ok && r.Remaining() == 0
	}
	return FoodComponent{}, false
}

// SetConsumable lays a minecraft:consumable on the stack in its wire shape,
// replacing any the stack had.
func (s *ItemStack) SetConsumable(c ConsumableComponent) {
	var value Buffer
	value.PutF32(c.Seconds)
	value.PutVarInt(c.Animation)
	PutSoundHolder(&value, c.Sound)
	value.PutBool(c.HasParticles)
	value.PutVarInt(int32(len(c.Effects)))
	for _, e := range c.Effects {
		putConsumeEffect(&value, e)
	}
	s.Raw = append([]RawComponent(nil), s.Raw...)
	s.putRaw(RawComponent{Type: DataComponentConsumable, Data: value.Bytes()})
}

// SetFood lays a minecraft:food on the stack in its wire shape, replacing
// any the stack had.
func (s *ItemStack) SetFood(f FoodComponent) {
	var value Buffer
	value.PutVarInt(f.Nutrition)
	value.PutF32(f.Saturation)
	value.PutBool(f.CanAlwaysEat)
	s.Raw = append([]RawComponent(nil), s.Raw...)
	s.putRaw(RawComponent{Type: DataComponentFood, Data: value.Bytes()})
}

// UseRemainderOf returns the stack's own minecraft:use_remainder.
func (s ItemStack) UseRemainderOf() (ItemStack, bool) {
	for _, raw := range s.Raw {
		if raw.Type == DataComponentUseRemainder {
			r := NewReader(raw.Data)
			item, ok := readItemTemplate(r, 0)
			return item, ok && r.Remaining() == 0
		}
	}
	return ItemStack{}, false
}

// SetUseRemainder replaces the stack's own minecraft:use_remainder.
// vanilla: UseRemainder.STREAM_CODEC
func (s *ItemStack) SetUseRemainder(item ItemStack) {
	if item.Empty() {
		return
	}
	var value Buffer
	value.PutItemTemplate(item)
	s.Raw = append([]RawComponent(nil), s.Raw...)
	s.putRaw(RawComponent{Type: DataComponentUseRemainder, Data: value.Bytes()})
}

// UseCooldownComponent is a stack's own minecraft:use_cooldown; an empty
// Group is the item's own.
// vanilla: UseCooldown.STREAM_CODEC
type UseCooldownComponent struct {
	Seconds float32
	Group   string
}

// readUseCooldown reads a minecraft:use_cooldown value: a float and an
// optional identifier.
func readUseCooldown(r *Reader) (UseCooldownComponent, bool) {
	c := UseCooldownComponent{Seconds: r.ReadF32()}
	if r.ReadBool() {
		c.Group = r.ReadString(32767)
		if r.Err() != nil || !ValidIdentifier(c.Group) {
			return c, false
		}
	}
	return c, r.Err() == nil
}

// UseCooldownOf returns the stack's own minecraft:use_cooldown, false for a
// stack without one.
func (s ItemStack) UseCooldownOf() (UseCooldownComponent, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentUseCooldown {
			continue
		}
		r := NewReader(raw.Data)
		c, ok := readUseCooldown(r)
		return c, ok && r.Remaining() == 0
	}
	return UseCooldownComponent{}, false
}

// SetUseCooldown lays a minecraft:use_cooldown on the stack in its wire
// shape, replacing any the stack had.
func (s *ItemStack) SetUseCooldown(c UseCooldownComponent) {
	var value Buffer
	value.PutF32(c.Seconds)
	value.PutBool(c.Group != "")
	if c.Group != "" {
		value.PutString(c.Group)
	}
	s.Raw = append([]RawComponent(nil), s.Raw...)
	s.putRaw(RawComponent{Type: DataComponentUseCooldown, Data: value.Bytes()})
}
