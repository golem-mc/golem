package server

import (
	"bytes"
	"encoding/hex"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// soundHolder is the Sound Event field the two sound packets open with, as
// either side of the wire may encode it: a registry reference, or the inline
// definition a name that is not in the registry travels as.
type soundHolder struct {
	registryID int32  // the minecraft:sound_event ID a reference names, -1 when inline
	name       string // the inline definition's name, empty for a reference
	fixedRange bool   // the inline definition's fixed-range flag, meaningless for a reference
}

// soundEffectFields are the fields a Sound Effect packet carries beside its
// sound: the category, the effect position in the eighths of a block the wire
// sends it in, and the volume and pitch. The seed is left out — both sides
// roll a fresh random long per packet, so it is never the same twice.
type soundEffectFields struct {
	sound    soundHolder
	category int32
	x, y, z  int32
	volume   float32
	pitch    float32
}

// decodeSoundEffect reads a Sound Effect packet into its fields, decoding the
// holder the way the ID-or-X encoding says: a zero ID opens the inline
// definition that follows, anything else is the registry ID one above the
// registry's own numbering.
func decodeSoundEffect(t *testing.T, data []byte) soundEffectFields {
	t.Helper()
	r := protocol.NewReader(data)
	var out soundEffectFields
	switch id := r.ReadVarInt(); id {
	case 0:
		out.sound = soundHolder{registryID: -1, name: r.ReadString(32767), fixedRange: r.ReadBool()}
	default:
		out.sound = soundHolder{registryID: id - 1}
	}
	out.category = r.ReadVarInt()
	out.x, out.y, out.z = r.ReadI32(), r.ReadI32(), r.ReadI32()
	out.volume, out.pitch = r.ReadF32(), r.ReadF32()
	if r.Remaining() != 8 {
		t.Fatalf("sound packet has %d bytes left after the fields the seed should fill", r.Remaining())
	}
	r.ReadI64() // the seed, random on both sides
	if r.Err() != nil {
		t.Fatalf("decode sound packet: %v", r.Err())
	}
	return out
}

// compareSoundEffect checks the decoded fields of one of our packets against
// the vanilla server's, field by field. It fails if the two encode the sound
// differently — a registry reference against an inline definition among
// them — or if any field but the seed disagrees.
func compareSoundEffect(t *testing.T, what string, got, want soundEffectFields) {
	t.Helper()
	if got.sound != want.sound {
		t.Errorf("%s sound = %+v, want the vanilla server's %+v", what, got.sound, want.sound)
	}
	if got.category != want.category {
		t.Errorf("%s category = %d, want %d", what, got.category, want.category)
	}
	if got.x != want.x || got.y != want.y || got.z != want.z {
		t.Errorf("%s position = (%d %d %d), want (%d %d %d)",
			what, got.x, got.y, got.z, want.x, want.y, want.z)
	}
	if math.Float32bits(got.volume) != math.Float32bits(want.volume) {
		t.Errorf("%s volume = %v, want %v", what, got.volume, want.volume)
	}
	if math.Float32bits(got.pitch) != math.Float32bits(want.pitch) {
		t.Errorf("%s pitch = %v, want %v", what, got.pitch, want.pitch)
	}
}

// TestSoundEffectMatchesVanillaCreeperPrimed builds the packet the creeper's
// fuse beginning sends and compares it, field by field, with the packet the
// vanilla server sent for the same sound, captured with the probe's
// HEXDUMP=0x75 against `summon minecraft:creeper ~ ~ ~ {ignited:1b}`. The
// seed is excluded: each side rolls a fresh random long per packet — the
// vanilla Level's soundSeedGenerator, ours the global source — so the two
// packets agree in every field but that one.
func TestSoundEffectMatchesVanillaCreeperPrimed(t *testing.T) {
	t.Parallel()
	// The whole packet: the sound event holder (the registry ID of
	// minecraft:entity.creeper.primed plus one, 481 + 1), the hostile
	// category, the effect position at the eighths of a block (30.5,
	// -60.0, 15.5), the volume and pitch the swell's playSound passes,
	// and the seed the capture happened to roll.
	vanilla := "e20305000000f4fffffe200000007c3f8000003f0000003b30337b196c7266"
	raw, err := hex.DecodeString(vanilla)
	if err != nil {
		t.Fatal(err)
	}
	want := decodeSoundEffect(t, raw)

	name := "minecraft:entity.creeper.primed"
	if id, ok := registry.SoundEventID(name); !ok || id != 481 {
		t.Fatalf("the sound registry has %s at %d (ok=%v), want the vanilla 481", name, id, ok)
	}
	got := decodeSoundEffect(t, soundEffectAt(name, protocol.SoundCategoryHostile, 30.5, -60.0, 15.5, 1.0, 0.5))
	compareSoundEffect(t, "creeper primed", got, want)
}

// TestSoundEffectRegistryReference checks the builder sends any known sound
// the same way: as the registry ID one above the
// registry's own numbering, with no name on the wire.
func TestSoundEffectRegistryReference(t *testing.T) {
	t.Parallel()
	fields := decodeSoundEffect(t, soundEffectAt("minecraft:entity.creeper.primed", protocol.SoundCategoryHostile, 30.5, -60, 15.5, 1.0, 0.5))
	if fields.sound.registryID != 481 || fields.sound.name != "" {
		t.Errorf("sound = %+v, want the registry reference of minecraft:entity.creeper.primed (481)", fields.sound)
	}
}

// TestSoundEffectInlineFallback checks the form a name that is not in the
// registry keeps: the zero ID, the name and the fixed-range flag of the
// inline definition, which is what the packet read as before the registry
// table existed and what keeps a name the version does not carry sendable.
func TestSoundEffectInlineFallback(t *testing.T) {
	t.Parallel()
	fields := decodeSoundEffect(t, soundEffectAt("minecraft:no.such.sound", protocol.SoundCategoryBlock, 0, 0, 0, 1.0, 1.0))
	if fields.sound.registryID != -1 || fields.sound.name != "minecraft:no.such.sound" || fields.sound.fixedRange {
		t.Errorf("sound = %+v, want the inline definition of the unknown name", fields.sound)
	}
}

func TestRegisteredSoundEffectIDsMatchNames(t *testing.T) {
	for id, name := range registry.SoundEvents {
		named := soundEffectAt(name, protocol.SoundCategoryBlock, -1.25, 64.5, 2.25, 0.3, 1.5)
		sound := registry.Sound(id + 1)
		byID := soundEffectIDAt(sound, protocol.SoundCategoryBlock, -1.25, 64.5, 2.25, 0.3, 1.5)
		if sound.Name() != name || !bytes.Equal(named[:len(named)-8], byID[:len(byID)-8]) {
			t.Fatalf("sound %s (%d): ID and named wire layouts differ", name, sound)
		}
	}
	if registry.SoundNone.Name() != "" || registry.Sound(-1).Name() != "" || registry.Sound(len(registry.SoundEvents)+1).Name() != "" {
		t.Fatal("invalid sound holder has a name")
	}
}

func TestRegisteredEntitySoundIDsMatchNames(t *testing.T) {
	t.Parallel()
	for i, name := range registry.SoundEvents {
		named := entitySoundEffect(name, protocol.SoundCategoryNeutral, 127, 0.8, 1.2)
		typed := entitySoundEffectID(registry.Sound(i+1), protocol.SoundCategoryNeutral, 127, 0.8, 1.2)
		if !bytes.Equal(named[:len(named)-8], typed[:len(typed)-8]) {
			t.Fatalf("%s: ID/name entity packets differ", name)
		}
	}
}

// Name-based encoders are test oracles for the typed playback builders.
// putSoundEvent writes the sound packets' leading Sound Event field as an
// ID-or-X holder: the minecraft:sound_event registry ID plus one for a known
// sound, else 0 and an inline definition (name, fixed-range flag).
func putSoundEvent(b *protocol.Buffer, name string) {
	if id, ok := registry.SoundEventID(name); ok {
		b.PutVarInt(id + 1)
		return
	}
	b.PutVarInt(0) // inline sound event, not a registry ID
	b.PutString(name)
	b.PutBool(false) // no fixed range
}

// soundEffectAt builds the Sound Effect packet at the exact position given,
// which is the wire's own units already — the coordinates travel as eighths
// of a block. Vanilla plays an entity's sounds from its exact position and a
// block's from its centre (Level.playSound with a BlockPos adds the half),
// so a caller with a block passes its centre. The sound travels by its
// registry reference, as the vanilla server sends its own.
func soundEffectAt(name string, category int32, x, y, z float64, volume, pitch float32) []byte {
	var b protocol.Buffer
	putSoundEvent(&b, name)
	return soundEffectTail(&b, category, x, y, z, volume, pitch)
}

// entitySoundEffect builds the Entity Sound Effect packet, which plays a
// sound as the body the packet names: the wire carries the entity ID and no
// position, so the client follows the entity. Used for a sound a player
// makes on another body, which the player's own client plays itself.
// vanilla: ServerLevel.playSeededSound(Entity)
func entitySoundEffect(name string, category, entityID int32, volume, pitch float32) []byte {
	var b protocol.Buffer
	putSoundEvent(&b, name)
	return entitySoundEffectTail(&b, category, entityID, volume, pitch)
}
