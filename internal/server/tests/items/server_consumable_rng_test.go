package items

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: Consumable.emitParticlesAndSounds, RandomSource.triangle, Mth.randomBetween
func TestConsumableAnimationsKeepTheSoundDrawOrder(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg)
	servertest.FreezeUntilHeld(t, srv)
	actor := connect().Join("Actor")
	actor.MoveToGround(0.5, 64, 0.5, true)
	witness := connect().Join("Witness")
	witness.MoveToGround(2.5, 64, 0.5, true)
	servertest.HoldTicks(t, srv)
	srv.SetSpawning(false)
	const seed = 73831
	srv.SetSoundSeed(seed)
	rng := rand.New(rand.NewSource(seed))
	for _, animation := range []string{"eat", "drink", "eat", "drink"} {
		srv.RunLine(`item replace entity Actor weapon.mainhand with stick[consumable={consume_seconds:0.25,animation:"` + animation + `",has_consume_particles:false,sound:"minecraft:block.amethyst_block.chime"}]`)
		actor.Settle()
		witness.Settle()
		actor.UseItemRightClicks(protocol.HandMain)
		actor.Settle()
		servertest.RunTicks(t, srv, 5)

		volume := float32(1)
		if rng.Int31n(2) == 0 {
			volume = 0.5
		}
		pitch := float32(1) + float32(0.2)*(rng.Float32()-rng.Float32())
		drinkPitch := rng.Float32()*(float32(1)-float32(0.9)) + float32(0.9)
		if animation == "drink" {
			volume, pitch = 0.5, drinkPitch
		}
		count := 0
		witness.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlaySoundEffect {
				return
			}
			sound := servertest.DecodePlaySoundEffect(t, data)
			name, _ := servertest.SoundEventName(sound.RegistryID)
			if name != "minecraft:block.amethyst_block.chime" {
				return
			}
			count++
			if sound.Volume != volume || sound.Pitch != pitch {
				t.Errorf("%s volume/pitch = %v/%v, want %v/%v", animation, sound.Volume, sound.Pitch, volume, pitch)
			}
		})
		if count != 1 {
			t.Fatalf("%s emitted %d consume sounds, want 1", animation, count)
		}
	}
}
