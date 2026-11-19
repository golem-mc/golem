package servertest

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func TestSoundReadersDecodeHolders(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		inline     bool
		fixedRange bool
	}{
		{name: "registered"},
		{name: "inline_without_range", inline: true},
		{name: "inline_with_range", inline: true, fixedRange: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const name = "minecraft:entity.cow.ambient"
			const category = protocol.SoundCategoryPlayer
			const x, y, z int32 = -101, 515, 203
			const volume, pitch float32 = 1.25, 0.75
			// vanilla: SoundEvent.DIRECT_STREAM_CODEC, ClientboundSoundPacket.write
			var b protocol.Buffer
			if tc.inline {
				b.PutVarInt(0)
				b.PutString(name)
				b.PutBool(tc.fixedRange)
				if tc.fixedRange {
					b.PutF32(32)
				}
			} else {
				b.PutVarInt(int32(registry.SoundEntityCowAmbient))
			}
			b.PutVarInt(category)
			b.PutI32(x)
			b.PutI32(y)
			b.PutI32(z)
			b.PutF32(volume)
			b.PutF32(pitch)
			b.PutI64(123456789)

			t.Run("ambient", func(t *testing.T) {
				gotName, gotCategory, gotX, gotY, gotZ, gotVolume, gotPitch := ReadAmbient(t, b.Bytes())
				if gotName != name || gotCategory != category || gotX != x || gotY != y || gotZ != z || gotVolume != volume || gotPitch != pitch {
					t.Errorf("sound = %q category %d at (%d,%d,%d), volume %g pitch %g; want %q category %d at (%d,%d,%d), volume %g pitch %g",
						gotName, gotCategory, gotX, gotY, gotZ, gotVolume, gotPitch, name, category, x, y, z, volume, pitch)
				}
			})
			t.Run("collection", func(t *testing.T) {
				heard, counts, _ := collectSounds(t, func(see func(int32, []byte)) {
					see(protocol.IDPlaySoundEffect, b.Bytes())
				})
				want := HeardSound{Name: name, Category: category, X: x, Y: y, Z: z, Volume: volume, Pitch: pitch}
				if len(heard) != 1 || heard[0] != want {
					t.Errorf("sounds = %+v, want [%+v]", heard, want)
				}
				if counts[protocol.IDPlaySoundEffect] != 1 {
					t.Errorf("sound packet count = %d, want 1", counts[protocol.IDPlaySoundEffect])
				}
			})
		})
	}
}
