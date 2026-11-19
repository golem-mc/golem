package protocol

// The minecraft:fireworks component is one of the unmodelled ones this
// server keeps as the bytes it arrived in, but a rocket's launch reads what
// its stack was loaded with, so its value is decoded here.

// Fireworks is what a stack's minecraft:fireworks says: how long the
// rocket's flight lasts and how many explosions it bursts into.
// vanilla: the Fireworks record
type Fireworks struct {
	// FlightDuration is the flight duration the rocket was crafted with —
	// the gunpowder count, one to three — which the rocket's life scales
	// by: ten ticks a duration plus one.
	FlightDuration int32
	// Explosions is the count of the stars the rocket bursts into, the
	// number the burst's damage scales by.
	Explosions int32
}

// FireworksOf returns what the fireworks component a stack carries says,
// reporting false for a stack that carries none — absent, the component is
// the item's own default, the one flight of a plain rocket.
func (s ItemStack) FireworksOf() (Fireworks, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentFireworks {
			continue
		}
		r := NewReader(raw.Data)
		fireworks := Fireworks{
			FlightDuration: r.ReadVarInt(),
			Explosions:     r.ReadVarInt(),
		}
		if r.Err() != nil || fireworks.Explosions < 0 {
			return Fireworks{}, false
		}
		return fireworks, true
	}
	return Fireworks{}, false
}

// FireworksComponentOf returns the whole of the fireworks component a stack
// carries, the explosions themselves rather than their count, false when it
// carries none or its value does not read.
func (s ItemStack) FireworksComponentOf() (flight int32, explosions []FireworkExplosion, ok bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentFireworks {
			continue
		}
		return readFireworksValue(NewReader(raw.Data))
	}
	return 0, nil, false
}

// FireworkExplosion is one burst a firework star carries and a rocket lists:
// the minecraft:firework_explosion component.
// vanilla: FireworkExplosion
type FireworkExplosion struct {
	// Shape is the burst's shape ID: 0 small ball, 1 large ball, 2 star,
	// 3 creeper, 4 burst. The reader keeps any VarInt; the client takes an
	// unknown one for the small ball.
	// vanilla: FireworkExplosion.Shape
	Shape int32
	// Colors and FadeColors are RGB ints, the burst's colours and the ones
	// it fades into.
	Colors, FadeColors []int32
	Trail, Twinkle     bool
}

// Put writes the explosion in the shape both its own component and a
// rocket's list carry it in.
// vanilla: FireworkExplosion.STREAM_CODEC
func (e FireworkExplosion) Put(b *Buffer) {
	b.PutVarInt(e.Shape)
	for _, colours := range [][]int32{e.Colors, e.FadeColors} {
		b.PutVarInt(int32(len(colours)))
		for _, c := range colours {
			b.PutI32(c)
		}
	}
	b.PutBool(e.Trail)
	b.PutBool(e.Twinkle)
}

// RawComponent is the explosion as the firework_explosion component of a
// stack's Raw.
func (e FireworkExplosion) RawComponent() RawComponent {
	var b Buffer
	e.Put(&b)
	return RawComponent{Type: DataComponentFireworkExplosion, Data: b.Bytes()}
}

// FireworkExplosionOf returns the firework_explosion component a stack
// carries, false when it carries none.
func (s ItemStack) FireworkExplosionOf() (FireworkExplosion, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentFireworkExplosion {
			continue
		}
		return readFireworkExplosion(NewReader(raw.Data))
	}
	return FireworkExplosion{}, false
}

// FireworksRawComponent is a fireworks component of the given flight
// duration and explosions, as a stack's Raw carries it.
// vanilla: Fireworks.STREAM_CODEC
func FireworksRawComponent(flight int32, explosions []FireworkExplosion) RawComponent {
	var b Buffer
	b.PutVarInt(flight)
	b.PutVarInt(int32(len(explosions)))
	for _, e := range explosions {
		e.Put(&b)
	}
	return RawComponent{Type: DataComponentFireworks, Data: b.Bytes()}
}

// SetRaw puts a raw component on the stack, replacing one of its type.
func (s *ItemStack) SetRaw(raw RawComponent) { s.putRaw(raw) }

// readFireworkExplosion reads the shape, the colour and fade colour lists of
// counted ints, then the trail and twinkle flags.
// vanilla: FireworkExplosion.STREAM_CODEC
func readFireworkExplosion(r *Reader) (FireworkExplosion, bool) {
	e := FireworkExplosion{Shape: r.ReadVarInt()}
	for _, list := range []*[]int32{&e.Colors, &e.FadeColors} {
		n := r.ReadVarInt()
		if r.Err() != nil || n < 0 {
			return FireworkExplosion{}, false
		}
		for range n {
			c := r.ReadI32()
			if r.Err() != nil {
				return FireworkExplosion{}, false
			}
			*list = append(*list, c)
		}
	}
	e.Trail, e.Twinkle = r.ReadBool(), r.ReadBool()
	return e, r.Err() == nil
}
