package sounds

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestHoglinAndZoglinAmbientSoundsFollowTheirActivity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, kind, mode, tag, want string
		fighting, repellent         bool
	}{
		{"idle zoglin", "zoglin", "creative", "", "ambient", false, false},
		{"fighting zoglin", "zoglin", "survival", "", "angry", true, false},
		{"idle hoglin", "hoglin", "creative", "IsImmuneToZombification:1b,", "ambient", false, false},
		{"fighting hoglin", "hoglin", "survival", "IsImmuneToZombification:1b,", "angry", true, false},
		{"converting hoglin", "hoglin", "survival", "", "retreat", true, false},
		{"repelled hoglin", "hoglin", "creative", "IsImmuneToZombification:1b,", "retreat", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, c := servertest.BreedingPlayer(t, tc.mode, "Listener", "", 0,
				func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
			servertest.HoldTicks(t, srv)
			if tc.repellent {
				srv.SetBlock(7, 64, 0, servertest.MustState(t, "minecraft:warped_fungus", nil))
			}
			srv.RunLine(fmt.Sprintf(`summon minecraft:%s 6.5 64 0.5 {%sattributes:[{id:"minecraft:movement_speed",base:0d}]}`, tc.kind, tc.tag))
			var mobID int32
			for _, m := range srv.Mobs() {
				if m.Kind == "minecraft:"+tc.kind {
					mobID = m.ID
				}
			}
			if mobID == 0 {
				t.Fatal("mob was not summoned")
			}
			// Discard the activity-change voice; the next one must come from
			// the ambient timer while the same target remains out of reach.
			srv.TickMobs(40)
			if tc.fighting && srv.MobTargetID(mobID) == 0 {
				t.Fatal("mob never acquired the player")
			}
			c.Settle()
			want := "minecraft:entity." + tc.kind + "." + tc.want
			heard := 0
			for tick := 0; tick < 240; tick += 10 {
				srv.TickMobs(10)
				c.SettleAndCollect(func(id int32, data []byte) {
					if id != protocol.IDPlaySoundEffect {
						return
					}
					name, category, _, _, _, volume, pitch := servertest.ReadAmbient(t, data)
					if name != "minecraft:entity."+tc.kind+".ambient" &&
						name != "minecraft:entity."+tc.kind+".angry" &&
						name != "minecraft:entity."+tc.kind+".retreat" {
						return
					}
					heard++
					if name != want {
						t.Errorf("ambient timer played %s, want %s", name, want)
					}
					if category != protocol.SoundCategoryHostile || volume != 1 || pitch < 0.79 || pitch > 1.21 {
						t.Errorf("voice category=%d volume=%v pitch=%v, want hostile/1/[0.79,1.21]", category, volume, pitch)
					}
				})
			}
			if heard == 0 {
				t.Fatal("no ambient timer sound heard")
			}
		})
	}
}
