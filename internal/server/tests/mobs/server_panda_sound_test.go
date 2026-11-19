package mobs

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Panda.addEatingParticles evaluates volume before pitch.
func TestPandaEatingSoundDrawsVolumeBeforePitch(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Keeper", "minecraft:bamboo", 4,
		func(s *server.Server) { servertest.FreezeUntilHeld(t, s) }, servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)
	srv.RunLine(`summon minecraft:panda 1.5 64 2.5 {NoAI:1b,MainGene:"normal",HiddenGene:"normal"}`)
	id := c.ExpectMobSpawn(pandaType()).EntityID
	// The first feeding starts love mode; the second makes it sit and snack.
	c.UseOn(id)
	c.Settle()
	c.UseOn(id)
	c.Settle()
	servertest.RunTicks(t, srv, 4)

	for _, seed := range []int64{1, 2, 3, 4} {
		// NoAI keeps goal draws out of the stream; the next tick reaches a sound's five-tick interval.
		srv.SeedMob(id, seed)
		rng := rand.New(rand.NewSource(seed))
		wantVolume := 0.5 + 0.5*float32(rng.Intn(2))
		wantPitch := (rng.Float32()-rng.Float32())*0.2 + 1
		heard := 0
		c.ReadTicks(srv, 1, func(packet int32, data []byte) bool {
			if packet != protocol.IDPlaySoundEffect {
				return false
			}
			name, category, x, y, z, volume, pitch := servertest.ReadAmbient(t, data)
			if name != registry.SoundEntityPandaEat.Name() {
				return false
			}
			heard++
			if holder := protocol.NewReader(data).ReadVarInt(); holder != int32(registry.SoundEntityPandaEat) {
				t.Errorf("seed %d: sound holder = %d, want registered panda eating sound", seed, holder)
			}
			if category != protocol.SoundCategoryNeutral || x != 12 || y != 512 || z != 20 {
				t.Errorf("seed %d: category/position = %d (%d,%d,%d), want neutral (12,512,20)", seed, category, x, y, z)
			}
			if volume != wantVolume || pitch != wantPitch {
				t.Errorf("seed %d: volume/pitch = %v/%v, want %v/%v", seed, volume, pitch, wantVolume, wantPitch)
			}
			return false
		})
		if heard != 1 {
			t.Fatalf("seed %d: heard %d eating sounds, want 1", seed, heard)
		}
		servertest.RunTicks(t, srv, 4)
	}
}
