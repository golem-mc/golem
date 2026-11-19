package movement

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestTeleportWithoutRotationKeepsEachTargetsFacing checks that a /tp that
// names no rotation leaves every target facing the way it already faced,
// whatever the runner or the execute chain faces.
// vanilla: TeleportCommand.teleportToPos
func TestTeleportWithoutRotationKeepsEachTargetsFacing(t *testing.T) {
	t.Parallel()
	srv, alpha, beta, _ := servertest.StartExecuteWorldForTwo(t, "Alpha", "Beta")

	alpha.MoveTo(0.5, 64, 0.5, 90, 0)
	beta.MoveTo(0.5, 64, 0.5, 45, 10)
	alpha.Settle()
	// Beta's move is read off Beta's own connection: settle it too, or the
	// first /tp can run before the server has applied Beta's 45/10.
	beta.Settle()

	faces := func(name string) (float32, float32) {
		p := srv.PlayerPosition(name)
		return p.Yaw, p.Pitch
	}
	check := func(what string) {
		t.Helper()
		alpha.Settle()
		if yaw, pitch := faces("Beta"); yaw != 45 || pitch != 10 {
			t.Errorf("%s: Beta faces %v/%v, want 45/10", what, yaw, pitch)
		}
	}

	alpha.ChatCommand("tp Beta 1 100 1")
	check("tp Beta 1 100 1")

	alpha.ChatCommand("execute as Beta run tp @s ~ ~ ~1")
	check("execute as Beta run tp @s ~ ~ ~1")

	alpha.ChatCommand("execute as Beta rotated 90 0 run tp @s ~ ~ ~")
	check("execute rotated 90 0 run tp @s ~ ~ ~")
}

// TestTeleportRefusesAPositionOutsideTheWorld checks the red
// commands.teleport.invalidPosition a /tp answers with for a block outside
// the world's horizontal limits or the height range spawning allows, and
// that nothing moves.
// vanilla: TeleportCommand.performTeleport, Level.isInSpawnableBounds
func TestTeleportRefusesAPositionOutsideTheWorld(t *testing.T) {
	t.Parallel()
	srv, alpha, _, _ := servertest.StartExecuteWorldForTwo(t, "Alpha", "Beta")

	for _, cmd := range []string{
		"tp @s 30000000 100 0",
		"tp @s -30000001 100 0",
		"tp @s 0 100 30000000",
		"tp @s 0 20000000 0",
		"tp @s 0 -20000001 0",
		"tp Beta 30000000 100 0",
	} {
		alpha.ChatCommand(cmd)
		servertest.AwaitCommandError(t, alpha, "commands.teleport.invalidPosition")
	}
	if p := srv.PlayerPosition("Alpha"); p.X != 0.5 || p.Z != 0.5 {
		t.Errorf("Alpha moved to (%v, %v) on a refused teleport", p.X, p.Z)
	}
	if p := srv.PlayerPosition("Beta"); p.X != 0.5 || p.Z != 0.5 {
		t.Errorf("Beta moved to (%v, %v) on a refused teleport", p.X, p.Z)
	}

	// The last block inside the limits is still reachable.
	alpha.ChatCommand("tp @s 29999999 100 0")
	alpha.Settle()
	if p := srv.PlayerPosition("Alpha"); p.X != 29999999.5 {
		t.Errorf("Alpha at x=%v, want the in-bounds 29999999.5", p.X)
	}
}
