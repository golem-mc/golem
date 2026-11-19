package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: AbstractCow.getSoundSet, getSoundVolume; LivingEntity.getVoicePitch
func TestMooshroomHurtAndDeathUseCowVoices(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
	c.MoveToGround(2.5, 64, 2.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)

	for _, tc := range []struct {
		name  string
		age   int
		pitch float32
	}{
		{"adult", 0, 1},
		{"baby", -24000, 1.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv.RunLine(fmt.Sprintf("summon minecraft:mooshroom 4.5 64 2.5 {NoAI:1b,Age:%d}", tc.age))
			c.AwaitSpawn(protocol.EntityTypeMooshroom)
			for _, voice := range []struct {
				command string
				sound   registry.Sound
			}{
				{"damage @e[type=mooshroom,limit=1] 1 minecraft:generic", registry.SoundEntityCowHurt},
				{"kill @e[type=mooshroom]", registry.SoundEntityCowDeath},
			} {
				srv.RunLine(voice.command)
				heard := 0
				c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
					if id != protocol.IDPlaySoundEffect {
						return false
					}
					heard++
					name, category, _, _, _, volume, pitch := servertest.ReadAmbient(t, data)
					if holder := protocol.NewReader(data).ReadVarInt(); holder != int32(voice.sound) {
						t.Errorf("heard %s (holder %d), want registered %s (holder %d)", name, holder, voice.sound.Name(), voice.sound)
					}
					if category != protocol.SoundCategoryNeutral || volume != 0.4 {
						t.Errorf("%s category/volume = %d/%v, want neutral/0.4", name, category, volume)
					}
					if pitch < tc.pitch-0.2 || pitch > tc.pitch+0.2 {
						t.Errorf("%s pitch = %v, want %v ± 0.2", name, pitch, tc.pitch)
					}
					return false
				})
				if heard != 1 {
					t.Errorf("heard %d sounds, want one %s", heard, voice.sound.Name())
				}
			}
		})
	}
}
