package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestCreeperAvoidKeepsCombatTargeting puts a creeper three blocks from a
// NoAI cat in a barrier pen: it flees an ordinary cat, but not a teammate,
// an invisible cat or an invulnerable one.
// vanilla: AvoidEntityGoal.canUse, TargetingConditions.test
func TestCreeperAvoidKeepsCombatTargeting(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, catTags string
		flees         bool
	}{
		{"control", `{NoAI:1b}`, true},
		{"same team", `{NoAI:1b,Tags:["zzt"]}`, false},
		{"invisible", `{NoAI:1b,active_effects:[{id:"minecraft:invisibility",duration:1000,show_particles:0b}]}`, false},
		{"invulnerable", `{NoAI:1b,Invulnerable:1b}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.MobTestConfig()
			srv, _ := servertest.Join(t, cfg, "Watcher", func(s *server.Server) {
				servertest.FreezeUntilHeld(t, s)
			})
			servertest.HoldTicks(t, srv)
			srv.RunLine("fill -8 179 -8 8 179 8 minecraft:smooth_stone")
			//PEN
			srv.RunLine("team add zzt")
			srv.RunLine("summon minecraft:cat -1.5 180 0.5 " + tc.catTags)
			srv.RunLine("summon minecraft:creeper 2.5 180 0.5 {Tags:[\"zzt\"]}")
			if tc.name == "same team" {
				srv.RunLine("team join zzt @e[tag=zzt]")
			}
			var creeper, cat int32
			for _, m := range srv.Mobs() {
				switch m.Kind {
				case "minecraft:creeper":
					creeper = m.ID
				case "minecraft:cat":
					cat = m.ID
				}
			}
			if creeper == 0 || cat == 0 {
				t.Fatalf("creeper %d, cat %d", creeper, cat)
			}
			servertest.RunTicks(t, srv, 40)
			cx, _, cz, ok1 := srv.MobPos(creeper)
			ax, _, az, ok2 := srv.MobPos(cat)
			if !ok1 || !ok2 {
				t.Fatalf("creeper present %t, cat present %t", ok1, ok2)
			}
			d := math.Hypot(cx-ax, cz-az)
			if tc.flees && d <= 6 {
				t.Errorf("creeper %.1f blocks from the cat, want more than 6", d)
			}
			if !tc.flees && d > 4 {
				t.Errorf("creeper %.1f blocks from the cat, want within 4", d)
			}
		})
	}
}
