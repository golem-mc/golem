package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: AgeableMob.setAgeLocked, Level.playSound(Entity, BlockPos, ...).
func TestGoldenDandelionSoundsAtBlockCenter(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, position string
		x, y, z        int32 // Wire coordinates in eighths of a block.
	}{
		{"positive", "1.25 64.25 2.75", 12, 516, 20},
		{"negative", "-1.25 64.75 -2.25", -12, 516, -20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, c := servertest.BreedingPlayer(t, "survival", "Locker", "minecraft:golden_dandelion", 2,
				func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
			servertest.HoldTicks(t, srv)
			srv.RunLine("summon minecraft:cow " + tc.position + " {Age:-24000,NoAI:1b,NoGravity:1b}")
			id := c.ExpectSummonedMob(protocol.EntityTypeCow)

			for i, sound := range []registry.Sound{registry.SoundItemGoldenDandelionUse, registry.SoundItemGoldenDandelionUnuse} {
				if i != 0 {
					// The age-lock particle timer gates the next interaction.
					servertest.RunTicks(t, srv, 40)
				}
				c.UseOn(id)
				heard := 0
				c.ReadTicks(srv, 0, func(packet int32, data []byte) bool {
					if packet != protocol.IDPlaySoundEffect {
						return false
					}
					name, category, x, y, z, volume, pitch := servertest.ReadAmbient(t, data)
					if name != sound.Name() {
						return false
					}
					heard++
					if x != tc.x || y != tc.y || z != tc.z {
						t.Errorf("%s position = (%d,%d,%d), want (%d,%d,%d)", name, x, y, z, tc.x, tc.y, tc.z)
					}
					if category != protocol.SoundCategoryPlayer || volume != 1 || pitch != 1 {
						t.Errorf("%s category/volume/pitch = %d/%v/%v, want player/1/1", name, category, volume, pitch)
					}
					return false
				})
				if heard != 1 {
					t.Fatalf("heard %d %s sounds, want 1", heard, sound.Name())
				}
			}
		})
	}
}
