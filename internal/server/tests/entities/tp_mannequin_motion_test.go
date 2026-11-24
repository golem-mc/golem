package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// vanilla: TeleportCommand.getRelatives, PositionMoveRotation.calculateAbsolute
func TestTeleportCommandMannequinHorizontalMotion(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, mannequinConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	t.Cleanup(srv.HoldChunkInDimension(world.NetherName, 3, 0))
	const target = `@e[type=mannequin,limit=1]`
	srv.RunLine(`summon mannequin 0.5 100 0.5`)

	for _, tc := range []struct {
		name, prefix, coords, motion string
	}{
		{"absolute", "", "3.5 100 3.5", "[0.0d, 0.0d, 0.0d]"},
		{"relative_x", "", "~ 100 3.5", "[0.5d, 0.0d, 0.0d]"},
		{"relative_z", "", "3.5 100 ~", "[0.0d, 0.0d, -0.25d]"},
		{"local", "", "^1 ^ ^", "[0.5d, 0.0d, -0.25d]"},
		{"across_dimensions", "execute in the_nether run ", "3.5 100 ~", "[0.0d, 0.0d, -0.25d]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv.RunLine(`data merge entity ` + target + ` {Motion:[0.5d,0.0d,-0.25d]}`)
			line := tc.prefix + `tp ` + target + ` ` + tc.coords
			feedback := srv.RunLine(line)
			if len(feedback) != 1 || feedback[0].Translate != "commands.teleport.success.location.single" {
				t.Fatalf("%s returned %v, want teleport success", line, feedback)
			}
			feedback = srv.RunLine(tc.prefix + `data get entity ` + target + ` Motion`)
			if len(feedback) != 1 || feedback[0].Translate != "commands.data.entity.query" || len(feedback[0].With) != 2 {
				t.Fatalf("querying Motion returned %v, want entity data", feedback)
			}
			if got := feedback[0].With[1].Plain(); got != tc.motion {
				t.Errorf("Motion after /tp = %s, want %s", got, tc.motion)
			}
		})
	}
}
