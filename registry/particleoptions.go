package registry

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// ParticleOptions encodes a saved particle's type and options for entity metadata.
// vanilla: ParticleTypes.CODEC, ParticleTypes.STREAM_CODEC
func ParticleOptions(tag nbt.Tag) (int32, []byte, bool) {
	opts, ok := nbt.CompoundView(tag)
	if !ok {
		return 0, nil, false
	}
	name, ok := nbt.Get[nbt.String](opts, "type")
	if !ok {
		return 0, nil, false
	}
	qualified := Qualify(string(name))
	id, _, ok := ParticleID(qualified)
	if !ok {
		return 0, nil, false
	}
	data, err := ParticleData(qualified, opts)
	return id, data, err == nil
}

// particleScale is the range of a dust particle's scale.
// vanilla: ScalableParticleOptionsBase.MIN_SCALE, MAX_SCALE
const (
	particleMinScale = 0.01
	particleMaxScale = 4.0
)

// ParticleData encodes the SNBT options of a particle type that carries
// data into the bytes that follow its type ID in the Level Particles packet.
// vanilla: ParticleType.codec, ParticleType.streamCodec
func ParticleData(name string, opts nbt.Compound) ([]byte, error) {
	var b protocol.Buffer
	var err error
	switch name {
	case "minecraft:block", "minecraft:block_marker", "minecraft:falling_dust",
		"minecraft:dust_pillar", "minecraft:block_crumble":
		var state uint16
		if state, err = optionBlockState(opts, "block_state"); err == nil {
			b.PutVarInt(int32(state))
		}
	case "minecraft:entity_effect", "minecraft:tinted_leaves", "minecraft:flash":
		var color int32
		if color, err = optionColor(opts, "color", true, 0, false); err == nil {
			b.PutI32(color)
		}
	case "minecraft:dust":
		err = putDust(&b, opts, false)
	case "minecraft:dust_color_transition":
		err = putDust(&b, opts, true)
	case "minecraft:effect", "minecraft:instant_effect":
		power, powerErr := optionFloat(opts, "power", false, 1)
		color, colorErr := optionColor(opts, "color", false, -1, true)
		if err = joinParticleErrors(powerErr, colorErr); err == nil {
			b.PutI32(color)
			b.PutF32(power)
		}
	case "minecraft:dragon_breath":
		var power float32
		if power, err = optionFloat(opts, "power", false, 1); err == nil {
			b.PutF32(power)
		}
	case "minecraft:geyser", "minecraft:geyser_plume", "minecraft:geyser_base", "minecraft:geyser_poof":
		err = putGeyser(&b, opts, name == "minecraft:geyser_base" || name == "minecraft:geyser_poof")
	case "minecraft:item":
		err = putItem(&b, opts)
	case "minecraft:sculk_charge":
		var roll float32
		if roll, err = optionFloat(opts, "roll", true, 0); err == nil {
			b.PutF32(roll)
		}
	case "minecraft:shriek":
		var delay int32
		if delay, err = optionInt(opts, "delay", true, 0); err == nil {
			b.PutVarInt(delay)
		}
	case "minecraft:trail":
		err = putTrail(&b, opts)
	case "minecraft:vibration":
		err = putVibration(&b, opts)
	default:
		// A type without data decodes any map and ignores it.
		// vanilla: SimpleParticleType.codec (MapCodec.unit)
		if _, withData, _ := ParticleID(name); withData {
			err = fmt.Errorf("%s has no option reader", name)
		}
	}
	if err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// missingKeyError formats DFU's FieldDecoder missing-key error.
// vanilla: FieldDecoder.decode
func missingKeyError(key string, opts nbt.Compound) error {
	return fmt.Errorf("No key %s in MapLike[%s]", key, nbt.Stringify(opts)) //nolint:staticcheck // shown to the player as vanilla words it
}

// floatRangeError formats a range check error with Java float formatting.
// vanilla: ScalableParticleOptionsBase.SCALE, ExtraCodecs.floatRange
func floatRangeError(minVal, maxVal, val float32) error {
	return fmt.Errorf("Value must be within range [%s;%s]: %s", //nolint:staticcheck // shown to the player as vanilla words it
		nbt.JavaFloatText(float64(minVal), 32),
		nbt.JavaFloatText(float64(maxVal), 32),
		nbt.JavaFloatText(float64(val), 32))
}

// positiveIntError formats ExtraCodecs.POSITIVE_INT's error.
// vanilla: ExtraCodecs.POSITIVE_INT
func positiveIntError(val int32) error {
	return fmt.Errorf("Value must be positive: %d", val) //nolint:staticcheck // shown to the player as vanilla words it
}

// joinParticleErrors joins non-nil record field errors with "; ", matching DFU's
// RecordCodecBuilder / DataResult.Error.ap (which accumulates in reverse field order).
func joinParticleErrors(errs ...error) error {
	var parts []string
	for _, err := range errs {
		if err != nil {
			parts = append(parts, err.Error())
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return errors.New(strings.Join(parts, "; "))
}

// putDust writes a dust particle's colour (or colour pair) and scale.
// vanilla: DustParticleOptions.CODEC, DustColorTransitionOptions.CODEC
func putDust(b *protocol.Buffer, opts nbt.Compound, transition bool) error {
	scale, scaleErr := optionFloat(opts, "scale", true, 0)
	if scaleErr == nil && (scale < particleMinScale || scale > particleMaxScale) {
		scaleErr = floatRangeError(particleMinScale, particleMaxScale, scale)
	}
	var to int32
	var toErr error
	key := "color"
	if transition {
		key = "from_color"
		to, toErr = optionColor(opts, "to_color", true, 0, true)
	}
	from, fromErr := optionColor(opts, key, true, 0, true)
	if err := joinParticleErrors(scaleErr, toErr, fromErr); err != nil {
		return err
	}
	b.PutI32(from)
	if transition {
		b.PutI32(to)
	}
	b.PutF32(scale)
	return nil
}

// putGeyser writes a geyser particle's water blocks and, for the base and
// poof types, the burst impulse.
// vanilla: GeyserParticleOptions, GeyserBaseParticleOptions
func putGeyser(b *protocol.Buffer, opts nbt.Compound, base bool) error {
	var burst float32
	var burstErr error
	if base {
		burst, burstErr = optionFloat(opts, "burst_impulse_base", true, 0)
	}
	water, waterErr := optionInt(opts, "water_blocks", true, 0)
	if waterErr == nil && water < 1 {
		waterErr = positiveIntError(water)
	}
	if err := joinParticleErrors(burstErr, waterErr); err != nil {
		return err
	}
	b.PutI32(water)
	if base {
		b.PutF32(burst)
	}
	return nil
}

// putItem writes an item particle's stack template: item, count, then the
// component patch, where a slot has the count first.
// The count must be 1-99.
// vanilla: ItemStackTemplate.CODEC, ItemStackTemplate.STREAM_CODEC
func putItem(b *protocol.Buffer, opts nbt.Compound) error {
	tag, ok := opts["item"]
	if !ok {
		return missingKeyError("item", opts)
	}
	entry, isCompound := nbt.CompoundView(tag)
	if name, isString := tag.(nbt.String); isString {
		entry, isCompound = nbt.Compound{"id": name}, true
	}
	if !isCompound {
		return errors.New("not an item")
	}
	if tag, present := entry["count"]; present {
		// The template codec is strict, unlike a stored stack's.
		n, ok := nbt.IntValue(tag)
		if !ok {
			return errors.New("not a valid item")
		}
		if n < 1 || n > 99 {
			return fmt.Errorf("Value must be within range [1;99]: %d", n) //nolint:staticcheck // shown to the player as vanilla words it
		}
	}
	stack, ok := StoredStack(entry)
	if !ok || stack.Empty() || stack.ItemID == 0 {
		return errors.New("not a valid item")
	}
	var slot protocol.Buffer
	slot.PutSlot(stack)
	r := protocol.NewReader(slot.Bytes())
	r.ReadVarInt()
	r.ReadVarInt()
	data := slot.Bytes()
	b.PutVarInt(stack.ItemID)
	b.PutVarInt(stack.Count)
	b.PutRaw(data[len(data)-r.Remaining():])
	return nil
}

// putTrail writes a trail particle's target, colour and duration.
// vanilla: TrailParticleOption.CODEC, TrailParticleOption.STREAM_CODEC
func putTrail(b *protocol.Buffer, opts nbt.Compound) error {
	duration, durationErr := optionInt(opts, "duration", true, 0)
	if durationErr == nil && duration < 1 {
		durationErr = positiveIntError(duration)
	}
	color, colorErr := optionColor(opts, "color", true, 0, true)
	var items []float64
	var targetErr error
	if tag, ok := opts["target"]; !ok {
		targetErr = missingKeyError("target", opts)
	} else if items, ok = numberList(tag, 3); !ok {
		targetErr = errors.New("input does not contain a list of 3 numbers")
	}
	if err := joinParticleErrors(durationErr, colorErr, targetErr); err != nil {
		return err
	}
	for _, v := range items {
		b.PutF64(v)
	}
	b.PutI32(color)
	b.PutVarInt(duration)
	return nil
}

// putVibration writes a vibration particle's block destination and arrival
// time. An entity destination is refused.
// vanilla: VibrationParticleOption.CODEC, PositionSourceType
func putVibration(b *protocol.Buffer, opts nbt.Compound) error {
	arrival, arrivalErr := optionInt(opts, "arrival_in_ticks", true, 0)
	xyz, destErr := optionVibrationDestination(opts)
	if err := joinParticleErrors(arrivalErr, destErr); err != nil {
		return err
	}
	b.PutVarInt(0) // minecraft:block in the position_source_type registry
	b.PutPosition(xyz[0], xyz[1], xyz[2])
	b.PutVarInt(arrival)
	return nil
}

// optionVibrationDestination reads a vibration particle's destination field.
// vanilla: VibrationParticleOption.SAFE_POSITION_SOURCE_CODEC, BlockPositionSource.CODEC
func optionVibrationDestination(opts nbt.Compound) ([3]int32, error) {
	var xyz [3]int32
	tag, ok := opts["destination"]
	if !ok {
		return xyz, missingKeyError("destination", opts)
	}
	dest, ok := nbt.CompoundView(tag)
	if !ok {
		return xyz, errors.New("not a map")
	}
	kind, ok := nbt.Get[nbt.String](dest, "type")
	if !ok {
		return xyz, missingKeyError("type", dest)
	}
	switch Qualify(string(kind)) {
	case "minecraft:block":
	case "minecraft:entity":
		return xyz, errors.New("Entity position sources are not allowed") //nolint:staticcheck // shown to the player as vanilla words it
	default:
		return xyz, fmt.Errorf("unknown registry key: %s", kind)
	}
	pos, ok := dest["pos"]
	if !ok {
		return xyz, missingKeyError("pos", dest)
	}
	items, ok := listItems(pos)
	if !ok || len(items) != 3 {
		return xyz, errors.New("input does not contain a list of 3 numbers")
	}
	for i, item := range items {
		if xyz[i], ok = nbt.IntValue(item); !ok {
			return xyz, errors.New("not a number")
		}
	}
	return xyz, nil
}

// optionInt reads an integer field; a missing one is def unless required.
func optionInt(opts nbt.Compound, key string, required bool, def int32) (int32, error) {
	tag, ok := opts[key]
	if !ok {
		if required {
			return 0, missingKeyError(key, opts)
		}
		return def, nil
	}
	v, ok := nbt.IntValue(tag)
	if !ok {
		return 0, fmt.Errorf("not a number: %s", key)
	}
	return v, nil
}

// optionFloat reads a float field; a missing one is def unless required.
func optionFloat(opts nbt.Compound, key string, required bool, def float32) (float32, error) {
	tag, ok := opts[key]
	if !ok {
		if required {
			return 0, missingKeyError(key, opts)
		}
		return def, nil
	}
	v, ok := numberValue(tag)
	if !ok {
		return 0, fmt.Errorf("not a number: %s", key)
	}
	return float32(v), nil
}

// numberValue is a numeric tag's value as a double.
func numberValue(tag nbt.Tag) (float64, bool) {
	switch v := tag.(type) {
	case nbt.Byte:
		return float64(v), true
	case nbt.Short:
		return float64(v), true
	case nbt.Int:
		return float64(v), true
	case nbt.Long:
		return float64(v), true
	case nbt.Float:
		return float64(v), true
	case nbt.Double:
		return float64(v), true
	}
	return 0, false
}

// listItems is the elements of a list or integer array tag. A list of mixed
// types is stored with each element wrapped in a compound under the empty
// key, which is unwrapped here.
// vanilla: ListTag.tryUnwrap
func listItems(tag nbt.Tag) ([]nbt.Tag, bool) {
	switch l := tag.(type) {
	case nbt.List:
		out := make([]nbt.Tag, len(l.Items))
		for i, item := range l.Items {
			if wrapped, ok := nbt.CompoundView(item); ok && len(wrapped) == 1 {
				if inner, ok := wrapped[""]; ok {
					item = inner
				}
			}
			out[i] = item
		}
		return out, true
	case nbt.IntArray:
		out := make([]nbt.Tag, len(l))
		for i, v := range l {
			out[i] = nbt.Int(v)
		}
		return out, true
	}
	return nil, false
}

// numberList reads a list of exactly n numbers.
func numberList(tag nbt.Tag, n int) ([]float64, bool) {
	items, ok := listItems(tag)
	if !ok || len(items) != n {
		return nil, false
	}
	out := make([]float64, n)
	for i, item := range items {
		if out[i], ok = numberValue(item); !ok {
			return nil, false
		}
	}
	return out, true
}

// optionColor reads a colour field: a packed integer, or a list of three
// floats, or four when alpha is allowed, each in 0..1.
// vanilla: ExtraCodecs.RGB_COLOR_CODEC, ARGB_COLOR_CODEC
func optionColor(opts nbt.Compound, key string, required bool, def int32, rgb bool) (int32, error) {
	tag, ok := opts[key]
	if !ok {
		if required {
			return 0, missingKeyError(key, opts)
		}
		return def, nil
	}
	if v, isNumber := nbt.IntValue(tag); isNumber {
		return v, nil
	}
	n := 4
	if rgb {
		n = 3
	}
	parts, ok := numberList(tag, n)
	if !ok {
		return 0, fmt.Errorf("not a colour: %s", key)
	}
	alpha := 1.0
	if !rgb {
		alpha = parts[3]
	}
	return colorFromFloat(float32(alpha), float32(parts[0]), float32(parts[1]), float32(parts[2])), nil
}

// colorFromFloat packs channels of 0..1 into ARGB.
// vanilla: ARGB.colorFromFloat
func colorFromFloat(a, r, g, b float32) int32 {
	return channel(a)<<24 | channel(r)&0xFF<<16 | channel(g)&0xFF<<8 | channel(b)&0xFF
}

// channel is Mth.floor(value * 255), the bits of a channel that are kept.
func channel(v float32) int32 {
	f := math.Floor(float64(v * 255))
	switch {
	case f != f:
		return 0
	case f > math.MaxInt32:
		return math.MaxInt32
	case f < math.MinInt32:
		return math.MinInt32
	}
	return int32(f) & 0xFF
}

// optionBlockState reads a block state field: a block name, or a compound
// with id and optional properties.
// vanilla: BlockParticleOption.BLOCK_STATE_CODEC
func optionBlockState(opts nbt.Compound, key string) (uint16, error) {
	tag, ok := opts[key]
	if !ok {
		return 0, missingKeyError(key, opts)
	}
	raw, props, ok := BlockStateOfCodec(tag)
	if !ok {
		return 0, fmt.Errorf("not a block state: %s", key)
	}
	name := Qualify(raw)
	id, known := FindState(name, props)
	if !known {
		return 0, fmt.Errorf("unknown registry key: %s", name)
	}
	return id, nil
}
