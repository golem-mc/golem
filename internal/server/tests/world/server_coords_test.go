package world

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestCoordinateParseErrors sends malformed position and rotation arguments
// and expects the parse errors a vanilla server answered the same
// commands with, the cursor where its reader stopped.
func TestCoordinateParseErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		command string
		key     string
		with    []string
		cursor  int
	}{
		{"setblock 1.5 64 0 stone", "parsing.int.invalid", []string{"1.5"}, 9},
		{"summon minecraft:zombie 1.2.3 64 0", "parsing.double.invalid", []string{"1.2.3"}, 24},
		{"summon minecraft:zombie 1 ^ 2", "argument.pos.mixed", nil, 26},
		{"summon minecraft:zombie 1 64", "argument.pos3d.incomplete", nil, 24},
		{"summon minecraft:zombie 0 64 1e3", "command.expected.separator", nil, 30},
		{"execute rotated 1 run say hi", "parsing.double.expected", nil, 18},
		{"setblock ~ ~ ^ stone", "argument.pos.mixed", nil, 13},
		// A node with more than one argument child speaks for all of
		// them, so these are unknown arguments where the branch begins,
		// not the coordinate parsers' own errors.
		{"tp ^ ^ 1", "command.unknown.argument", nil, 3},
		{"teleport @s 0 64 0 1.x 0", "command.unknown.argument", nil, 12},
		{"tp 1.2.3 64 0", "command.unknown.argument", nil, 9},
		{"tp @s 1 2", "command.unknown.argument", nil, 6},
		{"tp @s 1.2.3 64 0", "command.unknown.argument", nil, 6},
	} {
		t.Run(tc.command, func(t *testing.T) {
			t.Parallel()
			// A config per subtest: servertest.Start binds the default level name to
			// its t's TempDir, so a shared one would put every sibling in
			// the first subtest's directory, racing its removal.
			cfg := servertest.SpawningTestConfig()
			srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

			c.ChatCommand(tc.command)
			c.ExpectCommandError(
				servertest.CommandErrorMessageForTest(tc.key, tc.with...),
				servertest.CommandErrorContextForTest(tc.command, tc.cursor),
			)
			if got := srv.MobCount(); got != 0 {
				t.Errorf("%d mobs stand in the world after a refused command, want none", got)
			}

			c.Disconnect()
		})
	}
}

// TestLocalCoordinates moves and summons with ^left ^up ^forwards positions,
// which are measured from the source's anchor along its rotation; the
// expected places are where a vanilla server put the same commands.
func TestLocalCoordinates(t *testing.T) {
	t.Parallel()
	cfg := servertest.SpawningTestConfig()
	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	c.SpawnedAt() // (0.5, 64, 0.5), facing south
	c.SkipJoin()

	c.ChatCommand("tp ^ ^ ^1")
	servertest.AwaitCommandFeedbackText(t, c, "commands.teleport.success.location.single",
		servertest.PlayerDisplayComponent("Testificate"),
		protocol.Text{Text: "0.500000"}, protocol.Text{Text: "64.000000"}, protocol.Text{Text: "1.500000"})
	// The sine table's cosine of a right angle is not quite 0, here as in
	// vanilla, so the x is only 0.5 to the feedback's six places.
	if pos := servertest.ExpectTeleport(t, c); servertest.FormatCoordForTest(pos.X) != "0.500000" || pos.Y != 64 || pos.Z != 1.5 {
		t.Errorf("teleport = %v, want (0.5, 64, 1.5)", pos)
	}
	c.Settle()

	for _, tc := range []struct {
		command string
		want    [3]string
	}{
		{"execute positioned 0.5 100 0.5 facing 10 90 5 run summon minecraft:zombie ^ ^ ^2",
			[3]string{"1.833259", "98.666715", "1.166626"}},
		{"execute positioned 0.5 100 0.5 rotated 0 0 anchored eyes run summon minecraft:zombie ^ ^ ^2",
			[3]string{"0.500000", "101.620000", "2.500000"}},
		{"execute positioned 0.5 100 0.5 rotated 10 20 positioned ^1 ^2 ^3 run summon minecraft:zombie",
			[3]string{"0.876620", "100.853558", "4.123534"}},
	} {
		c.ChatCommand(tc.command)
		servertest.AwaitSummonFeedback(t, c, "minecraft:zombie", "entity.minecraft.zombie")
		e := c.AwaitSpawn(protocol.EntityTypeZombie)
		got := [3]string{servertest.FormatCoordForTest(e.X), servertest.FormatCoordForTest(e.Y), servertest.FormatCoordForTest(e.Z)}
		if got != tc.want {
			t.Errorf("%s: zombie stood up at %v, want %v", tc.command, got, tc.want)
		}
	}

	c.Disconnect()
}
