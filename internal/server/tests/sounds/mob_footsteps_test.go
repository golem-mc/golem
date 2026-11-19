package sounds

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: Frog.playStepSound, Parrot.playStepSound, Axolotl.getSwimSound
func TestMobMovementUsesKindSounds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind, floor, sound, wrong string
		etype                     int32
		water                     bool
	}{
		{"frog", "grass_block", "entity.frog.step", "block.grass.step", protocol.EntityTypeFrog, false},
		{"parrot", "stone", "entity.parrot.step", "block.stone.step", protocol.EntityTypeParrot, false},
		{"axolotl", "stone", "entity.axolotl.swim", "entity.generic.swim", protocol.EntityTypeAxolotl, true},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.MobTestConfig(), "Listener", servertest.OpsListing(t, "Listener"))
			servertest.HoldTicks(t, srv)
			floor := servertest.BlockState(t, "minecraft:"+tc.floor)
			water := servertest.BlockState(t, "minecraft:water")
			for x := range int32(16) {
				for z := range int32(8) {
					srv.SetBlock(x, 63, z, floor)
					if tc.water {
						srv.SetBlock(x, 64, z, water)
						srv.SetBlock(x, 65, z, water)
						srv.SetBlock(x, 66, z, water)
					}
				}
			}
			c.MoveToGround(6.5, 64, 10.5, true)
			c.Settle()
			y, motion := 64, 1.8
			if tc.water {
				y, motion = 65, 1
			}
			// An initial velocity drives real travel, including the parrot's
			// separate control, without depending on a random wander goal.
			c.ChatCommand(fmt.Sprintf("summon %s 2.5 %d 3.5 {Motion:[%gd,0d,0d],OnGround:1b,PersistenceRequired:1b}", tc.kind, y, motion))
			mob := c.ExpectMobSpawn(tc.etype)
			c.Settle()
			heard := 0
			c.PumpTicks(srv, 60, func(id int32, data []byte) bool {
				if id != protocol.IDPlaySoundEffect {
					return false
				}
				name, category, _, _, _, volume, pitch := servertest.ReadAmbient(t, data)
				if name == "minecraft:"+tc.wrong {
					t.Errorf("%s used %s", tc.kind, name)
				}
				if name == "minecraft:"+tc.sound {
					heard++
					if category != protocol.SoundCategoryNeutral {
						t.Errorf("sound category = %d, want neutral", category)
					}
					if !tc.water && (volume != 0.15 || pitch != 1) {
						t.Errorf("step volume/pitch = %g/%g, want 0.15/1", volume, pitch)
					}
					if tc.water && volume <= 0 {
						t.Errorf("swim volume = %g, want positive", volume)
					}
				}
				return false
			})
			if heard == 0 {
				x, y, z, _ := srv.MobPos(mob.EntityID)
				t.Fatalf("no %s in 60 ticks; mob at (%g,%g,%g)", tc.sound, x, y, z)
			}
		})
	}
}
