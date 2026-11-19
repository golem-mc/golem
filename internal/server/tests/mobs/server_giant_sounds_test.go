package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Giant extends Monster; Monster.getHurtSound, getDeathSound, getSoundSource
func TestGiantHurtAndDeathUseHostileVoices(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "GiantListener")
	c.MoveToGround(2.5, 64, 2.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)

	srv.RunLine("summon minecraft:giant 4.5 64 2.5 {NoAI:1b}")
	c.AwaitSpawn(protocol.EntityTypeGiant)
	for _, tc := range []struct {
		name    string
		command string
		sound   registry.Sound
	}{
		{"hurt", "damage @e[type=giant,limit=1] 1 minecraft:generic", registry.SoundEntityHostileHurt},
		{"death", "kill @e[type=giant]", registry.SoundEntityHostileDeath},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv.RunLine(tc.command)
			heard := 0
			c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
				if id != protocol.IDPlaySoundEffect {
					return false
				}
				heard++
				name, category, _, _, _, volume, pitch := servertest.ReadAmbient(t, data)
				if holder := protocol.NewReader(data).ReadVarInt(); holder != int32(tc.sound) {
					t.Errorf("heard %s (holder %d), want registered %s (holder %d)", name, holder, tc.sound.Name(), tc.sound)
				}
				if category != protocol.SoundCategoryHostile || volume != 1 {
					t.Errorf("%s category/volume = %d/%v, want hostile/1", name, category, volume)
				}
				if pitch < 0.8 || pitch > 1.2 {
					t.Errorf("%s pitch = %v, want 1 ± 0.2", name, pitch)
				}
				return false
			})
			if heard != 1 {
				t.Errorf("heard %d sounds, want one %s", heard, tc.sound.Name())
			}
		})
	}
}
