package server

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// vanilla: Consumable.emitParticlesAndSounds, RandomSource.triangle, Mth.randomBetween
func TestConsumableSoundDrawOrder(t *testing.T) {
	for _, item := range []registry.Item{registry.ItemBread, registry.ItemHoneyBottle} {
		t.Run(item.Name(), func(t *testing.T) {
			use, ok := registry.ConsumableOf(int32(item))
			if !ok {
				t.Fatal("missing consumable")
			}
			for seed := range int64(16) {
				s := &Server{soundRng: rand.New(rand.NewSource(seed))}
				rng := rand.New(rand.NewSource(seed))
				volume := float32(1)
				if rng.Int31n(2) == 0 {
					volume = 0.5
				}
				pitch := float32(1) + float32(0.2)*(rng.Float32()-rng.Float32())
				drinkPitch := rng.Float32()*(float32(1)-float32(0.9)) + float32(0.9)
				if use.Drink {
					volume, pitch = 0.5, drinkPitch
				}
				sound := decodeSoundEffect(t, s.eatBiteSoundAt(use, 0.5, 64, 0.5))
				if sound.volume != volume || sound.pitch != pitch {
					t.Errorf("seed %d: volume/pitch = %v/%v, want %v/%v", seed, sound.volume, sound.pitch, volume, pitch)
				}
				if got, want := s.soundRng.Int63(), rng.Int63(); got != want {
					t.Errorf("seed %d: next RNG value = %d, want %d", seed, got, want)
				}
			}
		})
	}
}
