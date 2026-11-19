package mobs

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Parrot.imitateNearbyMobs, Parrot.getPitch, ServerPlayer.playShoulderEntityAmbientSound
func TestParrotImitationsSendRegisteredSounds(t *testing.T) {
	t.Parallel()
	for _, shoulder := range []bool{false, true} {
		path := "ordinary"
		if shoulder {
			path = "shoulder"
		}
		t.Run(path, func(t *testing.T) {
			for _, tc := range []struct {
				mob   string
				sound registry.Sound
			}{
				{"minecraft:happy_ghast", registry.SoundIntentionallyEmpty},
				{"minecraft:zombie", registry.SoundEntityParrotImitateZombie},
				{"minecraft:cave_spider", registry.SoundEntityParrotImitateSpider},
			} {
				t.Run(tc.mob, func(t *testing.T) {
					var srv *server.Server
					var c *servertest.Client
					if shoulder {
						srv, c = shoulderSoundScene(t, false)
					} else {
						srv, c = parrotScene(t, "minecraft:air", 0)
					}
					target := srv.SpawnMob(tc.mob, 5.5, 64, 0.5)
					srv.SetMobNoAI(target)
					srv.SetDayTime(18000)
					category := int32(protocol.SoundCategoryNeutral)
					rng := rand.New(rand.NewSource(516))
					rounds := 1
					if shoulder {
						category = protocol.SoundCategoryPlayer
						rng = rand.New(rand.NewSource(0))
						srv.SetEnvironmentSeed(0)
						rounds = 4
					} else {
						id := srv.SpawnMob(parrotType, 0.5, 64, 0.5)
						srv.SeedMob(id, 1)
						srv.SetEnvironmentSeed(516)
					}
					c.ReadTicks(srv, 0, func(int32, []byte) bool { return false })
					for range rounds {
						sound, volume := tc.sound, float32(0.7)
						pitchDice := rng
						if shoulder {
							// Seed 21 opens the shoulder's 1/200 gate on each observed tick.
							srv.SetWearSeed(21)
							own := rand.New(rand.NewSource(21))
							if own.Intn(200) != 0 {
								t.Fatal("seed did not open shoulder gate")
							}
							if rng.Intn(2) != 0 {
								sound, volume, pitchDice = registry.SoundEntityParrotAmbient, 1, own
								if own.Intn(1000) == 0 {
									t.Fatal("seed unexpectedly chose an ambient imitation")
								}
							} else {
								rng.Intn(1)
							}
						} else {
							if rng.Intn(400) != 0 || rng.Intn(2) != 0 {
								t.Fatal("seed did not open imitation gates")
							}
							rng.Intn(1)
						}
						pitch := (pitchDice.Float32()-pitchDice.Float32())*0.2 + 1
						if !c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
							if id != protocol.IDPlaySoundEffect {
								return false
							}
							name, gotCategory, x, y, z, gotVolume, gotPitch := servertest.ReadAmbient(t, data)
							if gotCategory != category || gotVolume != volume {
								return false
							}
							holder := protocol.NewReader(data).ReadVarInt()
							if holder != int32(sound) || name != sound.Name() || x != 4 || y != 512 || z != 4 || gotPitch != pitch {
								t.Errorf("imitation = holder %d (%s), at (%d,%d,%d), pitch %g; want holder %d (%s), at (4,512,4), pitch %g", holder, name, x, y, z, gotPitch, sound, sound.Name(), pitch)
							}
							return true
						}) {
							t.Fatalf("no parrot sound with category %d and volume %g", category, volume)
						}
					}
				})
			}
		})
	}
}
