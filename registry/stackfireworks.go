package registry

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// fireworkShapes names each explosion shape by its ID, the string the file
// stores in place of the wire's VarInt.
// vanilla: FireworkExplosion.Shape
var fireworkShapes = [...]string{"small_ball", "large_ball", "star", "creeper", "burst"}

// maxFireworkExplosions caps a rocket's list on read.
// vanilla: Fireworks.MAX_EXPLOSIONS
const maxFireworkExplosions = 256

// fireworksTag writes a stack's fireworks component in the file's shape,
// false when it carries none or carries the rocket's own default, which the
// item's prototype supplies and the patch leaves out.
// vanilla: Fireworks.CODEC, PatchedDataComponentMap.set
func fireworksTag(stack protocol.ItemStack) (*nbt.OrderedCompound, bool) {
	flight, explosions, ok := stack.FireworksComponentOf()
	if !ok {
		return nil, false
	}
	if rocket, _ := ItemID("minecraft:firework_rocket"); stack.ItemID == rocket &&
		flight == 1 && len(explosions) == 0 {
		return nil, false
	}
	// The file's codec is an unsigned byte: it refuses only values above
	// 255, and saves a negative one as its low byte.
	// vanilla: ExtraCodecs.UNSIGNED_BYTE
	if flight > 255 {
		return nil, false
	}
	tag := nbt.NewOrderedCompound(0)
	if flight != 0 {
		tag.Set("flight_duration", nbt.Byte(int8(flight)))
	}
	if len(explosions) > 0 {
		list := nbt.List{Elem: nbt.TypeCompound, Items: make([]nbt.Tag, len(explosions))}
		for i, e := range explosions {
			list.Items[i] = fireworkExplosionTag(e)
		}
		tag.Set("explosions", list)
	}
	return tag, true
}

// fireworkExplosionTag writes one explosion, the fields at their defaults
// left out. A shape ID past the known ones is the small ball, the stream
// codec's out-of-bounds answer.
// vanilla: FireworkExplosion.CODEC, ByIdMap.OutOfBoundsStrategy.ZERO
func fireworkExplosionTag(e protocol.FireworkExplosion) *nbt.OrderedCompound {
	shape := fireworkShapes[0]
	if e.Shape >= 0 && int(e.Shape) < len(fireworkShapes) {
		shape = fireworkShapes[e.Shape]
	}
	tag := nbt.NewOrderedCompound(1)
	tag.Set("shape", nbt.String(shape))
	// Codec.INT.listOf builds a list of int tags, not an int array.
	for _, field := range []struct {
		name    string
		colours []int32
	}{{"colors", e.Colors}, {"fade_colors", e.FadeColors}} {
		name, colours := field.name, field.colours
		if len(colours) == 0 {
			continue
		}
		list := nbt.List{Elem: nbt.TypeInt, Items: make([]nbt.Tag, len(colours))}
		for i, c := range colours {
			list.Items[i] = nbt.Int(c)
		}
		tag.Set(name, list)
	}
	if e.Trail {
		tag.Set("has_trail", nbt.Byte(1))
	}
	if e.Twinkle {
		tag.Set("has_twinkle", nbt.Byte(1))
	}
	return tag
}

// fireworksOf reads a fireworks component: both fields optional, the
// duration an unsigned byte and the explosions a list of at most 256.
// vanilla: Fireworks.CODEC
func fireworksOf(value nbt.Tag) (int32, []protocol.FireworkExplosion, bool) {
	entry, ok := nbt.CompoundView(value)
	if !ok {
		return 0, nil, false
	}
	var flight int32
	var explosions []protocol.FireworkExplosion
	for name, field := range entry {
		switch name {
		case "flight_duration":
			n, ok := unsignedByteOf(field)
			if !ok {
				return 0, nil, false
			}
			flight = int32(n)
		case "explosions":
			list, ok := field.(nbt.List)
			if !ok || len(list.Items) > maxFireworkExplosions {
				return 0, nil, false
			}
			for _, item := range list.Items {
				e, ok := fireworkExplosionOf(item)
				if !ok {
					return 0, nil, false
				}
				explosions = append(explosions, e)
			}
		default:
			// The codec skips keys it does not know.
		}
	}
	return flight, explosions, true
}

// fireworkExplosionOf reads one explosion: the shape by a name it knows,
// required, and the colours, fade colours and two flags optional.
// vanilla: FireworkExplosion.CODEC
func fireworkExplosionOf(value nbt.Tag) (protocol.FireworkExplosion, bool) {
	entry, ok := nbt.CompoundView(value)
	if !ok {
		return protocol.FireworkExplosion{}, false
	}
	var e protocol.FireworkExplosion
	shaped := false
	for name, field := range entry {
		switch name {
		case "shape":
			text, ok := field.(nbt.String)
			if !ok {
				return protocol.FireworkExplosion{}, false
			}
			for id, shape := range fireworkShapes {
				if shape == string(text) {
					e.Shape, shaped = int32(id), true
				}
			}
		case "colors", "fade_colors":
			colours, ok := intListOf(field)
			if !ok {
				return protocol.FireworkExplosion{}, false
			}
			if name == "colors" {
				e.Colors = colours
			} else {
				e.FadeColors = colours
			}
		case "has_trail", "has_twinkle":
			flag, ok := boolValue(field)
			if !ok {
				return protocol.FireworkExplosion{}, false
			}
			if name == "has_trail" {
				e.Trail = flag
			} else {
				e.Twinkle = flag
			}
		default:
			// The codec skips keys it does not know.
		}
	}
	return e, shaped
}

// intListOf reads a Codec.INT list: the list of numbers it writes, or the
// int array NbtOps also streams to it.
// vanilla: NbtOps.getStream
func intListOf(value nbt.Tag) ([]int32, bool) {
	switch v := value.(type) {
	case nbt.IntArray:
		if len(v) == 0 {
			return nil, true
		}
		return append([]int32(nil), v...), true
	case nbt.List:
		var ints []int32
		for _, item := range v.Items {
			n, ok := nbt.IntValue(item)
			if !ok {
				return nil, false
			}
			ints = append(ints, n)
		}
		return ints, true
	default:
		return nil, false
	}
}
