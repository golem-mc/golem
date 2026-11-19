package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
	"testing"
)

var movementSoundPacket []byte

// BenchmarkMovementSound includes selection and encoding for an ordinary step,
// a carpet's two ordered sounds, and a landing's body and block sounds.
func BenchmarkMovementSound(b *testing.B) {
	stone := registry.BlockStone.DefaultState()
	carpet := registry.BlockWhiteCarpet.DefaultState()
	b.ReportAllocs()
	for b.Loop() {
		for _, above := range []uint16{0, carpet} {
			step, muffled := rules.PlayerStepSounds(stone, above, false)
			for _, snd := range []rules.StepSound{step, muffled} {
				if snd.Event != registry.SoundNone {
					movementSoundPacket = soundEffectIDAt(snd.Event, protocol.SoundCategoryPlayer, 1.5, 64, 2.5, snd.Volume, snd.Pitch)
				}
			}
		}
		movementSoundPacket = soundEffectIDAt(rules.FallSound(6, rules.FallPlayer), protocol.SoundCategoryPlayer, 1.5, 64, 2.5, 1, 1)
		sound, volume, pitch, _ := rules.BlockFallSound(registry.BlockAnvil)
		movementSoundPacket = soundEffectIDAt(sound, protocol.SoundCategoryPlayer, 1.5, 64, 2.5, volume, pitch)
	}
}
